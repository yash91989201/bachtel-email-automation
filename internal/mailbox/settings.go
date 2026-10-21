package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// SettingsCollection holds one operator-saved provider setup: the non-secret
// identifiers the operator entered plus the sealed application secret.
const SettingsCollection = "mail_provider_settings"

// Errors reported by the saved provider setup.
var (
	// ErrSettingsMissing reports an operator without a saved provider setup.
	ErrSettingsMissing = errors.New("save your provider settings before connecting")
	// ErrSettingsInvalid reports rejected setup input. The message names the
	// field, never the value.
	ErrSettingsInvalid = errors.New("the provider settings are not valid")
	// ErrSettingsStale reports a staged or referenced settings generation that
	// no longer matches the saved setup. The operator starts a fresh attempt.
	ErrSettingsStale = errors.New("the saved provider settings changed; start a new sign-in")
)

// uuidPattern accepts an explicit tenant or application UUID. Shared tenant
// aliases such as "common" or "organizations" are deliberately rejected.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Settings is the operator-visible provider setup. It never carries secrets.
type Settings struct {
	OwnerID    string
	Provider   Provider
	TenantID   string
	ClientID   string
	Generation int
	// Pending describes a staged registration change that becomes active only
	// after a successful sign-in.
	PendingTenantID   string
	PendingClientID   string
	PendingGeneration int
}

// HasPending reports a staged registration awaiting sign-in.
func (s *Settings) HasPending() bool { return s.PendingGeneration > 0 }

// Saved reports an active registration the operator can authorize against.
func (s *Settings) Saved() bool { return s.Generation > 0 }

// ProviderSettings is the server-side setup snapshot including the sealed
// secret. It is never rendered, logged, or returned to a browser.
type ProviderSettings struct {
	OwnerID      string
	Provider     Provider
	TenantID     string
	ClientID     string
	ClientSecret string
	Generation   int
	// Pending reports that this snapshot is the staged registration rather
	// than the active one.
	Pending bool
}

