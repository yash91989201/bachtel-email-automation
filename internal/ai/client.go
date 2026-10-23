package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

// probeContract is the required response shape.
type probeContract struct {
	Values []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"values"`
}

// validateConnection performs the one synthetic normalization request against
// the exact configuration. It never retries and never calls another model.
func validateConnection(ctx context.Context, o ValidationOptions, cfg Config, apiKey string) error {
	body, err := probeRequest(cfg)
	if err != nil {
		return err
	}
	text, err := postJSON(ctx, o, cfg, apiKey, "", body)
	if err != nil {
		return err
	}
	return checkContract(text)
}

// postJSON sends one exact-format request and returns the assistant text. It
// is the single transport boundary shared by connection validation and
// normalization, so both use the same endpoint policy, bounds and redaction.
func postJSON(ctx context.Context, o ValidationOptions, cfg Config, apiKey, session string, body []byte) (string, error) {
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
	raw, err := readBounded(o, resp, ErrModelUnavailable)
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
	return readBounded(o, resp, ErrDiscoveryUnavailable)
}

// readBounded maps one response to a fixed operator-safe outcome and returns
// its bounded body. notFound is the caller's fixed meaning of a 404.
func readBounded(o ValidationOptions, resp *http.Response, notFound error) ([]byte, error) {
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// The outcome is already decided; a close failure cannot change it.
			_ = err
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAuthentication
	case http.StatusNotFound:
		return nil, notFound
	case http.StatusTooManyRequests:
		return nil, ErrThrottled
	default:
		if resp.StatusCode >= 500 {
			return nil, ErrProviderFailure
		}
		return nil, fmt.Errorf("%w: the endpoint refused the request", ErrInvalidSetup)
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
