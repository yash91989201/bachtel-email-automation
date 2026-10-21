package mailbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

const (
	testTenantID = "11111111-1111-1111-1111-111111111111"
	testClientID = "22222222-2222-2222-2222-222222222222"
)

func saveSettings(t *testing.T, store *mailbox.Store, ownerID, secret string) int {
	t.Helper()
	generation, err := store.SaveSettings(context.Background(), ownerID, mailbox.ProviderMicrosoft, testTenantID, testClientID, secret)
	if err != nil {
		t.Fatalf("save provider settings: %v", err)
	}
	return generation
}

func microsoftCandidate(ownerID string, settingsGeneration int) mailbox.Candidate {
	return mailbox.Candidate{
		OwnerID:            ownerID,
		Provider:           mailbox.ProviderMicrosoft,
		Sender:             "user@tenant.example.test",
		Identity:           "graph-1@" + testTenantID,
		SettingsGeneration: settingsGeneration,
		PromoteSettings:    true,
		Credential: mailbox.Credential{
			Provider:     mailbox.ProviderMicrosoft,
			AccessToken:  "access-1",
			RefreshToken: "refresh-1",
		},
	}
}

// A saved registration is staged, encrypted and invisible to other operators
// until the sign-in that validates it activates it together with the tokens.
func TestSavedProviderSetupStagesThenActivatesWithSignIn(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	owner := provision(t, app, "owner@example.test")
	other := provision(t, app, "other@example.test")
	ctx := context.Background()

	if generation := saveSettings(t, store, owner, "client-secret-1"); generation != 1 {
		t.Fatalf("first saved generation = %d, want 1", generation)
	}
	saved, err := store.Settings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Saved() || !saved.HasPending() || saved.PendingGeneration != 1 {
		t.Fatalf("saved setup = %+v, want a pending generation 1", saved)
	}
	if _, err := store.ActiveSettings(ctx, owner); !errors.Is(err, mailbox.ErrSettingsMissing) {
		t.Fatalf("active settings before sign-in = %v, want ErrSettingsMissing", err)
	}
	pending, err := store.PendingSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if pending.ClientSecret != "client-secret-1" || pending.TenantID != testTenantID || pending.ClientID != testClientID {
		t.Fatalf("pending settings = %+v", pending)
	}

	// Another operator cannot read this setup at all.
	if _, err := store.Settings(ctx, other); !errors.Is(err, mailbox.ErrSettingsMissing) {
		t.Fatalf("foreign settings = %v, want ErrSettingsMissing", err)
	}

	connection, err := store.Install(ctx, microsoftCandidate(owner, 1))
	if err != nil {
		t.Fatal(err)
	}
	if connection.SettingsGeneration != 1 {
		t.Fatalf("connection settings generation = %d, want 1", connection.SettingsGeneration)
	}
	saved, err = store.Settings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved() || saved.HasPending() || saved.Generation != 1 {
		t.Fatalf("saved setup after sign-in = %+v, want active generation 1", saved)
	}
	active, err := store.ActiveSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.ClientSecret != "client-secret-1" {
		t.Fatalf("active secret = %q", active.ClientSecret)
	}
}

func TestSavedProviderSetupRejectsInvalidInput(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	owner := provision(t, app, "owner@example.test")
	ctx := context.Background()

	for name, input := range map[string][3]string{
		"shared tenant alias": {"common", testClientID, "secret"},
		"missing tenant":      {"", testClientID, "secret"},
		"malformed client":    {testTenantID, "client-id", "secret"},
		"empty secret":        {testTenantID, testClientID, "  "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := store.SaveSettings(ctx, owner, mailbox.ProviderMicrosoft, input[0], input[1], input[2])
			if !errors.Is(err, mailbox.ErrSettingsInvalid) {
				t.Fatalf("got %v, want ErrSettingsInvalid", err)
			}
			if value := strings.TrimSpace(input[2]); value != "" && strings.Contains(err.Error(), value) {
				t.Fatalf("validation error must not echo submitted values: %q", err)
			}
		})
	}
	if _, err := store.SaveSettings(ctx, owner, "proton", testTenantID, testClientID, "secret"); !errors.Is(err, mailbox.ErrUnsupportedProvider) {
		t.Fatalf("unsupported provider = %v, want ErrUnsupportedProvider", err)
	}
	// Nothing was saved by the rejected attempts.
	if _, err := store.Settings(ctx, owner); !errors.Is(err, mailbox.ErrSettingsMissing) {
		t.Fatalf("rejected input saved a setup: %v", err)
	}
}

// Saved setup is durable: a new store over the same database, sealed with the
// same deployment key, reads it back without a sign-in.
func TestSavedProviderSetupSurvivesRestart(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "owner@example.test")
	ctx := context.Background()

	first := newStore(t, app)
	saveSettings(t, first, owner, "client-secret-1")
	if _, err := first.Install(ctx, microsoftCandidate(owner, 1)); err != nil {
		t.Fatal(err)
	}

	restarted := newStore(t, app)
	saved, err := restarted.Settings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved() || saved.TenantID != testTenantID || saved.ClientID != testClientID {
		t.Fatalf("saved setup after restart = %+v", saved)
	}
	active, err := restarted.ActiveSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.ClientSecret != "client-secret-1" {
		t.Fatalf("active secret after restart = %q", active.ClientSecret)
	}
}

