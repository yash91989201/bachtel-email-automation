package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/yash91989201/bachtel-email-automation/internal/accountlock"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

const probeOutput = `{"values":[{"id":"probe","value":"probe@example.test"}]}`

func newApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func newStore(t *testing.T, app core.App) *ai.Store {
	t.Helper()
	store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
	store.SetValidationOptions(ai.ValidationOptions{Timeout: 2 * time.Second, MaxResponseBytes: 512 << 10, AllowPrivate: true})
	return store
}

func provision(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// fakeEndpoint simulates one supported protocol. It verifies the bearer
// credential and records the model, user agent and session headers.
type fakeEndpoint struct {
	server   *httptest.Server
	requests atomic.Int32
	models   []string
	agents   []string
	sessions []string
}

func newFakeEndpoint(t *testing.T, format ai.APIFormat, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *fakeEndpoint {
	t.Helper()
	fake := &fakeEndpoint{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.requests.Add(1)
		expectedPath := "/v1" + format.Path()
		if r.URL.Path != expectedPath {
			t.Errorf("path = %q, want %q", r.URL.Path, expectedPath)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || len(got) <= len("Bearer ") {
			t.Errorf("authorization header missing")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if model, ok := body["model"].(string); ok && model != "" {
			fake.models = append(fake.models, model)
		}
		fake.agents = append(fake.agents, r.Header.Get("User-Agent"))
		fake.sessions = append(fake.sessions, r.Header.Get("x-opencode-session"))
		handler(w, r, body)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func respondProbe(t *testing.T, format ai.APIFormat, text string) func(http.ResponseWriter, *http.Request, map[string]any) {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		var payload any
		switch format {
		case ai.FormatResponses:
			payload = map[string]any{
				"output": []map[string]any{{
					"type":    "message",
					"content": []map[string]string{{"type": "output_text", "text": text}},
				}},
			}
		default:
			payload = map[string]any{
				"choices": []map[string]any{{"message": map[string]string{"content": text}}},
			}
		}
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encode simulated response: %v", err)
		}
	}
}

func customConfig(root, model string, format ai.APIFormat) ai.Config {
	return ai.Config{Provider: ai.ProviderCustom, APIRoot: root + "/v1", APIFormat: format, Model: model}
}

func TestValidateAndConfirmBothFormats(t *testing.T) {
	for _, format := range []ai.APIFormat{ai.FormatResponses, ai.FormatChatCompletions} {
		t.Run(string(format), func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			fake := newFakeEndpoint(t, format, respondProbe(t, format, probeOutput))
			store := newStore(t, app)
			cfg := customConfig(fake.server.URL, "model-exact", format)

			staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: cfg, APIKey: "test-key"})
			if err != nil {
				t.Fatalf("stage validation: %v", err)
			}
			if staged.Config != cfg {
				t.Fatalf("staged config = %+v, want %+v", staged.Config, cfg)
			}
			if _, err := store.Connection(context.Background(), owner); !errors.Is(err, ai.ErrNotConfigured) {
				t.Fatalf("staged proposal became active: %v", err)
			}
			confirmed, err := store.Confirm(context.Background(), owner, staged.ID, 0)
			if err != nil {
				t.Fatalf("confirm: %v", err)
			}
			if confirmed.Config != cfg || confirmed.ConfigurationGeneration != 1 || confirmed.CredentialGeneration != 1 {
				t.Fatalf("confirmed = %+v", confirmed)
			}
			key, err := store.Credential(context.Background(), owner)
			if err != nil || key != "test-key" {
				t.Fatalf("credential = %q, err=%v", key, err)
			}
			if staged, err := store.Draft(context.Background(), owner); err != nil || staged != nil {
				t.Fatalf("draft after confirm = %+v, err=%v", staged, err)
			}
			if len(fake.models) != 1 || fake.models[0] != "model-exact" {
				t.Fatalf("models requested = %v", fake.models)
			}
			if fake.agents[0] != "bachtel-email-automation/1.0" {
				t.Fatalf("user agent = %q", fake.agents[0])
			}
		})
	}
}

func TestXiaomiPresetSurvivesRestart(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatChatCompletions, respondProbe(t, ai.FormatChatCompletions, probeOutput))
	store := newStore(t, app)
	root := fake.server.URL + "/v1"
	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: root, Model: "mimo-v2.6-pro"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, staged.ID, 0); err != nil {
		t.Fatal(err)
	}
	restarted := newStore(t, app)
	connection, err := restarted.Connection(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Config.Provider != ai.ProviderXiaomiMiMo ||
		connection.Config.APIFormat != ai.FormatChatCompletions ||
		connection.Config.APIRoot != root {
		t.Fatalf("restarted xiaomi connection = %+v", connection.Config)
	}
	if key, err := restarted.Credential(context.Background(), owner); err != nil || key != "test-key" {
		t.Fatalf("restarted credential = %q, err=%v", key, err)
	}
}

