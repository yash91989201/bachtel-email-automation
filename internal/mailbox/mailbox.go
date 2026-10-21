// Package mailbox owns exclusive operator-owned sending mailboxes: provider
// claims, connection status and generations, and encrypted provider
// credentials.
//
// A connection belongs to exactly one operator and its canonical sender
// address belongs to exactly one connection, including while it is
// disconnected or requires reconnection. Credentials are sealed with
// AES-256-GCM under the deployment key, bound to the owner, provider and
// connection id; secret values never leave this package as plain text except
// to the sender implementation that needs them.
package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Collection names owned by this package.
const (
	// ConnectionsCollection holds one current connection per operator plus a
	// staged target connection during replacement.
	ConnectionsCollection = "mail_connections"
	// ResetsCollection holds a durable mailbox replacement/reset operation.
	ResetsCollection = "connection_resets"
)

// Provider identifies a supported mailbox provider.
type Provider string

const (
	// ProviderMicrosoft is Microsoft 365 delegated OAuth.
	ProviderMicrosoft Provider = "microsoft365"
	// ProviderTitan is GoDaddy Titan authenticated SMTP.
	ProviderTitan Provider = "godaddy-titan"
)

// Providers lists every supported provider in display order.
var Providers = []Provider{ProviderMicrosoft, ProviderTitan}

// Valid reports whether the provider is supported.
func (p Provider) Valid() bool {
	return p == ProviderMicrosoft || p == ProviderTitan
}

// Status is the connection state shown to its owner.
type Status string

const (
	// StatusConnected means the connection can send.
	StatusConnected Status = "connected"
	// StatusReconnectRequired means stored credentials are unusable and the
	// owner must reconnect.
	StatusReconnectRequired Status = "reconnect_required"
	// StatusDisconnected means the owner removed usable credentials but the
	// mailbox claim is retained.
	StatusDisconnected Status = "disconnected"
)

