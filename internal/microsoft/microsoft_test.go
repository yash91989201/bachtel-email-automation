package microsoft

import (
	"context"
	"crypto/rand"
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
	"golang.org/x/oauth2"
)

const testOperator = "op-1"

// bootApp opens a PocketBase app on dir and ensures the connection collection
// exists. It mirrors the connection creation and credential expansion migrations;
// the real migration package cannot be imported here without an import cycle.
func bootApp(t *testing.T, dir string) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = app.ClearBootstrap()
	})
	if _, err := app.FindCollectionByNameOrId(CollectionName); err != nil {
		collection := core.NewBaseCollection(CollectionName)
		collection.Fields.Add(
			&core.TextField{Name: "status", Required: true, Max: 32},
			&core.TextField{Name: "account_id", Max: 255},
			&core.TextField{Name: "account_email", Max: 255},
			&core.TextField{Name: "tenant_id", Max: 255},
			&core.TextField{Name: "last_error", Max: 255},
			&core.TextField{Name: "credential", Max: 1 << 20},
			&core.DateField{Name: "credential_expires_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(collection); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

// testEnv points the Microsoft settings at the given mock servers and sets a
// random encryption key.
func testEnv(t *testing.T, authorityURL, graphBaseURL string) config.Config {
	t.Helper()
	key := make([]byte, config.EncryptionKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvEncryptionKey, base64.StdEncoding.EncodeToString(key))
	t.Setenv(config.EnvTenantID, "tenant-1")
	t.Setenv(config.EnvClientID, "client-1")
	t.Setenv(config.EnvClientSecret, "client-secret-1")
	t.Setenv(config.EnvRedirectURL, "https://mail.example.test/connection/callback")
	t.Setenv(config.EnvSenderAddress, "hr@bachtelcorporation.com")
	t.Setenv(config.EnvAuthorityURL, authorityURL)
	t.Setenv(config.EnvGraphBaseURL, graphBaseURL)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// mockMicrosoft stands in for the Microsoft token endpoint and Graph /me.
type mockMicrosoft struct {
	mu               sync.Mutex
	refreshTokens    int
	omitRefreshToken bool
	rejectRefresh    bool
	meID             string
	meMail           string
	meUPN            string
	expectBearer     string
	srv              *httptest.Server
}

func newMockMicrosoft(t *testing.T) *mockMicrosoft {
	t.Helper()
	m := &mockMicrosoft{}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockMicrosoft) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
		if err := r.ParseForm(); err != nil {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") == "" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"at-c1","refresh_token":"rt-c1","token_type":"Bearer","expires_in":3600}`)
		case "refresh_token":
			m.refreshTokens++
			if m.rejectRefresh {
				http.Error(w, `{"error":"invalid_grant","error_description":"token revoked"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			n := m.refreshTokens
			if m.omitRefreshToken {
				fmt.Fprintf(w, `{"access_token":"at-r%d","token_type":"Bearer","expires_in":3600}`, n)
			} else {
				fmt.Fprintf(w, `{"access_token":"at-r%d","refresh_token":"rt-r%d","token_type":"Bearer","expires_in":3600}`, n, n)
			}
		default:
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
		}
	case strings.HasSuffix(r.URL.Path, "/me"):
		if m.expectBearer != "" && r.Header.Get("Authorization") != "Bearer "+m.expectBearer {
			http.Error(w, `{"error":{"code":"InvalidAuthenticationToken"}}`, http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"id":%q,"mail":%q,"userPrincipalName":%q}`, m.meID, m.meMail, m.meUPN)
	default:
		http.NotFound(w, r)
	}
}

func (m *mockMicrosoft) refreshCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshTokens
}

func stateFromURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("authorization URL has no state")
	}
	return state
}

func connectionRecord(t *testing.T, app core.App) *core.Record {
	t.Helper()
	record, err := app.FindFirstRecordByFilter(CollectionName, "1=1")
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func countConnections(t *testing.T, app core.App) int64 {
	t.Helper()
	total, err := app.CountRecords(CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// connectNow runs BeginAuth + CompleteAuth against the mock, which must
// already answer with the configured identity.
func connectNow(t *testing.T, svc *Service, operator string) Connection {
	t.Helper()
	raw, err := svc.BeginAuth(context.Background(), operator)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := svc.CompleteAuth(context.Background(), operator, stateFromURL(t, raw), "code-1")
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

// overwriteCredential replaces the stored token, e.g. to make it expired.
func overwriteCredential(t *testing.T, app core.App, svc *Service, token oauth2.Token) string {
	t.Helper()
	credential, err := svc.sealToken(&token)
	if err != nil {
		t.Fatal(err)
	}
	record := connectionRecord(t, app)
	record.Set("credential", string(credential))
	record.Set("credential_expires_at", token.Expiry)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return string(credential)
}

func TestConfigError(t *testing.T) {
	svc := &Service{cfg: testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0")}
	if err := svc.ConfigError(); err != nil {
		t.Fatalf("complete settings: unexpected error %v", err)
	}

	t.Setenv(config.EnvTenantID, "")
	t.Setenv(config.EnvClientID, "")
	t.Setenv(config.EnvClientSecret, "   ")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg = cfg
	err = svc.ConfigError()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
	for _, name := range []string{config.EnvTenantID, config.EnvClientID, config.EnvClientSecret} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestBeginAuthURL(t *testing.T) {
	cfg := testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0")
	svc := NewService(bootApp(t, t.TempDir()), cfg)

	raw, err := svc.BeginAuth(context.Background(), testOperator)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "login.example.test" || parsed.Path != "/tenant-1/oauth2/v2.0/authorize" {
		t.Fatalf("unexpected authorize target %s%s", parsed.Host, parsed.Path)
	}
	q := parsed.Query()
	if q.Get("client_id") != "client-1" || q.Get("redirect_uri") != cfg.Microsoft.RedirectURL || q.Get("response_type") != "code" {
		t.Fatalf("unexpected auth params: %v", q)
	}
	if got, want := q.Get("scope"), "offline_access https://graph.microsoft.com/Mail.Send https://graph.microsoft.com/User.Read"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if q.Get("access_type") != "offline" || q.Get("login_hint") != cfg.Microsoft.SenderAddress {
		t.Fatalf("unexpected access_type/login_hint: %v", q)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE challenge: %v", q)
	}
	state := q.Get("state")
	svc.mu.Lock()
	attempt, ok := svc.attempts[state]
	svc.mu.Unlock()
	if !ok {
		t.Fatalf("state %q not stored", state)
	}
	if attempt.operatorID != testOperator {
		t.Fatalf("attempt bound to %q, want %q", attempt.operatorID, testOperator)
	}
	sum := sha256.Sum256([]byte(attempt.verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); q.Get("code_challenge") != want {
		t.Fatalf("challenge = %q, want S256 of the stored verifier", q.Get("code_challenge"))
	}
}

func TestBeginAuthGuards(t *testing.T) {
	svc := &Service{cfg: testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0"), attempts: make(map[string]pendingAttempt)}
	t.Setenv(config.EnvClientID, "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg = cfg
	if _, err := svc.BeginAuth(context.Background(), testOperator); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured: got %v, want ErrNotConfigured", err)
	}
	t.Setenv(config.EnvClientID, "client-1")
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg = cfg

	svc.SetDispatchGuard(stubGuard{n: 2})
	if _, err := svc.BeginAuth(context.Background(), testOperator); !errors.Is(err, ErrInFlight) {
		t.Fatalf("in-flight: got %v, want ErrInFlight", err)
	}
	svc.mu.Lock()
	pending := len(svc.attempts)
	svc.mu.Unlock()
	if pending != 0 {
		t.Fatalf("%d attempts stored, want 0", pending)
	}
}

func TestCompleteAuthStateHandling(t *testing.T) {
	ctx := context.Background()
	t.Run("unknown state", func(t *testing.T) {
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0"))
		if _, err := svc.CompleteAuth(ctx, testOperator, "no-such-state", "code"); !errors.Is(err, ErrAuthorizationState) {
			t.Fatalf("got %v, want ErrAuthorizationState", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})

	t.Run("expired attempt", func(t *testing.T) {
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0"))
		svc.attempts["stale"] = pendingAttempt{operatorID: testOperator, verifier: "v", expiresAt: time.Now().Add(-time.Minute)}
		if _, err := svc.CompleteAuth(ctx, testOperator, "stale", "code"); !errors.Is(err, ErrAuthorizationState) {
			t.Fatalf("got %v, want ErrAuthorizationState", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})

	t.Run("operator mismatch", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, mock.srv.URL, mock.srv.URL))
		raw, err := svc.BeginAuth(ctx, "op-1")
		if err != nil {
			t.Fatal(err)
		}
		state := stateFromURL(t, raw)
		if _, err := svc.CompleteAuth(ctx, "op-2", state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
			t.Fatalf("got %v, want ErrAuthorizationState", err)
		}
		// The attempt is consumed even on mismatch: the real operator cannot
		// complete it either.
		if _, err := svc.CompleteAuth(ctx, testOperator, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
			t.Fatalf("replay: got %v, want ErrAuthorizationState", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})

	t.Run("replay after failing exchange", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, mock.srv.URL, mock.srv.URL))
		raw, err := svc.BeginAuth(ctx, testOperator)
		if err != nil {
			t.Fatal(err)
		}
		state := stateFromURL(t, raw)
		// Empty code makes the token endpoint answer invalid_grant, so the
		// attempt is consumed on a later-step failure.
		if _, err := svc.CompleteAuth(ctx, testOperator, state, ""); err == nil {
			t.Fatal("expected exchange failure")
		}
		if _, err := svc.CompleteAuth(ctx, testOperator, state, "code-1"); !errors.Is(err, ErrAuthorizationState) {
			t.Fatalf("replay: got %v, want ErrAuthorizationState", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})
}

func TestCompleteAuthIdentity(t *testing.T) {
	ctx := context.Background()
	const sender = "hr@bachtelcorporation.com"

	t.Run("matching mailbox connects", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meID, mock.meUPN, mock.expectBearer = "aid-1", "HR@BachtelCorporation.com", "at-c1"
		cfg := testEnv(t, mock.srv.URL, mock.srv.URL)
		app := bootApp(t, t.TempDir())
		svc := NewService(app, cfg)

		conn := connectNow(t, svc, testOperator)
		if conn.Status != StatusConnected || conn.AccountID != "aid-1" || conn.AccountEmail != sender || conn.TenantID != "tenant-1" {
			t.Fatalf("unexpected connection %+v", conn)
		}
		if conn.ExpiresAt.IsZero() || conn.ConnectedAt.IsZero() {
			t.Fatalf("expiry/connected timestamps not set: %+v", conn)
		}
		if conn.SenderAddress != sender {
			t.Fatalf("sender = %q, want %q", conn.SenderAddress, sender)
		}
		record := connectionRecord(t, app)
		if record.GetString("status") != string(StatusConnected) || record.GetString("account_id") != "aid-1" ||
			record.GetString("account_email") != sender || record.GetString("tenant_id") != "tenant-1" ||
			record.GetString("last_error") != "" || !strings.HasPrefix(record.GetString("credential"), "v1.") {
			t.Fatalf("record not persisted correctly: %+v", record.FieldsData())
		}
	})

	t.Run("mail fallback connects", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meID, mock.meMail = "aid-2", sender
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, mock.srv.URL, mock.srv.URL))
		conn := connectNow(t, svc, testOperator)
		if conn.Status != StatusConnected || conn.AccountEmail != sender {
			t.Fatalf("unexpected connection %+v", conn)
		}
	})

	t.Run("different mailbox installs nothing", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meID, mock.meUPN = "aid-x", "someone@evil.test"
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, mock.srv.URL, mock.srv.URL))
		raw, err := svc.BeginAuth(ctx, testOperator)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CompleteAuth(ctx, testOperator, stateFromURL(t, raw), "code-1"); !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("got %v, want ErrIdentityMismatch", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})

	t.Run("empty account id rejected", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meUPN = sender // id stays empty
		svc := NewService(bootApp(t, t.TempDir()), testEnv(t, mock.srv.URL, mock.srv.URL))
		raw, err := svc.BeginAuth(ctx, testOperator)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CompleteAuth(ctx, testOperator, stateFromURL(t, raw), "code-1"); !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("got %v, want ErrIdentityMismatch", err)
		}
		if total := countConnections(t, svc.app); total != 0 {
			t.Fatalf("%d records installed, want 0", total)
		}
	})

	t.Run("mismatch leaves existing connection byte-identical", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meID, mock.meUPN = "aid-1", sender
		app := bootApp(t, t.TempDir())
		svc := NewService(app, testEnv(t, mock.srv.URL, mock.srv.URL))
		connectNow(t, svc, testOperator)

		record := connectionRecord(t, app)
		credentialBefore := record.GetString("credential")

		mock.mu.Lock()
		mock.meID, mock.meUPN = "aid-x", "someone@evil.test"
		mock.mu.Unlock()

		raw, err := svc.BeginAuth(ctx, testOperator)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CompleteAuth(ctx, testOperator, stateFromURL(t, raw), "code-1"); !errors.Is(err, ErrIdentityMismatch) {
			t.Fatalf("got %v, want ErrIdentityMismatch", err)
		}
		record = connectionRecord(t, app)
		if record.GetString("credential") != credentialBefore {
			t.Fatal("credential column changed on identity mismatch")
		}
		if record.GetString("status") != string(StatusConnected) {
			t.Fatalf("status = %q, want connected", record.GetString("status"))
		}
	})
}

func TestStatusReconnectRequiredRecord(t *testing.T) {
	app := bootApp(t, t.TempDir())
	svc := NewService(app, testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0"))

	conn, err := svc.Status(context.Background())
	if err != nil || conn.Status != StatusDisconnected || conn.SenderAddress != "hr@bachtelcorporation.com" {
		t.Fatalf("no record: got %+v err %v", conn, err)
	}

	collection, err := app.FindCollectionByNameOrId(CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("status", string(StatusReconnectRequired))
	record.Set("last_error", "Microsoft rejected the stored refresh token; reconnect the mailbox")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	conn, err = svc.Status(context.Background())
	if err != nil || conn.Status != StatusReconnectRequired || conn.LastError == "" {
		t.Fatalf("reconnect record: got %+v err %v", conn, err)
	}
}

func TestEncryption(t *testing.T) {
	ctx := context.Background()

	t.Run("seal open round trip", func(t *testing.T) {
		svc := &Service{cfg: testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0")}
		in := &oauth2.Token{AccessToken: "at-1", RefreshToken: "rt-1", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour).UTC().Truncate(time.Second)}
		sealed, err := svc.sealToken(in)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(sealed), "v1.") {
			t.Fatalf("blob %q lacks version prefix", sealed)
		}
		plaintext, err := svc.openCredential(string(sealed))
		if err != nil {
			t.Fatal(err)
		}
		var out oauth2.Token
		if err := json.Unmarshal(plaintext, &out); err != nil {
			t.Fatal(err)
		}
		if out.AccessToken != in.AccessToken || out.RefreshToken != in.RefreshToken || !out.Expiry.Equal(in.Expiry) {
			t.Fatalf("round trip mismatch: %+v", out)
		}
	})

	t.Run("tamper and truncation fail", func(t *testing.T) {
		svc := &Service{cfg: testEnv(t, "https://login.example.test", "https://graph.example.test/v1.0")}
		sealed, err := svc.sealToken(&oauth2.Token{AccessToken: "at-1"})
		if err != nil {
			t.Fatal(err)
		}
		tampered := []byte(string(sealed))
		tampered[len(tampered)-1] ^= 1
		if _, err := svc.openCredential(string(tampered)); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("tampered: got %v, want ErrCredentialsUnreadable", err)
		}
		if _, err := svc.openCredential(string(sealed[:len(sealed)-8])); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("truncated: got %v, want ErrCredentialsUnreadable", err)
		}
		if _, err := svc.openCredential("v2.AAA="); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("wrong version: got %v, want ErrCredentialsUnreadable", err)
		}
	})

	t.Run("wrong and missing key leave record untouched", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.meID, mock.meUPN = "aid-1", "hr@bachtelcorporation.com"
		app := bootApp(t, t.TempDir())
		svc := NewService(app, testEnv(t, mock.srv.URL, mock.srv.URL))
		connectNow(t, svc, testOperator)
		before := connectionRecord(t, app).GetString("credential")

		other := make([]byte, config.EncryptionKeySize)
		if _, err := rand.Read(other); err != nil {
			t.Fatal(err)
		}
		t.Setenv(config.EnvEncryptionKey, base64.StdEncoding.EncodeToString(other))

		if _, err := svc.AccessToken(ctx); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("wrong key: got %v, want ErrCredentialsUnreadable", err)
		}
		conn, err := svc.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if conn.Status != StatusReconnectRequired || !strings.Contains(conn.LastError, config.EnvEncryptionKey) {
			t.Fatalf("status = %+v, want reconnect_required naming the key setting", conn)
		}
		record := connectionRecord(t, app)
		if record.GetString("credential") != before || record.GetString("status") != string(StatusConnected) {
			t.Fatal("record was modified while credentials were unreadable")
		}

		t.Setenv(config.EnvEncryptionKey, "")
		if _, err := svc.AccessToken(ctx); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("missing key: got %v, want ErrCredentialsUnreadable", err)
		}
		if _, err := svc.sealToken(&oauth2.Token{AccessToken: "x"}); !errors.Is(err, ErrCredentialsUnreadable) {
			t.Fatalf("seal with missing key: got %v, want ErrCredentialsUnreadable", err)
		}
		if connectionRecord(t, app).GetString("credential") != before {
			t.Fatal("credential column changed")
		}
	})

	t.Run("restart recovery", func(t *testing.T) {
		dir := t.TempDir()
		mock := newMockMicrosoft(t)
		mock.meID, mock.meUPN = "aid-1", "hr@bachtelcorporation.com"
		cfg := testEnv(t, mock.srv.URL, mock.srv.URL)

		app1 := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir})
		if err := app1.Bootstrap(); err != nil {
			t.Fatal(err)
		}
		{
			collection := core.NewBaseCollection(CollectionName)
			collection.Fields.Add(
				&core.TextField{Name: "status", Required: true, Max: 32},
				&core.TextField{Name: "account_id", Max: 255},
				&core.TextField{Name: "account_email", Max: 255},
				&core.TextField{Name: "tenant_id", Max: 255},
				&core.TextField{Name: "last_error", Max: 255},
				&core.TextField{Name: "credential", Max: 1 << 20},
				&core.DateField{Name: "credential_expires_at"},
				&core.AutodateField{Name: "created", OnCreate: true},
				&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
			)
			if err := app1.Save(collection); err != nil {
				t.Fatal(err)
			}
		}
		connectNow(t, NewService(app1, cfg), testOperator)
		if err := app1.ClearBootstrap(); err != nil {
			t.Fatal(err)
		}

		// Second process on the same data dir: same key, no new sign-in.
		app2 := bootApp(t, dir)
		svc2 := NewService(app2, cfg)
		conn, err := svc2.Status(ctx)
		if err != nil || conn.Status != StatusConnected {
			t.Fatalf("after restart: %+v err %v", conn, err)
		}
		token, err := svc2.AccessToken(ctx)
		if err != nil || token != "at-c1" {
			t.Fatalf("after restart: token %q err %v", token, err)
		}
		if n := mock.refreshCount(); n != 0 {
			t.Fatalf("restart triggered %d refreshes, want 0", n)
		}
	})
}

