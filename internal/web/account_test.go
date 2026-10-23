package web

// Account settings surface tests: the sections live at /account/mailbox and
// /account/ai with the old landing URLs retired to 404, AI setup validates
// the exact configuration with a simulated endpoint, activation is explicit,
// and connection switches are locked by pending tasks.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
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

	page := do(t, handler, http.MethodGet, "/account/mailbox", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("account page: %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{"Account settings", `href="/account/ai"`, "Mailbox connection", `href="/account/mailbox?provider=godaddy-titan"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("account page missing %q: %.400s", want, body)
		}
	}
	if strings.Contains(body, "Client Secret") {
		t.Fatal("mailbox credentials rendered before provider selection")
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

// accountSectionsNav extracts the Account sections tab nav so current-tab
// assertions stay scoped to the tabs, not the layout's main navigation.
func accountSectionsNav(body string) string {
	before, _, ok := strings.Cut(body, `aria-label="Account sections"`)
	if !ok {
		return ""
	}
	start := strings.LastIndex(before, "<nav")
	if start < 0 {
		return ""
	}
	section, _, ok := strings.Cut(body[start:], "</nav>")
	if !ok {
		return ""
	}
	return section
}

// tabIsCurrent reports whether the anchor with the given href inside the
// tab nav carries aria-current="page".
func tabIsCurrent(nav, href string) bool {
	start := strings.Index(nav, `href="`+href+`"`)
	if start < 0 {
		return false
	}
	end := strings.Index(nav[start:], ">")
	if end < 0 {
		return false
	}
	return strings.Contains(nav[start:start+end], `aria-current="page"`)
}

func assertCurrentAccountTab(t *testing.T, body, wantHref, otherHref string) {
	t.Helper()
	nav := accountSectionsNav(body)
	if nav == "" {
		t.Fatalf("account sections nav missing: %.400s", body)
	}
	if got := strings.Count(nav, `aria-current="page"`); got != 1 {
		t.Fatalf("account tabs mark %d current, want exactly 1: %.400s", got, nav)
	}
	if !tabIsCurrent(nav, wantHref) {
		t.Fatalf("tab %s is not current: %.400s", wantHref, nav)
	}
	if tabIsCurrent(nav, otherHref) {
		t.Fatalf("tab %s must not be current: %.400s", otherHref, nav)
	}
}

// TestUnusedAccountLandingURLsReturn404 pins the canonical section paths:
// /account/mailbox and /account/ai are the only settings pages, and the old
// landing URLs are gone without redirects.
func TestUnusedAccountLandingURLsReturn404(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)

	for _, target := range []string{
		"/account",
		"/account/connections",
		"/connection",
		"/account?provider=microsoft365",
		"/account?notice=validated",
		"/account?change_provider=1",
		"/connection?provider=godaddy-titan&notice=state",
	} {
		rec := do(t, handler, http.MethodGet, target, nil, withCookie(cookie))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: status=%d, want 404", target, rec.Code)
		}
		if location := rec.Header().Get("Location"); location != "" {
			t.Fatalf("GET %s: Location=%q, want no redirect", target, location)
		}
		body := rec.Body.String()
		if strings.Contains(body, "Mailbox connection") || strings.Contains(body, "Account settings") {
			t.Fatalf("GET %s rendered a settings page: %.400s", target, body)
		}
	}

	// The canonical mailbox page is stable across consecutive requests.
	for range 2 {
		page := do(t, handler, http.MethodGet, "/account/mailbox", nil, withCookie(cookie))
		if page.Code != http.StatusOK {
			t.Fatalf("mailbox page: %d", page.Code)
		}
		assertCurrentAccountTab(t, page.Body.String(), "/account/mailbox", "/account/ai")
	}

	aiPage := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	if aiPage.Code != http.StatusOK {
		t.Fatalf("AI page: %d", aiPage.Code)
	}
	assertCurrentAccountTab(t, aiPage.Body.String(), "/account/ai", "/account/mailbox")

	anonymous := do(t, handler, http.MethodGet, "/account/mailbox", nil, nil)
	if anonymous.Code != http.StatusSeeOther || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous mailbox page: status=%d location=%q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	// An unknown provider renders the chooser, never a credential form.
	bogus := do(t, handler, http.MethodGet, "/account/mailbox?provider=bogus", nil, withCookie(cookie))
	if bogus.Code != http.StatusOK {
		t.Fatalf("bogus provider: status=%d", bogus.Code)
	}
	bogusBody := bogus.Body.String()
	if strings.Contains(bogusBody, `action="/connection/titan"`) || strings.Contains(bogusBody, `action="/connection/microsoft/settings"`) {
		t.Fatalf("bogus provider rendered a credential form: %.400s", bogusBody)
	}
	if !strings.Contains(bogusBody, `href="/account/mailbox?provider=godaddy-titan"`) {
		t.Fatalf("bogus provider page lost the Titan chooser: %.400s", bogusBody)
	}

	// An unknown notice code is ignored, never echoed.
	notices := do(t, handler, http.MethodGet, "/account/mailbox?notice=untrusted-text", nil, withCookie(cookie))
	if notices.Code != http.StatusOK {
		t.Fatalf("untrusted notice: status=%d", notices.Code)
	}
	if strings.Contains(notices.Body.String(), "untrusted-text") {
		t.Fatalf("notice parameter echoed back: %.400s", notices.Body.String())
	}

	// None of these landing requests created a mailbox connection.
	if count, err := app.CountRecords(mailbox.ConnectionsCollection); err != nil || count != 0 {
		t.Fatalf("landing requests created %d connection records (err=%v)", count, err)
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
	locked := do(t, handler, http.MethodGet, "/account/mailbox", nil, withCookie(cookie))
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

// fakeModelListEndpoint serves the documented OpenAI-style model list for
// discovery tests.
func fakeModelListEndpoint(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected model-list path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"data":[{"id":"mimo-v2.6-flash"},{"id":"mystery-model"}]}`)); err != nil {
			t.Errorf("write simulated model list: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// inputTag extracts one element by id so a control's own attributes can be
// asserted without parsing the whole page.
func inputTag(body, id string) string {
	start := strings.Index(body, `id="`+id+`"`)
	if start < 0 {
		return ""
	}
	open := strings.LastIndex(body[:start], "<")
	end := strings.Index(body[start:], ">")
	if open < 0 || end < 0 {
		return ""
	}
	return body[open : start+end+1]
}

func TestAIPresetSetupPage(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)

	page := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("AI page: %d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		`x-data="aiSetup($el)"`,
		`data-endpoint="/account/ai/models"`,
		"Restore default",
		"Fetch models",
		`name="model"`,
		"data-models=",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("preset setup missing %q: %.400s", want, body)
		}
	}
	if strings.Contains(body, `name="api_format"`) || strings.Contains(body, "ai-api-format") {
		t.Fatal("preset setup must not offer a manual API format control")
	}
	root := inputTag(body, "ai-api-root")
	if root == "" || strings.Contains(root, "readonly") {
		t.Fatalf("preset API root must be editable, got %q", root)
	}
	if !strings.Contains(root, ai.OpenCodeRoot) {
		t.Fatalf("preset API root lacks the documented default: %q", root)
	}
	// The verified catalog is server-rendered as the fallback list, each
	// option labeled with its single verified API format.
	if !strings.Contains(body, "deepseek-v4.1-flash · Chat Completions API") || !strings.Contains(body, "grok-4.7 · Responses API") {
		t.Fatalf("preset model options lack verified format labels: %.600s", body)
	}
}

func TestAIModelDiscovery(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)
	fake := fakeModelListEndpoint(t)

	rec := do(t, handler, http.MethodPost, "/account/ai/models", url.Values{
		"provider": {"xiaomi-mimo"},
		"api_root": {fake.URL},
		"api_key":  {"ai-secret-key"},
	}, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("discovery Cache-Control = %q", got)
	}
	if strings.Contains(rec.Body.String(), "ai-secret-key") {
		t.Fatal("discovery response leaked the API key")
	}
	var payload views.AIModelDiscovery
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("discovery JSON: %v body=%.400s", err, rec.Body.String())
	}
	if payload.Error != "" || len(payload.Models) != 2 {
		t.Fatalf("discovery payload = %+v", payload)
	}
	supported := payload.Models[0]
	if supported.Value != "mimo-v2.6-flash" || !supported.Supported || supported.Format != string(ai.FormatChatCompletions) || supported.FormatLabel == "" {
		t.Fatalf("supported option = %+v", supported)
	}
	unsupported := payload.Models[1]
	if unsupported.Value != "mystery-model" || unsupported.Supported || unsupported.Reason == "" {
		t.Fatalf("unsupported option = %+v", unsupported)
	}
	// Discovery is read-only: no draft and no confirmed connection.
	if count, err := app.CountRecords(ai.DraftsCollection); err != nil || count != 0 {
		t.Fatalf("discovery staged %d drafts (err=%v)", count, err)
	}
	if count, err := app.CountRecords(ai.ConnectionsCollection); err != nil || count != 0 {
		t.Fatalf("discovery created %d connections (err=%v)", count, err)
	}
}

func TestAIModelDiscoveryRejectsInvalidRequests(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)

	// Custom endpoints are configured manually; discovery refuses them.
	rec := do(t, handler, http.MethodPost, "/account/ai/models", url.Values{
		"provider": {"custom"},
		"api_root": {"https://example.test/v1"},
		"api_key":  {"ai-secret-key"},
	}, withCookie(cookie))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("custom discovery: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	var payload views.AIModelDiscovery
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("custom discovery JSON: %v", err)
	}
	if payload.Error == "" || len(payload.Models) != 0 {
		t.Fatalf("custom discovery payload = %+v", payload)
	}
	if strings.Contains(rec.Body.String(), "ai-secret-key") {
		t.Fatal("custom discovery response leaked the API key")
	}

	// An unsafe request without a session is refused before any handler.
	anonymous := do(t, handler, http.MethodPost, "/account/ai/models", url.Values{
		"provider": {"xiaomi-mimo"},
	}, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous discovery: status=%d body=%.200s", anonymous.Code, anonymous.Body.String())
	}
}

func TestAIPresetValidationResolvesFormatWithoutDiscovery(t *testing.T) {
	app, handler := newTestServer(t, testConfig("http://127.0.0.1:8090"))
	provisionOperator(t, app)
	cookie := login(t, handler)
	fake := fakeAIEndpoint(t, ai.FormatChatCompletions)
	root := fake.URL + "/v1"
	owner, err := app.FindAuthRecordByEmail("operators", testOperatorEmail)
	if err != nil {
		t.Fatal(err)
	}

	// No api_format field: the preset resolves the format from the model.
	rec := do(t, handler, http.MethodPost, "/account/ai/validate", url.Values{
		"provider": {"xiaomi-mimo"},
		"api_root": {root},
		"model":    {"mimo-v2.6-flash"},
		"api_key":  {"ai-secret-key"},
	}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/ai?notice=validated" {
		t.Fatalf("preset validate: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	draft := currentDraft(t, app, owner.Id)
	// Before confirmation, the page reopens the validated proposal's
	// non-secret selection, so guidance and controls match what was validated.
	draftPage := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	for _, want := range []string{`data-provider="xiaomi-mimo"`, `value="mimo-v2.6-flash" selected`} {
		if !strings.Contains(draftPage.Body.String(), want) {
			t.Fatalf("validated proposal page missing %q: %.600s", want, draftPage.Body.String())
		}
	}
	if confirm := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {draft.Id}, "expected_configuration_generation": {"0"}, "confirmed": {"yes"},
	}, withCookie(cookie)); confirm.Code != http.StatusSeeOther {
		t.Fatalf("preset confirm: status=%d body=%.400s", confirm.Code, confirm.Body.String())
	}
	store := ai.NewStore(app, testKey)
	connection, err := store.Connection(t.Context(), owner.Id)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Config.APIFormat != ai.FormatChatCompletions || connection.Config.APIRoot != root || connection.Config.Model != "mimo-v2.6-flash" {
		t.Fatalf("confirmed preset connection = %+v", connection.Config)
	}

	// The page reopens the saved non-secret configuration: selected provider,
	// root, model and inferred format, with no discovery round trip.
	page := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	body := page.Body.String()
	for _, want := range []string{
		`data-provider="xiaomi-mimo"`,
		`value="mimo-v2.6-flash" selected`,
		`id="ai-inferred-format"`,
		"mimo-v2.6-flash · Chat Completions API",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("reopened page missing %q: %.600s", want, body)
		}
	}
	if !strings.Contains(inputTag(body, "ai-api-root"), root) {
		t.Fatalf("reopened page does not show the saved root %q: %.400s", root, body)
	}

	// A same-configuration key repair still works without discovery.
	if rec := do(t, handler, http.MethodPost, "/account/ai/validate", url.Values{
		"provider": {"xiaomi-mimo"},
		"api_root": {root},
		"model":    {"mimo-v2.6-flash"},
		"api_key":  {"rotated-key"},
	}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("repair validate: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	repair := currentDraft(t, app, owner.Id)
	repairPage := do(t, handler, http.MethodGet, "/account/ai", nil, withCookie(cookie))
	if !strings.Contains(repairPage.Body.String(), "Replace API key") || strings.Contains(repairPage.Body.String(), "rotated-key") {
		t.Fatalf("repair proposal page wrong: %.600s", repairPage.Body.String())
	}
	if rec := do(t, handler, http.MethodPost, "/account/ai/confirm", url.Values{
		"draft": {repair.Id}, "expected_configuration_generation": {"1"}, "confirmed": {"yes"}, "repair": {"yes"},
	}, withCookie(cookie)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/ai?notice=repaired" {
		t.Fatalf("repair confirm: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
}