func TestRestartKeepsConnectionAndRevealsNoSecret(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, staged.ID, 0); err != nil {
		t.Fatal(err)
	}
	restarted := newStore(t, app)
	connection, err := restarted.Connection(context.Background(), owner)
	if err != nil || !connection.APIKeyConfigured {
		t.Fatalf("restart read = %+v, err=%v", connection, err)
	}
	key, err := restarted.Credential(context.Background(), owner)
	if err != nil || key != "test-key" {
		t.Fatalf("restart credential = %q, err=%v", key, err)
	}
	// A store with the wrong key reads the non-secret setup but must not
	// reveal or overwrite the ciphertext.
	wrongKey := ai.NewStore(app, func() ([]byte, error) { return []byte("ffffffffffffffffffffffffffffffff"), nil })
	wrongKey.SetValidationOptions(ai.ValidationOptions{Timeout: time.Second, AllowPrivate: true})
	if _, err := wrongKey.Credential(context.Background(), owner); !errors.Is(err, ai.ErrCredentialUnreadable) {
		t.Fatalf("wrong key credential err = %v", err)
	}
	if _, err := wrongKey.Connection(context.Background(), owner); err != nil {
		t.Fatalf("wrong key connection read err = %v", err)
	}
	key, err = restarted.Credential(context.Background(), owner)
	if err != nil || key != "test-key" {
		t.Fatalf("ciphertext changed after wrong-key read: %q err=%v", key, err)
	}
}

func TestMissingEncryptionKeyBlocksSetup(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := ai.NewStore(app, func() ([]byte, error) { return nil, errors.New("missing") })
	store.SetValidationOptions(ai.ValidationOptions{Timeout: time.Second, AllowPrivate: true})
	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model", ai.FormatResponses), APIKey: "test-key"})
	if !errors.Is(err, ai.ErrCredentialUnreadable) {
		t.Fatalf("stage without key err = %v", err)
	}
	if fake.requests.Load() != 0 {
		t.Fatal("provider was contacted without a usable encryption key")
	}
}

func TestValidationOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		text    string
		delay   time.Duration
		maxSize int64
		want    error
	}{
		{name: "malformed output", status: http.StatusOK, text: "not json", want: ai.ErrOutputContract},
		{name: "wrong probe value", status: http.StatusOK, text: `{"values":[{"id":"probe","value":"changed@example.test"}]}`, want: ai.ErrOutputContract},
		{name: "unauthorized", status: http.StatusUnauthorized, want: ai.ErrAuthentication},
		{name: "unknown model", status: http.StatusNotFound, want: ai.ErrModelUnavailable},
		{name: "throttled", status: http.StatusTooManyRequests, want: ai.ErrThrottled},
		{name: "provider failure", status: http.StatusBadGateway, want: ai.ErrProviderFailure},
		{name: "timeout", status: http.StatusOK, delay: 300 * time.Millisecond, text: probeOutput, want: ai.ErrEndpointTimeout},
		{name: "too large", status: http.StatusOK, text: strings.Repeat("x", 4096), maxSize: 256, want: ai.ErrResponseTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			fake := newFakeEndpoint(t, ai.FormatResponses, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				if tc.status != http.StatusOK {
					w.WriteHeader(tc.status)
					return
				}
				respondProbe(t, ai.FormatResponses, tc.text)(w, nil, nil)
			})
			store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
			opts := ai.ValidationOptions{Timeout: 150 * time.Millisecond, MaxResponseBytes: 512 << 10, AllowPrivate: true}
			if tc.maxSize > 0 {
				opts.MaxResponseBytes = tc.maxSize
			}
			store.SetValidationOptions(opts)
			_, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model", ai.FormatResponses), APIKey: "test-key"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Fatal("error leaked the API key")
			}
		})
	}
}

