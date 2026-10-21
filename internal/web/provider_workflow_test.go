package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

const (
	// secondOperator* identifies the operator who runs in their own Entra
	// tenant with their own application registration.
	secondOperatorEmail    = "second@bachtelcorporation.com"
	secondOperatorPassword = "second-horse-battery"
	// rotatedClientID and rotatedSecret stand in for a refreshed application
	// registration in the same tenant.
	rotatedClientID = "55555555-5555-5555-5555-555555555555"
	rotatedSecret   = "rotated-client-secret-value"
	secondTenantID  = "33333333-3333-3333-3333-333333333333"
	secondClientID  = "44444444-4444-4444-4444-444444444444"
	secondSecret    = "second-client-secret-value"
)

func provisionSecondOperator(env *acceptanceEnv) {
	env.t.Helper()
	if err := operator.Provision(env.app, secondOperatorEmail, secondOperatorPassword); err != nil {
		env.t.Fatal(err)
	}
}

func (env *acceptanceEnv) signInAs(email, password string) *http.Cookie {
	env.t.Helper()
	rec := env.request(http.MethodPost, "/login", nil, url.Values{"email": {email}, "password": {password}})
	if rec.Code != http.StatusSeeOther {
		env.t.Fatalf("login %s: status=%d body=%.300s", email, rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "operator_session" && cookie.Value != "" {
			return cookie
		}
	}
	env.t.Fatalf("login %s did not set the operator session cookie", email)
	return nil
}

// connectMicrosoft drives the real connect + callback routes with the given
// authorization code and returns the callback redirect.
func (env *acceptanceEnv) connectMicrosoft(session *http.Cookie, code string) *httptest.ResponseRecorder {
	env.t.Helper()
	start := env.request(http.MethodPost, "/connection/microsoft/connect", []*http.Cookie{session}, url.Values{})
	if start.Code != http.StatusSeeOther {
		env.t.Fatalf("connect: status=%d body=%.300s", start.Code, start.Body.String())
	}
	authorizeURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil || authorizeURL.Host == "" {
		env.t.Fatalf("connect location %q", start.Header().Get("Location"))
	}
	state := authorizeURL.Query().Get("state")
	if state == "" {
		env.t.Fatalf("connect location lacks state: %q", authorizeURL.String())
	}
	return env.request(http.MethodGet, "/connection/callback?state="+url.QueryEscape(state)+"&code="+url.QueryEscape(code), []*http.Cookie{session}, nil)
}

func (env *acceptanceEnv) connection(t *testing.T, ownerID string) *mailbox.Connection {
	t.Helper()
	connection, err := env.mailboxes.Current(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("load connection: %v", err)
	}
	return connection
}

// lastGrant returns the most recent token request for a grant type.
func (env *acceptanceEnv) lastGrant(t *testing.T, grant string) tokenRegistration {
	t.Helper()
	env.mu.Lock()
	defer env.mu.Unlock()
	for _, entry := range slices.Backward(env.tokenMeta) {
		if entry.Grant == grant {
			return entry
		}
	}
	t.Fatalf("no %s token request was recorded", grant)
	return tokenRegistration{}
}

// expireCredential rewrites the stored credential to an expired access token,
// keeping the connection generation and settings generation untouched.
func (env *acceptanceEnv) expireCredential(t *testing.T, connection *mailbox.Connection) {
	t.Helper()
	credential, err := env.mailboxes.Credential(context.Background(), connection)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.mailboxes.Reconnect(context.Background(), connection, mailbox.Credential{
		Provider:     mailbox.ProviderMicrosoft,
		AccessToken:  "expired-" + credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
}

// Two operators save their own Entra registrations in different tenants,
// connect independently, refresh with the registration that issued their
// tokens, rotate that registration without losing mailbox identity, and stay
// isolated when a setup change fails.
func TestProviderWorkflowAcrossIndependentRegistrations(t *testing.T) {
	env := newAcceptanceEnv(t)
	provisionSecondOperator(env)
	sessionA := env.signIn()
	sessionB := env.signInAs(secondOperatorEmail, secondOperatorPassword)

	env.saveMicrosoftSettings(sessionA, testTenantID, testClientID, testSecret)
	if rec := env.connectMicrosoft(sessionA, "code-a"); rec.Header().Get("Location") != "/connection" {
		t.Fatalf("operator A connect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	env.saveMicrosoftSettings(sessionB, secondTenantID, secondClientID, secondSecret)
	if rec := env.connectMicrosoft(sessionB, "code-b-alias"); rec.Header().Get("Location") != "/connection" {
		t.Fatalf("operator B connect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	ownerA := operatorIDFor(t, env.app, testEmail)
	ownerB := operatorIDFor(t, env.app, secondOperatorEmail)
	connectionA := env.connection(t, ownerA)
	connectionB := env.connection(t, ownerB)
	if connectionA.ID == connectionB.ID {
		t.Fatal("both operators share one connection")
	}
	if connectionA.Identity != "account-code-a@"+testTenantID {
		t.Fatalf("operator A identity = %q", connectionA.Identity)
	}
	if connectionB.Identity != "account-code-b-alias@"+secondTenantID {
		t.Fatalf("operator B identity = %q", connectionB.Identity)
	}

	// Each authorization-code exchange used that operator's own registration.
	env.mu.Lock()
	exchanges := append([]tokenRegistration(nil), env.tokenMeta...)
	env.mu.Unlock()
	if len(exchanges) != 2 {
		t.Fatalf("expected two authorization-code exchanges, got %d", len(exchanges))
	}
	if exchanges[0].Tenant != testTenantID || exchanges[0].ClientID != testClientID || exchanges[0].ClientSecret != testSecret {
		t.Fatalf("operator A exchange used %+v", exchanges[0])
	}
	if exchanges[1].Tenant != secondTenantID || exchanges[1].ClientID != secondClientID || exchanges[1].ClientSecret != secondSecret {
		t.Fatalf("operator B exchange used %+v", exchanges[1])
	}

	// A different registration never widens the mailbox claim: B signing in
	// with A's mailbox is refused without disclosing A.
	env.saveMicrosoftSettings(sessionB, secondTenantID, secondClientID, secondSecret)
	if rec := env.connectMicrosoft(sessionB, "code-shared"); rec.Header().Get("Location") != "/connection?notice=claimed" {
		t.Fatalf("cross-operator claim: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	if current := env.connection(t, ownerB); current.ID != connectionB.ID || current.Status != mailbox.StatusConnected {
		t.Fatalf("refused claim changed operator B's connection: %+v", current)
	}

	// A restart keeps both saved registrations and both usable connections.
	if err := env.app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	env.start(t, false)
	sessionA = env.signIn()
	sessionB = env.signInAs(secondOperatorEmail, secondOperatorPassword)
	for _, session := range []*http.Cookie{sessionA, sessionB} {
		page := env.request(http.MethodGet, "/connection", []*http.Cookie{session}, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-status="connected"`) {
			t.Fatalf("connection page after restart: status=%d body=%.400s", page.Code, page.Body.String())
		}
	}

	// Refresh uses the saved registration that issued the stored token.
	connectionA = env.connection(t, ownerA)
	env.expireCredential(t, connectionA)
	service := microsoft.NewService(env.app, env.cfg, env.mailboxes)
	if _, err := service.AccessToken(context.Background(), connectionA); err != nil {
		t.Fatalf("refresh with the saved registration: %v", err)
	}
	if grant := env.lastGrant(t, "refresh_token"); grant.Tenant != testTenantID || grant.ClientID != testClientID || grant.ClientSecret != testSecret {
		t.Fatalf("refresh used %+v", grant)
	}

	// Rotating the application registration for the same mailbox keeps the
	// mailbox generation and replaces the registration only with the tokens
	// the new sign-in produced.
	env.saveMicrosoftSettings(sessionA, testTenantID, rotatedClientID, rotatedSecret)
	if rec := env.connectMicrosoft(sessionA, "code-a"); rec.Header().Get("Location") != "/connection" {
		t.Fatalf("rotated connect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	rotated := env.connection(t, ownerA)
	if rotated.ID != connectionA.ID {
		t.Fatal("rotation replaced the mailbox connection")
	}
	if rotated.Generation != 1 {
		t.Fatalf("mailbox generation = %d, want 1 (approvals stay applicable)", rotated.Generation)
	}
	if rotated.SettingsGeneration != 2 {
		t.Fatalf("settings generation = %d, want 2", rotated.SettingsGeneration)
	}
	settings, err := env.mailboxes.Settings(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClientID != rotatedClientID || settings.HasPending() {
		t.Fatalf("active registration after rotation = %+v", settings)
	}
	env.expireCredential(t, rotated)
	if _, err := service.AccessToken(context.Background(), env.connection(t, ownerA)); err != nil {
		t.Fatalf("refresh after rotation: %v", err)
	}
	if grant := env.lastGrant(t, "refresh_token"); grant.Tenant != testTenantID || grant.ClientID != rotatedClientID || grant.ClientSecret != rotatedSecret {
		t.Fatalf("refresh after rotation used %+v", grant)
	}

	// An invalid setup change for B fails alone: neither connection and no
	// saved registration changes.
	beforeB := env.connection(t, ownerB)
	rec := env.request(http.MethodPost, "/connection/microsoft/settings", []*http.Cookie{sessionB}, url.Values{
		"tenant_id": {"organizations"}, "client_id": {secondClientID}, "client_secret": {secondSecret},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Tenant ID must be the directory UUID") {
		t.Fatalf("invalid setup: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	if after := env.connection(t, ownerB); after.ID != beforeB.ID || after.SettingsGeneration != beforeB.SettingsGeneration {
		t.Fatalf("invalid setup changed operator B's connection: %+v", after)
	}
	if after := env.connection(t, ownerA); after.SettingsGeneration != 2 {
		t.Fatalf("operator A's registration changed from B's failure: %+v", after)
	}
	if rec := env.request(http.MethodGet, "/connection", []*http.Cookie{sessionA}, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), rotatedClientID) {
		t.Fatalf("operator A's saved registration is not shown: status=%d", rec.Code)
	}

	// A different verified mailbox requires the explicit replacement flow; a
	// cancelled proposal leaves the connection untouched.
	replacement := env.connectMicrosoft(sessionA, "code-other-alias")
	if replacement.Header().Get("Location") != "/connection/replace" {
		t.Fatalf("replacement staging: status=%d location=%q", replacement.Code, replacement.Header().Get("Location"))
	}
	page := env.request(http.MethodGet, "/connection/replace", []*http.Cookie{sessionA}, nil)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "Permanently deleted") || !strings.Contains(body, "alias-code-other") {
		t.Fatalf("replacement page: status=%d body=%.600s", page.Code, body)
	}
	reset, err := env.mailboxes.ActiveReset(context.Background(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if rec := env.request(http.MethodPost, "/connection/replace/cancel", []*http.Cookie{sessionA}, url.Values{"reset": {reset.ID}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("cancel replacement: status=%d", rec.Code)
	}
	if current := env.connection(t, ownerA); current.ID != rotated.ID || current.Sender != rotated.Sender {
		t.Fatalf("cancelled replacement changed the connection: %+v", current)
	}

	// The other operator is untouched by all of A's lifecycle work.
	if other := env.connection(t, ownerB); other.ID != connectionB.ID || other.Status != mailbox.StatusConnected {
		t.Fatalf("operator B was affected: %+v", other)
	}
}

// A missing or unreadable registration blocks only refresh for that mailbox;
// another operator's saved registration keeps working.
func TestProviderWorkflowRefreshRefusesSupersededRegistration(t *testing.T) {
	env := newAcceptanceEnv(t)
	session := env.signIn()
	env.saveMicrosoftSettings(session, testTenantID, testClientID, testSecret)
	if rec := env.connectMicrosoft(session, "code-a"); rec.Header().Get("Location") != "/connection" {
		t.Fatalf("connect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	owner := operatorIDFor(t, env.app, testEmail)
	connection := env.connection(t, owner)

	// Stage a registration change without completing a sign-in: the stored
	// tokens still belong to the active generation, so refresh keeps working.
	staged, err := env.mailboxes.SaveSettings(context.Background(), owner, mailbox.ProviderMicrosoft, testTenantID, rotatedClientID, rotatedSecret)
	if err != nil {
		t.Fatal(err)
	}
	env.expireCredential(t, connection)
	service := microsoft.NewService(env.app, env.cfg, env.mailboxes)
	if _, err := service.AccessToken(context.Background(), env.connection(t, owner)); err != nil {
		t.Fatalf("refresh while a change is staged: %v", err)
	}
	if grant := env.lastGrant(t, "refresh_token"); grant.ClientID != testClientID {
		t.Fatalf("refresh used the staged registration: %+v", grant)
	}

	// A connection issued under a superseded generation must not refresh.
	record, err := env.app.FindRecordById(mailbox.ConnectionsCollection, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("settings_generation", staged+1)
	if err := env.app.Save(record); err != nil {
		t.Fatal(err)
	}
	superseded := env.connection(t, owner)
	env.expireCredential(t, superseded)
	if _, err := service.AccessToken(context.Background(), superseded); !errors.Is(err, mailbox.ErrSettingsStale) {
		t.Fatalf("superseded registration refresh = %v, want ErrSettingsStale", err)
	}
	current := env.connection(t, owner)
	if current.Status != mailbox.StatusReconnectRequired {
		t.Fatalf("connection status = %q, want reconnect_required", current.Status)
	}
}
