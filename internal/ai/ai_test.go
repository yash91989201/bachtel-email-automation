package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

const probeOutput = `{"values":[{"id":"probe","value":"probe@example.test"}]}`

func newApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := testutil.NewApp(t)
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

// fakeEndpoint simulates one or more protocol paths on the same root. It
// records every request so tests can assert exact counts, paths and bodies.
type fakeEndpoint struct {
	server   *httptest.Server
	requests atomic.Int32
	paths    []string
	bodies   []map[string]any
	agents   []string
	sessions []string
	auths    []string
}

func newFakeEndpoint(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *fakeEndpoint {
	t.Helper()
	fake := &fakeEndpoint{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.requests.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		fake.paths = append(fake.paths, r.URL.Path)
		fake.bodies = append(fake.bodies, body)
		fake.agents = append(fake.agents, r.Header.Get("User-Agent"))
		fake.sessions = append(fake.sessions, r.Header.Get("x-opencode-session"))
		fake.auths = append(fake.auths, r.Header.Get("Authorization"))
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

// respondProbeByPath answers the probe contract over whichever format path
// the request used, so one fake can serve both protocols.
func respondProbeByPath(t *testing.T, text string) func(http.ResponseWriter, *http.Request, map[string]any) {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		format := ai.FormatChatCompletions
		if strings.HasSuffix(r.URL.Path, "/responses") {
			format = ai.FormatResponses
		}
		respondProbe(t, format, text)(w, nil, nil)
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
			fake := newFakeEndpoint(t, respondProbe(t, format, probeOutput))
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
			if len(fake.bodies) != 1 || fake.bodies[0]["model"] != "model-exact" {
				t.Fatalf("request bodies = %v", fake.bodies)
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatChatCompletions, probeOutput))
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
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
	fake := newFakeEndpoint(t, respondProbeByPath(t, probeOutput))
	store := ai.NewStore(app, func() ([]byte, error) { return nil, errors.New("missing") })
	store.SetValidationOptions(ai.ValidationOptions{Timeout: time.Second, AllowPrivate: true})
	for name, proposal := range map[string]ai.Proposal{
		"custom explicit format": {Config: customConfig(fake.server.URL, "model", ai.FormatResponses), APIKey: "test-key"},
		"preset new selection":   {Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"}, APIKey: "test-key"},
	} {
		if _, err := store.StageValidation(context.Background(), owner, proposal); !errors.Is(err, ai.ErrCredentialUnreadable) {
			t.Fatalf("%s stage without key err = %v", name, err)
		}
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
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
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
	if !slices.Equal(opencode.Formats, []ai.APIFormat{ai.FormatResponses, ai.FormatChatCompletions}) {
		t.Fatalf("opencode formats = %v", opencode.Formats)
	}
	xiaomi, ok := ai.PresetFor(ai.ProviderXiaomiMiMo)
	if !ok || xiaomi.DefaultRoot != ai.XiaomiRoot || ai.XiaomiRoot != "https://api.xiaomimimo.com/v1" {
		t.Fatalf("xiaomi preset = %+v", xiaomi)
	}
	if !slices.Equal(xiaomi.Formats, []ai.APIFormat{ai.FormatChatCompletions}) {
		t.Fatalf("xiaomi formats = %v", xiaomi.Formats)
	}
	custom, ok := ai.PresetFor(ai.ProviderCustom)
	if !ok || custom.DefaultRoot != "" || !slices.Equal(custom.Formats, []ai.APIFormat{ai.FormatResponses, ai.FormatChatCompletions}) {
		t.Fatalf("custom preset = %+v", custom)
	}
}

// A caller-supplied preset format is ignored: the staged format comes from
// the endpoint's first passing format, and the submitted root override is
// preserved exactly.
func TestPresetIgnoresSubmittedFormatAndPreservesRoot(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respondProbe(t, ai.FormatChatCompletions, probeOutput)(w, nil, nil)
	})
	store := newStore(t, app)
	root := fake.server.URL + "/v1"

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{
			Provider:  ai.ProviderOpenCodeGo,
			APIRoot:   root,
			APIFormat: ai.FormatResponses, // ignored for a new preset selection
			Model:     "brand-new-model",
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
		t.Fatalf("staged format = %q, want the first passing format", staged.Config.APIFormat)
	}
	if len(fake.sessions) == 0 || fake.sessions[0] == "" {
		t.Fatal("opencode session header missing")
	}
}

// A model no compiled catalog has ever listed validates and stages on the
// first format with exactly one request to its path.
func TestOpenCodeUnknownModelStagesFirstFormat(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, respondProbeByPath(t, probeOutput))
	store := newStore(t, app)
	root := fake.server.URL + "/v1"

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: root, Model: "brand-new-model"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIFormat != ai.FormatResponses {
		t.Fatalf("staged format = %q, want Responses", staged.Config.APIFormat)
	}
	if staged.Config.APIRoot != root || staged.Config.Model != "brand-new-model" {
		t.Fatalf("staged config = %+v", staged.Config)
	}
	if fake.requests.Load() != 1 || len(fake.paths) != 1 || fake.paths[0] != "/v1/responses" {
		t.Fatalf("requests = %d paths = %v", fake.requests.Load(), fake.paths)
	}
	if fake.bodies[0]["model"] != "brand-new-model" {
		t.Fatalf("request body model = %v", fake.bodies[0]["model"])
	}
	if fake.auths[0] != "Bearer test-key" {
		t.Fatalf("authorization = %q", fake.auths[0])
	}
}

// A first-format compatibility rejection continues to the second format with
// the same endpoint, model and key, in exactly two requests.
func TestOpenCodeFallsBackToChatCompletions(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		respondProbe(t, ai.FormatChatCompletions, probeOutput)(w, nil, nil)
	})
	store := newStore(t, app)

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIFormat != ai.FormatChatCompletions {
		t.Fatalf("staged format = %q, want Chat Completions", staged.Config.APIFormat)
	}
	if fake.requests.Load() != 2 ||
		fake.paths[0] != "/v1/responses" || fake.paths[1] != "/v1/chat/completions" {
		t.Fatalf("requests = %d paths = %v", fake.requests.Load(), fake.paths)
	}
	for i, body := range fake.bodies {
		if body["model"] != "brand-new-model" {
			t.Fatalf("request %d model = %v", i, body["model"])
		}
		if fake.auths[i] != "Bearer test-key" {
			t.Fatalf("request %d authorization = %q", i, fake.auths[i])
		}
	}
}

