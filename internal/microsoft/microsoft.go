// Package microsoft owns the single Microsoft 365 mailbox connection: the
// OAuth 2.0 authorization-code flow with PKCE, Microsoft Graph identity
// verification, encrypted credential storage, and token refresh.
//
// Secret material (access tokens, refresh tokens, PKCE verifiers, the client
// secret) never leaves the server. Stored credentials are sealed with
// AES-256-GCM before they touch the database, and errors name settings and
// outcomes, never values.
package microsoft

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"golang.org/x/oauth2"
)

// CollectionName is the PocketBase collection holding the single connection
// record.
const CollectionName = "microsoft_connection"

// Status is the connection state stored on the record and shown to the
// operator.
type Status string

const (
	// StatusDisconnected means no connection record exists yet.
	StatusDisconnected Status = "disconnected"
	// StatusConnected means a verified, decryptable credential is stored.
	StatusConnected Status = "connected"
	// StatusReconnectRequired means stored credentials are unusable (rejected
	// by Microsoft or undecryptable) and the operator must sign in again.
	StatusReconnectRequired Status = "reconnect_required"
)

// Connection is the operator-visible state of the mailbox connection. It
// never carries token values.
type Connection struct {
	Status        Status
	AccountID     string
	AccountEmail  string
	TenantID      string
	SenderAddress string
	ConnectedAt   time.Time
	ExpiresAt     time.Time
	LastError     string
}

// Errors reported by the connection service.
var (
	// ErrNotConfigured wraps ConfigError: the required Microsoft settings are
	// incomplete, so no flow can start.
	ErrNotConfigured = errors.New("microsoft connection is not configured")
	// ErrInFlight guards BeginAuth and Disconnect against racing an active
	// mail dispatch.
	ErrInFlight = errors.New("a mail dispatch is in flight")
	// ErrIdentityMismatch reports a sign-in whose verified mailbox is not the
	// configured sender; nothing is changed when it happens.
	ErrIdentityMismatch = errors.New("the signed-in Microsoft account is not the configured sender mailbox")
	// ErrAuthorizationState reports an unknown, expired, already-used, or
	// operator-mismatched OAuth state parameter.
	ErrAuthorizationState = errors.New("the Microsoft sign-in attempt is unknown, expired, or already used")
	// ErrCredentialsUnreadable reports stored credentials that cannot be
	// decrypted or parsed; the stored ciphertext is never overwritten.
	ErrCredentialsUnreadable = errors.New("the stored Microsoft credentials cannot be read")
)

// DispatchGuard reports how many mail dispatches are currently in flight so
// the connection cannot change underneath an active send.
type DispatchGuard interface {
	InFlight(ctx context.Context) (int, error)
}

const (
	// attemptTTL bounds how long a pending sign-in attempt stays valid.
	attemptTTL = 10 * time.Minute
	// refreshSkew: a stored access token with more than this lifetime left is
	// used as-is; anything closer to expiry is refreshed first.
	refreshSkew = 60 * time.Second
	// tokenAccessTimeout includes waiting for another refresh and its HTTP call.
	tokenAccessTimeout = 20 * time.Second
)

// scopes: offline_access yields the refresh token, Mail.Send is the only
// write scope the dispatcher needs, and User.Read verifies the signed-in
// mailbox against the configured sender address.
var scopes = []string{
	"offline_access",
	"https://graph.microsoft.com/Mail.Send",
	"https://graph.microsoft.com/User.Read",
}

// pendingAttempt is one in-memory OAuth sign-in attempt.
type pendingAttempt struct {
	operatorID string
	verifier   string
	expiresAt  time.Time
}

// Service implements the mailbox connection against a PocketBase app.
type Service struct {
	app core.App
	cfg config.Config

	mu       sync.Mutex
	attempts map[string]pendingAttempt

	// A context-aware semaphore serializes refresh and persistence. Initialize
	// lazily so directly constructed services retain the same safe behavior.
	refreshOnce sync.Once
	refreshSlot chan struct{}

	guardMu sync.RWMutex
	guard   DispatchGuard
}

// NewService builds the connection service for the app and configuration.
func NewService(app core.App, cfg config.Config) *Service {
	return &Service{app: app, cfg: cfg, attempts: make(map[string]pendingAttempt)}
}

