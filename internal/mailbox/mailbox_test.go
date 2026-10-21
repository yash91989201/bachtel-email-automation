package mailbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newApp(t *testing.T) *pocketbase.PocketBase {
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
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	return app
}

func newStore(t *testing.T, app core.App) *mailbox.Store {
	t.Helper()
	return mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
}

func provision(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

func candidate(ownerID string, provider mailbox.Provider, sender string) mailbox.Candidate {
	credential := mailbox.Credential{Provider: provider}
	if provider == mailbox.ProviderTitan {
		credential.Password = "mailbox-password"
	} else {
		credential.AccessToken = "access"
		credential.RefreshToken = "refresh"
	}
	return mailbox.Candidate{OwnerID: ownerID, Provider: provider, Sender: sender, Credential: credential}
}

func TestExclusiveClaimPerOperatorAndMailbox(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	first := provision(t, app, "first@example.test")
	second := provision(t, app, "second@example.test")
	ctx := context.Background()

	connection, err := store.Install(ctx, candidate(first, mailbox.ProviderTitan, "shared@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if connection.Generation != 1 || !connection.Current || connection.Sender != "shared@example.test" {
		t.Fatalf("installed connection = %+v", connection)
	}

	// The same operator cannot hold a second current connection.
	if _, err := store.Install(ctx, candidate(first, mailbox.ProviderTitan, "other@example.test")); !errors.Is(err, mailbox.ErrAlreadyConnected) {
		t.Fatalf("second install for one operator = %v, want ErrAlreadyConnected", err)
	}
	// Another operator cannot claim the same mailbox, whatever the case.
	if _, err := store.Install(ctx, candidate(second, mailbox.ProviderTitan, " Shared@Example.Test ")); !errors.Is(err, mailbox.ErrClaimed) {
		t.Fatalf("cross-operator claim = %v, want ErrClaimed", err)
	}
	if _, err := store.Current(ctx, second); !errors.Is(err, mailbox.ErrNotConnected) {
		t.Fatalf("refused claim created a connection: %v", err)
	}
}

func TestCredentialIsBoundToOwnerProviderAndConnection(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	ownerID := provision(t, app, "owner@example.test")
	ctx := context.Background()

	connection, err := store.Install(ctx, candidate(ownerID, mailbox.ProviderTitan, "owner@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	credential, err := store.Credential(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Password != "mailbox-password" {
		t.Fatalf("credential password = %q", credential.Password)
	}

	// A wrong key must not silently replace the ciphertext.
	wrong := mailbox.NewStore(app, func() ([]byte, error) { return []byte("ffffffffffffffffffffffffffffffff"), nil })
	if _, err := wrong.Credential(ctx, connection); !errors.Is(err, mailbox.ErrCredentialsUnreadable) {
		t.Fatalf("wrong key = %v, want ErrCredentialsUnreadable", err)
	}
	record, err := app.FindRecordById(mailbox.ConnectionsCollection, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	before := record.GetString("credential")
	if before == "" {
		t.Fatal("ciphertext was erased by a failed read")
	}
	if _, err := wrong.Credential(ctx, connection); err == nil {
		t.Fatal("second read with the wrong key succeeded")
	}
	record, err = app.FindRecordById(mailbox.ConnectionsCollection, connection.ID)
	if err != nil || record.GetString("credential") != before {
		t.Fatal("stored ciphertext changed after failed decryption")
	}
}

func TestDisconnectedMailboxKeepsItsClaim(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	first := provision(t, app, "first@example.test")
	second := provision(t, app, "second@example.test")
	ctx := context.Background()

	connection, err := store.Install(ctx, candidate(first, mailbox.ProviderTitan, "shared@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Disconnect(ctx, connection); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != mailbox.StatusDisconnected || current.Sender != "shared@example.test" {
		t.Fatalf("disconnected connection = %+v", current)
	}
	if _, err := store.Credential(ctx, current); !errors.Is(err, mailbox.ErrCredentialsUnreadable) {
		t.Fatalf("disconnected credential = %v, want unreadable", err)
	}
	if _, err := store.Install(ctx, candidate(second, mailbox.ProviderTitan, "shared@example.test")); !errors.Is(err, mailbox.ErrClaimed) {
		t.Fatalf("claim after disconnect = %v, want ErrClaimed", err)
	}
}

func TestReconnectPreservesGeneration(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	ownerID := provision(t, app, "owner@example.test")
	ctx := context.Background()

	connection, err := store.Install(ctx, candidate(ownerID, mailbox.ProviderTitan, "owner@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Disconnect(ctx, connection); err != nil {
		t.Fatal(err)
	}
	if err := store.Reconnect(ctx, connection, mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "new-password"}); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Generation != connection.Generation {
		t.Fatalf("generation = %d, want preserved %d", current.Generation, connection.Generation)
	}
	if current.Status != mailbox.StatusConnected {
		t.Fatalf("status = %q, want connected", current.Status)
	}
	credential, err := store.Credential(ctx, current)
	if err != nil || credential.Password != "new-password" {
		t.Fatalf("credential = %+v, %v", credential, err)
	}
}

func TestReplacementStagesTargetWithoutChangingCurrent(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	ownerID := provision(t, app, "owner@example.test")
	ctx := context.Background()

	current, err := store.Install(ctx, candidate(ownerID, mailbox.ProviderTitan, "old@example.test"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.StageReplacement(ctx, candidate(ownerID, mailbox.ProviderTitan, "old@example.test")); !errors.Is(err, mailbox.ErrSameMailbox) {
		t.Fatalf("same mailbox stage = %v, want ErrSameMailbox", err)
	}

	operation, err := store.StageReplacement(ctx, candidate(ownerID, mailbox.ProviderTitan, "new@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != mailbox.ResetStaging {
		t.Fatalf("reset status = %q", operation.Status)
	}
	after, err := store.Current(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != current.ID || after.Sender != "old@example.test" {
		t.Fatalf("staging changed the current connection: %+v", after)
	}
	target, err := store.ByID(ctx, operation.ToConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Generation != current.Generation+1 || target.Current {
		t.Fatalf("staged target = %+v", target)
	}
	// A competing operator cannot claim the staged target.
	other := provision(t, app, "other@example.test")
	if _, err := store.Install(ctx, candidate(other, mailbox.ProviderTitan, "new@example.test")); !errors.Is(err, mailbox.ErrClaimed) {
		t.Fatalf("competing claim on staged target = %v, want ErrClaimed", err)
	}

	if _, err := store.ConfirmReset(ctx, ownerID, operation.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateReset(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	final, err := store.Current(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if final.ID != target.ID || final.Sender != "new@example.test" || final.Generation != current.Generation+1 {
		t.Fatalf("activated connection = %+v", final)
	}
	if _, err := app.FindRecordById(mailbox.ConnectionsCollection, current.ID); err == nil {
		t.Fatal("the replaced connection still exists after activation")
	}
}

func TestCancelResetReleasesStagedClaim(t *testing.T) {
	app := newApp(t)
	store := newStore(t, app)
	ownerID := provision(t, app, "owner@example.test")
	other := provision(t, app, "other@example.test")
	ctx := context.Background()

	if _, err := store.Install(ctx, candidate(ownerID, mailbox.ProviderTitan, "old@example.test")); err != nil {
		t.Fatal(err)
	}
	operation, err := store.StageReplacement(ctx, candidate(ownerID, mailbox.ProviderTitan, "new@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CancelReset(ctx, ownerID, operation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActiveReset(ctx, ownerID); !errors.Is(err, mailbox.ErrNoReset) {
		t.Fatalf("active reset after cancel = %v", err)
	}
	if _, err := store.Install(ctx, candidate(other, mailbox.ProviderTitan, "new@example.test")); err != nil {
		t.Fatalf("cancelled target claim was not released: %v", err)
	}
}