func TestUnsafeEndpointsAreRefusedBeforeSendingSecrets(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
	store.SetValidationOptions(ai.ValidationOptions{Timeout: time.Second})
	store.SetValidationOptions(ai.ValidationOptions{Timeout: time.Second})
	for _, root := range []string{"http://127.0.0.1:9/v1", "https://10.0.0.1/v1", "https://169.254.169.254/v1", "http://example.test/v1"} {
		_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
			Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: root, APIFormat: ai.FormatResponses, Model: "model"},
			APIKey: "test-key",
		})
		if !errors.Is(err, ai.ErrEndpointUnsafe) {
			t.Fatalf("root %q err = %v, want endpoint refusal", root, err)
		}
	}
}

func TestCrossOriginRedirectNeverReceivesCredentials(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	var received atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	store := newStore(t, app)
	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(redirect.URL, "model", ai.FormatResponses), APIKey: "test-key"})
	if !errors.Is(err, ai.ErrEndpointUnsafe) {
		t.Fatalf("redirect err = %v, want endpoint refusal", err)
	}
	if received.Load() != 0 {
		t.Fatal("credentials or request reached the redirect target")
	}
}

func TestSwitchLockAndSameConfigurationRepair(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	first, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-a", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, first.ID, 0); err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("pending tasks block this change")
	store.SetLockCheck(func(context.Context, core.App, string) error { return blocked })

	// A configuration switch is refused and leaves the active setup intact.
	switching, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-b", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, switching.ID, 1); !errors.Is(err, blocked) {
		t.Fatalf("switch while pending err = %v", err)
	}
	active, err := store.Connection(context.Background(), owner)
	if err != nil || active.Config.Model != "model-a" || active.ConfigurationGeneration != 1 {
		t.Fatalf("active after refused switch = %+v err=%v", active, err)
	}

	// The same configuration may rotate its key while tasks are pending.
	repair, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-a", ai.FormatResponses), APIKey: "new-key"})
	if err != nil {
		t.Fatal(err)
	}
	if repair.BaseConfigurationGeneration != 1 {
		t.Fatalf("repair base generation = %d", repair.BaseConfigurationGeneration)
	}
	confirmed, err := store.Confirm(context.Background(), owner, repair.ID, 1)
	if err != nil {
		t.Fatalf("repair confirm: %v", err)
	}
	if confirmed.ConfigurationGeneration != 1 || confirmed.CredentialGeneration != 2 || confirmed.Config.Model != "model-a" {
		t.Fatalf("repair result = %+v", confirmed)
	}
}

func TestStaleProposalNeverOverwritesNewerConfiguration(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	first, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-a", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	// A second validation replaces the staged proposal; the first id is stale.
	second, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-b", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, first.ID, 0); !errors.Is(err, ai.ErrDraftStale) {
		t.Fatalf("stale confirm err = %v", err)
	}
	if _, err := store.Confirm(context.Background(), owner, second.ID, 0); err != nil {
		t.Fatal(err)
	}
	// Replaying the confirmed proposal is refused too.
	if _, err := store.Confirm(context.Background(), owner, second.ID, 0); !errors.Is(err, ai.ErrDraftStale) {
		t.Fatalf("replayed confirm err = %v", err)
	}
}