// An unreadable secret is reported and never rewritten: the operator restores
// the key or saves the registration again.
func TestUnreadableProviderSettingsPreserveCiphertext(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "owner@example.test")
	ctx := context.Background()

	store := newStore(t, app)
	saveSettings(t, store, owner, "client-secret-1")
	if _, err := store.Install(ctx, microsoftCandidate(owner, 1)); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindFirstRecordByFilter(mailbox.SettingsCollection, "owner = {:owner}", map[string]any{"owner": owner})
	if err != nil {
		t.Fatal(err)
	}
	sealed := record.GetString("secret")
	if sealed == "" || strings.Contains(sealed, "client-secret-1") {
		t.Fatalf("stored secret must be sealed, got %q", sealed)
	}

	wrongKey := mailbox.NewStore(app, func() ([]byte, error) { return []byte("fedcba9876543210fedcba9876543210"), nil })
	if _, err := wrongKey.ActiveSettings(ctx, owner); !errors.Is(err, mailbox.ErrCredentialsUnreadable) {
		t.Fatalf("wrong key = %v, want ErrCredentialsUnreadable", err)
	}
	reloaded, err := app.FindRecordById(mailbox.SettingsCollection, record.Id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetString("secret") != sealed {
		t.Fatal("an unreadable secret was overwritten")
	}
	if _, err := store.ActiveSettings(ctx, owner); err != nil {
		t.Fatalf("original key stopped working: %v", err)
	}
}

// Rotating application settings for the same mailbox updates the registration
// and the credentials together while the mailbox generation stays put, so
// approvals for the exact content keep authorizing sends.
func TestRotatePromotesStagedSettingsWithoutBumpingGeneration(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	owner := provision(t, app, "owner@example.test")
	ctx := context.Background()

	saveSettings(t, store, owner, "client-secret-1")
	connection, err := store.Install(ctx, microsoftCandidate(owner, 1))
	if err != nil {
		t.Fatal(err)
	}

	staged := saveSettings(t, store, owner, "client-secret-2")
	if staged != 2 {
		t.Fatalf("staged generation = %d, want 2", staged)
	}
	rotated := mailbox.Credential{Provider: mailbox.ProviderMicrosoft, AccessToken: "access-2", RefreshToken: "refresh-2"}
	if err := store.Rotate(ctx, connection, rotated, staged, true); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != 1 {
		t.Fatalf("mailbox generation = %d, want 1 (approvals stay valid)", current.Generation)
	}
	if current.SettingsGeneration != 2 {
		t.Fatalf("settings generation = %d, want 2", current.SettingsGeneration)
	}
	active, err := store.ActiveSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.ClientSecret != "client-secret-2" || active.Generation != 2 {
		t.Fatalf("active settings = %+v", active)
	}
	credential, err := store.Credential(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "access-2" {
		t.Fatalf("rotated access token = %q", credential.AccessToken)
	}

	// A reference to a superseded generation changes nothing.
	stale := mailbox.Credential{Provider: mailbox.ProviderMicrosoft, AccessToken: "access-stale"}
	if err := store.Rotate(ctx, current, stale, 99, false); !errors.Is(err, mailbox.ErrSettingsStale) {
		t.Fatalf("stale rotation = %v, want ErrSettingsStale", err)
	}
	credential, err = store.Credential(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "access-2" {
		t.Fatalf("stale rotation overwrote credentials: %q", credential.AccessToken)
	}
	active, err = store.ActiveSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.ClientSecret != "client-secret-2" {
		t.Fatalf("stale rotation changed the registration: %+v", active)
	}
}

// A replacement activates the staged registration only when it completes: a
// cancelled proposal leaves the active registration untouched.
func TestReplacementActivationPromotesStagedRegistration(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	owner := provision(t, app, "owner@example.test")
	ctx := context.Background()

	if _, err := store.Install(ctx, candidate(owner, mailbox.ProviderTitan, "titan@example.test")); err != nil {
		t.Fatal(err)
	}
	saveSettings(t, store, owner, "client-secret-1")

	microsoft := microsoftCandidate(owner, 1)
	proposal, err := store.StageReplacement(ctx, microsoft)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.RotationGeneration != 1 {
		t.Fatalf("proposal rotation generation = %d, want 1", proposal.RotationGeneration)
	}
	if _, err := store.ActiveSettings(ctx, owner); !errors.Is(err, mailbox.ErrSettingsMissing) {
		t.Fatalf("staging activated the registration: %v", err)
	}
	if err := store.CancelReset(ctx, owner, proposal.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Settings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HasPending() || saved.Generation != 0 {
		t.Fatalf("cancelled replacement changed the registration: %+v", saved)
	}

	proposal, err = store.StageReplacement(ctx, microsoft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmReset(ctx, owner, proposal.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateReset(ctx, proposal.ID); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if current.Provider != mailbox.ProviderMicrosoft || current.SettingsGeneration != 1 {
		t.Fatalf("activated connection = %+v", current)
	}
	active, err := store.ActiveSettings(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if active.ClientSecret != "client-secret-1" {
		t.Fatalf("active registration after activation = %+v", active)
	}
}
