package microsoft

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// testKey is the fixed deployment key the tests seal credentials under; it
// stands in for APP_ENCRYPTION_KEY.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// Saved provider registrations used by the tests. The deployment holds no
// Microsoft credentials: every operator signs in with their own tenant,
// application and secret, so the values below are per-operator UUIDs.
const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	clientA = "22222222-2222-4222-8222-222222222222"
	secretA = "client-secret-a"

	tenantB = "33333333-3333-4333-8333-333333333333"
	clientB = "44444444-4444-4444-8444-444444444444"
	secretB = "client-secret-b"
)

// bootApp opens a PocketBase app on a temporary directory and mirrors the
// operator, mail connection, reset and provider settings collections from the
// migrations; the real migration package cannot be imported here because
// sibling packages it pulls in are mid-migration.
func bootApp(t *testing.T) *pocketbase.PocketBase {
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

	operators := core.NewBaseCollection(operator.CollectionName)
	operators.Fields.Add(&core.TextField{Name: "email", Required: true, Max: 255})
	if err := app.Save(operators); err != nil {
		t.Fatal(err)
	}

	connections := core.NewBaseCollection(mailbox.ConnectionsCollection)
	connections.Fields.Add(
		&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
		&core.TextField{Name: "provider", Required: true, Max: 32},
		&core.TextField{Name: "sender", Required: true, Max: 320},
		&core.TextField{Name: "identity", Max: 320},
		&core.TextField{Name: "status", Required: true, Max: 32},
		&core.NumberField{Name: "generation", Required: true, OnlyInt: true, Min: new(1.0)},
		// The saved provider setup generation the credentials were issued
		// under; a refresh refuses to run when it no longer matches.
		&core.NumberField{Name: "settings_generation", OnlyInt: true, Min: new(0.0)},
		&core.BoolField{Name: "current"},
		&core.TextField{Name: "credential", Max: 1 << 20},
		&core.DateField{Name: "credential_expires_at"},
		&core.TextField{Name: "last_error", Max: 500},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	connections.AddIndex("idx_mail_connections_owner", true, "owner", "current = true")
	connections.AddIndex("idx_mail_connections_sender", true, "sender", "")
	connections.AddIndex("idx_mail_connections_identity", true, "identity", "identity != ''")
	if err := app.Save(connections); err != nil {
		t.Fatal(err)
	}

	resets := core.NewBaseCollection(mailbox.ResetsCollection)
	resets.Fields.Add(
		&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
		&core.RelationField{Name: "from_connection", MaxSelect: 1, CollectionId: connections.Id},
		&core.RelationField{Name: "to_connection", Required: true, MaxSelect: 1, CollectionId: connections.Id},
		&core.TextField{Name: "status", Required: true, Max: 32},
		&core.JSONField{Name: "manifest", MaxSize: 256 << 10},
		&core.TextField{Name: "error", Max: 500},
		// Staged registration generation to activate with this replacement.
		&core.NumberField{Name: "rotation_generation", OnlyInt: true, Min: new(0.0)},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	resets.AddIndex("idx_connection_resets_active", true, "owner", "status != 'completed'")
	if err := app.Save(resets); err != nil {
		t.Fatal(err)
	}

	settings := core.NewBaseCollection(mailbox.SettingsCollection)
	settings.Fields.Add(
		&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
		&core.TextField{Name: "provider", Required: true, Max: 32},
		&core.TextField{Name: "tenant_id", Max: 128},
		&core.TextField{Name: "client_id", Max: 128},
		&core.TextField{Name: "secret", Max: 1 << 20},
		&core.NumberField{Name: "generation", OnlyInt: true, Min: new(0.0)},
		&core.TextField{Name: "pending_tenant_id", Max: 128},
		&core.TextField{Name: "pending_client_id", Max: 128},
		&core.TextField{Name: "pending_secret", Max: 1 << 20},
		&core.NumberField{Name: "pending_generation", OnlyInt: true, Min: new(0.0)},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	settings.AddIndex("idx_provider_settings_owner", true, "owner", "")
	if err := app.Save(settings); err != nil {
		t.Fatal(err)
	}
	return app
}

// provision creates an operator record and returns its id.
func provision(t *testing.T, app core.App, email string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(operator.CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("email", email)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// testEnv points the Microsoft endpoints at the given mock servers. Tenant,
// client and secret are per-operator saved settings, never deployment
// configuration.
func testEnv(t *testing.T, authorityURL, graphBaseURL string) config.Config {
	t.Helper()
	t.Setenv(config.EnvOrigin, "https://mail.example.test")
	t.Setenv(config.EnvAuthorityURL, authorityURL)
	t.Setenv(config.EnvGraphBaseURL, graphBaseURL)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// newService builds the service over a store sealed with the fixed test key.
func newService(t *testing.T, app core.App, cfg config.Config) (*Service, *mailbox.Store) {
	t.Helper()
	store := mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
	return NewService(app, cfg, store), store
}

// saveSettings stages a provider registration for the operator and returns the
// staged generation.
func saveSettings(t *testing.T, store *mailbox.Store, ownerID, tenantID, clientID, secret string) int {
	t.Helper()
	generation, err := store.SaveSettings(context.Background(), ownerID, mailbox.ProviderMicrosoft, tenantID, clientID, secret)
	if err != nil {
		t.Fatal(err)
	}
	return generation
}

// ensureSettings stages the default registration when the operator saved none.
func ensureSettings(t *testing.T, store *mailbox.Store, ownerID string) {
	t.Helper()
	_, err := store.Settings(context.Background(), ownerID)
	if err == nil {
		return
	}
	if !errors.Is(err, mailbox.ErrSettingsMissing) {
		t.Fatal(err)
	}
	saveSettings(t, store, ownerID, tenantA, clientA, secretA)
}

// grant is one recorded token request. The tenant comes from the request path
// and the client credentials from the request itself, so a test can prove
// which registration was used.
type grant struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	GrantType    string
}

// mockMicrosoft stands in for the Microsoft token endpoint and Graph /me.
type mockMicrosoft struct {
	t      *testing.T
	mu     sync.Mutex
	meID   string
	meMail string
	meUPN  string

	exchanges    int
	refreshes    int
	codeVerifier string

	grants []grant

	rejectedRefresh map[string]bool

	// Refresh barrier: while armed, every refresh grant waits until
	// barrierN refreshes have arrived, proving they are concurrent.
	barrierN       int
	barrierRelease chan struct{}

	refreshConcurrent    int
	maxRefreshConcurrent int

	srv *httptest.Server
}

func newMockMicrosoft(t *testing.T, id, upn string) *mockMicrosoft {
	t.Helper()
	m := &mockMicrosoft{t: t, meID: id, meUPN: upn, rejectedRefresh: map[string]bool{}}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockMicrosoft) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
		if err := r.ParseForm(); err != nil {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		clientID, clientSecret := clientCredentials(r)
		m.mu.Lock()
		m.grants = append(m.grants, grant{
			Tenant:       tenantFromPath(r.URL.Path),
			ClientID:     clientID,
			ClientSecret: clientSecret,
			GrantType:    r.PostForm.Get("grant_type"),
		})
		m.mu.Unlock()
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			m.mu.Lock()
			m.exchanges++
			n := m.exchanges
			m.codeVerifier = r.PostForm.Get("code_verifier")
			m.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if _, err := fmt.Fprintf(w, `{"access_token":"at-c%d","refresh_token":"rt-c%d","token_type":"Bearer","expires_in":3600}`, n, n); err != nil {
				m.t.Errorf("write mock response: %v", err)
			}
		case "refresh_token":
			m.mu.Lock()
			m.refreshes++
			n := m.refreshes
			reject := m.rejectedRefresh[r.PostForm.Get("refresh_token")]
			m.refreshConcurrent++
			if m.refreshConcurrent > m.maxRefreshConcurrent {
				m.maxRefreshConcurrent = m.refreshConcurrent
			}
			release := m.barrierRelease
			if m.barrierN > 0 && n == m.barrierN && release != nil {
				close(release)
				m.barrierRelease = nil
			}
			m.mu.Unlock()
			if release != nil {
				select {
				case <-release:
				case <-time.After(3 * time.Second):
				}
			}
			m.mu.Lock()
			m.refreshConcurrent--
			m.mu.Unlock()
			if reject {
				http.Error(w, `{"error":"invalid_grant","error_description":"token revoked"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			rt := r.PostForm.Get("refresh_token")
			if _, err := fmt.Fprintf(w, `{"access_token":"at-of-%s","refresh_token":"rt-of-%s","token_type":"Bearer","expires_in":3600}`, rt, rt); err != nil {
				m.t.Errorf("write mock response: %v", err)
			}
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		}
	case strings.HasSuffix(r.URL.Path, "/me"):
		m.mu.Lock()
		id, mail, upn := m.meID, m.meMail, m.meUPN
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"id": id, "mail": mail, "userPrincipalName": upn}); err != nil {
			m.t.Errorf("write mock response: %v", err)
		}
	default:
		http.NotFound(w, r)
	}
}

// clientCredentials reads the client id and secret of a token request. The
// oauth2 client sends them in the Authorization header unless the endpoint
// asks for parameters.
func clientCredentials(r *http.Request) (clientID, clientSecret string) {
	if id, secret, ok := r.BasicAuth(); ok {
		return id, secret
	}
	return r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
}

// tenantFromPath extracts the tenant of a tenant-specific Microsoft endpoint.
func tenantFromPath(path string) string {
	return strings.Trim(strings.TrimSuffix(path, "/oauth2/v2.0/token"), "/")
}

func (m *mockMicrosoft) setIdentity(id, upn string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.meID, m.meUPN = id, upn
}

func (m *mockMicrosoft) setMail(upn, mail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.meUPN, m.meMail = upn, mail
}

func (m *mockMicrosoft) verifier() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codeVerifier
}

func (m *mockMicrosoft) maxConcurrentRefresh() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxRefreshConcurrent
}

// refreshCount reports how many refresh grants reached the mock.
func (m *mockMicrosoft) refreshCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshes
}

// recordedGrants returns the recorded grants of one grant type.
func (m *mockMicrosoft) recordedGrants(grantType string) []grant {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []grant
	for _, g := range m.grants {
		if g.GrantType == grantType {
			out = append(out, g)
		}
	}
	return out
}

func stateAndChallenge(t *testing.T, authURL string) (state, challenge string) {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	state, challenge = query.Get("state"), query.Get("code_challenge")
	if state == "" || challenge == "" {
		t.Fatalf("authorization URL lacks state or PKCE challenge: %s", authURL)
	}
	if got := query.Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}
	return state, challenge
}

// connectNow runs BeginAuth + CompleteAuth against the mock, which must
// already answer with the wanted identity. The operator's registration is
// staged when they saved none yet.
func connectNow(t *testing.T, svc *Service, store *mailbox.Store, owner string) *mailbox.Connection {
	t.Helper()
	ensureSettings(t, store, owner)
	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)
	conn, err := svc.CompleteAuth(context.Background(), owner, state, "code-1")
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// expireToken rewrites the connection credential to an already-expired
// access token with the given refresh token.
func expireToken(t *testing.T, store *mailbox.Store, conn *mailbox.Connection, refreshToken string) {
	t.Helper()
	err := store.Reconnect(context.Background(), conn, mailbox.Credential{
		Provider:     mailbox.ProviderMicrosoft,
		AccessToken:  "stale-" + refreshToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stubDrainer reports in-flight submissions for an owner. always keeps
// reporting them; remaining reports them only for the first calls.
type stubDrainer struct {
	mu        sync.Mutex
	always    bool
	remaining int
	calls     int
}

func (d *stubDrainer) InFlightFor(ctx context.Context, ownerID string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.always {
		return 1, nil
	}
	if d.remaining > 0 {
		d.remaining--
		return 1, nil
	}
	return 0, nil
}

func TestBeginAuthWithoutSavedSettingsNamesRequiredFields(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, _ := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	_, err := svc.BeginAuth(context.Background(), owner)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("BeginAuth without saved settings = %v, want ErrNotConfigured", err)
	}
	msg := err.Error()
	for _, field := range []string{"Tenant ID", "Client ID", "Client Secret"} {
		if !strings.Contains(msg, field) {
			t.Fatalf("error must name %s, got %q", field, msg)
		}
	}
	// The message names the fields, never their values.
	for _, value := range []string{tenantA, clientA, secretA, tenantB, clientB, secretB} {
		if strings.Contains(msg, value) {
			t.Fatalf("error must never contain a saved value, got %q", msg)
		}
	}
}

func TestBeginAuthURL(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")
	saveSettings(t, store, owner, tenantA, clientA, secretA)

	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Path, "/"+tenantA+"/oauth2/v2.0/authorize") {
		t.Fatalf("authorize URL is not tenant-specific: %s", authURL)
	}
	query := parsed.Query()
	if query.Get("redirect_uri") != cfg.CallbackURL() {
		t.Fatalf("redirect_uri = %q, want %q", query.Get("redirect_uri"), cfg.CallbackURL())
	}
	if query.Get("client_id") != clientA {
		t.Fatalf("client_id = %q, want the operator's saved client %q", query.Get("client_id"), clientA)
	}
	if query.Get("login_hint") != "" {
		t.Fatal("there is no fixed sender; the URL must not carry a login_hint")
	}
	if !strings.Contains(query.Get("scope"), "offline_access") ||
		!strings.Contains(query.Get("scope"), "Mail.Send") ||
		!strings.Contains(query.Get("scope"), "User.Read") {
		t.Fatalf("scopes missing minimum delegated set: %q", query.Get("scope"))
	}

	// The exchanged code must carry the PKCE verifier matching the challenge.
	state, challenge := stateAndChallenge(t, authURL)
	if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(mock.verifier()))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Fatalf("PKCE verifier does not match challenge: %q vs %q", got, challenge)
	}
}

func TestAttemptExpiryReplayAndOperatorBinding(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")
	ensureSettings(t, store, owner)

	// Expired attempt: backdate the stored expiry.
	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)
	svc.mu.Lock()
	attempt := svc.attempts[state]
	attempt.expiresAt = time.Now().Add(-time.Minute)
	svc.attempts[state] = attempt
	svc.mu.Unlock()
	if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
		t.Fatalf("expired attempt = %v, want ErrAuthorizationState", err)
	}

	// Operator binding: op-2 cannot consume op-1's attempt.
	authURL, err = svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ = stateAndChallenge(t, authURL)
	other := provision(t, app, "op2@bachtelcorporation.com")
	if _, err := svc.CompleteAuth(context.Background(), other, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
		t.Fatalf("cross-operator attempt = %v, want ErrAuthorizationState", err)
	}
	// The attempt was consumed by the failed try; the rightful owner is
	// rejected too.
	if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
		t.Fatalf("replayed attempt = %v, want ErrAuthorizationState", err)
	}

	// Successful connect, then replay the same state.
	conn := connectNow(t, svc, store, owner)
	if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
		t.Fatalf("replay after success = %v, want ErrAuthorizationState", err)
	}
	if conn.Generation != 1 || !conn.Current {
		t.Fatalf("first connect = generation %d current %v, want generation 1 current", conn.Generation, conn.Current)
	}
	if conn.Sender != "user1@tenant1.example.test" || conn.Identity != "graph-1@"+tenantA {
		t.Fatalf("connection identity = %q/%q", conn.Sender, conn.Identity)
	}
	if conn.SettingsGeneration != 1 {
		t.Fatalf("settings generation = %d, want 1", conn.SettingsGeneration)
	}
}

func TestFirstConnectAndGraphFallback(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "")
	// userPrincipalName empty: the mail address is the fallback.
	mock.setMail("", "Fallback@Tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	conn := connectNow(t, svc, store, owner)
	if got, want := conn.Sender, "fallback@tenant1.example.test"; got != want {
		t.Fatalf("canonical sender = %q, want %q", got, want)
	}
	token, err := svc.AccessToken(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if token != "at-c1" {
		t.Fatalf("access token = %q, want the freshly issued at-c1", token)
	}
	// The stored credential unseals for the owner through the store.
	credential, err := store.Credential(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if credential.RefreshToken != "rt-c1" {
		t.Fatalf("stored refresh token = %q", credential.RefreshToken)
	}
}

func TestDuplicateIdentityRefusedWithoutTouchingConnection(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")
	// Both operators registered in the same tenant, so the mailbox identity
	// is the same one.
	saveSettings(t, store, ownerA, tenantA, clientA, secretA)
	saveSettings(t, store, ownerB, tenantA, clientB, secretB)

	first := connectNow(t, svc, store, ownerA)

	// ownerB signs into the same mailbox.
	authURL, err := svc.BeginAuth(context.Background(), ownerB)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)
	_, err = svc.CompleteAuth(context.Background(), ownerB, state, "code-1")
	if !errors.Is(err, mailbox.ErrClaimed) {
		t.Fatalf("duplicate identity = %v, want mailbox.ErrClaimed", err)
	}
	if err.Error() == "" || strings.Contains(err.Error(), ownerA) {
		t.Fatalf("claim refusal must not disclose the other owner: %q", err)
	}

	// ownerA's connection is unchanged: same record, still connected,
	// generation intact, original credential still usable.
	current, err := svc.store.Current(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != first.ID || current.Status != mailbox.StatusConnected || current.Generation != 1 {
		t.Fatalf("owner A connection changed: %+v", current)
	}
	token, err := svc.AccessToken(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if token != "at-c1" {
		t.Fatalf("owner A token = %q, want at-c1", token)
	}
}

func TestSameIdentityReconnectPreservesGeneration(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	first := connectNow(t, svc, store, owner)
	second := connectNow(t, svc, store, owner)

	if second.ID != first.ID {
		t.Fatalf("reconnect must keep the connection: %q vs %q", second.ID, first.ID)
	}
	if second.Generation != 1 {
		t.Fatalf("generation = %d, want 1 (approvals for exact content survive)", second.Generation)
	}
	if second.SettingsGeneration != 1 {
		t.Fatalf("settings generation = %d, want 1", second.SettingsGeneration)
	}
	if second.Status != mailbox.StatusConnected || !second.Current {
		t.Fatalf("status = %q current %v", second.Status, second.Current)
	}
	// The credential was replaced with the new token.
	credential, err := store.Credential(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-c2" {
		t.Fatalf("access token = %q, want the reissued at-c2", credential.AccessToken)
	}
}

func TestDifferentIdentityStagesReplacement(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	first := connectNow(t, svc, store, owner)

	// The same operator signs into a different mailbox.
	mock.setIdentity("graph-2", "user2@tenant1.example.test")
	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)
	conn, err := svc.CompleteAuth(context.Background(), owner, state, "code-1")
	if conn != nil {
		t.Fatalf("replacement must not return a connection: %+v", conn)
	}
	var replacement *ReplacementRequired
	if !errors.As(err, &replacement) {
		t.Fatalf("different identity = %v, want *ReplacementRequired", err)
	}
	if replacement.Reset == nil || replacement.Reset.ID == "" {
		t.Fatalf("ReplacementRequired must carry the staged reset: %+v", replacement)
	}

	// A staged claim exists for the new mailbox.
	reset, err := store.ActiveReset(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if reset.ID != replacement.Reset.ID || reset.Status != mailbox.ResetStaging {
		t.Fatalf("active reset = %+v, want the staged reset", reset)
	}
	target, err := store.ByID(context.Background(), reset.ToConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Sender != "user2@tenant1.example.test" || target.Identity != "graph-2@"+tenantA || target.Current {
		t.Fatalf("staged target = %+v", target)
	}

	// The current connection is untouched.
	current, err := store.Current(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != first.ID || current.Sender != "user1@tenant1.example.test" ||
		current.Status != mailbox.StatusConnected || current.Generation != 1 {
		t.Fatalf("current connection changed: %+v", current)
	}
	token, err := svc.AccessToken(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if token != "at-c1" {
		t.Fatalf("current token = %q, want at-c1", token)
	}
}

func TestOperatorsUseTheirOwnSavedRegistration(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")
	saveSettings(t, store, ownerA, tenantA, clientA, secretA)
	saveSettings(t, store, ownerB, tenantB, clientB, secretB)

	// Each operator authorizes against their own tenant and application.
	authA, err := svc.BeginAuth(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(authA, "/"+tenantA+"/oauth2/v2.0/authorize") {
		t.Fatalf("operator A authorize URL is not tenant-specific: %s", authA)
	}
	if got := queryValue(t, authA, "client_id"); got != clientA {
		t.Fatalf("operator A client_id = %q, want %q", got, clientA)
	}
	stateA, _ := stateAndChallenge(t, authA)
	if _, err := svc.CompleteAuth(context.Background(), ownerA, stateA, "code-1"); err != nil {
		t.Fatal(err)
	}

	// The second operator signs in with another mailbox.
	mock.setIdentity("graph-2", "user2@tenant2.example.test")
	authB, err := svc.BeginAuth(context.Background(), ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(authB, "/"+tenantB+"/oauth2/v2.0/authorize") {
		t.Fatalf("operator B authorize URL is not tenant-specific: %s", authB)
	}
	if got := queryValue(t, authB, "client_id"); got != clientB {
		t.Fatalf("operator B client_id = %q, want %q", got, clientB)
	}

	stateB, _ := stateAndChallenge(t, authB)
	connB, err := svc.CompleteAuth(context.Background(), ownerB, stateB, "code-1")
	if err != nil {
		t.Fatal(err)
	}

	// The token exchange of each operator used its own registration.
	exchanges := mock.recordedGrants("authorization_code")
	if len(exchanges) != 2 {
		t.Fatalf("recorded %d code exchanges, want 2", len(exchanges))
	}
	for _, g := range exchanges {
		switch g.ClientID {
		case clientA:
			if g.Tenant != tenantA || g.ClientSecret != secretA {
				t.Fatalf("operator A exchanged with %+v, want tenant %q and its own secret", g, tenantA)
			}
		case clientB:
			if g.Tenant != tenantB || g.ClientSecret != secretB {
				t.Fatalf("operator B exchanged with %+v, want tenant %q and its own secret", g, tenantB)
			}
		default:
			t.Fatalf("unexpected client in the token exchange: %+v", g)
		}
	}

	// The refresh of each connection uses the same operator's registration.
	currentA, err := store.Current(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	expireToken(t, store, currentA, "rt-a")
	expireToken(t, store, connB, "rt-b")
	if _, err := svc.AccessToken(context.Background(), currentA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccessToken(context.Background(), connB); err != nil {
		t.Fatal(err)
	}
	refreshes := mock.recordedGrants("refresh_token")
	if len(refreshes) != 2 {
		t.Fatalf("recorded %d refresh grants, want 2", len(refreshes))
	}
	for _, g := range refreshes {
		switch g.ClientID {
		case clientA:
			if g.Tenant != tenantA || g.ClientSecret != secretA {
				t.Fatalf("operator A refreshed with %+v, want its own registration", g)
			}
		case clientB:
			if g.Tenant != tenantB || g.ClientSecret != secretB {
				t.Fatalf("operator B refreshed with %+v, want its own registration", g)
			}
		default:
			t.Fatalf("unexpected client in the refresh grant: %+v", g)
		}
	}
	// Neither operator ever saw the other's secret.
	for _, g := range append(exchanges, refreshes...) {
		if g.ClientID == clientA && g.ClientSecret == secretB {
			t.Fatalf("operator A used operator B's secret: %+v", g)
		}
		if g.ClientID == clientB && g.ClientSecret == secretA {
			t.Fatalf("operator B used operator A's secret: %+v", g)
		}
	}
}

func TestSameMailboxRotationPromotesNewSettings(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	first := connectNow(t, svc, store, owner)
	if first.SettingsGeneration != 1 || first.Generation != 1 {
		t.Fatalf("first connect = settings generation %d, generation %d", first.SettingsGeneration, first.Generation)
	}

	// The operator saves a new application registration for the same tenant.
	generation := saveSettings(t, store, owner, tenantA, clientB, secretB)
	if generation != 2 {
		t.Fatalf("staged generation = %d, want 2", generation)
	}
	staged, err := store.Settings(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Generation != 1 || staged.PendingGeneration != 2 {
		t.Fatalf("staged settings = %+v, want active 1 pending 2", staged)
	}

	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)
	rotated, err := svc.CompleteAuth(context.Background(), owner, state, "code-1")
	if err != nil {
		t.Fatal(err)
	}

	// The mailbox stays the same, so the mailbox generation is preserved:
	// approvals for exact content stay valid across a registration change.
	if rotated.ID != first.ID {
		t.Fatalf("rotation must keep the connection: %q vs %q", rotated.ID, first.ID)
	}
	if rotated.Generation != first.Generation {
		t.Fatalf("mailbox generation = %d, want the unchanged %d", rotated.Generation, first.Generation)
	}
	if rotated.SettingsGeneration != 2 {
		t.Fatalf("settings generation = %d, want the staged 2", rotated.SettingsGeneration)
	}
	// The staged registration is now the active one, with nothing pending.
	active, err := store.Settings(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.Generation != 2 || active.HasPending() {
		t.Fatalf("settings after rotation = %+v, want generation 2 and no pending", active)
	}
	if active.TenantID != tenantA || active.ClientID != clientB {
		t.Fatalf("active registration = %q/%q, want the new client %q", active.TenantID, active.ClientID, clientB)
	}
	// The credential came from the new registration.
	credential, err := store.Credential(context.Background(), rotated)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-c2" {
		t.Fatalf("access token = %q, want the reissued at-c2", credential.AccessToken)
	}
}

func TestSettingsEditBetweenBeginAndCallbackIsRefused(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	first := connectNow(t, svc, store, owner)

	// The operator stages a new registration and starts signing in.
	saveSettings(t, store, owner, tenantA, clientB, secretB)
	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := stateAndChallenge(t, authURL)

	// The settings change again before the callback arrives.
	saveSettings(t, store, owner, tenantA, clientB, "client-secret-b2")

	_, err = svc.CompleteAuth(context.Background(), owner, state, "code-1")
	if !errors.Is(err, mailbox.ErrSettingsStale) {
		t.Fatalf("stale settings callback = %v, want mailbox.ErrSettingsStale", err)
	}

	// Nothing was installed: the connection keeps its old credential and its
	// old settings generation.
	current, err := store.Current(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != first.ID || current.SettingsGeneration != 1 || current.Status != mailbox.StatusConnected {
		t.Fatalf("connection changed: %+v", current)
	}
	credential, err := store.Credential(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "at-c1" {
		t.Fatalf("access token = %q, want the unchanged at-c1", credential.AccessToken)
	}
	// The active registration is untouched; only the pending draft moved on.
	settings, err := store.Settings(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Generation != 1 || settings.TenantID != tenantA || settings.ClientID != clientA {
		t.Fatalf("active registration changed: %+v", settings)
	}
	if settings.PendingGeneration != 3 {
		t.Fatalf("pending generation = %d, want the latest edit 3", settings.PendingGeneration)
	}
}

func TestRotationWaitsForInFlightSubmissions(t *testing.T) {
	t.Run("refused while submissions are in flight", func(t *testing.T) {
		app := bootApp(t)
		mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
		cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
		svc, store := newService(t, app, cfg)
		owner := provision(t, app, "op1@bachtelcorporation.com")
		svc.SetDrainer(&stubDrainer{always: true})
		svc.drainTimeout = 50 * time.Millisecond

		connectNow(t, svc, store, owner)
		saveSettings(t, store, owner, tenantA, clientB, secretB)

		authURL, err := svc.BeginAuth(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		state, _ := stateAndChallenge(t, authURL)
		if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); !errors.Is(err, ErrRotationInFlight) {
			t.Fatalf("rotation with submissions in flight = %v, want ErrRotationInFlight", err)
		}

		// Settings and credentials are unchanged.
		settings, err := store.Settings(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if settings.Generation != 1 || settings.PendingGeneration != 2 {
			t.Fatalf("settings = %+v, want active 1 pending 2", settings)
		}
		current, err := store.Current(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if current.SettingsGeneration != 1 {
			t.Fatalf("settings generation = %d, want the unchanged 1", current.SettingsGeneration)
		}
		credential, err := store.Credential(context.Background(), current)
		if err != nil {
			t.Fatal(err)
		}
		if credential.AccessToken != "at-c1" {
			t.Fatalf("access token = %q, want the unchanged at-c1", credential.AccessToken)
		}
	})

	t.Run("succeeds once submissions settle", func(t *testing.T) {
		app := bootApp(t)
		mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
		cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
		svc, store := newService(t, app, cfg)
		owner := provision(t, app, "op1@bachtelcorporation.com")
		svc.SetDrainer(&stubDrainer{remaining: 2})

		first := connectNow(t, svc, store, owner)
		saveSettings(t, store, owner, tenantA, clientB, secretB)

		authURL, err := svc.BeginAuth(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		state, _ := stateAndChallenge(t, authURL)
		rotated, err := svc.CompleteAuth(context.Background(), owner, state, "code-1")
		if err != nil {
			t.Fatal(err)
		}
		if rotated.ID != first.ID || rotated.Generation != first.Generation {
			t.Fatalf("rotation changed the connection: %+v", rotated)
		}
		if rotated.SettingsGeneration != 2 {
			t.Fatalf("settings generation = %d, want 2", rotated.SettingsGeneration)
		}
		settings, err := store.Settings(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if settings.Generation != 2 || settings.HasPending() {
			t.Fatalf("settings = %+v, want generation 2 and no pending", settings)
		}
	})
}

func TestRefreshesAreSerializedPerConnection(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")
	saveSettings(t, store, ownerA, tenantA, clientA, secretA)
	saveSettings(t, store, ownerB, tenantB, clientB, secretB)

	connA := connectNow(t, svc, store, ownerA)
	mock.setIdentity("graph-2", "user2@tenant2.example.test")
	connB := connectNow(t, svc, store, ownerB)

	expireToken(t, store, connA, "rt-a")
	expireToken(t, store, connB, "rt-b")

	// Arm the barrier: both refresh grants must be in flight simultaneously
	// or neither returns. A global refresh lock would deadlock them.
	mock.mu.Lock()
	mock.barrierN = 2
	mock.barrierRelease = make(chan struct{})
	mock.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i, conn := range []*mailbox.Connection{connA, connB} {
		wg.Add(1)
		go func(i int, conn *mailbox.Connection) {
			defer wg.Done()
			tokens[i], errs[i] = svc.AccessToken(ctx, conn)
		}(i, conn)
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("concurrent refresh %d failed: %v", i, errs[i])
		}
	}
	if tokens[0] != "at-of-rt-a" || tokens[1] != "at-of-rt-b" {
		t.Fatalf("tokens = %q/%q, want each connection's own refreshed token", tokens[0], tokens[1])
	}
	if got := mock.maxConcurrentRefresh(); got != 2 {
		t.Fatalf("max concurrent refreshes = %d, want 2 (per-connection serialization)", got)
	}

	// The rotated credentials were persisted per connection.
	credentialA, err := store.Credential(context.Background(), connA)
	if err != nil {
		t.Fatal(err)
	}
	if credentialA.AccessToken != "at-of-rt-a" || credentialA.RefreshToken != "rt-of-rt-a" {
		t.Fatalf("persisted credential A = %+v", credentialA)
	}
}

func TestRejectedRefreshMarksOnlyThatConnection(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")
	saveSettings(t, store, ownerA, tenantA, clientA, secretA)
	saveSettings(t, store, ownerB, tenantB, clientB, secretB)

	connA := connectNow(t, svc, store, ownerA)
	mock.setIdentity("graph-2", "user2@tenant2.example.test")
	connB := connectNow(t, svc, store, ownerB)

	expireToken(t, store, connA, "rt-a")
	expireToken(t, store, connB, "rt-bad")
	mock.mu.Lock()
	mock.rejectedRefresh["rt-bad"] = true
	mock.mu.Unlock()

	if _, err := svc.AccessToken(context.Background(), connB); !errors.Is(err, ErrRefreshRejected) {
		t.Fatalf("rejected refresh = %v, want ErrRefreshRejected", err)
	}

	// Only connection B is marked; its ciphertext was not overwritten.
	currentB, err := store.Current(context.Background(), ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if currentB.Status != mailbox.StatusReconnectRequired || currentB.LastError == "" {
		t.Fatalf("connection B = %+v, want reconnect_required with a reason", currentB)
	}
	credentialB, err := store.Credential(context.Background(), currentB)
	if err != nil {
		t.Fatal(err)
	}
	if credentialB.AccessToken != "stale-rt-bad" {
		t.Fatalf("rejected refresh must not overwrite the credential: %+v", credentialB)
	}

	// Connection A still refreshes fine and stays connected.
	tokenA, err := svc.AccessToken(context.Background(), connA)
	if err != nil {
		t.Fatal(err)
	}
	if tokenA != "at-of-rt-a" {
		t.Fatalf("connection A token = %q, want its own refreshed token", tokenA)
	}
	currentA, err := store.Current(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if currentA.Status != mailbox.StatusConnected {
		t.Fatalf("connection A status = %q, want connected", currentA.Status)
	}
}

func TestRefreshRefusesWhenSettingsGenerationChanged(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	conn := connectNow(t, svc, store, owner)
	expireToken(t, store, conn, "rt-a")

	// The active registration moved on to a newer generation while this
	// connection still carries the credentials of the old one.
	record, err := app.FindFirstRecordByFilter(mailbox.SettingsCollection, "owner = {:owner}", dbx.Params{"owner": owner})
	if err != nil {
		t.Fatal(err)
	}
	record.Set("generation", 2)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	before, err := app.FindRecordById(mailbox.ConnectionsCollection, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored := before.GetString("credential")

	_, err = svc.AccessToken(context.Background(), conn)
	if !errors.Is(err, mailbox.ErrSettingsStale) {
		t.Fatalf("refresh under a changed registration = %v, want mailbox.ErrSettingsStale", err)
	}
	// No refresh grant reached Microsoft.
	if got := mock.refreshCount(); got != 0 {
		t.Fatalf("refresh grants = %d, want none", got)
	}
	// The connection is marked reconnect required and the stored ciphertext
	// is unchanged.
	current, err := store.Current(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != mailbox.StatusReconnectRequired || current.LastError == "" {
		t.Fatalf("connection = %+v, want reconnect_required with a reason", current)
	}
	after, err := app.FindRecordById(mailbox.ConnectionsCollection, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.GetString("credential"); got != stored {
		t.Fatalf("stored ciphertext changed: %q vs %q", got, stored)
	}
}

func TestUnreadableCiphertextIsActionableAndNeverOverwritten(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	conn := connectNow(t, svc, store, owner)

	// Corrupt the stored ciphertext directly.
	record, err := app.FindRecordById(mailbox.ConnectionsCollection, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("credential", "garbage-ciphertext")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	_, err = svc.AccessToken(context.Background(), conn)
	if !errors.Is(err, mailbox.ErrCredentialsUnreadable) {
		t.Fatalf("unreadable ciphertext = %v, want ErrCredentialsUnreadable", err)
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("unreadable ciphertext error must be actionable: %q", err)
	}

	// The stored value was left alone and the status was not changed behind
	// the operator's back.
	record, err = app.FindRecordById(mailbox.ConnectionsCollection, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.GetString("credential"); got != "garbage-ciphertext" {
		t.Fatalf("stored ciphertext = %q, want it unchanged", got)
	}
	current, err := store.Current(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != mailbox.StatusConnected {
		t.Fatalf("status = %q, want unchanged (only CompleteAuth may rewrite)", current.Status)
	}
}

// queryValue reads one query parameter of a URL.
func queryValue(t *testing.T, rawURL, name string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get(name)
}
