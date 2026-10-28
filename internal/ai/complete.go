package ai

import (
	"context"
	"encoding/json"
	"fmt"
)

// Inference bounds. A preparation batch is one inferred normalization of a
// bounded number of rows; these ceilings keep a single response small enough
// to validate completely instead of streaming an unbounded workbook through
// the provider.
const (
	// DefaultMaxTokens bounds one inference response when the caller does not
	// choose a tighter bound.
	DefaultMaxTokens = 4096
	// MaxTokens bounds any single inference response.
	MaxTokens = 16384
)

// InferenceRequest is one bounded normalization call. System carries trusted
// application rules only; cell values belong in User.
type InferenceRequest struct {
	System    string
	User      string
	MaxTokens int
	// Temperature is the sampling setting recorded with the run.
	Temperature float64
	// Session is reused across the batches of one run for providers that
	// document a stable conversation header.
	Session string
}

// inferenceRequest builds the exact-format request body. It deliberately sets
// no tools, fallback or routing fields, so a call can never delegate to a
// different model.
func inferenceRequest(cfg Config, request InferenceRequest) ([]byte, error) {
	maxTokens := request.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if maxTokens > MaxTokens {
		return nil, fmt.Errorf("%w: the response bound is too large", ErrInvalidSetup)
	}
	switch cfg.APIFormat {
	case FormatResponses:
		return json.Marshal(map[string]any{
			"model":             cfg.Model,
			"input":             inputMessages(request.System, request.User),
			"max_output_tokens": maxTokens,
			"temperature":       request.Temperature,
			"store":             false,
		})
	case FormatChatCompletions:
		return json.Marshal(map[string]any{
			"model":       cfg.Model,
			"messages":    chatMessages(request.System, request.User),
			"max_tokens":  maxTokens,
			"temperature": request.Temperature,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported API format", ErrInvalidSetup)
	}
}

func inputMessages(system, user string) []map[string]any {
	return []map[string]any{
		{"role": "system", "content": []map[string]string{{"type": "input_text", "text": system}}},
		{"role": "user", "content": []map[string]string{{"type": "input_text", "text": user}}},
	}
}

func chatMessages(system, user string) []map[string]string {
	return []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": user},
	}
}

// Complete sends one normalization request over the exact confirmed
// configuration and returns the assistant text. It never retries, never calls
// another model and never probes or falls back to another format.
func Complete(ctx context.Context, o ValidationOptions, cfg Config, apiKey string, request InferenceRequest) (string, error) {
	body, err := inferenceRequest(cfg, request)
	if err != nil {
		return "", err
	}
	if o.InferenceTimeout > 0 {
		o.Timeout = o.InferenceTimeout
	}
	return postJSON(ctx, o, cfg, apiKey, request.Session, body, runtimeStatus)
}

// CompleteFor sends one bounded inference request using the given captured
// configuration and the owner's confirmed credential. Callers verify that the
// captured configuration still matches the confirmed connection before the
// first batch, so no request can silently move to another model.
func (s *Store) CompleteFor(ctx context.Context, ownerID string, cfg Config, request InferenceRequest) (string, error) {
	apiKey, err := s.Credential(ctx, ownerID)
	if err != nil {
		return "", err
	}
	return Complete(ctx, s.opts, cfg, apiKey, request)
}

// CompleteWith sends one bounded inference request over the exact captured
// configuration and credential using the store's request bounds. It is the
// temporary-sample boundary: the credential came from the same read as the
// configuration, so no request pairs one endpoint with another's key.
func (s *Store) CompleteWith(ctx context.Context, cfg Config, apiKey string, request InferenceRequest) (string, error) {
	return Complete(ctx, s.opts, cfg, apiKey, request)
}
