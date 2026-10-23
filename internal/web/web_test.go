package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/ui"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

const (
	testOperatorEmail    = "operator@example.com"
	testOperatorPassword = "correct-horse-battery-staple"
)

// testKey is the fixed deployment key every test store seals credentials
// with; it never comes from the environment.
func testKey() ([]byte, error) { return bytes.Repeat([]byte("k"), config.EncryptionKeySize), nil }

// testServices assembles the same process-owned dependencies the running
// application binds, without a live dispatcher or export worker.
func testServices(t *testing.T, app core.App, cfg config.Config, dispatcher Dispatcher) Services {
	t.Helper()
	mailboxes := mailbox.NewStore(app, testKey)
	aiStore := ai.NewStore(app, testKey)
	// Simulated endpoints run on loopback plaintext; only tests enable this.
	aiStore.SetValidationOptions(ai.ValidationOptions{Timeout: 5 * time.Second, MaxResponseBytes: 512 << 10, AllowPrivate: true})
	campaigns := campaign.NewService(app, cfg.PandocPath)
	tasks := intake.NewService(app, campaigns, cfg.Intake)
	prepared := preparation.NewService(app, tasks, campaigns, aiStore)
	prepared.SetOptions(preparation.Options{BatchSize: 20, MaxTokens: 4096})
	prepared.SetSettled(func(taskID string) {
		if err := sending.NewStore(app, sending.StoreOptions{}).SettleClosure(context.Background(), taskID); err != nil {
			t.Logf("settle closure after preparation: %v", err)
		}
	})
	return Services{
		Connections:  microsoft.NewService(app, cfg, mailboxes),
		Mailboxes:    mailboxes,
		Sending:      sending.NewStore(app, sending.StoreOptions{}),
		Dispatcher:   dispatcher,
		Exports:      exports.NewService(app, cfg),
		Resets:       reset.NewService(app, mailboxes),
		AI:           aiStore,
		Preparation:  prepared,
		Finalization: finalization.NewService(app, tasks, campaigns, prepared),
	}
}

// newTestServer boots a temp-data PocketBase app with the operator UI mounted
// the same way the running application mounts it.
func newTestServer(t *testing.T, cfg config.Config) (*pocketbase.PocketBase, http.Handler) {
	t.Helper()
	app, handler, _ := newTestServerWithServices(t, cfg)
	return app, handler
}

// newTestServerWithServices boots the same server and returns its process-owned
// services so a test can drive a worker exactly as the application does.
func newTestServerWithServices(t *testing.T, cfg config.Config) (*pocketbase.PocketBase, http.Handler, Services) {
	t.Helper()
	app := testutil.NewApp(t)
	services := testServices(t, app, cfg, nil)
	return app, wrapWithPocketBase(t, app, NewHandler(app, cfg, services)), services
}

// wrapWithPocketBase mounts the UI handler behind the PocketBase router, as
// apis.Serve does in the running application.
func wrapWithPocketBase(t *testing.T, app core.App, handler http.Handler) http.Handler {
	t.Helper()
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	router.GET("/_/{path...}", apis.Static(ui.DistDirFS, false))
	router.Any("/{path...}", apis.WrapStdHandler(handler))
	built, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func testConfig(origin string) config.Config {
	return config.Config{
		Origin: origin,
		Microsoft: config.Microsoft{
			AuthorityURL: config.DefaultAuthorityURL,
			GraphBaseURL: config.DefaultGraphBaseURL,
		},
		Titan: config.Titan{Address: config.DefaultTitanSMTPAddress},
	}
}

// do runs one request against the handler and returns the recorder.
func do(t *testing.T, handler http.Handler, method, target string, form url.Values, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func loginForm(email, password string) url.Values {
	return url.Values{"email": {email}, "password": {password}}
}

// login performs a successful sign-in and returns the session cookie.
func login(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, testOperatorPassword), nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: status=%d body=%.200s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("login response did not set a session cookie")
	return nil
}

func withCookie(cookie *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(cookie) }
}

func provisionOperator(t *testing.T, app core.App) {
	t.Helper()
	if err := operator.Provision(app, testOperatorEmail, testOperatorPassword); err != nil {
		t.Fatal(err)
	}
}