// When both formats would pass, only the first is attempted and staged: the
// choice is deterministic and no second request is spent.
func TestOpenCodeKeepsFirstPassingFormat(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, respondProbeByPath(t, probeOutput))
	store := newStore(t, app)

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIFormat != ai.FormatResponses {
		t.Fatalf("staged format = %q, want Responses", staged.Config.APIFormat)
	}
	if fake.requests.Load() != 1 || fake.paths[0] != "/v1/responses" {
		t.Fatalf("requests = %d paths = %v", fake.requests.Load(), fake.paths)
	}
}

// The Xiaomi preset supports one format, so exactly one Chat Completions
// request is made even when the endpoint would also serve Responses.
func TestXiaomiValidatesChatCompletionsOnly(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, respondProbeByPath(t, probeOutput))
	store := newStore(t, app)

	staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderXiaomiMiMo, APIRoot: fake.server.URL + "/v1", Model: "mimo-v2.6-flash"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Config.APIFormat != ai.FormatChatCompletions {
		t.Fatalf("staged format = %q, want Chat Completions", staged.Config.APIFormat)
	}
	if fake.requests.Load() != 1 || fake.paths[0] != "/v1/chat/completions" {
		t.Fatalf("requests = %d paths = %v", fake.requests.Load(), fake.paths)
	}
}

