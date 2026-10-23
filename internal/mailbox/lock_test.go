package mailbox_test

// Connection-change lock tests: the injected account lock refuses switching,
// replacement and disconnect while pending work exists, and never blocks
// same-mailbox repair.

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

func TestChangeGuardRefusesSwitchButNotSameMailboxRepair(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "owner@example.test")
	store := newStore(t, app)
	connection, err := store.Install(context.Background(), candidate(owner, mailbox.ProviderTitan, "mine@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("pending tasks block this change")
	store.SetChangeGuard(func(context.Context, core.App, string) error { return blocked })

	if err := store.Disconnect(context.Background(), connection); !errors.Is(err, blocked) {
		t.Fatalf("disconnect err = %v", err)
	}
	current, err := store.Current(context.Background(), owner)
	if err != nil || current.Status != mailbox.StatusConnected {
		t.Fatalf("disconnect mutated the connection: %+v err=%v", current, err)
	}
	if _, err := store.StageReplacement(context.Background(), candidate(owner, mailbox.ProviderTitan, "other@example.test")); !errors.Is(err, blocked) {
		t.Fatalf("replacement err = %v", err)
	}
	if err := store.Reconnect(context.Background(), current, mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "new-password"}); err != nil {
		t.Fatalf("same-mailbox reconnect was refused: %v", err)
	}
	if _, err := store.Credential(context.Background(), current); err != nil {
		t.Fatalf("repaired credential unreadable: %v", err)
	}
}
