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

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// testKey is the fixed deployment key the tests seal credentials under; it
// stands in for APP_ENCRYPTION_KEY.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// bootApp opens a PocketBase app on a temporary directory and mirrors the
// operator, mail connection and reset collections from the migrations; the
// real migration package cannot be imported here because sibling packages it
// pulls in are mid-migration.
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
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	resets.AddIndex("idx_connection_resets_active", true, "owner", "status != 'completed'")
	if err := app.Save(resets); err != nil {
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

// testEnv points the Microsoft settings at the given mock servers.
func testEnv(t *testing.T, authorityURL, graphBaseURL string) config.Config {
	t.Helper()
	t.Setenv(config.EnvTenantID, "tenant-1")
	t.Setenv(config.EnvClientID, "client-1")
	t.Setenv(config.EnvClientSecret, "client-secret-1")
	t.Setenv(config.EnvRedirectURL, "https://mail.example.test/connection/callback")
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
// already answer with the wanted identity.
func connectNow(t *testing.T, svc *Service, owner string) *mailbox.Connection {
	t.Helper()
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

func TestConfigErrorNamesEnvVars(t *testing.T) {
	app := bootApp(t)
	cfg := testEnv(t, "https://login.example.test", "https://graph.example.test")
	cfg.Microsoft.TenantID = ""
	cfg.Microsoft.ClientID = ""
	svc, _ := newService(t, app, cfg)

	err := svc.ConfigError()
	if err == nil {
		t.Fatal("expected a configuration error")
	}
	msg := err.Error()
	if !strings.Contains(msg, config.EnvTenantID) || !strings.Contains(msg, config.EnvClientID) {
		t.Fatalf("config error must name the env vars, got %q", msg)
	}
	if strings.Contains(msg, "client-secret-1") {
		t.Fatalf("config error must never contain values, got %q", msg)
	}
	if _, err := svc.BeginAuth(context.Background(), "op-1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("BeginAuth with incomplete settings = %v, want ErrNotConfigured", err)
	}
}

func TestBeginAuthURL(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, _ := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	authURL, err := svc.BeginAuth(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.Path, "/tenant-1/oauth2/v2.0/authorize") {
		t.Fatalf("authorize URL is not tenant-specific: %s", authURL)
	}
	query := parsed.Query()
	if query.Get("redirect_uri") != cfg.Microsoft.RedirectURL {
		t.Fatalf("redirect_uri = %q, want %q", query.Get("redirect_uri"), cfg.Microsoft.RedirectURL)
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
	svc, _ := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

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
	conn := connectNow(t, svc, owner)
	if _, err := svc.CompleteAuth(context.Background(), owner, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
		t.Fatalf("replay after success = %v, want ErrAuthorizationState", err)
	}
	if conn.Generation != 1 || !conn.Current {
		t.Fatalf("first connect = generation %d current %v, want generation 1 current", conn.Generation, conn.Current)
	}
	if conn.Sender != "user1@tenant1.example.test" || conn.Identity != "graph-1@tenant-1" {
		t.Fatalf("connection identity = %q/%q", conn.Sender, conn.Identity)
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

	conn := connectNow(t, svc, owner)
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
	svc, _ := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")

	first := connectNow(t, svc, ownerA)

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

	first := connectNow(t, svc, owner)
	second := connectNow(t, svc, owner)

	if second.ID != first.ID {
		t.Fatalf("reconnect must keep the connection: %q vs %q", second.ID, first.ID)
	}
	if second.Generation != 1 {
		t.Fatalf("generation = %d, want 1 (approvals for exact content survive)", second.Generation)
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

	first := connectNow(t, svc, owner)

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
	if target.Sender != "user2@tenant1.example.test" || target.Identity != "graph-2@tenant-1" || target.Current {
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

func TestRefreshesAreSerializedPerConnection(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	ownerA := provision(t, app, "opa@bachtelcorporation.com")
	ownerB := provision(t, app, "opb@bachtelcorporation.com")

	connA := connectNow(t, svc, ownerA)
	mock.setIdentity("graph-2", "user2@tenant1.example.test")
	connB := connectNow(t, svc, ownerB)

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

	connA := connectNow(t, svc, ownerA)
	mock.setIdentity("graph-2", "user2@tenant1.example.test")
	connB := connectNow(t, svc, ownerB)

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

func TestUnreadableCiphertextIsActionableAndNeverOverwritten(t *testing.T) {
	app := bootApp(t)
	mock := newMockMicrosoft(t, "graph-1", "user1@tenant1.example.test")
	cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
	svc, store := newService(t, app, cfg)
	owner := provision(t, app, "op1@bachtelcorporation.com")

	conn := connectNow(t, svc, owner)

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