func TestOwnershipIsolation(t *testing.T) {
	app := newApp(t)
	alice := provision(t, app, "alice@example.test")
	bob := provision(t, app, "bob@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	staged, err := store.StageValidation(context.Background(), alice, ai.Proposal{Config: customConfig(fake.server.URL, "model", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), alice, staged.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Connection(context.Background(), bob); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("bob read alice connection: %v", err)
	}
	if _, err := store.Confirm(context.Background(), bob, staged.ID, 0); !errors.Is(err, ai.ErrDraftStale) {
		t.Fatalf("bob confirmed alice draft: %v", err)
	}
}

func TestInvalidSetupIsRejected(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	store := newStore(t, app)
	cases := []ai.Proposal{
		{Config: ai.Config{Provider: "unknown", APIRoot: "https://example.test/v1", APIFormat: ai.FormatResponses, Model: "m"}, APIKey: "k"},
		{Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: "https://example.test/v1", APIFormat: "unknown", Model: "m"}, APIKey: "k"},
		{Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: "https://example.test/v1", APIFormat: ai.FormatResponses, Model: "  "}, APIKey: "k"},
		{Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: "https://example.test/v1", APIFormat: ai.FormatResponses, Model: "m"}, APIKey: ""},
	}
	for i, proposal := range cases {
		if _, err := store.StageValidation(context.Background(), owner, proposal); !errors.Is(err, ai.ErrInvalidSetup) {
			t.Fatalf("case %d err = %v, want invalid setup", i, err)
		}
	}
}

func TestPresetReferenceData(t *testing.T) {
	opencode, ok := ai.PresetFor(ai.ProviderOpenCodeGo)
	if !ok || opencode.DefaultRoot != ai.OpenCodeRoot || ai.OpenCodeRoot != "https://opencode.ai/inference/go/openai/v1" {
		t.Fatalf("opencode preset = %+v", opencode)
	}
	xiaomi, ok := ai.PresetFor(ai.ProviderXiaomiMiMo)
	if !ok || xiaomi.DefaultRoot != ai.XiaomiRoot || ai.XiaomiRoot != "https://api.xiaomimimo.com/v1" {
		t.Fatalf("xiaomi preset = %+v", xiaomi)
	}
	if !xiaomi.Formats[0].Valid() {
		t.Fatalf("xiaomi formats = %v", xiaomi.Formats)
	}
	for _, tc := range []struct {
		provider ai.Provider
		model    string
		format   ai.APIFormat
	}{
		{ai.ProviderOpenCodeGo, "muse-spark-1.3", ai.FormatResponses},
		{ai.ProviderOpenCodeGo, "deepseek-v4.1-flash", ai.FormatChatCompletions},
		{ai.ProviderXiaomiMiMo, "mimo-v2.6-pro", ai.FormatChatCompletions},
	} {
		format, ok := ai.ModelFormat(tc.provider, tc.model)
		if !ok || format != tc.format {
			t.Fatalf("%s/%s format = %q, %v; want %q", tc.provider, tc.model, format, ok, tc.format)
		}
	}
	if _, ok := ai.ModelFormat(ai.ProviderXiaomiMiMo, "mimo-unknown-suffix"); ok {
		t.Fatal("an unknown model contract was assigned a format")
	}
	if _, ok := ai.ModelFormat(ai.ProviderCustom, "any-model"); ok {
		t.Fatal("a custom model was assigned a format")
	}
}

