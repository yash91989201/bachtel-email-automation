package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Store reads and writes confirmed AI connections and staged proposals.
type Store struct {
	app  core.App
	key  func() ([]byte, error)
	lock LockCheck
	opts ValidationOptions
}

// NewStore builds the store. key resolves the deployment encryption key on
// demand so a missing key reports an actionable error and never overwrites
// stored ciphertext.
func NewStore(app core.App, key func() ([]byte, error)) *Store {
	return &Store{app: app, key: key, opts: DefaultValidationOptions()}
}

// App exposes the PocketBase app for callers that must run their own
// transaction over connection records.
func (s *Store) App() core.App { return s.app }

// SetLockCheck binds the account-wide pending-task lock used before a
// configuration switch. It is called inside the confirmation transaction with
// that transaction's app.
func (s *Store) SetLockCheck(fn LockCheck) { s.lock = fn }

// SetValidationOptions bounds the synthetic validation request. Production
// callers never change the defaults.
func (s *Store) SetValidationOptions(o ValidationOptions) { s.opts = o }

// Connection returns the confirmed connection, or ErrNotConfigured.
func (s *Store) Connection(ctx context.Context, ownerID string) (*Connection, error) {
	return connection(s.app, ownerID)
}

func connection(app core.App, ownerID string) (*Connection, error) {
	record, err := findConnection(app, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	return connectionFromRecord(record), nil
}

func findConnection(app core.App, ownerID string) (*core.Record, error) {
	return app.FindFirstRecordByFilter(ConnectionsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
}

// Draft returns the staged proposal, or nil when none exists.
func (s *Store) Draft(ctx context.Context, ownerID string) (*Draft, error) {
	return draft(s.app, ownerID)
}

func draft(app core.App, ownerID string) (*Draft, error) {
	record, err := findDraftRecord(app, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return draftFromRecord(record), nil
}

func findDraftRecord(app core.App, ownerID string) (*core.Record, error) {
	return app.FindFirstRecordByFilter(DraftsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
}

// StageValidation validates a proposed connection with the exact model and its
// server-resolved API format, then stores it as the owner's single staged
// proposal. The active configuration and its credential stay untouched until
// confirmation.
func (s *Store) StageValidation(ctx context.Context, ownerID string, proposal Proposal) (*Draft, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, fmt.Errorf("%w: a connection owner is required", ErrInvalidSetup)
	}
	active, err := connection(s.app, ownerID)
	if errors.Is(err, ErrNotConfigured) {
		active, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, apiKey, err := normalizeProposal(proposal, active, s.opts.AllowPrivate)
	if err != nil {
		return nil, err
	}
	// Resolve the encryption key before any provider request: a deployment
	// without a usable key must fail without spending provider usage.
	if _, err := s.cipher(); err != nil {
		return nil, err
	}
	// A pinned configuration (custom setup or same-selection repair) is
	// validated against exactly its one format; a new preset selection
	// resolves its format from the preset's supported list, in order.
	candidates := []APIFormat{cfg.APIFormat}
	if cfg.APIFormat == "" {
		preset, _ := PresetFor(cfg.Provider)
		candidates = preset.Formats
	}
	resolved, err := resolveFormat(ctx, s.opts, cfg, apiKey, candidates)
	if err != nil {
		return nil, err
	}
	_, credentialGeneration := nextGenerations(active, resolved)
	sealed, err := s.sealKey(ownerID, credentialGeneration, apiKey)
	if err != nil {
		return nil, err
	}

	var staged *Draft
	err = s.app.RunInTransaction(func(txApp core.App) error {
		collection, err := txApp.FindCollectionByNameOrId(DraftsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", DraftsCollection, err)
		}
		existing, err := findDraftRecord(txApp, ownerID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			existing = nil
		case err != nil:
			return err
		}
		// A re-validation replaces the staged proposal with a new record, so
		// the previous draft id is stale and its confirmation is refused.
		if existing != nil {
			if err := txApp.Delete(existing); err != nil {
				return err
			}
		}
		record := core.NewRecord(collection)
		record.Set("owner", ownerID)
		record.Set("provider", string(resolved.Provider))
		record.Set("api_root", resolved.APIRoot)
		record.Set("api_format", string(resolved.APIFormat))
		record.Set("model", resolved.Model)
		record.Set("credential", sealed)
		record.Set("base_configuration_generation", baseGeneration(active))
		record.Set("base_credential_generation", baseCredential(active))
		record.Set("fingerprint", resolved.Fingerprint())
		record.Set("validated_at", time.Now().UTC())
		if err := txApp.Save(record); err != nil {
			return err
		}
		staged = draftFromRecord(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return staged, nil
}

// Confirm activates the staged proposal with its expected configuration
// generation. A configuration switch is refused while account tasks are
// pending; an initial setup or a same-configuration credential repair is
// allowed. A stale proposal or a lost race changes nothing.
func (s *Store) Confirm(ctx context.Context, ownerID, draftID string, expectedConfigurationGeneration int) (*Connection, error) {
	if strings.TrimSpace(ownerID) == "" || strings.TrimSpace(draftID) == "" {
		return nil, ErrDraftStale
	}
	var confirmed *Connection
	err := s.app.RunInTransaction(func(txApp core.App) error {
		record, err := txApp.FindFirstRecordByFilter(
			DraftsCollection,
			"id = {:id} && owner = {:owner}",
			dbx.Params{"id": draftID, "owner": ownerID},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDraftStale
		}
		if err != nil {
			return err
		}
		staged := draftFromRecord(record)
		active, err := connection(txApp, ownerID)
		if errors.Is(err, ErrNotConfigured) {
			active, err = nil, nil
		}
		if err != nil {
			return err
		}
		// A validation that completed before another committed change is
		// stale: it must never overwrite the newer confirmed configuration.
		if staged.BaseConfigurationGeneration != expectedConfigurationGeneration ||
			staged.BaseConfigurationGeneration != baseGeneration(active) ||
			staged.BaseCredentialGeneration != baseCredential(active) {
			return ErrDraftStale
		}
		switching := active != nil && !staged.Config.Equal(active.Config)
		if switching && s.lock != nil {
			if err := s.lock(ctx, txApp, ownerID); err != nil {
				return err
			}
		}
		configGeneration, credentialGeneration := nextGenerations(active, staged.Config)
		collection, err := txApp.FindCollectionByNameOrId(ConnectionsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", ConnectionsCollection, err)
		}
		target := (*core.Record)(nil)
		if active != nil {
			target, err = txApp.FindRecordById(ConnectionsCollection, active.ID)
			if err != nil {
				return err
			}
		} else {
			target = core.NewRecord(collection)
			target.Set("owner", ownerID)
		}
		target.Set("provider", string(staged.Config.Provider))
		target.Set("api_root", staged.Config.APIRoot)
		target.Set("api_format", string(staged.Config.APIFormat))
		target.Set("model", staged.Config.Model)
		target.Set("configuration_generation", configGeneration)
		target.Set("credential_generation", credentialGeneration)
		target.Set("credential", record.GetString("credential"))
		target.Set("validated_at", record.GetString("validated_at"))
		if err := txApp.Save(target); err != nil {
			return err
		}
		if err := txApp.Delete(record); err != nil {
			return err
		}
		confirmed = connectionFromRecord(target)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return confirmed, nil
}

// Credential unseals the confirmed API key for a future normalization run. It
// reports ErrCredentialUnreadable for a missing key or tampered ciphertext and
// never rewrites the stored value.
func (s *Store) Credential(ctx context.Context, ownerID string) (string, error) {
	active, err := s.Connection(ctx, ownerID)
	if err != nil {
		return "", err
	}
	record, err := s.app.FindRecordById(ConnectionsCollection, active.ID)
	if err != nil {
		return "", err
	}
	return s.openKey(ownerID, active.CredentialGeneration, record.GetString("credential"))
}

// normalizeProposal checks the submitted values and returns the immutable
// configuration, whose API format is empty for a new preset selection: that
// format is established by synthetic validation, so a submitted preset
// format is ignored and never selects a wire contract. A custom setup keeps
// its explicit supported format, and an exact same-selection credential
// repair keeps the recorded format. Every preset default is only a starting
// value: a submitted root is validated and preserved, and an empty root
// receives the preset default.
func normalizeProposal(proposal Proposal, active *Connection, allowPrivate bool) (Config, string, error) {
	cfg := proposal.Config
	cfg.Provider = Provider(strings.TrimSpace(string(cfg.Provider)))
	cfg.APIRoot = strings.TrimRight(strings.TrimSpace(cfg.APIRoot), "/")
	cfg.Model = strings.TrimSpace(cfg.Model)
	if !cfg.Provider.Valid() {
		return Config{}, "", fmt.Errorf("%w: choose a supported provider", ErrInvalidSetup)
	}
	preset, ok := PresetFor(cfg.Provider)
	if !ok {
		return Config{}, "", fmt.Errorf("%w: choose a supported provider", ErrInvalidSetup)
	}
	if cfg.APIRoot == "" {
		cfg.APIRoot = preset.DefaultRoot
	}
	if cfg.APIRoot == "" {
		return Config{}, "", fmt.Errorf("%w: an API root is required", ErrInvalidSetup)
	}
	if !validModelID(cfg.Model) {
		return Config{}, "", fmt.Errorf("%w: enter the exact model identifier", ErrInvalidSetup)
	}
	repairing := active != nil && active.Config.APIFormat.Valid() && active.Config.SameSelection(cfg)
	switch {
	case cfg.Provider == ProviderCustom:
		cfg.APIFormat = APIFormat(strings.TrimSpace(string(proposal.Config.APIFormat)))
		if !cfg.APIFormat.Valid() {
			return Config{}, "", fmt.Errorf("%w: choose a supported API format", ErrInvalidSetup)
		}
	case repairing:
		cfg.APIFormat = active.Config.APIFormat
	default:
		// A new preset selection carries no format: validation resolves it
		// from the preset's supported list.
		cfg.APIFormat = ""
	}
	if cfg.APIFormat != "" && !slices.Contains(preset.Formats, cfg.APIFormat) {
		return Config{}, "", fmt.Errorf("%w: the API format is not supported by this provider", ErrInvalidSetup)
	}
	if _, err := apiRoot(cfg, allowPrivate); err != nil {
		return Config{}, "", err
	}
	apiKey := strings.TrimSpace(proposal.APIKey)
	if apiKey == "" || len(apiKey) > maxAPIKeyLength || strings.IndexFunc(apiKey, unicode.IsControl) >= 0 {
		return Config{}, "", fmt.Errorf("%w: enter the API key", ErrInvalidSetup)
	}
	return cfg, apiKey, nil
}

// nextGenerations returns the configuration and credential generations a
// confirmation installs. Switching the configuration bumps the configuration
// generation; every activated credential bumps only its own version.
func nextGenerations(active *Connection, cfg Config) (int, int) {
	if active == nil {
		return 1, 1
	}
	configGeneration := active.ConfigurationGeneration
	if !cfg.Equal(active.Config) {
		configGeneration++
	}
	return configGeneration, active.CredentialGeneration + 1
}

func baseGeneration(active *Connection) int {
	if active == nil {
		return 0
	}
	return active.ConfigurationGeneration
}

func baseCredential(active *Connection) int {
	if active == nil {
		return 0
	}
	return active.CredentialGeneration
}

func connectionFromRecord(record *core.Record) *Connection {
	return &Connection{
		ID: record.Id,
		Config: Config{
			Provider:  Provider(record.GetString("provider")),
			APIRoot:   record.GetString("api_root"),
			APIFormat: APIFormat(record.GetString("api_format")),
			Model:     record.GetString("model"),
		},
		ConfigurationGeneration: record.GetInt("configuration_generation"),
		CredentialGeneration:    record.GetInt("credential_generation"),
		APIKeyConfigured:        record.GetString("credential") != "",
		ValidatedAt:             record.GetDateTime("validated_at").Time(),
		UpdatedAt:               record.GetDateTime("updated").Time(),
	}
}

func draftFromRecord(record *core.Record) *Draft {
	return &Draft{
		ID: record.Id,
		Config: Config{
			Provider:  Provider(record.GetString("provider")),
			APIRoot:   record.GetString("api_root"),
			APIFormat: APIFormat(record.GetString("api_format")),
			Model:     record.GetString("model"),
		},
		BaseConfigurationGeneration: record.GetInt("base_configuration_generation"),
		BaseCredentialGeneration:    record.GetInt("base_credential_generation"),
		ValidatedAt:                 record.GetDateTime("validated_at").Time(),
	}
}