// Connection is the operator-visible state of one mailbox connection. It never
// carries credential values.
type Connection struct {
	ID         string
	OwnerID    string
	Provider   Provider
	Sender     string
	Identity   string
	Status     Status
	Generation int
	Current    bool
	LastError  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Usable reports whether the connection may send right now.
func (c *Connection) Usable() bool { return c.Status == StatusConnected }

// Credential is the sealed payload of one connection. Exactly one provider
// field is meaningful: OAuth tokens for Microsoft, a password for Titan.
type Credential struct {
	Provider     Provider  `json:"provider"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Password     string    `json:"password,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
}

// Errors reported by this package.
var (
	// ErrNotConnected reports an operator without a current connection.
	ErrNotConnected = errors.New("this operator has no mailbox connection")
	// ErrClaimed reports a mailbox already claimed by any connection. The
	// message never discloses the owning operator.
	ErrClaimed = errors.New("this mailbox is already connected to another workspace")
	// ErrAlreadyConnected reports an operator that already owns a current
	// connection; changing it requires the replacement or reconnect flow.
	ErrAlreadyConnected = errors.New("this operator already has a mailbox connection")
	// ErrUnsupportedProvider reports a provider key that is not supported.
	ErrUnsupportedProvider = errors.New("that mail provider is not supported")
	// ErrCredentialsUnreadable reports sealed credentials that cannot be
	// decrypted or decoded. Stored ciphertext is never overwritten on this path.
	ErrCredentialsUnreadable = errors.New("the stored mailbox credentials cannot be read")
)

// Candidate is a validated proposed connection, ready to be claimed.
type Candidate struct {
	OwnerID    string
	Provider   Provider
	Sender     string
	Identity   string
	Credential Credential
}

// Store reads and writes mailbox connections and their credentials.
type Store struct {
	app core.App
	key func() ([]byte, error)
}

// NewStore builds the store. key resolves the deployment encryption key on
// demand so a missing key reports an actionable error instead of failing at
// startup.
func NewStore(app core.App, key func() ([]byte, error)) *Store {
	return &Store{app: app, key: key}
}

// App exposes the PocketBase app for callers that must run their own
// transaction over connection records.
func (s *Store) App() core.App { return s.app }

// CanonicalSender trims and lowercases a mailbox address. It deliberately does
// not rewrite dots or plus tags: a claim covers exactly the authenticated
// address.
func CanonicalSender(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// Current returns the operator's current connection. It reports
// ErrNotConnected when the operator has none.
func (s *Store) Current(ctx context.Context, ownerID string) (*Connection, error) {
	record, err := s.app.FindFirstRecordByFilter(
		ConnectionsCollection,
		"owner = {:owner} && current = true",
		dbx.Params{"owner": ownerID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, fmt.Errorf("read the mailbox connection: %w", err)
	}
	return connectionFromRecord(record), nil
}

// ByID loads one connection by id.
func (s *Store) ByID(ctx context.Context, id string) (*Connection, error) {
	record, err := s.app.FindRecordById(ConnectionsCollection, id)
	if err != nil {
		return nil, fmt.Errorf("read the mailbox connection: %w", err)
	}
	return connectionFromRecord(record), nil
}

// ListCurrent returns every current connection, oldest first.
func (s *Store) ListCurrent(ctx context.Context) ([]*Connection, error) {
	records, err := s.app.FindRecordsByFilter(ConnectionsCollection, "current = true", "created", 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list mailbox connections: %w", err)
	}
	connections := make([]*Connection, 0, len(records))
	for _, record := range records {
		connections = append(connections, connectionFromRecord(record))
	}
	return connections, nil
}

// Install claims a validated mailbox as the owner's current connection. It
// fails with ErrAlreadyConnected when the owner already has one and ErrClaimed
// when the address or provider identity belongs to any existing connection.
func (s *Store) Install(ctx context.Context, candidate Candidate) (*Connection, error) {
	if err := validateCandidate(candidate); err != nil {
		return nil, err
	}
	var out *Connection
	err := s.app.RunInTransaction(func(app core.App) error {
		if _, err := app.FindFirstRecordByFilter(
			ConnectionsCollection,
			"owner = {:owner} && current = true",
			dbx.Params{"owner": candidate.OwnerID},
		); err == nil {
			return ErrAlreadyConnected
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := claimFree(app, candidate.Sender, candidate.Identity, ""); err != nil {
			return err
		}
		collection, err := app.FindCollectionByNameOrId(ConnectionsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", ConnectionsCollection, err)
		}
		record := core.NewRecord(collection)
		// The credential is sealed with the connection id as authenticated
		// data, so the id must exist before sealing rather than being assigned
		// by Save.
		record.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
		record.Set("owner", candidate.OwnerID)
		record.Set("provider", string(candidate.Provider))
		record.Set("sender", candidate.Sender)
		record.Set("identity", candidate.Identity)
		record.Set("status", string(StatusConnected))
		record.Set("generation", 1)
		record.Set("current", true)
		sealed, err := s.seal(candidate.OwnerID, candidate.Provider, record.Id, candidate.Credential)
		if err != nil {
			return err
		}
		record.Set("credential", sealed)
		record.Set("credential_expires_at", candidate.Credential.ExpiresAt.UTC())
		if err := app.Save(record); err != nil {
			// A concurrent claim loses the unique constraint race; report the
			// same refusal the pre-check produces instead of a database error.
			if isUniqueViolation(err) {
				return ErrClaimed
			}
			return err
		}
		out = connectionFromRecord(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Reconnect refreshes credentials for the same verified identity on an
// existing connection. It preserves the generation, so approvals for exact
// content remain valid.
func (s *Store) Reconnect(ctx context.Context, connection *Connection, credential Credential) error {
	if credential.Provider != connection.Provider {
		return ErrUnsupportedProvider
	}
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ConnectionsCollection, connection.ID)
		if err != nil {
			return fmt.Errorf("read the mailbox connection: %w", err)
		}
		if record.GetString("owner") != connection.OwnerID {
			return ErrNotConnected
		}
		sealed, err := s.seal(connection.OwnerID, connection.Provider, record.Id, credential)
		if err != nil {
			return err
		}
		record.Set("credential", sealed)
		record.Set("credential_expires_at", credential.ExpiresAt.UTC())
		record.Set("status", string(StatusConnected))
		record.Set("last_error", "")
		return app.Save(record)
	})
}

// Credential unseals the stored provider credential. It reports
// ErrCredentialsUnreadable for a missing key, wrong key, tampering, or a
// provider mismatch, and never rewrites the stored ciphertext.
func (s *Store) Credential(ctx context.Context, connection *Connection) (Credential, error) {
	record, err := s.app.FindRecordById(ConnectionsCollection, connection.ID)
	if err != nil {
		return Credential{}, fmt.Errorf("read the mailbox connection: %w", err)
	}
	return s.open(connection.OwnerID, connection.Provider, record.Id, record.GetString("credential"))
}

// SetStatus records a connection state change without touching credentials.
func (s *Store) SetStatus(ctx context.Context, connection *Connection, status Status, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ConnectionsCollection, connection.ID)
		if err != nil {
			return fmt.Errorf("read the mailbox connection: %w", err)
		}
		record.Set("status", string(status))
		record.Set("last_error", reason)
		return app.Save(record)
	})
}

// Disconnect removes usable credentials but keeps the exclusive claim, so the
// mailbox cannot be claimed by another operator until it is replaced or
// explicitly released.
func (s *Store) Disconnect(ctx context.Context, connection *Connection) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ConnectionsCollection, connection.ID)
		if err != nil {
			return fmt.Errorf("read the mailbox connection: %w", err)
		}
		record.Set("credential", "")
		record.Set("credential_expires_at", "")
		record.Set("status", string(StatusDisconnected))
		record.Set("last_error", "")
		return app.Save(record)
	})
}

