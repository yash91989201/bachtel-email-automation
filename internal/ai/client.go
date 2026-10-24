package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The synthetic probe uses no workbook, recipient or candidate value. It
// verifies that the exact selected model answers the normalization contract
// over the exact selected API format.
const (
	probeSystem = "You normalize spreadsheet recipient fields. Reply with one JSON object only, no prose and no code fences: {\"values\":[{\"id\":\"probe\",\"value\":\"<normalized address>\"}]}."
	probeUser   = "Normalize this synthetic value to a lowercase, trimmed email address: {\"fields\":[{\"id\":\"probe\",\"value\":\"  Probe@Example.TEST \"}]}"
	probeValue  = "probe@example.test"
)

// errFormatIncompatible reports that this endpoint answered but cannot serve
// the attempted API format. It is internal: resolution maps it to an
// operator-safe error before anything is shown or staged.
var errFormatIncompatible = errors.New("the endpoint cannot serve this API format")

// probeContract is the required response shape.
type probeContract struct {
	Values []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"values"`
}

// validateConnection performs one synthetic normalization request against the
// exact configuration. It never retries and never calls another model.
func validateConnection(ctx context.Context, o ValidationOptions, cfg Config, apiKey string) error {
	body, err := probeRequest(cfg)
	if err != nil {
		return err
	}
	text, err := postJSON(ctx, o, cfg, apiKey, "", body, probeStatus)
	if err != nil {
		return err
	}
	return checkContract(text)
}

// resolveFormat establishes one exact configuration's API format through
// bounded synthetic validation. Candidates are attempted once each in order,
// against the same endpoint, model and key under a single overall deadline;
// the first passing format wins. Only bounded compatibility failures continue
// to the next candidate; every other outcome stops the resolution immediately
// and is returned unchanged. No retry or backoff is introduced.
func resolveFormat(ctx context.Context, o ValidationOptions, cfg Config, apiKey string, candidates []APIFormat) (Config, error) {
	if o.Timeout <= 0 {
		o.Timeout = DefaultValidationOptions().Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	var incompatible error
	for _, format := range candidates {
		if ctx.Err() != nil {
			return Config{}, ErrEndpointTimeout
		}
		candidate := cfg
		candidate.APIFormat = format
		err := validateConnection(ctx, o, candidate, apiKey)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, errFormatIncompatible) && !errors.Is(err, ErrOutputContract) {
			return Config{}, err
		}
		incompatible = err
	}
	// Every candidate proved incompatible. A pinned single format reports its
	// own outcome; a resolved selection reports one safe summary.
	if len(candidates) == 1 && errors.Is(incompatible, ErrOutputContract) {
		return Config{}, ErrOutputContract
	}
	return Config{}, fmt.Errorf("%w: none of the supported API formats passed validation at this endpoint", ErrModelUnavailable)
}

// statusClassifier maps one non-200 HTTP status outside the centrally handled
// classes (401/403, 429 and 5xx) to a fixed operator-safe error.
type statusClassifier func(status int) error

// errRefused is the fixed outcome for any status a classifier does not
// specially map. It never includes provider prose or bodies.
var errRefused = fmt.Errorf("%w: the endpoint refused the request", ErrInvalidSetup)

// probeStatus classifies validation statuses: a format-level rejection is a
// bounded compatibility failure; anything else refuses the setup.
func probeStatus(status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return errFormatIncompatible
	}
	return errRefused
}

// discoveryStatus classifies model-list statuses: only a missing list path is
// a discovery outage; anything else refuses the setup.
func discoveryStatus(status int) error {
	if status == http.StatusNotFound {
		return ErrDiscoveryUnavailable
	}
	return errRefused
}

// runtimeStatus classifies runtime inference statuses: a missing model path
// is an unavailable model; anything else refuses the setup. The runtime path
// never probes or falls back.
func runtimeStatus(status int) error {
	if status == http.StatusNotFound {
		return ErrModelUnavailable
	}
	return errRefused
}

// postJSON sends one exact-format request and returns the assistant text. It
// is the single transport boundary shared by connection validation and
// normalization, so both use the same endpoint policy, bounds and redaction;
// only the status mapping of a refusal differs per caller.
func postJSON(ctx context.Context, o ValidationOptions, cfg Config, apiKey, session string, body []byte, classify statusClassifier) (string, error) {
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = DefaultValidationOptions().MaxResponseBytes
	}
	target, err := endpointURL(cfg, o.AllowPrivate)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return "", ErrEndpointUnsafe
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Identify this application rather than a generic HTTP library; OpenCode
	// documents that clients must identify themselves and send a stable
	// session per conversation.
	req.Header.Set("User-Agent", "bachtel-email-automation/1.0")
	if cfg.Provider == ProviderOpenCodeGo {
		if session == "" {
			generated, err := newSessionID()
			if err != nil {
				return "", fmt.Errorf("%w: %v", ErrProviderFailure, "could not create a request session")
			}
			session = generated
		}
		req.Header.Set("x-opencode-session", session)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := newClient(o).Do(req)
	if err != nil {
		return "", classifyTransport(err)
	}
	raw, err := readBounded(o, resp, classify)
	if err != nil {
		return "", err
	}
	return extractText(cfg.APIFormat, raw)
}

// fetchJSON sends one bounded, credential-bearing GET. Discovery shares the
// inference client, endpoint policy and response bounds, so a read-only model
// list can never reach a destination validation would refuse.
func fetchJSON(ctx context.Context, o ValidationOptions, target, apiKey string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, ErrEndpointUnsafe
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bachtel-email-automation/1.0")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := newClient(o).Do(req)
	if err != nil {
		return nil, classifyTransport(err)
	}
	return readBounded(o, resp, discoveryStatus)
}

// readBounded maps one response to a fixed operator-safe outcome and returns
// its bounded body. Authentication, throttling and service failures are
// handled centrally; every other non-200 status is mapped by the caller's
// classifier.
func readBounded(o ValidationOptions, resp *http.Response, classify statusClassifier) ([]byte, error) {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// The outcome is already decided; a close failure cannot change it.
			_ = err
		}
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, ErrAuthentication
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrThrottled
	case resp.StatusCode >= 500:
		return nil, ErrProviderFailure
	default:
		return nil, classify(resp.StatusCode)
	}

	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = DefaultValidationOptions().MaxResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, o.MaxResponseBytes+1))
	if err != nil {
		return nil, classifyTransport(err)
	}
	if int64(len(raw)) > o.MaxResponseBytes {
		return nil, ErrResponseTooLarge
	}
	return raw, nil
}

// probeRequest builds the exact-format request body. It deliberately sets no
// tools and no fallback or routing fields.
func probeRequest(cfg Config) ([]byte, error) {
	switch cfg.APIFormat {
	case FormatResponses:
		return json.Marshal(map[string]any{
			"model":             cfg.Model,
			"input":             inputMessages(probeSystem, probeUser),
			"max_output_tokens": 256,
			"store":             false,
		})
	case FormatChatCompletions:
		return json.Marshal(map[string]any{
			"model":       cfg.Model,
			"messages":    chatMessages(probeSystem, probeUser),
			"max_tokens":  256,
			"temperature": 0,
		})
	default:
		return nil, fmt.Errorf("%w: unsupported API format", ErrInvalidSetup)
	}
}

// extractText reads the assistant text from the documented response envelope
// of one API format.
func extractText(format APIFormat, raw []byte) (string, error) {
	switch format {
	case FormatResponses:
		var body struct {
			OutputText string `json:"output_text"`
			Output     []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return "", ErrOutputContract
		}
		if strings.TrimSpace(body.OutputText) != "" {
			return body.OutputText, nil
		}
		var parts []string
		for _, item := range body.Output {
			if item.Type != "" && item.Type != "message" {
				continue
			}
			for _, content := range item.Content {
				if content.Type == "output_text" && content.Text != "" {
					parts = append(parts, content.Text)
				}
			}
		}
		if len(parts) == 0 {
			return "", ErrOutputContract
		}
		return strings.Join(parts, ""), nil
	case FormatChatCompletions:
		var body struct {
			Choices []struct {
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return "", ErrOutputContract
		}
		if len(body.Choices) == 0 {
			return "", ErrOutputContract
		}
		return chatContent(body.Choices[0].Message.Content)
	default:
		return "", ErrOutputContract
	}
}

// chatContent accepts the plain-string form and the content-parts form.
func chatContent(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return "", ErrOutputContract
		}
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", ErrOutputContract
	}
	var joined strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "output_text" {
			joined.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(joined.String()) == "" {
		return "", ErrOutputContract
	}
	return joined.String(), nil
}

// checkContract validates the synthetic normalization output: one known id and
// exactly the expected normalized value. Omitted, duplicated or unknown output
// is a failure, never an exclusion.
func checkContract(text string) error {
	normalized := strings.TrimSpace(text)
	if strings.HasPrefix(normalized, "```") {
		if newline := strings.IndexByte(normalized, '\n'); newline >= 0 {
			normalized = normalized[newline+1:]
		}
		normalized = strings.TrimSuffix(strings.TrimSpace(normalized), "```")
	}
	start, end := strings.IndexByte(normalized, '{'), strings.LastIndexByte(normalized, '}')
	if start < 0 || end <= start {
		return ErrOutputContract
	}
	var contract probeContract
	if err := json.Unmarshal([]byte(normalized[start:end+1]), &contract); err != nil {
		return ErrOutputContract
	}
	if len(contract.Values) != 1 || contract.Values[0].ID != "probe" {
		return ErrOutputContract
	}
	if strings.TrimSpace(contract.Values[0].Value) != probeValue {
		return ErrOutputContract
	}
	return nil
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
