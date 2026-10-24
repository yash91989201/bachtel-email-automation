package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

func TestFormatResolutionErrorRetainsSafeAttemptReasons(t *testing.T) {
	const privateMarker = "PRIVATE_PROVIDER_BODY_MARKER"
	app := newApp(t)
	owner := provision(t, app, "diagnostics@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		status := http.StatusNotFound
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			status = http.StatusBadRequest
		}
		http.Error(w, `{"error":"`+privateMarker+`"}`, status)
	})
	store := newStore(t, app)

	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{
			Provider: ai.ProviderOpenCodeGo,
			APIRoot:  fake.server.URL + "/v1",
			Model:    "deepseek-v4.1-flash",
		},
		APIKey: "test-key",
	})
	if !errors.Is(err, ai.ErrModelUnavailable) {
		t.Fatalf("stage validation error = %v, want ErrModelUnavailable", err)
	}
	for _, want := range []string{"responses", "HTTP 404", "chat-completions", "HTTP 400"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not include %q", err, want)
		}
	}
	if strings.Contains(err.Error(), privateMarker) {
		t.Fatalf("error exposed provider response content: %q", err)
	}
	if fake.requests.Load() != 2 {
		t.Fatalf("request count = %d, want 2", fake.requests.Load())
	}
	if draft, draftErr := store.Draft(context.Background(), owner); draftErr != nil || draft != nil {
		t.Fatalf("failed validation draft = %+v, err = %v", draft, draftErr)
	}
	if _, connectionErr := store.Connection(context.Background(), owner); !errors.Is(connectionErr, ai.ErrNotConfigured) {
		t.Fatalf("failed validation connection error = %v, want ErrNotConfigured", connectionErr)
	}
}