// provisionOperatorAs creates an additional operator account and returns its
// record id.
func provisionOperatorAs(t *testing.T, app core.App, email, password string) string {
	t.Helper()
	if err := operator.Provision(app, email, password); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// operatorID resolves the primary test operator's record id.
func operatorID(t *testing.T, app core.App) string {
	t.Helper()
	return operatorIDFor(t, app, testOperatorEmail)
}

// operatorIDFor resolves any provisioned operator's record id.
func operatorIDFor(t *testing.T, app core.App, email string) string {
	t.Helper()
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// loginAs performs a successful sign-in for any provisioned operator.
func loginAs(t *testing.T, handler http.Handler, email, password string) *http.Cookie {
	t.Helper()
	rec := do(t, handler, http.MethodPost, "/login", loginForm(email, password), nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login %s: status=%d body=%.200s", email, rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("login %s did not set a session cookie", email)
	return nil
}

// installMailbox claims an exclusive Titan mailbox for the owner through the
// real store, so sealing, claiming and status behave exactly as in production.
func installMailbox(t *testing.T, app core.App, ownerID, sender string) *mailbox.Connection {
	t.Helper()
	connection, err := mailbox.NewStore(app, testKey).Install(context.Background(), mailbox.Candidate{
		OwnerID:    ownerID,
		Provider:   mailbox.ProviderTitan,
		Sender:     mailbox.CanonicalSender(sender),
		Identity:   mailbox.CanonicalSender(sender),
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "mailbox-password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestPublicSurface(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	_ = app

	rec := do(t, handler, http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("/health: status=%d type=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	rec = do(t, handler, http.MethodGet, "/robots.txt", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("/robots.txt: status=%d body=%q", rec.Code, rec.Body.String())
	}

	for _, path := range []string{"/static/app.css", "/static/alpine.min.js", "/static/htmx.min.js", "/static/workspace.js"} {
		rec = do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Fatalf("%s: status=%d", path, rec.Code)
		}
		// The adapter ships through the asset pipeline and is embedded, so a
		// production build serves the same file the workspace markup expects.
		if path == "/static/workspace.js" && !strings.Contains(rec.Body.String(), "data-workspace-tabs") {
			t.Fatal("/static/workspace.js is served but does not contain the workspace adapter")
		}
	}

	rec = do(t, handler, http.MethodGet, "/login", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `src="/static/workspace.js"`) {
		t.Fatalf("/login must load the workspace adapter: status=%d", rec.Code)
	}

	rec = do(t, handler, http.MethodPost, "/status", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /status: got %d, want 405", rec.Code)
	}
}

func TestAnonymousRequests(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)

	// Safe methods redirect to the login page without leaking private content.
	for _, path := range []string{"/", "/status", "/account/mailbox", "/connection/callback"} {
		rec := do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("GET %s: status=%d location=%q", path, rec.Code, rec.Header().Get("Location"))
		}
		body := rec.Body.String()
		for _, secret := range []string{"Database connection", "Email operations, organized.", "Sign out", "Sender address"} {
			if strings.Contains(body, secret) {
				t.Fatalf("GET %s leaks %q", path, secret)
			}
		}
	}

	// Removed settings paths return a plain 404 with no redirect and no page.
	for _, path := range []string{"/account", "/account/connections", "/connection"} {
		rec := do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Fatalf("GET %s: status=%d location=%q", path, rec.Code, rec.Header().Get("Location"))
		}
	}

	// Unsafe methods get a plain 401 and change nothing.
	for _, path := range []string{"/logout", "/connection/microsoft/connect", "/connection/disconnect", "/connection/titan"} {
		rec := do(t, handler, http.MethodPost, path, url.Values{}, nil)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "authentication required") {
			t.Fatalf("POST %s anonymous: status=%d body=%q", path, rec.Code, rec.Body.String())
		}
	}
	if count, err := app.CountRecords(mailbox.ConnectionsCollection); err != nil || count != 0 {
		t.Fatalf("anonymous POSTs created %d connection records (err=%v)", count, err)
	}

	// A valid session reaches the dashboard; GET /login bounces to /.
	cookie := login(t, handler)
	rec := do(t, handler, http.MethodGet, "/", nil, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Email operations, organized.") {
		t.Fatalf("GET / with session: status=%d", rec.Code)
	}
	rec = do(t, handler, http.MethodGet, "/login", nil, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("GET /login with session: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginFlow(t *testing.T) {
	t.Run("sets session cookie with correct attributes", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)

		rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, testOperatorPassword), nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Fatalf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("got %d cookies, want 1", len(cookies))
		}
		cookie := cookies[0]
		if cookie.Name != sessionCookieName || cookie.Value == "" {
			t.Fatalf("cookie=%q", cookie.Name)
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
			t.Fatalf("attrs: HttpOnly=%v SameSite=%v Path=%q", cookie.HttpOnly, cookie.SameSite, cookie.Path)
		}
		if cookie.Secure {
			t.Fatal("Secure must follow the http origin")
		}
		collection, err := app.FindCollectionByNameOrId(operator.CollectionName)
		if err != nil {
			t.Fatal(err)
		}
		if cookie.MaxAge != int(collection.AuthToken.Duration) {
			t.Fatalf("MaxAge=%d, want %d", cookie.MaxAge, collection.AuthToken.Duration)
		}
	})

	t.Run("secure cookie for https origin", func(t *testing.T) {
		cfg := testConfig("https://mail.example.com")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)

		rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, testOperatorPassword), nil)
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure {
			t.Fatalf("Secure cookie missing for https origin: %+v", cookies)
		}
	})

	t.Run("identical failure for unknown account and wrong password", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)

		unknown := do(t, handler, http.MethodPost, "/login", loginForm("nobody@example.com", "whatever"), nil)
		wrong := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), nil)
		if unknown.Code != http.StatusOK || wrong.Code != http.StatusOK {
			t.Fatalf("statuses: unknown=%d wrong=%d", unknown.Code, wrong.Code)
		}
		if unknown.Body.String() != wrong.Body.String() {
			t.Fatal("failure responses differ between unknown account and wrong password")
		}
		if !strings.Contains(wrong.Body.String(), loginFailedMessage) {
			t.Fatalf("missing generic error: %q", wrong.Body.String())
		}
		for _, rec := range []*httptest.ResponseRecorder{unknown, wrong} {
			if len(rec.Result().Cookies()) != 0 {
				t.Fatal("failed login set a cookie")
			}
		}
	})

	t.Run("logout expires the cookie", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)
		cookie := login(t, handler)

		rec := do(t, handler, http.MethodPost, "/logout", url.Values{}, withCookie(cookie))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("logout: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
		expired := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == sessionCookieName && c.MaxAge < 0 {
				expired = true
			}
		}
		if !expired {
			t.Fatal("logout did not expire the session cookie")
		}
		// The browser drops the expired cookie; the next request is anonymous.
		rec = do(t, handler, http.MethodGet, "/", nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("GET / after logout: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("tampered cookie behaves as anonymous", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)

		forged := &http.Cookie{Name: sessionCookieName, Value: "forged-token-value"}
		rec := do(t, handler, http.MethodGet, "/", nil, withCookie(forged))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("forged cookie: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("expired cookie behaves as anonymous", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)

		// A short-lived static auth token expires almost immediately.
		record, err := app.FindAuthRecordByEmail(operator.CollectionName, testOperatorEmail)
		if err != nil {
			t.Fatal(err)
		}
		token, err := record.NewStaticAuthToken(time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
		expired := &http.Cookie{Name: sessionCookieName, Value: token}
		rec := do(t, handler, http.MethodGet, "/", nil, withCookie(expired))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("expired cookie: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
	})
}

func TestLoginRateLimit(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)

	clientA := func(r *http.Request) { r.RemoteAddr = "203.0.113.5:4000" }
	clientB := func(r *http.Request) { r.RemoteAddr = "203.0.113.6:4000" }

	// Ten failed attempts pass; the eleventh is limited.
	for i := 1; i <= 10; i++ {
		rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientA)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status=%d, want 200", i, rec.Code)
		}
	}
	rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientA)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 11: status=%d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("429 without Retry-After")
	}

	// Another client is unaffected.
	rec = do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientB)
	if rec.Code != http.StatusOK {
		t.Fatalf("other client: status=%d, want 200", rec.Code)
	}

	// A successful sign-in resets that client's counter.
	clientC := func(r *http.Request) { r.RemoteAddr = "203.0.113.7:4000" }
	for i := 1; i <= 9; i++ {
		if rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientC); rec.Code != http.StatusOK {
			t.Fatalf("client C attempt %d: status=%d, want 200", i, rec.Code)
		}
	}
	rec = do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, testOperatorPassword), clientC)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("successful login: status=%d, want 303", rec.Code)
	}
	// The counter restarted: ten more failures are allowed before limiting.
	for i := 1; i <= 10; i++ {
		if rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientC); rec.Code != http.StatusOK {
			t.Fatalf("post-reset attempt %d: status=%d, want 200", i, rec.Code)
		}
	}
	if rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, "wrong-password"), clientC); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("post-reset attempt 11: status=%d, want 429", rec.Code)
	}
}