func TestRefresh(t *testing.T) {
	ctx := context.Background()
	const sender = "hr@bachtelcorporation.com"

	newConnected := func(t *testing.T, mock *mockMicrosoft) (*pocketbase.PocketBase, *Service) {
		t.Helper()
		mock.meID, mock.meUPN = "aid-1", sender
		app := bootApp(t, t.TempDir())
		svc := NewService(app, testEnv(t, mock.srv.URL, mock.srv.URL))
		connectNow(t, svc, testOperator)
		return app, svc
	}

	t.Run("concurrent refresh happens exactly once", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		app, svc := newConnected(t, mock)
		overwriteCredential(t, app, svc, oauth2.Token{
			AccessToken: "at-old", RefreshToken: "rt-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour),
		})

		const goroutines = 6
		var wg sync.WaitGroup
		results := make([]string, goroutines)
		errs := make([]error, goroutines)
		for i := range goroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = svc.AccessToken(ctx)
			}()
		}
		wg.Wait()
		for i := range goroutines {
			if errs[i] != nil || results[i] != "at-r1" {
				t.Fatalf("goroutine %d: token %q err %v, want at-r1", i, results[i], errs[i])
			}
		}
		if n := mock.refreshCount(); n != 1 {
			t.Fatalf("%d refresh requests, want 1", n)
		}

		record := connectionRecord(t, app)
		plaintext, err := svc.openCredential(record.GetString("credential"))
		if err != nil {
			t.Fatal(err)
		}
		var stored oauth2.Token
		if err := json.Unmarshal(plaintext, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.AccessToken != "at-r1" || stored.RefreshToken != "rt-r1" || !stored.Expiry.After(time.Now()) {
			t.Fatalf("rotated token not persisted: %+v", stored)
		}
		if record.GetDateTime("credential_expires_at").Time().Before(time.Now()) {
			t.Fatal("expiry not persisted")
		}

		// Next call is served from storage without a network round trip.
		token, err := svc.AccessToken(ctx)
		if err != nil || token != "at-r1" {
			t.Fatalf("token %q err %v", token, err)
		}
		if n := mock.refreshCount(); n != 1 {
			t.Fatalf("%d refresh requests after cached call, want 1", n)
		}
	})

	t.Run("omitted large refresh token keeps previous", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.omitRefreshToken = true
		app, svc := newConnected(t, mock)
		previousRefresh := strings.Repeat("rt-old", 2048)
		overwriteCredential(t, app, svc, oauth2.Token{
			AccessToken: "at-old", RefreshToken: previousRefresh, TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour),
		})
		if token, err := svc.AccessToken(ctx); err != nil || token != "at-r1" {
			t.Fatalf("token %q err %v", token, err)
		}
		plaintext, err := svc.openCredential(connectionRecord(t, app).GetString("credential"))
		if err != nil {
			t.Fatal(err)
		}
		var stored oauth2.Token
		if err := json.Unmarshal(plaintext, &stored); err != nil {
			t.Fatal(err)
		}
		if stored.RefreshToken != previousRefresh {
			t.Fatal("previous large refresh token was not kept")
		}
		if len(connectionRecord(t, app).GetString("credential")) <= 5000 {
			t.Fatal("refreshed credential must exceed the old storage limit")
		}
		if token, err := NewService(app, svc.cfg).AccessToken(ctx); err != nil || token != "at-r1" {
			t.Fatalf("large refreshed credential was not persisted correctly (err=%v)", err)
		}
	})

	t.Run("invalid grant requires reconnect", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		mock.rejectRefresh = true
		app, svc := newConnected(t, mock)
		before := overwriteCredential(t, app, svc, oauth2.Token{
			AccessToken: "at-old", RefreshToken: "rt-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour),
		})
		if _, err := svc.AccessToken(ctx); err == nil {
			t.Fatal("expected refresh failure")
		}
		record := connectionRecord(t, app)
		if record.GetString("status") != string(StatusReconnectRequired) || record.GetString("last_error") == "" {
			t.Fatalf("record = %+v, want reconnect_required with a reason", record.FieldsData())
		}
		if record.GetString("credential") != before {
			t.Fatal("ciphertext changed on rejected refresh")
		}
	})

	t.Run("persistence failure returns error not token", func(t *testing.T) {
		mock := newMockMicrosoft(t)
		app, svc := newConnected(t, mock)
		overwriteCredential(t, app, svc, oauth2.Token{
			AccessToken: "at-old", RefreshToken: "rt-old", TokenType: "Bearer", Expiry: time.Now().Add(-time.Hour),
		})
		failing := &Service{app: failingSaveApp{app}, cfg: svc.cfg, attempts: make(map[string]pendingAttempt)}
		token, err := failing.AccessToken(ctx)
		if err == nil || token != "" {
			t.Fatalf("got token %q err %v, want error and empty token", token, err)
		}
		if n := mock.refreshCount(); n != 1 {
			t.Fatalf("%d refresh requests, want 1", n)
		}
	})
}

