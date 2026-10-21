// Package microsoft owns the operator-scoped Microsoft 365 mailbox connection:
// the OAuth 2.0 authorization-code flow with PKCE, Microsoft Graph identity
// verification, and token refresh on top of mailbox.Store.
//
// There is no fixed sender address: every operator signs in with their own
// mailbox. The verified mailbox becomes an exclusive claim through
// mailbox.Store. Signing in a different mailbox while one is connected stages
// a replacement claim and reports ReplacementRequired; the current connection
// is never silently replaced.
//
// Secret material (access tokens, refresh tokens, PKCE verifiers, the client
// secret) never leaves the server. Credential ciphertext is sealed and
// unsealed exclusively by mailbox.Store. Errors name settings and outcomes,
// never values.
package microsoft

import (
	"context"
	"crypto/rand"
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
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"golang.org/x/oauth2"
)

// Errors reported by the connection service.
var (
	// ErrNotConfigured wraps ConfigError: the required Microsoft settings are
	// incomplete, so no flow can start.
	ErrNotConfigured = errors.New("microsoft connection is not configured")
	// ErrAuthorizationState reports an unknown, expired, already-used, or
	// operator-mismatched OAuth state parameter.
	ErrAuthorizationState = errors.New("the Microsoft sign-in attempt is unknown, expired, or already used")
	// ErrNoMailbox reports a sign-in whose Graph identity has no mailbox
	// address; nothing is changed when it happens.
	ErrNoMailbox = errors.New("the signed-in Microsoft account has no usable mailbox address")
	// ErrRefreshRejected reports Microsoft refusing the stored refresh token.
	// The connection is marked reconnect required; the operator must sign in
	// again.
	ErrRefreshRejected = errors.New("the stored mailbox credentials were rejected; reconnect the mailbox")
)

// ReplacementRequired reports a sign-in whose verified mailbox differs from
// the operator's current connection. The new mailbox is staged as a
// replacement claim and the current connection is untouched; the web layer
// redirects to the replacement confirmation instead of silently switching.
type ReplacementRequired struct {
	Reset *mailbox.Reset
}

