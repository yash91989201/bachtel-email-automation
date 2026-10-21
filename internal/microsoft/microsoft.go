// Package microsoft owns the operator-scoped Microsoft 365 mailbox connection:
// the OAuth 2.0 authorization-code flow with PKCE, Microsoft Graph identity
// verification, and token refresh on top of mailbox.Store.
//
// There is no fixed sender address and no deployment-wide tenant: every
// operator saves their own Tenant ID, Client ID and Client Secret, signs in
// with their own mailbox, and the verified mailbox becomes an exclusive claim
// through mailbox.Store. Signing in a different mailbox while one is connected
// stages a replacement claim and reports ReplacementRequired; the current
// connection is never silently replaced. Signing in the same mailbox under
// changed application settings rotates those settings without losing history.
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
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"golang.org/x/oauth2"
)

// Errors reported by the connection service.
var (
	// ErrNotConfigured reports an operator without saved provider settings, so
	// no flow can start. Each operator saves their own Tenant ID, Client ID
	// and Client Secret; the deployment holds no Microsoft credentials.
	ErrNotConfigured = errors.New("save your Microsoft provider settings before signing in")
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
	// ErrRotationInFlight reports a same-mailbox settings change that did not
	// settle in-flight submissions in time. The saved settings and credentials
	// stay unchanged and the operator signs in again.
	ErrRotationInFlight = errors.New("email submissions are in flight; the application settings were not changed, so sign in again once they settle")
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
	// rotationDrainTimeout bounds how long a same-mailbox application-settings
	// rotation waits for in-flight submissions before asking the operator to
	// sign in again.
	rotationDrainTimeout = 30 * time.Second
)

// scopes: offline_access yields the refresh token, Mail.Send is the only
// write scope the dispatcher needs, and User.Read verifies the signed-in
// mailbox against the Graph identity. Nothing more is requested.
var scopes = []string{
	"offline_access",
	"https://graph.microsoft.com/Mail.Send",
	"https://graph.microsoft.com/User.Read",
}

// pendingAttempt is one in-memory OAuth sign-in attempt. It carries an
// immutable snapshot of the saved registration the operator started with, so
// a later settings edit can neither redirect the callback to another
// registration nor be overwritten by it.
type pendingAttempt struct {
	ownerID   string
	verifier  string
	settings  *mailbox.ProviderSettings
	expiresAt time.Time
}

// Service implements the operator-scoped Microsoft mailbox connection against
// a PocketBase app and the shared mailbox store.
type Service struct {
	app   core.App
	cfg   config.Config
	store *mailbox.Store
	drain Drainer
	// drainTimeout bounds the rotation wait; tests shorten it.
	drainTimeout time.Duration

	mu       sync.Mutex
	attempts map[string]pendingAttempt

	refreshMu sync.Mutex
	refreshes map[string]chan struct{}
}

// Drainer reports how many submissions the operator's mailbox still has in
// flight. Application settings are swapped only after they settle.
type Drainer interface {
	InFlightFor(ctx context.Context, ownerID string) (int, error)
}

// SetDrainer binds the dispatcher's per-owner in-flight query. Without one, no
// submissions are assumed to be running.
func (s *Service) SetDrainer(drainer Drainer) { s.drain = drainer }

// NewService builds the Microsoft connection service for the app, its
// configuration, and the shared mailbox connection store.
func NewService(app core.App, cfg config.Config, connections *mailbox.Store) *Service {
	return &Service{
		app:          app,
		cfg:          cfg,
		store:        connections,
		attempts:     make(map[string]pendingAttempt),
		refreshes:    make(map[string]chan struct{}),
		drainTimeout: rotationDrainTimeout,
	}
}