// SaveSettings stages a provider registration change for the operator. The
// active registration and its credentials stay untouched until a sign-in under
// the staged generation succeeds. It reports the staged generation.
func (s *Store) SaveSettings(ctx context.Context, ownerID string, provider Provider, tenantID, clientID, clientSecret string) (int, error) {
	if ownerID == "" {
		return 0, errors.New("a settings owner is required")
	}
	if !provider.Valid() {
		return 0, ErrUnsupportedProvider
	}
	tenantID = strings.TrimSpace(tenantID)
	clientID = strings.TrimSpace(clientID)
	if !uuidPattern.MatchString(tenantID) {
		return 0, fmt.Errorf("%w: Tenant ID must be the directory UUID", ErrSettingsInvalid)
	}
	if !uuidPattern.MatchString(clientID) {
		return 0, fmt.Errorf("%w: Client ID must be the application UUID", ErrSettingsInvalid)
	}
	if strings.TrimSpace(clientSecret) == "" {
		return 0, fmt.Errorf("%w: Client Secret is required", ErrSettingsInvalid)
	}

	var generation int
	err := s.app.RunInTransaction(func(app core.App) error {
		// A staged or running replacement already binds the pending generation;
		// changing it would make that confirmation promote the wrong setup.
		if _, err := app.FindFirstRecordByFilter(
			ResetsCollection,
			"owner = {:owner} && status != {:done}",
			dbx.Params{"owner": ownerID, "done": ResetCompleted},
		); err == nil {
			return ErrResetInProgress
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		collection, err := app.FindCollectionByNameOrId(SettingsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", SettingsCollection, err)
		}
		record, err := app.FindFirstRecordByFilter(SettingsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			record = core.NewRecord(collection)
			record.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
			record.Set("owner", ownerID)
		case err != nil:
			return fmt.Errorf("read the saved provider setup: %w", err)
		case record.GetString("provider") != "" && record.GetString("provider") != string(provider):
			return ErrUnsupportedProvider
		}
		if record.GetInt("generation") == 0 && record.GetInt("pending_generation") == 0 {
			// First save: nothing is active yet, so this draft is generation 1.
			generation = 1
		} else {
			generation = max(record.GetInt("generation"), record.GetInt("pending_generation")) + 1
		}
		sealed, err := s.seal(ownerID, provider, settingsPurpose(record.Id), Credential{Provider: provider, ClientSecret: clientSecret})
		if err != nil {
			return err
		}
		record.Set("provider", string(provider))
		record.Set("pending_tenant_id", tenantID)
		record.Set("pending_client_id", clientID)
		record.Set("pending_secret", sealed)
		record.Set("pending_generation", generation)
		return app.Save(record)
	})
	if err != nil {
		return 0, err
	}
	return generation, nil
}

// Settings returns the operator-visible saved setup without secrets.
func (s *Store) Settings(ctx context.Context, ownerID string) (*Settings, error) {
	record, err := s.app.FindFirstRecordByFilter(SettingsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSettingsMissing
	}
	if err != nil {
		return nil, fmt.Errorf("read the saved provider setup: %w", err)
	}
	return &Settings{
		OwnerID:           ownerID,
		Provider:          Provider(record.GetString("provider")),
		TenantID:          record.GetString("tenant_id"),
		ClientID:          record.GetString("client_id"),
		Generation:        record.GetInt("generation"),
		PendingTenantID:   record.GetString("pending_tenant_id"),
		PendingClientID:   record.GetString("pending_client_id"),
		PendingGeneration: record.GetInt("pending_generation"),
	}, nil
}

// ActiveSettings returns the active registration, including the secret for
// server-side authorization and refresh.
func (s *Store) ActiveSettings(ctx context.Context, ownerID string) (*ProviderSettings, error) {
	return s.settings(ctx, ownerID, false)
}

// PendingSettings returns the staged registration awaiting sign-in.
func (s *Store) PendingSettings(ctx context.Context, ownerID string) (*ProviderSettings, error) {
	return s.settings(ctx, ownerID, true)
}

func (s *Store) settings(ctx context.Context, ownerID string, pending bool) (*ProviderSettings, error) {
	record, err := s.app.FindFirstRecordByFilter(SettingsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSettingsMissing
	}
	if err != nil {
		return nil, fmt.Errorf("read the saved provider setup: %w", err)
	}
	out := &ProviderSettings{
		OwnerID:  ownerID,
		Provider: Provider(record.GetString("provider")),
		Pending:  pending,
	}
	if pending {
		out.TenantID = record.GetString("pending_tenant_id")
		out.ClientID = record.GetString("pending_client_id")
		out.Generation = record.GetInt("pending_generation")
	} else {
		out.TenantID = record.GetString("tenant_id")
		out.ClientID = record.GetString("client_id")
		out.Generation = record.GetInt("generation")
	}
	if out.Generation == 0 {
		return nil, ErrSettingsMissing
	}
	sealed := record.GetString("secret")
	if pending {
		sealed = record.GetString("pending_secret")
	}
	credential, err := s.open(ownerID, out.Provider, settingsPurpose(record.Id), sealed)
	if err != nil {
		return nil, err
	}
	if credential.ClientSecret == "" {
		return nil, ErrSettingsMissing
	}
	out.ClientSecret = credential.ClientSecret
	return out, nil
}

// promoteSettings makes the staged registration active inside the caller's
// transaction, but only when it is still the generation the validated
// credential was issued under. A zero generation means no staged change.
func promoteSettings(app core.App, ownerID string, provider Provider, generation int) error {
	if generation == 0 {
		return nil
	}
	record, err := app.FindFirstRecordByFilter(SettingsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSettingsStale, err)
	}
	if record.GetInt("generation") == generation {
		// A retried activation already promoted this generation.
		return nil
	}
	if record.GetInt("pending_generation") != generation || record.GetString("provider") != string(provider) {
		return ErrSettingsStale
	}
	record.Set("tenant_id", record.GetString("pending_tenant_id"))
	record.Set("client_id", record.GetString("pending_client_id"))
	record.Set("secret", record.GetString("pending_secret"))
	record.Set("generation", generation)
	record.Set("pending_tenant_id", "")
	record.Set("pending_client_id", "")
	record.Set("pending_secret", "")
	record.Set("pending_generation", 0)
	return app.Save(record)
}

func settingsPurpose(recordID string) string {
	return "settings:" + recordID
}