func TestPresetResolvesFormatAndPreservesSubmittedRoot(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatChatCompletions, respondProbe(t, ai.FormatChatCompletions, probeOutput))
	store := newStore(t, app)
	root := fake.server.URL + "/v1"

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{
			Provider:  ai.ProviderOpenCodeGo,
			APIRoot:   root,
			APIFormat: ai.FormatResponses,
			Model:     "deepseek-v4.1-flash",
		},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIRoot != root {
		t.Fatalf("staged root = %q, want the submitted override %q", staged.Config.APIRoot, root)
	}
	if staged.Config.APIFormat != ai.FormatChatCompletions {
		t.Fatalf("staged format = %q, want the model's verified format", staged.Config.APIFormat)
	}
	if _, err := store.Confirm(context.Background(), owner, staged.ID, 0); err != nil {
		t.Fatal(err)
	}
	if len(fake.sessions) == 0 || fake.sessions[0] == "" {
		t.Fatal("opencode session header missing")
	}
}

func TestPresetRefusesUnknownModelContractBeforeContactingProvider(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)
	store := newStore(t, app)
	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: server.URL + "/v1", Model: "guessed-model"},
		APIKey: "test-key",
	})
	if !errors.Is(err, ai.ErrInvalidSetup) {
		t.Fatalf("err = %v, want invalid setup", err)
	}
	if requests.Load() != 0 {
		t.Fatal("an unverified model contract reached the provider")
	}
}

// seedConnection writes one confirmed configuration without a provider call,
// so tests can model a model that has since left the verified catalog.
func seedConnection(t *testing.T, app core.App, owner string, cfg ai.Config) {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(ai.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("owner", owner)
	record.Set("provider", string(cfg.Provider))
	record.Set("api_root", cfg.APIRoot)
	record.Set("api_format", string(cfg.APIFormat))
	record.Set("model", cfg.Model)
	record.Set("configuration_generation", 1)
	record.Set("credential_generation", 1)
	record.Set("credential", "seeded-ciphertext")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
}

func TestRepairKeepsStoredFormatForRetiredModel(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatChatCompletions, respondProbe(t, ai.FormatChatCompletions, probeOutput))
	root := fake.server.URL + "/v1"
	seedConnection(t, app, owner, ai.Config{
		Provider:  ai.ProviderOpenCodeGo,
		APIRoot:   root,
		APIFormat: ai.FormatChatCompletions,
		Model:     "retired-model",
	})
	store := newStore(t, app)

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: root, Model: "retired-model"},
		APIKey: "replacement-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIFormat != ai.FormatChatCompletions {
		t.Fatalf("retired model format = %q, want the recorded format", staged.Config.APIFormat)
	}
	if !staged.Repair(&ai.Connection{Config: ai.Config{
		Provider: ai.ProviderOpenCodeGo, APIRoot: root, APIFormat: ai.FormatChatCompletions, Model: "retired-model",
	}}) {
		t.Fatal("same-configuration repair was not recognised")
	}
	confirmed, err := store.Confirm(context.Background(), owner, staged.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Config.APIFormat != ai.FormatChatCompletions || confirmed.ConfigurationGeneration != 1 || confirmed.CredentialGeneration != 2 {
		t.Fatalf("repaired connection = %+v", confirmed)
	}
}