func TestDisconnect(t *testing.T) {
	ctx := context.Background()
	mock := newMockMicrosoft(t)
	mock.meID, mock.meUPN = "aid-1", "hr@bachtelcorporation.com"
	app := bootApp(t, t.TempDir())
	svc := NewService(app, testEnv(t, mock.srv.URL, mock.srv.URL))
	connectNow(t, svc, testOperator)

	svc.SetDispatchGuard(stubGuard{n: 1})
	err := svc.Disconnect(ctx)
	if !errors.Is(err, ErrInFlight) || !strings.Contains(err.Error(), "1 in flight") {
		t.Fatalf("got %v, want ErrInFlight naming the count", err)
	}
	if total := countConnections(t, app); total != 1 {
		t.Fatalf("%d records after blocked disconnect, want 1", total)
	}

	svc.SetDispatchGuard(nil)
	if err := svc.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if total := countConnections(t, app); total != 0 {
		t.Fatalf("%d records after disconnect, want 0", total)
	}
	conn, err := svc.Status(ctx)
	if err != nil || conn.Status != StatusDisconnected {
		t.Fatalf("after disconnect: %+v err %v", conn, err)
	}
	// Idempotent: disconnecting twice is not an error.
	if err := svc.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
}

type stubGuard struct {
	n   int
	err error
}

func (g stubGuard) InFlight(context.Context) (int, error) { return g.n, g.err }

type failingSaveApp struct{ core.App }

func (a failingSaveApp) Save(core.Model) error { return errors.New("simulated disk failure") }
