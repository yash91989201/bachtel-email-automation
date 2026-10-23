package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/ui"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

// Acceptance checks for change add-operator-and-microsoft-auth: they drive the
// real HTTP routes, a real PocketBase app on temporary data, and simulated
// Microsoft authorization/Graph servers.
const (
	testOrigin   = "http://127.0.0.1:8090"
	testEmail    = "operator@bachtelcorporation.com"
	testPassword = "correct-horse-battery"
	// testSender is the mailbox address the simulated Graph reports for the
	// signed-in account; each operator owns it exclusively.
	testSender = "operator@bachtelcorporation.com"
	// Each operator saves their own registration; these stand in for one
	// Entra application.
	testTenantID  = "11111111-1111-1111-1111-111111111111"
	testClientID  = "22222222-2222-2222-2222-222222222222"
	testSecret    = "client-secret-value"
	otherTenantID = "33333333-3333-3333-3333-333333333333"
	otherClientID = "44444444-4444-4444-4444-444444444444"
)

// tokenRegistration records which operator registration a token request used.
type tokenRegistration struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	Grant        string
}

type acceptanceEnv struct {
	t         *testing.T
	dataDir   string
	app       *pocketbase.PocketBase
	handler   http.Handler
	cfg       config.Config
	mailboxes *mailbox.Store
	authority *httptest.Server
	graph     *httptest.Server

	mu            sync.Mutex
	tokenRequests []url.Values
	tokenMeta     []tokenRegistration
}