func TestCSRFProtection(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:8090")
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	cookie := login(t, handler)

	evilOrigin := func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }
	crossSite := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }

	mutations := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/login"},
		{http.MethodPost, "/logout"},
		{http.MethodPost, "/connection/microsoft/settings"},
		{http.MethodPost, "/connection/microsoft/connect"},
		{http.MethodPost, "/connection/disconnect"},
		{http.MethodPost, "/connection/titan"},
		{http.MethodPost, "/connection/replace/confirm"},
	}
	for _, m := range mutations {
		for name, mutate := range map[string]func(*http.Request){"origin": evilOrigin, "sec-fetch-site": crossSite} {
			rec := do(t, handler, m.method, m.path, url.Values{}, func(r *http.Request) {
				mutate(r)
				if m.path != "/login" {
					r.AddCookie(cookie)
				}
			})
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s with %s: status=%d, want 403", m.method, m.path, name, rec.Code)
			}
		}
	}

	// A rejected /login attempt neither sets a cookie nor consumes the account.
	rec := do(t, handler, http.MethodPost, "/login", loginForm(testOperatorEmail, testOperatorPassword), evilOrigin)
	if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("cross-site login: status=%d cookies=%d", rec.Code, len(rec.Result().Cookies()))
	}

	// Safe GETs are unaffected by a foreign Origin.
	rec = do(t, handler, http.MethodGet, "/", nil, func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.AddCookie(cookie)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("safe GET with evil origin: status=%d", rec.Code)
	}
}