func TestEmptyTokenLimitResponsesAreSafeOutputContractErrors(t *testing.T) {
	const privateMarker = "PRIVATE_REASONING_OR_BODY_MARKER"
	tests := []struct {
		name   string
		format ai.APIFormat
		body   string
	}{
		{
			name:   "Responses max output tokens",
			format: ai.FormatResponses,
			body:   `{"output_text":"","incomplete_details":{"reason":"max_output_tokens"},"reasoning":"` + privateMarker + `"}`,
		},
		{
			name:   "Chat Completions length finish reason",
			format: ai.FormatChatCompletions,
			body:   `{"choices":[{"message":{"content":"","reasoning_content":"` + privateMarker + `"},"finish_reason":"length"}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "token-limit@example.test")
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, writeErr := w.Write([]byte(tc.body)); writeErr != nil {
					t.Errorf("write response: %v", writeErr)
				}
			})
			store := newStore(t, app)
			_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
				Config: customConfig(fake.server.URL, "model", tc.format),
				APIKey: "test-key",
			})
			if !errors.Is(err, ai.ErrOutputContract) {
				t.Fatalf("stage validation error = %v, want ErrOutputContract", err)
			}
			if !strings.Contains(err.Error(), "output token limit reached") || !strings.Contains(err.Error(), "HTTP 200") {
				t.Fatalf("error = %q, want HTTP 200 and output token limit reason", err)
			}
			if strings.Contains(err.Error(), privateMarker) {
				t.Fatalf("error exposed reasoning or raw response: %q", err)
			}
			if draft, draftErr := store.Draft(context.Background(), owner); draftErr != nil || draft != nil {
				t.Fatalf("failed validation draft = %+v, err = %v", draft, draftErr)
			}
			if _, connectionErr := store.Connection(context.Background(), owner); !errors.Is(connectionErr, ai.ErrNotConfigured) {
				t.Fatalf("failed validation connection error = %v, want ErrNotConfigured", connectionErr)
			}
		})
	}
}

func TestValidationAllowsReasoningBeforeProbeOutput(t *testing.T) {
	for _, tc := range []struct {
		provider ai.Provider
		format   ai.APIFormat
	}{
		{provider: ai.ProviderXiaomiMiMo, format: ai.FormatChatCompletions},
		{provider: ai.ProviderCustom, format: ai.FormatResponses},
		{provider: ai.ProviderCustom, format: ai.FormatChatCompletions},
	} {
		t.Run(string(tc.provider)+"/"+string(tc.format), func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "reasoning@example.test")
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
				tokenField := "max_tokens"
				if tc.format == ai.FormatResponses {
					tokenField = "max_output_tokens"
				}
				budget, ok := body[tokenField].(float64)
				if !ok || budget <= 256 || budget > ai.MaxTokens {
					// Simulate a provider that consumes the old 256-token budget
					// on reasoning before it can return the normalization object.
					respondProbe(t, tc.format, "")(w, nil, nil)
					return
				}
				respondProbe(t, tc.format, probeOutput)(w, nil, nil)
			})
			store := newStore(t, app)
			cfg := customConfig(fake.server.URL, "model", tc.format)
			cfg.Provider = tc.provider
			staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
				Config: cfg,
				APIKey: "test-key",
			})
			if err != nil {
				t.Fatalf("validation exhausted the probe output budget: %v", err)
			}
			if staged.Config != cfg || fake.requests.Load() != 1 {
				t.Fatalf("configuration = %+v, requests = %d; want %+v and one request", staged.Config, fake.requests.Load(), cfg)
			}
			if staged.BaseConfigurationGeneration != 0 || staged.BaseCredentialGeneration != 0 {
				t.Fatal("initial draft did not preserve zero base generations")
			}
			confirmed, err := store.Confirm(context.Background(), owner, staged.ID, 0)
			if err != nil {
				t.Fatalf("confirm initial connection: %v", err)
			}
			if confirmed.Config != cfg || confirmed.ConfigurationGeneration != 1 || confirmed.CredentialGeneration != 1 {
				t.Fatalf("unexpected initial connection: %+v", confirmed)
			}
		})
	}
}

func TestProbeContractDiagnosticsAreSpecificAndRedacted(t *testing.T) {
	const privateMarker = "PRIVATE_PROBE_MARKER"
	tests := []struct {
		name     string
		content  string
		want     string
		envelope string
	}{
		{name: "no JSON object", content: "provider prose " + privateMarker, want: "no JSON object"},
		{name: "invalid JSON", content: `{"values":[}` + privateMarker, want: "invalid JSON"},
		{name: "unexpected probe fields", content: `{"values":[{"id":"other","value":"` + privateMarker + `"}]}`, want: "unexpected probe fields"},
		{name: "normalized value mismatch", content: `{"values":[{"id":"probe","value":"private@example.test"}]}`, want: "normalized probe value mismatch"},
		{name: "malformed HTTP 200 envelope", envelope: `{"private":"` + privateMarker + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "contract@example.test")
			body := tc.envelope
			if body == "" {
				content, marshalErr := json.Marshal(tc.content)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				body = `{"choices":[{"message":{"content":` + string(content) + `}}]}`
			}
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, writeErr := w.Write([]byte(body)); writeErr != nil {
					t.Errorf("write response: %v", writeErr)
				}
			})
			store := newStore(t, app)
			_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
				Config: customConfig(fake.server.URL, "model", ai.FormatChatCompletions),
				APIKey: "test-key",
			})
			if !errors.Is(err, ai.ErrOutputContract) {
				t.Fatalf("stage validation error = %v, want ErrOutputContract", err)
			}
			if !strings.Contains(err.Error(), "HTTP 200") || tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want HTTP 200 and diagnostic %q", err, tc.want)
			}
			if strings.Contains(err.Error(), privateMarker) || strings.Contains(err.Error(), "private@example.test") {
				t.Fatalf("error exposed probe output: %q", err)
			}
			if tc.name == "malformed HTTP 200 envelope" && strings.Contains(err.Error(), "normalized probe value mismatch") {
				t.Fatalf("malformed envelope was reported as a value mismatch: %q", err)
			}
		})
	}
}
