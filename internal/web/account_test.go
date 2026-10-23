package web

// Account settings surface tests: the mailbox section keeps legacy paths,
// AI setup validates the exact configuration with a simulated endpoint,
// activation is explicit, and connection switches are locked by pending tasks.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

const aiProbeOutput = `{"values":[{"id":"probe","value":"probe@example.test"}]}`

// fakeAIEndpoint answers the synthetic probe over either supported protocol.
func fakeAIEndpoint(t *testing.T, format ai.APIFormat) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1"+format.Path() {
			t.Errorf("unexpected AI path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || len(got) <= len("Bearer ") {
			t.Errorf("missing bearer credential")
		}
		w.Header().Set("Content-Type", "application/json")
		var payload string
		switch format {
		case ai.FormatResponses:
			payload = `{"output":[{"type":"message","content":[{"type":"output_text","text":` + quoteJSON(aiProbeOutput) + `}]}]}`
		default:
			payload = `{"choices":[{"message":{"content":` + quoteJSON(aiProbeOutput) + `}}]}`
		}
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("write simulated response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func quoteJSON(raw string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(raw) + `"`
}

func validateAI(t *testing.T, handler http.Handler, cookie *http.Cookie, provider, root, format, model, key string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, handler, http.MethodPost, "/account/ai/validate", url.Values{
		"provider":   {provider},
		"api_root":   {root},
		"api_format": {format},
		"model":      {model},
		"api_key":    {key},
	}, withCookie(cookie))
}

func currentDraft(t *testing.T, app core.App, ownerID string) *core.Record {
	t.Helper()
	draft, err := app.FindFirstRecordByFilter(ai.DraftsCollection, "owner = {:o}", dbx.Params{"o": ownerID})
	if err != nil {
		t.Fatalf("read staged draft: %v", err)
	}
	return draft
}

func TestAccountSettingsSections(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)

	page := do(t, handler, http.MethodGet, "/account", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("account page: %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{"Account settings", `href="/account/ai"`, "Mailbox connection", `href="/account?provider=godaddy-titan"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("account page missing %q: %.400s", want, body)
		}
	}
	if strings.Contains(body, "Client Secret") {
		t.Fatal("mailbox credentials rendered before provider selection")
	}

	// The legacy mailbox path renders the same section, not a second surface.
	legacy := do(t, handler, http.MethodGet, "/connection", nil, withCookie(cookie))
	if legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "Mailbox connection") {
		t.Fatalf("legacy mailbox page: %d", legacy.Code)
	}
	if strings.Count(legacy.Body.String(), `href="/account/ai"`) != 1 {
		t.Fatal("legacy page is not the Account shell")
	}

	aiPage := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	if aiPage.Code != http.StatusOK {
		t.Fatalf("AI page: %d", aiPage.Code)
	}
	aiBody := aiPage.Body.String()
	for _, want := range []string{"AI connection", "Not configured", `action="/account/ai/validate"`, "OpenCode Go"} {
		if !strings.Contains(aiBody, want) {
			t.Fatalf("AI page missing %q: %.400s", want, aiBody)
		}
	}
	if strings.Contains(aiBody, "ai-secret-key") {
		t.Fatal("AI page leaked a submitted key")
	}
}

func TestAIConfigurationValidateAndConfirm(t *testing.T) {
	for _, format := range []ai.APIFormat{ai.FormatResponses, ai.FormatChatCompletions} {
		t.Run(string(format), func(t *testing.T) {
			app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
			provisionOperator(t, app)
			cookie := login(t, handler)
			fake := fakeAIEndpoint(t, format)

			rec := validateAI(t, handler, cookie, "custom", fake.URL+"/v1", string(format), "model-exact", "ai-secret-key")
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/ai?notice=validated" {
				t.Fatalf("validate: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			record, err := app.FindAuthRecordByEmail("operators", testOperatorEmail)
			if err != nil {
				t.Fatal(err)
			}
			draft := currentDraft(t, app, record.Id)
			page := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
			body := page.Body.String()
			for _, want := range []string{"Validated proposal", "model-exact", `action="/account/ai/confirm"`, draft.Id} {
				if !strings.Contains(body, want) {
					t.Fatalf("validated page missing %q: %.400s", want, body)
				}
			}
			if strings.Contains(body, "ai-secret-key") {
				t.Fatal("validated page leaked the API key")
			}

			confirm := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
				"draft":                             {draft.Id},
				"expected_configuration_generation": {"0"},
				"confirmed":                         {"yes"},
			}, withCookie(cookie))
			if confirm.Code != http.StatusSeeOther || confirm.Header().Get("Location") != "/account/ai?notice=confirmed" {
				t.Fatalf("confirm: status=%d location=%q", confirm.Code, confirm.Header().Get("Location"))
			}
			store := ai.NewStore(app, testKey)
			connection, err := store.Connection(t.Context(), record.Id)
			if err != nil {
				t.Fatal(err)
			}
			if connection.Config.Model != "model-exact" || connection.Config.APIFormat != format || connection.ConfigurationGeneration != 1 {
				t.Fatalf("confirmed connection = %+v", connection)
			}
			after := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
			if !strings.Contains(after.Body.String(), "Confirmed") || strings.Contains(after.Body.String(), "ai-secret-key") {
				t.Fatalf("confirmed page wrong: %.600s", after.Body.String())
			}
		})
	}
}