// SetDispatchGuard binds the mail-dispatch guard. A nil guard disables the
// in-flight checks until the dispatcher is wired in.
func (s *Service) SetDispatchGuard(guard DispatchGuard) {
	s.guardMu.Lock()
	defer s.guardMu.Unlock()
	s.guard = guard
}

func (s *Service) dispatchGuard() DispatchGuard {
	s.guardMu.RLock()
	defer s.guardMu.RUnlock()
	return s.guard
}

// ConfigError reports the required settings that are absent, naming only
// environment variable names, never values. It never blocks server startup.
func (s *Service) ConfigError() error {
	missing := s.cfg.Microsoft.Missing()
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: set %s", ErrNotConfigured, strings.Join(missing, ", "))
}

// Status returns the operator-visible connection state. It is read-only: an
// undecryptable credential is reported as reconnect required without
// touching the stored record.
func (s *Service) Status(ctx context.Context) (Connection, error) {
	conn := Connection{Status: StatusDisconnected, SenderAddress: s.cfg.Microsoft.SenderAddress}
	record, err := s.app.FindFirstRecordByFilter(CollectionName, "1=1")
	if errors.Is(err, sql.ErrNoRows) {
		return conn, nil
	}
	if err != nil {
		return Connection{}, fmt.Errorf("reading the microsoft connection failed: %w", err)
	}

	conn.AccountID = record.GetString("account_id")
	conn.AccountEmail = record.GetString("account_email")
	conn.TenantID = record.GetString("tenant_id")
	conn.LastError = record.GetString("last_error")
	conn.ConnectedAt = record.GetDateTime("created").Time()
	conn.ExpiresAt = record.GetDateTime("credential_expires_at").Time()

	if Status(record.GetString("status")) == StatusReconnectRequired {
		conn.Status = StatusReconnectRequired
		return conn, nil
	}
	if _, err := s.openCredential(record.GetString("credential")); err != nil {
		// Read-only on purpose: only CompleteAuth may rewrite the record.
		conn.Status = StatusReconnectRequired
		conn.LastError = "the stored credentials cannot be decrypted; check APP_ENCRYPTION_KEY and reconnect the mailbox"
		return conn, nil
	}
	conn.Status = StatusConnected
	return conn, nil
}

// BeginAuth starts the authorization-code flow and returns the Microsoft
// consent URL. The returned state binds the eventual callback to this
// operator for a single use within attemptTTL.
func (s *Service) BeginAuth(ctx context.Context, operatorID string) (string, error) {
	if err := s.ConfigError(); err != nil {
		return "", err
	}
	if guard := s.dispatchGuard(); guard != nil {
		n, err := guard.InFlight(ctx)
		if err != nil {
			return "", fmt.Errorf("checking in-flight dispatches failed: %w", err)
		}
		if n > 0 {
			return "", fmt.Errorf("%w (%d in flight)", ErrInFlight, n)
		}
	}

	state, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	now := time.Now()
	// ponytail: attempts live in process memory, single-instance deployment;
	// move to a table if multiple instances must share sign-in state.
	for key, attempt := range s.attempts {
		if now.After(attempt.expiresAt) {
			delete(s.attempts, key)
		}
	}
	s.attempts[state] = pendingAttempt{operatorID: operatorID, verifier: verifier, expiresAt: now.Add(attemptTTL)}
	s.mu.Unlock()

	return s.oauthConfig().AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("login_hint", s.cfg.Microsoft.SenderAddress),
	), nil
}

// CompleteAuth consumes the sign-in attempt for state, exchanges the code,
// verifies the signed-in mailbox against the configured sender, and upserts
// the single connection record. The attempt is consumed even when a later
// step fails, so a replayed callback can never install credentials.
func (s *Service) CompleteAuth(ctx context.Context, operatorID, state, code string) (Connection, error) {
	if err := s.ConfigError(); err != nil {
		return Connection{}, err
	}
	attempt, ok := s.consumeAttempt(state, operatorID)
	if !ok {
		return Connection{}, ErrAuthorizationState
	}

	token, err := s.oauthConfig().Exchange(ctx, code, oauth2.VerifierOption(attempt.verifier))
	if err != nil {
		return Connection{}, fmt.Errorf("the Microsoft token exchange failed: %w", err)
	}

	accountID, address, err := s.graphIdentity(ctx, token.AccessToken)
	if err != nil {
		return Connection{}, err
	}
	address = strings.ToLower(strings.TrimSpace(address))
	if accountID == "" || address != strings.ToLower(s.cfg.Microsoft.SenderAddress) {
		s.app.Logger().WarnContext(ctx, "microsoft sign-in identity mismatch; existing connection unchanged")
		return Connection{}, ErrIdentityMismatch
	}

	credential, err := s.sealToken(token)
	if err != nil {
		return Connection{}, err
	}
	if err := s.saveConnection(accountID, address, s.cfg.Microsoft.TenantID, credential, token.Expiry); err != nil {
		return Connection{}, fmt.Errorf("persisting the microsoft connection failed: %w", err)
	}
	s.app.Logger().InfoContext(ctx, "microsoft mailbox connected")
	return s.Status(ctx)
}