func TestDiscoverModelsDerivesURLsAndClassifies(t *testing.T) {
	tests := []struct {
		name     string
		provider ai.Provider
		root     func(base string) string
		listPath string
		payload  string
		wantIDs  []string
	}{
		{
			name:     "opencode documented suffix",
			provider: ai.ProviderOpenCodeGo,
			root:     func(base string) string { return base + "/inference/go/openai/v1" },
			listPath: "/inference/go/v1/models",
			payload:  `{"data":[{"id":"deepseek-v4.1-flash"},{"id":"mystery-model"},{"id":"deepseek-v4.1-flash"},{"id":"bad model"},{"id":""}]}`,
			wantIDs:  []string{"deepseek-v4.1-flash", "mystery-model"},
		},
		{
			name:     "xiaomi appends models",
			provider: ai.ProviderXiaomiMiMo,
			root:     func(base string) string { return base + "/v1" },
			listPath: "/v1/models",
			payload:  `{"data":[{"id":"mimo-v2.6-flash"}]}`,
			wantIDs:  []string{"mimo-v2.6-flash"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			var path, auth, agent string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				auth = r.Header.Get("Authorization")
				agent = r.Header.Get("User-Agent")
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tc.payload)); err != nil {
					t.Errorf("write model list: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			store := newStore(t, app)
			options, err := store.DiscoverModels(context.Background(), owner, ai.Proposal{
				Config: ai.Config{Provider: tc.provider, APIRoot: tc.root(server.URL)},
				APIKey: "test-key",
			})
			if err != nil {
				t.Fatal(err)
			}
			if path != tc.listPath {
				t.Fatalf("list path = %q, want %q", path, tc.listPath)
			}
			if auth != "Bearer test-key" || agent != "bachtel-email-automation/1.0" {
				t.Fatalf("headers = %q %q", auth, agent)
			}
			if len(options) != len(tc.wantIDs) {
				t.Fatalf("options = %+v", options)
			}
			for i, id := range tc.wantIDs {
				if options[i].ID != id {
					t.Fatalf("option %d = %+v, want id %q", i, options[i], id)
				}
			}
			format, ok := ai.ModelFormat(tc.provider, tc.wantIDs[0])
			if !ok || !options[0].Supported || options[0].Format != format {
				t.Fatalf("supported option = %+v", options[0])
			}
			if len(options) > 1 {
				if options[1].Supported || options[1].Reason == "" || options[1].Format != "" {
					t.Fatalf("unsupported option = %+v", options[1])
				}
			}
			// Discovery is read-only: nothing was staged or activated.
			if _, err := store.Connection(context.Background(), owner); !errors.Is(err, ai.ErrNotConfigured) {
				t.Fatalf("discovery activated a connection: %v", err)
			}
			if staged, err := store.Draft(context.Background(), owner); err != nil || staged != nil {
				t.Fatalf("discovery staged a proposal: %+v err=%v", staged, err)
			}
		})
	}
}

func TestDiscoverModelsOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		maxSize int64
		want    error
	}{
		{name: "malformed list", status: http.StatusOK, body: `not json`, want: ai.ErrDiscoveryUnavailable},
		{name: "missing data envelope", status: http.StatusOK, body: `{"models":[]}`, want: ai.ErrDiscoveryUnavailable},
		{name: "oversized list", status: http.StatusOK, body: `{"data":[{"id":"` + strings.Repeat("x", 4096) + `"}]}`, maxSize: 256, want: ai.ErrResponseTooLarge},
		{name: "rejected key", status: http.StatusUnauthorized, want: ai.ErrAuthentication},
		{name: "throttled", status: http.StatusTooManyRequests, want: ai.ErrThrottled},
		{name: "provider failure", status: http.StatusBadGateway, want: ai.ErrProviderFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				if tc.body != "" {
					if _, err := w.Write([]byte(tc.body)); err != nil {
						t.Errorf("write model list: %v", err)
					}
				}
			}))
			t.Cleanup(server.Close)
			store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
			opts := ai.ValidationOptions{Timeout: time.Second, MaxResponseBytes: 512 << 10, AllowPrivate: true}
			if tc.maxSize > 0 {
				opts.MaxResponseBytes = tc.maxSize
			}
			store.SetValidationOptions(opts)
			_, err := store.DiscoverModels(context.Background(), owner, ai.Proposal{
				Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: server.URL + "/v1"},
				APIKey: "test-key",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Fatal("discovery error leaked the API key")
			}
		})
	}
}