func TestAISwitchIsLockedByPendingTaskButInitialSetupAndRepairWork(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Locked account")
	upload := uploadWorkbook(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email"},
		{"one@example.test"},
	}))
	taskID := taskIDFromRedirect(t, upload, campaignID)
	locked := do(t, handler, http.MethodGet, "/account", nil, withCookie(cookie))
	if !strings.Contains(locked.Body.String(), "book.xlsx") || !strings.Contains(locked.Body.String(), taskID) {
		t.Fatalf("account page lacks the named blocking task: %.600s", locked.Body.String())
	}

	fake := fakeAIEndpoint(t, ai.FormatResponses)
	owner, err := app.FindAuthRecordByEmail("operators", testOperatorEmail)
	if err != nil {
		t.Fatal(err)
	}

	// Initial setup is allowed while a staged task exists.
	if rec := validateAI(t, handler, cookie, "custom", fake.URL+"/v1", string(ai.FormatResponses), "model-a", "ai-secret-key"); rec.Code != http.StatusSeeOther {
		t.Fatalf("initial validate: %d %.400s", rec.Code, rec.Body.String())
	}
	draft := currentDraft(t, app, owner.Id)
	if rec := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {draft.Id}, "expected_configuration_generation": {"0"}, "confirmed": {"yes"},
	}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("initial confirm: %d %.400s", rec.Code, rec.Body.String())
	}

	// Switching while the task is pending is refused with the named warning.
	if rec := validateAI(t, handler, cookie, "custom", fake.URL+"/v1", string(ai.FormatResponses), "model-b", "ai-secret-key"); rec.Code != http.StatusSeeOther {
		t.Fatalf("switch validate: %d", rec.Code)
	}
	switching := currentDraft(t, app, owner.Id)
	rec := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {switching.Id}, "expected_configuration_generation": {"1"}, "confirmed": {"yes"},
	}, withCookie(cookie))
	if rec.Code != http.StatusConflict {
		t.Fatalf("switch confirm status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "locked until all pending tasks") || !strings.Contains(body, "book.xlsx") {
		t.Fatalf("switch refusal lacks named task warning: %.600s", body)
	}
	store := ai.NewStore(app, testKey)
	connection, err := store.Connection(t.Context(), owner.Id)
	if err != nil || connection.Config.Model != "model-a" {
		t.Fatalf("refused switch changed the connection: %+v err=%v", connection, err)
	}

	// A same-configuration key repair stays available while the task is pending.
	if rec := validateAI(t, handler, cookie, "custom", fake.URL+"/v1", string(ai.FormatResponses), "model-a", "rotated-key"); rec.Code != http.StatusSeeOther {
		t.Fatalf("repair validate: %d", rec.Code)
	}
	repair := currentDraft(t, app, owner.Id)
	if rec := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {repair.Id}, "expected_configuration_generation": {"1"}, "confirmed": {"yes"}, "repair": {"yes"},
	}, withCookie(cookie)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/ai?notice=repaired" {
		t.Fatalf("repair confirm: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	repairStore := ai.NewStore(app, testKey)
	key, err := repairStore.Credential(t.Context(), owner.Id)
	if err != nil || key != "rotated-key" {
		t.Fatalf("repair key = %q err=%v", key, err)
	}
	if connection, err := repairStore.Connection(t.Context(), owner.Id); err != nil || connection.ConfigurationGeneration != 1 || connection.CredentialGeneration != 2 {
		t.Fatalf("repair generations = %+v err=%v", connection, err)
	}
}

func TestAccountSettingsAreOwnerOnly(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	otherID := provisionOperatorAs(t, app, "other@example.test", "other-horse-battery")
	fake := fakeAIEndpoint(t, ai.FormatResponses)
	cookie := login(t, handler)
	owner, err := app.FindAuthRecordByEmail("operators", testOperatorEmail)
	if err != nil {
		t.Fatal(err)
	}
	if rec := validateAI(t, handler, cookie, "custom", fake.URL+"/v1", string(ai.FormatResponses), "owner-model", "ai-secret-key"); rec.Code != http.StatusSeeOther {
		t.Fatalf("owner validate: %d", rec.Code)
	}
	draft := currentDraft(t, app, owner.Id)
	if rec := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {draft.Id}, "expected_configuration_generation": {"0"}, "confirmed": {"yes"},
	}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("owner confirm: %d", rec.Code)
	}

	otherCookie := loginAs(t, handler, "other@example.test", "other-horse-battery")
	page := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(otherCookie))
	body := page.Body.String()
	if strings.Contains(body, "owner-model") || strings.Contains(body, "ai-secret-key") {
		t.Fatalf("another operator saw private AI configuration: %.600s", body)
	}
	if !strings.Contains(body, "Not configured") {
		t.Fatalf("other operator should have no AI connection: %.400s", body)
	}
	store := ai.NewStore(app, testKey)
	if _, err := store.Connection(t.Context(), otherID); err == nil {
		t.Fatal("other operator read the owner's AI connection")
	}
}

func TestDisconnectIsRefusedWhileTaskIsPending(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Pending work")
	upload := uploadWorkbook(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email"},
		{"pending@example.test"},
	}))
	taskID := taskIDFromRedirect(t, upload, campaignID)
	owner, err := app.FindAuthRecordByEmail("operators", testOperatorEmail)
	if err != nil {
		t.Fatal(err)
	}
	installMailbox(t, app, owner.Id, "sender@example.test")

	rec := do(t, handler, http.MethodPost, "/connection/disconnect", url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusConflict {
		t.Fatalf("disconnect status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "locked until all pending tasks") || !strings.Contains(body, "book.xlsx") || !strings.Contains(body, taskID) {
		t.Fatalf("disconnect refusal lacks named blockers: %.600s", body)
	}
	current, err := mailbox.NewStore(app, testKey).Current(t.Context(), owner.Id)
	if err != nil || current.Status != mailbox.StatusConnected {
		t.Fatalf("pending task did not protect the mailbox: %+v err=%v", current, err)
	}
}