func (e *ReplacementRequired) Error() string {
	return "the signed-in mailbox is different from the connected one; confirm the replacement to finish connecting it"
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
// mailbox against the Graph identity. Nothing more is requested.
var scopes = []string{
	"offline_access",
	"https://graph.microsoft.com/Mail.Send",
	"https://graph.microsoft.com/User.Read",
}

// pendingAttempt is one in-memory OAuth sign-in attempt.
type pendingAttempt struct {
	ownerID   string
	verifier  string
	expiresAt time.Time
}

// Service implements the operator-scoped Microsoft mailbox connection against
// a PocketBase app and the shared mailbox store.
type Service struct {
	app   core.App
	cfg   config.Config
	store *mailbox.Store

	mu       sync.Mutex
	attempts map[string]pendingAttempt

	refreshMu sync.Mutex
	refreshes map[string]chan struct{}
}

// NewService builds the Microsoft connection service for the app, its
// configuration, and the shared mailbox connection store.
func NewService(app core.App, cfg config.Config, connections *mailbox.Store) *Service {
	return &Service{
		app:       app,
		cfg:       cfg,
		store:     connections,
		attempts:  make(map[string]pendingAttempt),
		refreshes: make(map[string]chan struct{}),
	}
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

// BeginAuth starts the authorization-code flow and returns the Microsoft
// consent URL. The returned state binds the eventual callback to this
// operator for a single use within attemptTTL.
func (s *Service) BeginAuth(ctx context.Context, ownerID string) (string, error) {
	if err := s.ConfigError(); err != nil {
		return "", err
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
	s.attempts[state] = pendingAttempt{ownerID: ownerID, verifier: verifier, expiresAt: now.Add(attemptTTL)}
	s.mu.Unlock()

	return s.oauthConfig().AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
	), nil
}

// CompleteAuth consumes the sign-in attempt for state, exchanges the code,
// verifies the signed-in mailbox through Microsoft Graph, and routes it to
// the owner's mailbox operation: first install, same-identity reconnect, or a
// staged replacement for a different mailbox. The attempt is consumed even
// when a later step fails, so a replayed callback can never install
// credentials.
func (s *Service) CompleteAuth(ctx context.Context, ownerID, state, code string) (*mailbox.Connection, error) {
	if err := s.ConfigError(); err != nil {
		return nil, err
	}
	attempt, ok := s.consumeAttempt(state, ownerID)
	if !ok {
		return nil, ErrAuthorizationState
	}

	token, err := s.oauthConfig().Exchange(ctx, code, oauth2.VerifierOption(attempt.verifier))
	if err != nil {
		return nil, fmt.Errorf("the Microsoft token exchange failed: %w", err)
	}

	graphID, address, err := s.graphIdentity(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}
	sender := mailbox.CanonicalSender(address)
	if graphID == "" || sender == "" {
		s.app.Logger().WarnContext(ctx, "microsoft sign-in without a mailbox identity; nothing changed")
		return nil, ErrNoMailbox
	}
	// The identity carries the configured tenant so one account cannot be
	// claimed under two different tenants.
	candidate := mailbox.Candidate{
		OwnerID:  ownerID,
		Provider: mailbox.ProviderMicrosoft,
		Sender:   sender,
		Identity: graphID + "@" + s.cfg.Microsoft.TenantID,
		Credential: mailbox.Credential{
			Provider:     mailbox.ProviderMicrosoft,
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    token.Expiry,
		},
	}

	current, err := s.store.Current(ctx, ownerID)
	if errors.Is(err, mailbox.ErrNotConnected) {
		installed, err := s.store.Install(ctx, candidate)
		if err != nil {
			// ErrClaimed (mailbox owned by another operator) surfaces as-is
			// and never discloses the other owner.
			return nil, err
		}
		s.app.Logger().InfoContext(ctx, "microsoft mailbox connected", "owner", ownerID)
		return installed, nil
	}
	if err != nil {
		return nil, err
	}

	if current.Identity == candidate.Identity {
		// Same verified account: refresh credentials in place. The generation
		// is preserved, so exact-content approvals remain valid.
		if err := s.store.Reconnect(ctx, current, candidate.Credential); err != nil {
			return nil, fmt.Errorf("refreshing the microsoft connection failed: %w", err)
		}
		s.app.Logger().InfoContext(ctx, "microsoft mailbox reconnected", "owner", ownerID)
		return s.store.ByID(ctx, current.ID)
	}

	// A different mailbox: stage the claim and require an explicit
	// replacement confirmation. The current connection stays untouched.
	reset, err := s.store.StageReplacement(ctx, candidate)
	if err != nil {
		return nil, err
	}
	s.app.Logger().InfoContext(ctx, "microsoft mailbox replacement staged", "owner", ownerID)
	return nil, &ReplacementRequired{Reset: reset}
}

// AccessToken returns a usable access token for the connection, refreshing
// the stored credential when it is close to expiry. Refreshes are serialized
// per connection, so unrelated mailboxes refresh independently. A refresh
// Microsoft rejects marks only this connection reconnect required; unreadable
// ciphertext is reported and never overwritten.
func (s *Service) AccessToken(ctx context.Context, connection *mailbox.Connection) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenAccessTimeout)
	defer cancel()
	// Fast path: a comfortably valid stored token needs no lock and no
	// network call.
	if token, err := s.storedToken(ctx, connection); err == nil && time.Until(token.Expiry) > refreshSkew {
		return token.AccessToken, nil
	}

	slot := s.refreshSlot(connection.ID)
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Double-check: another goroutine may have refreshed while we waited.
	token, err := s.storedToken(ctx, connection)
	if err != nil {
		return "", err
	}
	if time.Until(token.Expiry) > refreshSkew {
		return token.AccessToken, nil
	}

	refreshed, err := s.refreshGrant(ctx, token.RefreshToken)
	if err != nil {
		if refreshRejected(err) {
			s.app.Logger().WarnContext(ctx, "microsoft refresh token rejected", "connection", connection.ID)
			s.MarkReconnectRequired(ctx, connection, "Microsoft rejected the stored refresh token; reconnect the mailbox")
			return "", ErrRefreshRejected
		}
		return "", fmt.Errorf("the Microsoft token refresh failed: %w", err)
	}
	if refreshed.RefreshToken == "" {
		// Microsoft may omit the refresh token; keep the previous one.
		refreshed.RefreshToken = token.RefreshToken
	}

	// Persist before returning: a rotated token the database never saw must
	// not be used, or the next request would silently hold a stale credential.
	// Reconnect keeps the connection generation.
	err = s.store.Reconnect(ctx, connection, mailbox.Credential{
		Provider:     mailbox.ProviderMicrosoft,
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		ExpiresAt:    refreshed.Expiry,
	})
	if err != nil {
		return "", fmt.Errorf("persisting the refreshed Microsoft credentials failed: %w", err)
	}
	return refreshed.AccessToken, nil
}