// AccessToken returns a usable access token, refreshing the stored credential
// when it is close to expiry. A credential Microsoft rejects is marked
// reconnect required; unreadable ciphertext is never overwritten.
func (s *Service) AccessToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenAccessTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Fast path: a comfortably valid stored token needs no lock and no
	// network call.
	if _, token, err := s.storedToken(); err == nil && time.Until(token.Expiry) > refreshSkew {
		return token.AccessToken, nil
	}

	s.refreshOnce.Do(func() { s.refreshSlot = make(chan struct{}, 1) })
	select {
	case s.refreshSlot <- struct{}{}:
		defer func() { <-s.refreshSlot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Double-check: another goroutine may have refreshed while we waited for
	// the mutex.
	record, token, err := s.storedToken()
	if err != nil {
		return "", err
	}
	if time.Until(token.Expiry) > refreshSkew {
		return token.AccessToken, nil
	}

	_, tokenURL := s.cfg.Microsoft.Endpoints()
	oauthCfg := &oauth2.Config{
		ClientID:     s.cfg.Microsoft.ClientID,
		ClientSecret: s.cfg.Microsoft.ClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: tokenURL},
	}
	// An already-expired token forces TokenSource to use the refresh grant.
	source := oauthCfg.TokenSource(ctx, &oauth2.Token{RefreshToken: token.RefreshToken, Expiry: time.Now().Add(-time.Minute)})
	refreshed, err := source.Token()
	if err != nil {
		if refreshRejected(err) {
			s.markReconnectRequired(ctx, "Microsoft rejected the stored refresh token; reconnect the mailbox")
		}
		return "", fmt.Errorf("the Microsoft token refresh failed: %w", err)
	}
	if refreshed.RefreshToken == "" {
		// Microsoft may omit the refresh token; keep the previous one.
		refreshed.RefreshToken = token.RefreshToken
	}

	credential, err := s.sealToken(refreshed)
	if err != nil {
		return "", err
	}
	record.Set("credential", string(credential))
	record.Set("credential_expires_at", refreshed.Expiry)
	// Persist before returning: a rotated token the database never saw must
	// not be used, or the next start would silently hold a stale credential.
	if err := s.app.SaveWithContext(ctx, record); err != nil {
		return "", fmt.Errorf("persisting the refreshed Microsoft credentials failed: %w", err)
	}
	return refreshed.AccessToken, nil
}

// Disconnect deletes the connection record. An in-flight dispatch blocks it
// so mail never loses its credential mid-send.
func (s *Service) Disconnect(ctx context.Context) error {
	if guard := s.dispatchGuard(); guard != nil {
		n, err := guard.InFlight(ctx)
		if err != nil {
			return fmt.Errorf("checking in-flight dispatches failed: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("%w (%d in flight)", ErrInFlight, n)
		}
	}
	record, err := s.app.FindFirstRecordByFilter(CollectionName, "1=1")
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the microsoft connection failed: %w", err)
	}
	if err := s.app.Delete(record); err != nil {
		return fmt.Errorf("deleting the microsoft connection failed: %w", err)
	}
	s.app.Logger().InfoContext(ctx, "microsoft mailbox disconnected")
	return nil
}

// oauthConfig builds the tenant-specific OAuth config from the service
// settings. The client secret stays inside this server-side value.
func (s *Service) oauthConfig() *oauth2.Config {
	authorizeURL, tokenURL := s.cfg.Microsoft.Endpoints()
	return &oauth2.Config{
		ClientID:     s.cfg.Microsoft.ClientID,
		ClientSecret: s.cfg.Microsoft.ClientSecret,
		RedirectURL:  s.cfg.Microsoft.RedirectURL,
		Endpoint:     oauth2.Endpoint{AuthURL: authorizeURL, TokenURL: tokenURL},
		Scopes:       scopes,
	}
}