func TestDiscoverModelsEmptyListAndUnsupportedInputs(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(`{"data":[]}`)); err != nil {
			t.Errorf("write model list: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	store := newStore(t, app)

	options, err := store.DiscoverModels(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: server.URL + "/v1"},
		APIKey: "test-key",
	})
	if err != nil || len(options) != 0 {
		t.Fatalf("empty list = %+v, err=%v", options, err)
	}
	for name, proposal := range map[string]ai.Proposal{
		"custom provider":  {Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: server.URL + "/v1"}, APIKey: "test-key"},
		"unknown provider": {Config: ai.Config{Provider: "unknown", APIRoot: server.URL + "/v1"}, APIKey: "test-key"},
		"missing key":      {Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: server.URL + "/v1"}},
	} {
		if _, err := store.DiscoverModels(context.Background(), owner, proposal); !errors.Is(err, ai.ErrInvalidSetup) {
			t.Fatalf("%s err = %v, want invalid setup", name, err)
		}
	}
}

func TestDiscoverModelsCancellationAndRedirectAreRefused(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	store := newStore(t, app)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := store.DiscoverModels(ctx, owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: slow.URL + "/v1"},
		APIKey: "test-key",
	}); !errors.Is(err, ai.ErrEndpointTimeout) {
		t.Fatalf("cancelled discovery err = %v, want timeout", err)
	}

	var foreign atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreign.Add(1)
	}))
	t.Cleanup(target.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	if _, err := store.DiscoverModels(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: redirect.URL + "/v1"},
		APIKey: "test-key",
	}); !errors.Is(err, ai.ErrEndpointUnsafe) {
		t.Fatalf("cross-origin discovery err = %v, want endpoint refusal", err)
	}
	if foreign.Load() != 0 {
		t.Fatal("a discovery redirect delivered the credential to another origin")
	}
}

// pendingTask inserts one durable staging task so the lock can observe a task
// that appeared after a proposal was validated.
func pendingTask(t *testing.T, app core.App, owner string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("campaigns")
	if err != nil {
		t.Fatal(err)
	}
	campaign := core.NewRecord(collection)
	campaign.Set("owner", owner)
	campaign.Set("name", "Race campaign")
	campaign.Set("recipient_header", "Email")
	if err := app.Save(campaign); err != nil {
		t.Fatal(err)
	}
	id := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	now := types.NowDateTime().String()
	if _, err := app.DB().NewQuery("INSERT INTO send_tasks (id, campaign, status, source_name, schedule_state, created, updated) VALUES ({:id}, {:campaign}, 'staging', 'race.xlsx', 'none', {:now}, {:now})").
		Bind(dbx.Params{"id": id, "campaign": campaign.Id, "now": now}).Execute(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTaskAppearingDuringValidationBlocksActivation(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, ai.FormatResponses, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	first, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-a", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Confirm(context.Background(), owner, first.ID, 0); err != nil {
		t.Fatal(err)
	}
	store.SetLockCheck(accountlock.Check)

	// The proposal validates while no task exists; a task appears before the
	// confirmation callback runs.
	switching, err := store.StageValidation(context.Background(), owner, ai.Proposal{Config: customConfig(fake.server.URL, "model-b", ai.FormatResponses), APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := pendingTask(t, app, owner)
	if _, err := store.Confirm(context.Background(), owner, switching.ID, 1); accountlock.Blockers(err) == nil {
		t.Fatalf("activation was not refused by the task that appeared: %v", err)
	}
	active, err := store.Connection(context.Background(), owner)
	if err != nil || active.Config.Model != "model-a" || active.ConfigurationGeneration != 1 {
		t.Fatalf("refused activation changed the connection: %+v err=%v", active, err)
	}
	// Closing the task releases the lock and the same proposal still confirms.
	closedAt := types.NowDateTime().String()
	if _, err := app.DB().NewQuery("UPDATE send_tasks SET closed_at = {:closed} WHERE id = {:id}").
		Bind(dbx.Params{"closed": closedAt, "id": taskID}).Execute(); err != nil {
		t.Fatal(err)
	}
	confirmed, err := store.Confirm(context.Background(), owner, switching.ID, 1)
	if err != nil || confirmed.Config.Model != "model-b" || confirmed.ConfigurationGeneration != 2 {
		t.Fatalf("confirmed after close = %+v err=%v", confirmed, err)
	}
}