// MarkReconnectRequired records that stored credentials are unusable and the
// owner must sign in again. Failures are logged, never fatal: the caller
// already holds the error that triggered the mark.
func (s *Service) MarkReconnectRequired(ctx context.Context, connection *mailbox.Connection, reason string) {
	if err := s.store.SetStatus(ctx, connection, mailbox.StatusReconnectRequired, reason); err != nil {
		s.app.Logger().WarnContext(ctx, "cannot mark the mailbox connection for reconnect", "connection", connection.ID, "error", err.Error())
	}
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
func (s *Service) consumeAttempt(state, ownerID string) (pendingAttempt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[state]
	if !ok {
		return pendingAttempt{}, false
	}
	// Consume before validating so neither a mismatch nor a later failure
	// leaves the state reusable.
	delete(s.attempts, state)
	if attempt.ownerID != ownerID || time.Now().After(attempt.expiresAt) {
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

// storedToken unseals the connection's credential and returns it as an OAuth
// token. Unreadable ciphertext produces an actionable error and leaves the
// stored value untouched.
func (s *Service) storedToken(ctx context.Context, connection *mailbox.Connection) (*oauth2.Token, error) {
	credential, err := s.store.Credential(ctx, connection)
	if err != nil {
		if errors.Is(err, mailbox.ErrCredentialsUnreadable) {
			return nil, fmt.Errorf("%w: reconnect the mailbox; the stored value was not changed", err)
		}
		return nil, err
	}
	if credential.AccessToken == "" {
		return nil, fmt.Errorf("%w: reconnect the mailbox; the stored value was not changed", mailbox.ErrCredentialsUnreadable)
	}
	return &oauth2.Token{
		AccessToken:  credential.AccessToken,
		RefreshToken: credential.RefreshToken,
		Expiry:       credential.ExpiresAt,
	}, nil
}

// refreshSlot returns the per-connection serialization slot for refreshes,
// created on first use. One mailbox waiting on Microsoft never blocks
// another.
func (s *Service) refreshSlot(connectionID string) chan struct{} {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	slot, ok := s.refreshes[connectionID]
	if !ok {
		slot = make(chan struct{}, 1)
		s.refreshes[connectionID] = slot
	}
	return slot
}

// refreshGrant exchanges the refresh token at the tenant-specific token
// endpoint.
func (s *Service) refreshGrant(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	_, tokenURL := s.cfg.Microsoft.Endpoints()
	oauthCfg := &oauth2.Config{
		ClientID:     s.cfg.Microsoft.ClientID,
		ClientSecret: s.cfg.Microsoft.ClientSecret,
		Endpoint:     oauth2.Endpoint{TokenURL: tokenURL},
	}
	// An already-expired token forces TokenSource to use the refresh grant.
	source := oauthCfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Now().Add(-time.Minute)})
	return source.Token()
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