// consumeAttempt removes the attempt for state, single use. Unknown,
// expired, and operator-mismatched attempts all report false.
func (s *Service) consumeAttempt(state, operatorID string) (pendingAttempt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[state]
	if !ok {
		return pendingAttempt{}, false
	}
	// Consume before validating so neither a mismatch nor a later failure
	// leaves the state reusable.
	delete(s.attempts, state)
	if attempt.operatorID != operatorID || time.Now().After(attempt.expiresAt) {
		return pendingAttempt{}, false
	}
	return attempt, true
}

// graphIdentity fetches the signed-in account from Microsoft Graph and
// returns its id and preferred mail address (userPrincipalName, else mail).
func (s *Service) graphIdentity(ctx context.Context, accessToken string) (id, address string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.cfg.Microsoft.GraphBaseURL+"/me?$select=id,mail,userPrincipalName", nil)
	if err != nil {
		return "", "", fmt.Errorf("the Microsoft identity check failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("the Microsoft identity check failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.app.Logger().Warn("close Microsoft identity response", "error", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("the Microsoft identity check returned status %d", resp.StatusCode)
	}
	var me struct {
		ID                string `json:"id"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return "", "", fmt.Errorf("the Microsoft identity check returned an unreadable response: %w", err)
	}
	address = me.UserPrincipalName
	if address == "" {
		address = me.Mail
	}
	return me.ID, address, nil
}

// saveConnection upserts the single connection record as connected.
func (s *Service) saveConnection(accountID, accountEmail, tenantID string, credential []byte, expiresAt time.Time) error {
	return s.app.RunInTransaction(func(txApp core.App) error {
		collection, err := txApp.FindCollectionByNameOrId(CollectionName)
		if err != nil {
			return err
		}
		record, err := txApp.FindFirstRecordByFilter(CollectionName, "1=1")
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if record == nil {
			record = core.NewRecord(collection)
		}
		record.Set("status", string(StatusConnected))
		record.Set("account_id", accountID)
		record.Set("account_email", accountEmail)
		record.Set("tenant_id", tenantID)
		record.Set("credential", string(credential))
		record.Set("credential_expires_at", expiresAt)
		record.Set("last_error", "")
		return txApp.Save(record)
	})
}

// storedToken loads and unseals the stored credential together with its
// record. Any storage problem reports ErrCredentialsUnreadable and leaves the
// record untouched.
func (s *Service) storedToken() (*core.Record, *oauth2.Token, error) {
	record, err := s.app.FindFirstRecordByFilter(CollectionName, "1=1")
	if err != nil {
		return nil, nil, ErrCredentialsUnreadable
	}
	plaintext, err := s.openCredential(record.GetString("credential"))
	if err != nil {
		return nil, nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal(plaintext, &token); err != nil || token.AccessToken == "" {
		return nil, nil, ErrCredentialsUnreadable
	}
	return record, &token, nil
}

// markReconnectRequired flags the record after Microsoft rejected the stored
// refresh token. Failures are logged, never fatal: the returned error is the
// refresh failure itself.
func (s *Service) markReconnectRequired(ctx context.Context, reason string) {
	record, err := s.app.FindFirstRecordByFilter(CollectionName, "1=1")
	if err != nil {
		s.app.Logger().WarnContext(ctx, "cannot mark the microsoft connection for reconnect", "error", err.Error())
		return
	}
	record.Set("status", string(StatusReconnectRequired))
	record.Set("last_error", reason)
	if err := s.app.Save(record); err != nil {
		s.app.Logger().WarnContext(ctx, "cannot mark the microsoft connection for reconnect", "error", err.Error())
	}
}

// refreshRejected reports whether Microsoft refused the refresh grant
// (invalid_grant or an HTTP 400 from the token endpoint).
func refreshRejected(err error) bool {
	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) {
		return false
	}
	if retrieveErr.ErrorCode == "invalid_grant" {
		return true
	}
	return retrieveErr.Response != nil && retrieveErr.Response.StatusCode == http.StatusBadRequest
}

// randomToken returns 32 cryptographically random bytes, URL-safe encoded.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("gathering randomness failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