func TestConnectionPage(t *testing.T) {
	t.Run("saving provider setup is required before Microsoft sign-in", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)
		cookie := login(t, handler)
		owner := operatorID(t, app)

		rec := do(t, handler, http.MethodGet, "/account/mailbox?provider=microsoft365", nil, withCookie(cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
		body := rec.Body.String()
		// The setup form and the deployment-derived callback are shown, and
		// sign-in stays unavailable until a registration is saved.
		for _, want := range []string{
			`action="/connection/microsoft/settings"`,
			`name="tenant_id"`,
			`name="client_id"`,
			`name="client_secret"`,
			"data-callback-url>" + cfg.CallbackURL() + "</code>",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("connection page missing %q: %.600s", want, body)
			}
		}
		if strings.Contains(body, ">Continue with Microsoft 365<") {
			t.Fatal("sign-in is offered before any registration is saved")
		}
		if strings.Contains(body, `action="/connection/titan"`) {
			t.Fatal("Microsoft selection rendered the Titan credentials")
		}

		// Sign-in without saved settings reports the actionable requirement.
		rec = do(t, handler, http.MethodPost, "/connection/microsoft/connect", url.Values{}, withCookie(cookie))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Save your Microsoft Tenant ID, Client ID and Client Secret") {
			t.Fatalf("connect without settings: status=%d body=%.400s", rec.Code, rec.Body.String())
		}

		// Invalid identifiers are refused with field guidance, not values.
		rec = do(t, handler, http.MethodPost, "/connection/microsoft/settings", url.Values{
			"tenant_id": {"common"}, "client_id": {"22222222-2222-2222-2222-222222222222"}, "client_secret": {"super-secret-value"},
		}, withCookie(cookie))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tenant ID must be the directory UUID") {
			t.Fatalf("invalid tenant: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "super-secret-value") {
			t.Fatal("rejected form echoed the submitted secret")
		}
		assertCurrentAccountTab(t, rec.Body.String(), "/account/mailbox", "/account/ai")
		if _, err := mailbox.NewStore(app, testKey).Settings(context.Background(), owner); !errors.Is(err, mailbox.ErrSettingsMissing) {
			t.Fatalf("rejected input saved a setup: %v", err)
		}

		// A valid save stages the registration and offers sign-in.
		rec = do(t, handler, http.MethodPost, "/connection/microsoft/settings", url.Values{
			"tenant_id": {"11111111-1111-1111-1111-111111111111"}, "client_id": {"22222222-2222-2222-2222-222222222222"}, "client_secret": {"super-secret-value"},
		}, withCookie(cookie))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/account/mailbox?provider=microsoft365&notice=settings-saved" {
			t.Fatalf("save settings: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
		}
		rec = do(t, handler, http.MethodGet, "/account/mailbox?provider=microsoft365", nil, withCookie(cookie))
		body = rec.Body.String()
		for _, want := range []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
			">Continue with Microsoft 365<",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("saved setup missing %q: %.600s", want, body)
			}
		}
		if strings.Contains(body, "super-secret-value") {
			t.Fatal("page rendered the saved client secret")
		}
	})

	t.Run("stored credential never reaches the page", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)
		cookie := login(t, handler)
		owner := operatorID(t, app)

		store := mailbox.NewStore(app, testKey)
		if _, err := store.SaveSettings(context.Background(), owner, mailbox.ProviderMicrosoft,
			"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "super-secret-value"); err != nil {
			t.Fatal(err)
		}

		collection, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
		if err != nil {
			t.Fatal(err)
		}
		const garbage = "GARBAGE-CIPHERTEXT-9f3a"
		record := core.NewRecord(collection)
		record.Set("owner", owner)
		record.Set("provider", string(mailbox.ProviderMicrosoft))
		record.Set("sender", "hr@bachtelcorporation.com")
		record.Set("identity", "account-1@11111111-1111-1111-1111-111111111111")
		record.Set("status", string(mailbox.StatusReconnectRequired))
		record.Set("generation", 1)
		record.Set("settings_generation", 1)
		record.Set("current", true)
		record.Set("credential", garbage)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}

		rec := do(t, handler, http.MethodGet, "/account/mailbox?provider=microsoft365", nil, withCookie(cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d", rec.Code)
		}
		body := rec.Body.String()
		for _, leaked := range []string{garbage, "super-secret-value"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("page renders stored secret material %q", leaked)
			}
		}
		if !strings.Contains(body, "Reconnect required") {
			t.Fatalf("missing reconnect_required badge: %.400s", body)
		}
		if !strings.Contains(body, "/connection/microsoft/connect") {
			t.Fatal("reconnect must be actionable from the page")
		}
	})

	t.Run("saved setup is owner-scoped", func(t *testing.T) {
		cfg := testConfig("http://127.0.0.1:8090")
		app, handler := newTestServer(t, cfg)
		provisionOperator(t, app)
		provisionOperatorAs(t, app, "second@example.com", "second-horse-battery-staple")
		first := login(t, handler)
		second := loginAs(t, handler, "second@example.com", "second-horse-battery-staple")

		rec := do(t, handler, http.MethodPost, "/connection/microsoft/settings", url.Values{
			"tenant_id": {"11111111-1111-1111-1111-111111111111"}, "client_id": {"22222222-2222-2222-2222-222222222222"}, "client_secret": {"first-secret"},
		}, withCookie(first))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("save first: status=%d body=%.300s", rec.Code, rec.Body.String())
		}

		rec = do(t, handler, http.MethodGet, "/account/mailbox?provider=microsoft365", nil, withCookie(second))
		if rec.Code != http.StatusOK {
			t.Fatalf("second page: status=%d", rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "11111111-1111-1111-1111-111111111111") || strings.Contains(body, "22222222-2222-2222-2222-222222222222") {
			t.Fatal("another operator's saved registration is visible")
		}
		if !strings.Contains(body, `action="/connection/microsoft/settings"`) {
			t.Fatal("the second operator must see their own setup form")
		}

		rec = do(t, handler, http.MethodPost, "/connection/microsoft/connect", url.Values{}, withCookie(second))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Save your Microsoft Tenant ID, Client ID and Client Secret") {
			t.Fatalf("foreign settings authorized sign-in: status=%d", rec.Code)
		}
	})
}