func newAcceptanceEnv(t *testing.T) *acceptanceEnv {
	t.Helper()
	env := &acceptanceEnv{t: t, dataDir: t.TempDir()}

	t.Setenv(config.EnvOrigin, testOrigin)
	t.Setenv(config.EnvEncryptionKey, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", config.EncryptionKeySize))))
	t.Setenv(config.EnvAuthorityURL, "http://authority.invalid")

	// Simulated Microsoft authorization server: token exchange only.
	env.authority = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("token request parse: %v", err)
		}
		env.mu.Lock()
		env.tokenRequests = append(env.tokenRequests, r.PostForm)
		// oauth2 sends the client credentials with HTTP Basic auth by default.
		clientID, clientSecret, _ := r.BasicAuth()
		if clientID == "" {
			clientID, clientSecret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		env.tokenMeta = append(env.tokenMeta, tokenRegistration{
			Tenant:       strings.Split(strings.Trim(r.URL.Path, "/"), "/")[0],
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Grant:        r.PostForm.Get("grant_type"),
		})
		env.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// The access token carries the authorization code so the Graph mock
		// can resolve each operator's own mailbox. Real Microsoft token
		// envelopes can exceed PocketBase's default text limit once encrypted
		// and base64 encoded. Keep this fixture large so all connection
		// acceptance tests exercise the production schema limit.
		if _, err := fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600,"refresh_token":%q,"scope":"offline_access"}`, "access-token-"+r.PostForm.Get("code"), "refresh-token-1"+strings.Repeat("r", 8192)); err != nil {
			t.Errorf("write mock response: %v", err)
		}
	}))
	t.Cleanup(env.authority.Close)
	t.Setenv(config.EnvAuthorityURL, env.authority.URL)

	// Simulated Microsoft Graph: the verified mailbox identity. Each access
	// token maps to one mailbox address so every operator connects their own.
	env.graph = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me" {
			t.Errorf("unexpected graph path %q", r.URL.Path)
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mailboxAddress := graphMailbox(token)
		if mailboxAddress == "" {
			t.Errorf("unexpected graph authorization header %q", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// The account id follows the authorization code, so two simulated
		// tenants hold genuinely different identities while a repeated code
		// represents the same account.
		id := "account-" + strings.TrimPrefix(token, "access-token-")
		if _, err := fmt.Fprintf(w, `{"id":%q,"mail":%q,"userPrincipalName":%q}`, id, mailboxAddress, mailboxAddress); err != nil {
			t.Errorf("write mock response: %v", err)
		}
	}))
	t.Cleanup(env.graph.Close)
	t.Setenv(config.EnvGraphBaseURL, env.graph.URL+"/v1.0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	env.cfg = cfg
	env.start(t, true)
	return env
}

// graphMailbox maps a simulated access token to the mailbox address its owner
// signed in with. Tokens minted for an authorization code carrying an "-alias"
// suffix resolve to that alias, so one test can drive two operators with
// different mailboxes.
func graphMailbox(token string) string {
	code, ok := strings.CutPrefix(token, "access-token-")
	if !ok {
		return ""
	}
	if alias, ok := strings.CutSuffix(code, "-alias"); ok {
		return "alias-" + alias + "@bachtelcorporation.com"
	}
	return testSender
}

// start bootstraps the app (and optionally the operator) on the current data dir.
func (env *acceptanceEnv) start(t *testing.T, provision bool) {
	t.Helper()
	var app *pocketbase.PocketBase
	if provision {
		app = testutil.NewApp(t)
		env.dataDir = app.DataDir()
	} else {
		app = pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: env.dataDir})
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
	}
	if provision {
		if err := operator.Provision(app, testEmail, testPassword); err != nil {
			t.Fatal(err)
		}
	}
	mailboxes := mailbox.NewStore(app, env.cfg.EncryptionKey)
	env.mailboxes = mailboxes
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/_/{path...}", apis.Static(ui.DistDirFS, false))
	router.Any("/{path...}", apis.WrapStdHandler(NewHandler(app, env.cfg, Services{
		Connections: microsoft.NewService(app, env.cfg, mailboxes),
		Mailboxes:   mailboxes,
		Sending:     sending.NewStore(app, sending.StoreOptions{}),
		Exports:     exports.NewService(app, env.cfg),
		Resets:      reset.NewService(app, mailboxes),
	})))
	env.handler, err = router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	env.app = app
}

func (env *acceptanceEnv) request(method, target string, cookies []*http.Cookie, body url.Values) *httptest.ResponseRecorder {
	env.t.Helper()
	var reader *strings.Reader
	if body != nil {
		reader = strings.NewReader(body.Encode())
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	req.Host = strings.TrimPrefix(testOrigin, "http://")
	// Browser-equivalent proof that the request is same-origin.
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	return rec
}

// saveMicrosoftSettings stages one operator's own Entra registration exactly
// as the connection form does.
func (env *acceptanceEnv) saveMicrosoftSettings(session *http.Cookie, tenantID, clientID, secret string) {
	env.t.Helper()
	rec := env.request(http.MethodPost, "/connection/microsoft/settings", []*http.Cookie{session}, url.Values{
		"tenant_id":     {tenantID},
		"client_id":     {clientID},
		"client_secret": {secret},
	})
	if rec.Code != http.StatusSeeOther {
		env.t.Fatalf("save provider settings: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
}

func (env *acceptanceEnv) signIn() *http.Cookie {
	env.t.Helper()
	rec := env.request(http.MethodPost, "/login", nil, url.Values{"email": {testEmail}, "password": {testPassword}})
	if rec.Code != http.StatusSeeOther {
		env.t.Fatalf("login: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "operator_session" && cookie.Value != "" {
			return cookie
		}
	}
	env.t.Fatal("login did not set the operator session cookie")
	return nil
}

func TestAcceptanceFullMicrosoftConnection(t *testing.T) {
	env := newAcceptanceEnv(t)

	// Anonymous access to private surfaces is refused.
	for _, target := range []string{"/", "/status", "/account/mailbox"} {
		if rec := env.request(http.MethodGet, target, nil, nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("anonymous %s: status=%d location=%q", target, rec.Code, rec.Header().Get("Location"))
		}
	}
	// The public probe stays reachable and minimal.
	rec := env.request(http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("health: status=%d body=%q", rec.Code, rec.Body.String())
	}

	session := env.signIn()
	if rec := env.request(http.MethodGet, "/account/mailbox", []*http.Cookie{session}, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "1. Choose your provider") {
		t.Fatalf("connection page before connect: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	// The operator saves their own Entra registration, then starts
	// authorization and follows the callback.
	env.saveMicrosoftSettings(session, testTenantID, testClientID, testSecret)
	rec = env.request(http.MethodPost, "/connection/microsoft/connect", []*http.Cookie{session}, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("connect: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	authorizeURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || authorizeURL.Host == "" {
		t.Fatalf("connect location %q", rec.Header().Get("Location"))
	}
	state := authorizeURL.Query().Get("state")
	if state == "" || authorizeURL.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL missing PKCE state: %q", authorizeURL.String())
	}
	if !strings.Contains(authorizeURL.Path, "/"+testTenantID+"/") {
		t.Fatalf("authorization URL is not scoped to the saved tenant: %q", authorizeURL.Path)
	}
	if got := authorizeURL.Query().Get("redirect_uri"); got != env.cfg.CallbackURL() {
		t.Fatalf("redirect_uri = %q, want the origin-derived %q", got, env.cfg.CallbackURL())
	}

	rec = env.request(http.MethodGet, "/connection/callback?state="+url.QueryEscape(state)+"&code=authorization-code", []*http.Cookie{session}, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/mailbox" {
		t.Fatalf("callback: status=%d location=%q body=%.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "code=") || strings.Contains(loc, "state=") {
		t.Fatalf("callback redirect must not echo provider parameters: %q", loc)
	}

	env.mu.Lock()
	exchanges := len(env.tokenRequests)
	var verifier string
	if exchanges > 0 {
		verifier = env.tokenRequests[0].Get("code_verifier")
	}
	env.mu.Unlock()
	if exchanges != 1 || verifier == "" {
		t.Fatalf("expected one PKCE token exchange with a verifier, got %d exchanges (verifier set: %v)", exchanges, verifier != "")
	}

	page := env.request(http.MethodGet, "/account/mailbox", []*http.Cookie{session}, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("connection page after connect: status=%d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, testSender) || !strings.Contains(body, "connected") {
		t.Fatalf("connection page missing connected identity: %.600s", body)
	}
	for _, secret := range []string{"access-token-", "refresh-token-", testSecret, "v1."} {
		if strings.Contains(body, secret) {
			t.Fatalf("connection page leaked %q", secret)
		}
	}

	// A replayed callback must not install credentials again.
	if rec := env.request(http.MethodGet, "/connection/callback?state="+url.QueryEscape(state)+"&code=authorization-code", []*http.Cookie{session}, nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/mailbox?provider=microsoft365&notice=state" {
		t.Fatalf("replayed callback: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	env.mu.Lock()
	exchanges = len(env.tokenRequests)
	env.mu.Unlock()
	if exchanges != 1 {
		t.Fatalf("replayed callback performed a token exchange: %d exchanges", exchanges)
	}

	// The operator session ends: the cookie is cleared and private views require sign-in again.
	logout := env.request(http.MethodPost, "/logout", []*http.Cookie{session}, url.Values{})
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("logout: status=%d", logout.Code)
	}
	cleared := false
	for _, cookie := range logout.Result().Cookies() {
		if cookie.Name == "operator_session" && (cookie.MaxAge < 0 || cookie.Value == "") {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear the session cookie")
	}
	if rec := env.request(http.MethodGet, "/account/mailbox", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("private view without a session: status=%d", rec.Code)
	}
}

func TestAcceptanceRestartDuringMicrosoftSignIn(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()
	cookies := []*http.Cookie{session}

	env.saveMicrosoftSettings(session, testTenantID, testClientID, testSecret)
	start := env.request(http.MethodPost, "/connection/microsoft/connect", cookies, url.Values{})
	oldURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil || start.Code != http.StatusSeeOther || oldURL.Query().Get("state") == "" {
		t.Fatalf("start sign-in: status=%d location=%q", start.Code, start.Header().Get("Location"))
	}
	if err := env.app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	env.start(t, false)

	callback := "/connection/callback?state=" + url.QueryEscape(oldURL.Query().Get("state")) + "&code=old-code"
	stale := env.request(http.MethodGet, callback, cookies, nil)
	if stale.Code != http.StatusSeeOther || stale.Header().Get("Location") != "/account/mailbox?provider=microsoft365&notice=state" {
		t.Fatalf("stale callback: status=%d location=%q", stale.Code, stale.Header().Get("Location"))
	}
	if stale.Header().Get("Cache-Control") != "no-store" || stale.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback response permits caching or referrer disclosure")
	}
	// Following and refreshing the error page must not repeat the callback.
	for range 2 {
		page := env.request(http.MethodGet, stale.Header().Get("Location"), cookies, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "cleared by a server restart") || !strings.Contains(page.Body.String(), `action="/connection/microsoft/connect"`) {
			t.Fatalf("retry page: status=%d body=%.400s", page.Code, page.Body.String())
		}
	}
	env.mu.Lock()
	exchanges := len(env.tokenRequests)
	env.mu.Unlock()
	if exchanges != 0 {
		t.Fatalf("stale callback reached token endpoint: %d exchanges", exchanges)
	}
	if records, err := env.app.FindAllRecords(mailbox.ConnectionsCollection); err != nil || len(records) != 0 {
		t.Fatalf("stale callback installed credentials: %d records (err=%v)", len(records), err)
	}

	// Connect creates a fresh state that can complete on the restarted server.
	retry := env.request(http.MethodPost, "/connection/microsoft/connect", cookies, url.Values{})
	newURL, err := url.Parse(retry.Header().Get("Location"))
	if err != nil || retry.Code != http.StatusSeeOther || newURL.Query().Get("state") == "" || newURL.Query().Get("state") == oldURL.Query().Get("state") {
		t.Fatal("retry did not create a fresh sign-in attempt")
	}
	callback = "/connection/callback?state=" + url.QueryEscape(newURL.Query().Get("state")) + "&code=new-code"
	done := env.request(http.MethodGet, callback, cookies, nil)
	if done.Code != http.StatusSeeOther || done.Header().Get("Location") != "/account/mailbox" {
		t.Fatalf("retry callback: status=%d location=%q", done.Code, done.Header().Get("Location"))
	}
	page := env.request(http.MethodGet, "/account/mailbox", cookies, nil)
	if !strings.Contains(page.Body.String(), `data-status="connected"`) {
		t.Fatal("fresh attempt did not connect the mailbox")
	}
}

func TestAcceptanceCallbackFailureRedirects(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()
	for _, tc := range []struct {
		query, notice string
	}{
		{"", "incomplete"},
		{"?state=unknown&error=access_denied&error_description=provider-secret", "incomplete"},
		{"?state=unknown&code=private-code", "state"},
	} {
		rec := env.request(http.MethodGet, "/connection/callback"+tc.query, []*http.Cookie{session}, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/mailbox?provider=microsoft365&notice="+tc.notice {
			t.Fatalf("callback failure: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
		for _, secret := range []string{"provider-secret", "private-code", "unknown"} {
			if strings.Contains(rec.Body.String()+rec.Header().Get("Location"), secret) {
				t.Fatalf("callback redirect forwarded %q", secret)
			}
		}
	}
	page := env.request(http.MethodGet, "/account/mailbox?notice=untrusted-text", []*http.Cookie{session}, nil)
	if strings.Contains(page.Body.String(), "untrusted-text") {
		t.Fatal("arbitrary notice text was rendered")
	}
}

func TestAcceptanceRestartKeepsConnection(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()

	env.saveMicrosoftSettings(session, testTenantID, testClientID, testSecret)
	rec := env.request(http.MethodPost, "/connection/microsoft/connect", []*http.Cookie{session}, url.Values{})
	authorizeURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorizeURL.Query().Get("state")
	if rec := env.request(http.MethodGet, "/connection/callback?state="+url.QueryEscape(state)+"&code=code", []*http.Cookie{session}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("callback: status=%d body=%.300s", rec.Code, rec.Body.String())
	}

	// Simulate a restart: drop the app and bootstrap a new one on the same data dir.
	if err := env.app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	env.start(t, false)

	records, err := env.app.FindAllRecords(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly one connection record after restart, got %d", len(records))
	}
	if got := records[0].GetString("sender"); got != testSender {
		t.Fatalf("connection sender after restart = %q", got)
	}
	if got := records[0].GetString("credential"); !strings.HasPrefix(got, "v1.") {
		t.Fatalf("stored credential is not sealed versioned ciphertext: %q", got)
	}

	session = env.signIn()
	page := env.request(http.MethodGet, "/account/mailbox", []*http.Cookie{session}, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), testSender) || !strings.Contains(page.Body.String(), `data-status="connected"`) {
		t.Fatalf("connection page after restart: status=%d body=%.400s", page.Code, page.Body.String())
	}
	connection, err := env.mailboxes.Current(context.Background(), operatorIDFor(t, env.app, testEmail))
	if err != nil {
		t.Fatalf("load connection after restart: %v", err)
	}
	service := microsoft.NewService(env.app, env.cfg, env.mailboxes)
	if token, err := service.AccessToken(context.Background(), connection); err != nil || token != "access-token-code" {
		t.Fatalf("large stored credential cannot be used after restart (err=%v)", err)
	}
}

func TestAcceptancePrivateRecordBoundariesAndNoSendEndpoint(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()

	// Private collections stay closed through the public PocketBase API, even for
	// an authenticated operator.
	for _, target := range []string{
		"/api/collections/operators/records",
		"/api/collections/" + mailbox.ConnectionsCollection + "/records",
		"/api/collections/" + mailbox.ResetsCollection + "/records",
		"/api/collections/" + mailbox.SettingsCollection + "/records",
	} {
		if rec := env.request(http.MethodGet, target, nil, nil); rec.Code < 400 {
			t.Fatalf("anonymous %s: status=%d body=%.200s", target, rec.Code, rec.Body.String())
		}
		if rec := env.request(http.MethodGet, target, []*http.Cookie{session}, nil); rec.Code < 400 {
			t.Fatalf("operator %s: status=%d body=%.200s", target, rec.Code, rec.Body.String())
		}
	}

	// Public registration and the public password endpoint stay closed.
	publicCreate := env.request(http.MethodPost, "/api/collections/operators/records", nil, url.Values{
		"email": {"second@bachtelcorporation.com"}, "password": {"another-password"}, "passwordConfirm": {"another-password"},
	})
	if publicCreate.Code < 400 {
		t.Fatalf("public operator creation: status=%d body=%.200s", publicCreate.Code, publicCreate.Body.String())
	}
	publicAuth := env.request(http.MethodPost, "/api/collections/operators/auth-with-password", nil, url.Values{
		"identity": {testEmail}, "password": {testPassword},
	})
	if publicAuth.Code < 400 {
		t.Fatalf("public password endpoint: status=%d body=%.200s", publicAuth.Code, publicAuth.Body.String())
	}
	if total, err := env.app.CountRecords(operator.CollectionName); err != nil || total != 1 {
		t.Fatalf("operator count = %d (err=%v)", total, err)
	}

	// No candidate-send surface exists yet.
	for _, target := range []string{"/send", "/candidates/send", "/campaigns/send", "/api/send"} {
		if rec := env.request(http.MethodPost, target, []*http.Cookie{session}, url.Values{}); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status=%d, want 404", target, rec.Code)
		}
	}
}

// TestAcceptanceAnonymousMutationsRejected guards the CSRF and authorization
// boundary on every state-changing route.
func TestAcceptanceAnonymousMutationsRejected(t *testing.T) {
	env := newAcceptanceEnv(t)

	for _, target := range []string{"/logout", "/connection/microsoft/settings", "/connection/microsoft/connect", "/connection/disconnect", "/connection/titan", "/connection/replace/confirm"} {
		if rec := env.request(http.MethodPost, target, nil, url.Values{}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: status=%d body=%.200s", target, rec.Code, rec.Body.String())
		}
	}

	// Cross-origin mutations are refused before authentication is even considered.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"email": {testEmail}, "password": {testPassword}}.Encode()))
	req.Host = strings.TrimPrefix(testOrigin, "http://")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login: status=%d", rec.Code)
	}
	if records, err := env.app.FindAllRecords(mailbox.ConnectionsCollection); err != nil || len(records) != 0 {
		t.Fatalf("cross-origin request changed state: %d records (err=%v)", len(records), err)
	}
}

// TestAcceptanceConnectionRecordStoredEncrypted asserts the persisted form of
// the credential: versioned ciphertext, never the raw token.
func TestAcceptanceConnectionRecordStoredEncrypted(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()
	env.saveMicrosoftSettings(session, testTenantID, testClientID, testSecret)
	rec := env.request(http.MethodPost, "/connection/microsoft/connect", []*http.Cookie{session}, url.Values{})
	authorizeURL, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorizeURL.Query().Get("state")
	if rec := env.request(http.MethodGet, "/connection/callback?state="+url.QueryEscape(state)+"&code=code", []*http.Cookie{session}, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("callback: status=%d body=%.300s", rec.Code, rec.Body.String())
	}

	records, err := env.app.FindAllRecords(mailbox.ConnectionsCollection)
	if err != nil || len(records) != 1 {
		t.Fatalf("connection records: %d (err=%v)", len(records), err)
	}
	stored := records[0].GetString("credential")
	if len(stored) <= 5000 {
		t.Fatal("fixture must exceed the old 5000-character credential limit")
	}
	if !strings.HasPrefix(stored, "v1.") {
		t.Fatalf("credential not versioned: %q", stored)
	}
	for _, raw := range []string{"access-token-code", "refresh-token-1", testSecret} {
		if strings.Contains(stored, raw) {
			t.Fatalf("stored credential contains raw value %q", raw)
		}
	}

	serialized, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), "refresh-token-1") {
		t.Fatal("record serialization leaked the token")
	}
}