// claimFree rejects a sender address or provider identity already held by any
// connection, including a staged replacement target and a disconnected one.
// excludeID skips the connection being replaced.
func claimFree(app core.App, sender, identity, excludeID string) error {
	sender = CanonicalSender(sender)
	records, err := app.FindRecordsByFilter(ConnectionsCollection, "sender = {:sender}", "", 0, 0, dbx.Params{"sender": sender})
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Id != excludeID {
			return ErrClaimed
		}
	}
	if identity == "" {
		return nil
	}
	records, err = app.FindRecordsByFilter(ConnectionsCollection, "identity = {:identity}", "", 0, 0, dbx.Params{"identity": identity})
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Id != excludeID {
			return ErrClaimed
		}
	}
	return nil
}

// ClaimFree is claimFree for callers that must check inside their own
// transaction, such as the replacement reset.
func ClaimFree(app core.App, sender, identity, excludeID string) error {
	return claimFree(app, sender, identity, excludeID)
}

func validateCandidate(candidate Candidate) error {
	if candidate.OwnerID == "" {
		return errors.New("a mailbox owner is required")
	}
	if !candidate.Provider.Valid() {
		return ErrUnsupportedProvider
	}
	if CanonicalSender(candidate.Sender) == "" {
		return errors.New("a mailbox address is required")
	}
	if candidate.Credential.Provider != candidate.Provider {
		return ErrUnsupportedProvider
	}
	return nil
}

// isUniqueViolation reports a constraint failure from a concurrent claim. The
// driver surfaces it as text; the caller converts it to the same refusal the
// pre-check produces so no database detail reaches the operator.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func connectionFromRecord(record *core.Record) *Connection {
	return &Connection{
		ID:         record.Id,
		OwnerID:    record.GetString("owner"),
		Provider:   Provider(record.GetString("provider")),
		Sender:     record.GetString("sender"),
		Identity:   record.GetString("identity"),
		Status:     Status(record.GetString("status")),
		Generation: record.GetInt("generation"),
		Current:    record.GetBool("current"),
		LastError:  record.GetString("last_error"),
		ExpiresAt:  record.GetDateTime("credential_expires_at").Time().UTC(),
		CreatedAt:  record.GetDateTime("created").Time().UTC(),
		UpdatedAt:  record.GetDateTime("updated").Time().UTC(),
	}
}