// A 200 answer that fails the response envelope or the normalization
// contract is a bounded compatibility failure: validation continues to the
// second format and stages that one.
func TestInvalidNormalizationContinuesToNextFormat(t *testing.T) {
	for name, bad := range map[string]string{
		"malformed envelope": "not json",
		"wrong probe value":  `{"values":[{"id":"probe","value":"changed@example.test"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if strings.HasSuffix(r.URL.Path, "/responses") {
					w.Header().Set("Content-Type", "application/json")
					if _, err := w.Write([]byte(bad)); err != nil {
						t.Errorf("write simulated response: %v", err)
					}
					return
				}
				respondProbe(t, ai.FormatChatCompletions, probeOutput)(w, nil, nil)
			})
			store := newStore(t, app)

			staged, err := store.StageValidation(context.Background(), owner, ai.Proposal{
				Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"},
				APIKey: "test-key",
			})
			if err != nil {
				t.Fatal(err)
			}
			if staged.Config.APIFormat != ai.FormatChatCompletions {
				t.Fatalf("staged format = %q, want Chat Completions", staged.Config.APIFormat)
			}
			if fake.requests.Load() != 2 ||
				fake.paths[0] != "/v1/responses" || fake.paths[1] != "/v1/chat/completions" {
				t.Fatalf("requests = %d paths = %v", fake.requests.Load(), fake.paths)
			}
		})
	}
}

// The pinned single format reports the contract failure itself and stages
// nothing.
func TestSingleFormatInvalidOutputStagesNothing(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write simulated response: %v", err)
		}
	})
	store := newStore(t, app)

	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: customConfig(fake.server.URL, "model", ai.FormatResponses),
		APIKey: "test-key",
	})
	if !errors.Is(err, ai.ErrOutputContract) {
		t.Fatalf("err = %v, want output contract failure", err)
	}
	if fake.requests.Load() != 1 {
		t.Fatalf("requests = %d, want exactly one", fake.requests.Load())
	}
	if staged, err := store.Draft(context.Background(), owner); err != nil || staged != nil {
		t.Fatalf("failed validation staged a proposal: %+v err=%v", staged, err)
	}
	if _, err := store.Connection(context.Background(), owner); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("failed validation activated a connection: %v", err)
	}
}

// When every supported format is incompatible, both are attempted once and
// nothing is staged; the error names no secret.
func TestAllFormatsIncompatibleStagesNothing(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusNotFound)
	})
	store := newStore(t, app)

	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"},
		APIKey: "test-key",
	})
	if !errors.Is(err, ai.ErrModelUnavailable) {
		t.Fatalf("err = %v, want model unavailable", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatal("error leaked the API key")
	}
	if fake.requests.Load() != 2 {
		t.Fatalf("requests = %d, want one attempt per format", fake.requests.Load())
	}
	if staged, err := store.Draft(context.Background(), owner); err != nil || staged != nil {
		t.Fatalf("incompatible formats staged a proposal: %+v err=%v", staged, err)
	}
	if _, err := store.Connection(context.Background(), owner); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("incompatible formats activated a connection: %v", err)
	}
}

// Every failure outside the compatibility continuation classes stops the
// resolution after exactly one request: no second format is attempted.
func TestResolutionStopsOnFirstFailure(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		delay   time.Duration
		maxSize int64
		cancel  bool
		want    error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: ai.ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, want: ai.ErrAuthentication},
		{name: "throttled", status: http.StatusTooManyRequests, want: ai.ErrThrottled},
		{name: "service failure", status: http.StatusBadGateway, want: ai.ErrProviderFailure},
		{name: "unclassified client error", status: http.StatusPaymentRequired, want: ai.ErrInvalidSetup},
		{name: "deadline exhausted by slow response", delay: 300 * time.Millisecond, body: probeOutput, want: ai.ErrEndpointTimeout},
		{name: "oversized response", body: strings.Repeat("x", 4096), maxSize: 256, want: ai.ErrResponseTooLarge},
		{name: "parent cancellation", cancel: true, want: ai.ErrEndpointTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newApp(t)
			owner := provision(t, app, "operator@example.test")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if tc.cancel {
					cancel()
					<-r.Context().Done()
					return
				}
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("write simulated response: %v", err)
				}
			})
			store := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
			opts := ai.ValidationOptions{Timeout: 150 * time.Millisecond, MaxResponseBytes: 512 << 10, AllowPrivate: true}
			if tc.maxSize > 0 {
				opts.MaxResponseBytes = tc.maxSize
			}
			store.SetValidationOptions(opts)

			_, err := store.StageValidation(ctx, owner, ai.Proposal{
				Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: fake.server.URL + "/v1", Model: "brand-new-model"},
				APIKey: "test-key",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "test-key") {
				t.Fatal("error leaked the API key")
			}
			if fake.requests.Load() != 1 {
				t.Fatalf("requests = %d, want exactly one attempt", fake.requests.Load())
			}
		})
	}
}

// A failed same-selection repair stays pinned to the recorded format: the
// endpoint's willingness to serve another format changes nothing.
func TestFailedRepairNeverSwitchesFormat(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	fake := newFakeEndpoint(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		respondProbe(t, ai.FormatChatCompletions, probeOutput)(w, nil, nil)
	})
	root := fake.server.URL + "/v1"
	seedConnection(t, app, owner, ai.Config{
		Provider:  ai.ProviderOpenCodeGo,
		APIRoot:   root,
		APIFormat: ai.FormatResponses,
		Model:     "saved-model",
	})
	store := newStore(t, app)

	_, err := store.StageValidation(context.Background(), owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderOpenCodeGo, APIRoot: root, Model: "saved-model"},
		APIKey: "new-key",
	})
	if !errors.Is(err, ai.ErrModelUnavailable) {
		t.Fatalf("err = %v, want model unavailable", err)
	}
	if fake.requests.Load() != 1 || fake.paths[0] != "/v1/responses" {
		t.Fatalf("requests = %d paths = %v, want one pinned-format attempt", fake.requests.Load(), fake.paths)
	}
	active, err := store.Connection(context.Background(), owner)
	if err != nil || active.Config.APIFormat != ai.FormatResponses || active.ConfigurationGeneration != 1 || active.CredentialGeneration != 1 {
		t.Fatalf("active connection changed after failed repair: %+v err=%v", active, err)
	}
	if staged, err := store.Draft(context.Background(), owner); err != nil || staged != nil {
		t.Fatalf("failed repair staged a proposal: %+v err=%v", staged, err)
	}
}

// seedConnection writes one confirmed configuration without a provider call,
// so tests can model a model the provider no longer lists.
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatChatCompletions, probeOutput))
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

func TestDiscoverModelsReturnsUnvalidatedChoices(t *testing.T) {
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
			payload:  `{"data":[{"id":"brand-new-model"},{"id":"mystery-model"},{"id":"brand-new-model"},{"id":"bad model"},{"id":""}]}`,
			wantIDs:  []string{"brand-new-model", "mystery-model"},
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
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
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