// BeginAuth starts the authorization-code flow with the operator's saved
// registration and returns the Microsoft consent URL. The returned state binds
// the eventual callback to this operator and to this registration snapshot for
// a single use within attemptTTL.
func (s *Service) BeginAuth(ctx context.Context, ownerID string) (string, error) {
	settings, err := s.savedSettings(ctx, ownerID)
	if err != nil {
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
	s.attempts[state] = pendingAttempt{ownerID: ownerID, verifier: verifier, settings: settings, expiresAt: now.Add(attemptTTL)}
	s.mu.Unlock()

	return s.oauthConfig(settings).AuthCodeURL(state,
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
	attempt, ok := s.consumeAttempt(state, ownerID)
	if !ok {
		return nil, ErrAuthorizationState
	}
	settings := attempt.settings

	token, err := s.oauthConfig(settings).Exchange(ctx, code, oauth2.VerifierOption(attempt.verifier))
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
	// The identity carries the tenant the tokens were issued in, so one account
	// cannot be claimed twice through different registrations.
	candidate := mailbox.Candidate{
		OwnerID:            ownerID,
		Provider:           mailbox.ProviderMicrosoft,
		Sender:             sender,
		Identity:           graphID + "@" + settings.TenantID,
		SettingsGeneration: settings.Generation,
		PromoteSettings:    settings.Pending,
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
		// Same verified account. Newly saved application settings replace the
		// active ones only after in-flight submissions settle, and only with
		// the tokens this sign-in produced. A plain reconnect keeps the
		// generation, so exact-content approvals remain valid.
		if settings.Pending || current.SettingsGeneration != settings.Generation {
			if err := s.drainOwner(ctx, ownerID); err != nil {
				return nil, err
			}
			if err := s.store.Rotate(ctx, current, candidate.Credential, settings.Generation, settings.Pending); err != nil {
				return nil, fmt.Errorf("rotating the microsoft application settings failed: %w", err)
			}
			s.app.Logger().InfoContext(ctx, "microsoft application settings rotated", "owner", ownerID)
			return s.store.ByID(ctx, current.ID)
		}
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

	refreshed, err := func() (*oauth2.Token, error) {
		settings, err := s.registrationFor(ctx, connection)
		if err != nil {
			if errors.Is(err, mailbox.ErrSettingsStale) || errors.Is(err, mailbox.ErrSettingsMissing) || errors.Is(err, ErrNotConfigured) {
				s.MarkReconnectRequired(ctx, connection, "The saved Microsoft application settings changed; sign in again to reconnect the mailbox")
			}
			return nil, err
		}
		return s.refreshGrant(ctx, settings, token.RefreshToken)
	}()
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

// oauthConfig builds the tenant-specific OAuth config from one saved
// registration. The client secret stays inside this server-side value.
func (s *Service) oauthConfig(settings *mailbox.ProviderSettings) *oauth2.Config {
	authorizeURL, tokenURL := s.cfg.Microsoft.Endpoints(settings.TenantID)
	return &oauth2.Config{
		ClientID:     settings.ClientID,
		ClientSecret: settings.ClientSecret,
		RedirectURL:  s.cfg.CallbackURL(),
		Endpoint:     oauth2.Endpoint{AuthURL: authorizeURL, TokenURL: tokenURL},
		Scopes:       scopes,
	}
}

// savedSettings returns the registration an operator should authorize with:
// the staged one when they saved a change, otherwise the active one.
func (s *Service) savedSettings(ctx context.Context, ownerID string) (*mailbox.ProviderSettings, error) {
	pending, err := s.store.PendingSettings(ctx, ownerID)
	switch {
	case err == nil:
		return pending, nil
	case !errors.Is(err, mailbox.ErrSettingsMissing):
		return nil, err
	}
	active, err := s.store.ActiveSettings(ctx, ownerID)
	if errors.Is(err, mailbox.ErrSettingsMissing) {
		return nil, fmt.Errorf("%w: save the Tenant ID, Client ID and Client Secret first", ErrNotConfigured)
	}
	if err != nil {
		return nil, err
	}
	return active, nil
}

// registrationFor loads the saved registration the connection's credentials
// were issued under. A refresh never uses another registration: a mismatch
// requires a fresh sign-in.
func (s *Service) registrationFor(ctx context.Context, connection *mailbox.Connection) (*mailbox.ProviderSettings, error) {
	settings, err := s.store.ActiveSettings(ctx, connection.OwnerID)
	if errors.Is(err, mailbox.ErrSettingsMissing) {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	if err != nil {
		return nil, err
	}
	if settings.Generation != connection.SettingsGeneration {
		return nil, fmt.Errorf("%w: sign in again to use the changed application settings", mailbox.ErrSettingsStale)
	}
	return settings, nil
}

// drainOwner waits a bounded time for the owner's in-flight submissions to
// settle before application settings are swapped. It never blocks another
// operator's mailbox.
func (s *Service) drainOwner(ctx context.Context, ownerID string) error {
	if s.drain == nil {
		return nil
	}
	deadline := time.Now().Add(s.drainTimeout)
	for {
		inFlight, err := s.drain.InFlightFor(ctx, ownerID)
		if err != nil {
			return err
		}
		if inFlight == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrRotationInFlight
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
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
func (s *Service) refreshGrant(ctx context.Context, settings *mailbox.ProviderSettings, refreshToken string) (*oauth2.Token, error) {
	_, tokenURL := s.cfg.Microsoft.Endpoints(settings.TenantID)
	oauthCfg := &oauth2.Config{
		ClientID:     settings.ClientID,
		ClientSecret: settings.ClientSecret,
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
