package preparation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Sample bounds. These are support limits, not tunables.
const (
	// SampleDeadline bounds one explicit AI sample from admission to return.
	SampleDeadline = 30 * time.Second
	// MaxConcurrentSamples bounds AI samples across one process. Replicas
	// each admit their own bound.
	MaxConcurrentSamples = 4
)

// Sample errors. Every message is fixed operator-facing text.
var (
	// ErrSampleBusy reports another sample already using this operator's or the
	// server's capacity. Nothing is queued.
	ErrSampleBusy = errors.New("another AI sample is already running; wait for it to finish and try again")
	// ErrSampleChanged reports a connection change during sample processing.
	// The result is discarded and no automatic rerun happens.
	ErrSampleChanged = errors.New("the confirmed AI connection changed while the sample was prepared; review Account settings and run the sample again")
	// ErrSampleTimeout reports the sample deadline expiring.
	ErrSampleTimeout = errors.New("the AI sample did not finish within 30 seconds; run the sample again")
)

// SampleResult is the temporary result of one explicit campaign sample. It
// carries the provenance of the call in memory only; nothing here is stored.
type SampleResult struct {
	Output        Output
	Provider      ai.Provider
	APIRoot       string
	Model         string
	PromptVersion int
	Prompt        string
}

// Sample normalizes one sample row over the owner's confirmed AI connection.
// It uses the revision's mappings and the current base policy without task
// instructions, makes at most one provider request, and writes no task, run,
// job, artifact or approval record.
func (s *Service) Sample(ctx context.Context, ownerID string, configuration *campaign.Configuration, values map[string]string) (SampleResult, error) {
	if s.ai == nil {
		return SampleResult{}, ErrSetup
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return SampleResult{}, ErrNotFound
	}
	if err := s.acquireSample(ownerID); err != nil {
		return SampleResult{}, err
	}
	defer s.releaseSample(ownerID)
	options := s.options.normalize()
	ctx, cancel := context.WithTimeout(ctx, options.SampleDeadline)
	defer cancel()

	// One synthetic source row: the sample has no workbook, so it never
	// becomes task input evidence.
	input, err := BuildInput(configuration, "", []intake.MappedRow{{Number: 1, Values: values}})
	if err != nil {
		return SampleResult{}, err
	}
	connection, apiKey, err := s.ai.Capture(ctx, ownerID)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) || errors.Is(err, ai.ErrCredentialUnreadable) {
			return SampleResult{}, ErrSetup
		}
		return SampleResult{}, err
	}
	// The captured configuration must still be the confirmed one before any
	// provider usage is spent.
	if err := s.sampleSettingsCurrent(ownerID, connection); err != nil {
		return SampleResult{}, err
	}

	op := operation{
		inference: func(ctx context.Context, config ai.Config, request ai.InferenceRequest) (string, error) {
			return s.ai.CompleteWith(ctx, config, apiKey, request)
		},
		config: connection.Config,
		prompt: CurrentBasePrompt(),
		options: RunOptions{
			BatchSize:   1,
			MaxTokens:   options.MaxTokens,
			Temperature: options.Temperature,
		},
		session: newToken(),
	}
	results, err := op.normalize(ctx, input, input.Rows)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return SampleResult{}, ErrSampleTimeout
		}
		return SampleResult{}, err
	}
	// A connection change while the provider answered discards the result:
	// the operator must decide explicitly whether to try the new settings.
	if err := s.sampleSettingsCurrent(ownerID, connection); err != nil {
		return SampleResult{}, err
	}
	return SampleResult{
		Output:        Derive(input, results),
		Provider:      connection.Config.Provider,
		APIRoot:       connection.Config.APIRoot,
		Model:         connection.Config.Model,
		PromptVersion: BasePromptVersion,
		Prompt:        CurrentBasePrompt(),
	}, nil
}

// acquireSample admits one sample or refuses immediately. A refusal never
// queues work and never calls the provider.
func (s *Service) acquireSample(ownerID string) error {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()
	if _, active := s.sampleOwners[ownerID]; active {
		return ErrSampleBusy
	}
	if s.sampleActive >= MaxConcurrentSamples {
		return ErrSampleBusy
	}
	if s.sampleOwners == nil {
		s.sampleOwners = make(map[string]struct{})
	}
	s.sampleOwners[ownerID] = struct{}{}
	s.sampleActive++
	return nil
}

// releaseSample frees the operator's slot and drops the owner entry so the map
// cannot grow with historical operators.
func (s *Service) releaseSample(ownerID string) {
	s.sampleMu.Lock()
	defer s.sampleMu.Unlock()
	if _, active := s.sampleOwners[ownerID]; !active {
		return
	}
	delete(s.sampleOwners, ownerID)
	s.sampleActive--
}

// sampleSettingsCurrent reports whether the captured connection is still the
// confirmed one. It reads the record again; it never reuses the captured key.
func (s *Service) sampleSettingsCurrent(ownerID string, captured *ai.Connection) error {
	current, err := s.ai.Connection(context.Background(), ownerID)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			return ErrSampleChanged
		}
		return err
	}
	if !current.Config.Equal(captured.Config) ||
		current.ConfigurationGeneration != captured.ConfigurationGeneration ||
		current.CredentialGeneration != captured.CredentialGeneration {
		return ErrSampleChanged
	}
	return nil
}

// SampleFailureText maps one sample refusal or provider failure to fixed
// operator-facing text. Raw provider bodies, requests and credentials never
// reach a page.
func SampleFailureText(err error) string {
	for _, sentinel := range []error{ErrSampleBusy, ErrSampleChanged, ErrSampleTimeout, ErrSetup, ErrNotFound} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	if errors.Is(err, ErrInvalidResponse) {
		return "the model returned an incomplete or invalid sample response; nothing was prepared"
	}
	return failureText(err)
}
