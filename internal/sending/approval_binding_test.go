package sending_test

import (
	"context"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Approval is bound to the exact configuration, the mailbox connection and its
// generation: replacing the mailbox invalidates it, while a same-identity
// credential refresh preserves it.
func TestApprovalFollowsConnectionGeneration(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	task := f.task(t, 2)
	d := f.dispatcher(t, &acceptanceMailbox{})
	f.approve(t, task, d)

	approved, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if !approved.Approved {
		t.Fatal("a submitted test was not approved for the current connection")
	}

	// A replacement bumps the generation. The same revision is no longer
	// approved even though the approval record still exists.
	connection, err := f.app.FindRecordById(mailbox.ConnectionsCollection, f.connectionID)
	if err != nil {
		t.Fatal(err)
	}
	connection.Set("generation", connection.GetInt("generation")+1)
	if err = f.app.Save(connection); err != nil {
		t.Fatal(err)
	}
	replaced, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Approved {
		t.Fatal("an approval survived a mailbox replacement")
	}
	if _, err = f.store.Act(ctx, f.ownerID, sending.ActionRequest{ActionID: "start-after-replacement", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: replaced.Eligible}); err == nil {
		t.Fatal("candidate start was accepted without a test for the replaced mailbox")
	}
}

// A same-identity reconnect keeps the generation, so a still-applicable
// approval continues to authorize candidate sending.
func TestSameIdentityReconnectPreservesApproval(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	task := f.task(t, 2)
	d := f.dispatcher(t, &acceptanceMailbox{})
	f.approve(t, task, d)

	store := mailbox.NewStore(f.app, func() ([]byte, error) { return testKey, nil })
	connection, err := store.Current(ctx, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Reconnect(ctx, connection, mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "rotated-password"}); err != nil {
		t.Fatal(err)
	}
	progress, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if !progress.Approved {
		t.Fatal("a same-identity reconnect invalidated the approval")
	}
	if _, err = f.store.Act(ctx, f.ownerID, sending.ActionRequest{ActionID: "start-after-reconnect", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: progress.Eligible}); err != nil {
		t.Fatalf("candidate start after reconnect: %v", err)
	}
}

// A provider authorization failure pauses only the affected operator's tasks.
func TestMailboxPauseIsScopedToOneOperator(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	otherOwner, _ := f.addOperator(t, "second@example.invalid", "second@example.invalid")
	otherCampaign := f.createCampaign(t, otherOwner, "Second workspace")

	taskA := f.task(t, 2)
	taskB := f.taskFor(t, otherOwner, otherCampaign, 2)
	d := f.dispatcher(t, &acceptanceMailbox{})
	f.approve(t, taskA, d)
	approveFor(t, f, otherOwner, taskB)

	startFor(t, f, f.ownerID, taskA, "start-a")
	startFor(t, f, otherOwner, taskB, "start-b")

	if err := f.store.PauseMailbox(ctx, f.ownerID, "authorization requires operator attention"); err != nil {
		t.Fatal(err)
	}
	first, err := f.app.FindRecordById("send_tasks", taskA)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetString("status") != "paused" {
		t.Fatalf("affected task status = %q, want paused", first.GetString("status"))
	}
	second, err := f.app.FindRecordById("send_tasks", taskB)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetString("status") != "running" {
		t.Fatalf("other operator's task status = %q, want running", second.GetString("status"))
	}

	// A shared storage failure still stops every launch.
	if err := f.store.PauseAll(ctx, "shared storage failure"); err != nil {
		t.Fatal(err)
	}
	second, err = f.app.FindRecordById("send_tasks", taskB)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetString("status") != "paused" {
		t.Fatalf("shared storage failure left a task %q", second.GetString("status"))
	}
}

func startFor(t *testing.T, f *acceptanceFixture, ownerID, task, actionID string) {
	t.Helper()
	progress, err := f.store.Preview(context.Background(), ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Act(context.Background(), ownerID, sending.ActionRequest{ActionID: actionID, TaskID: task, Kind: "start", Actor: "acceptance", Confirmed: true, ExpectedCount: progress.Eligible}); err != nil {
		t.Fatalf("start %s: %v", task, err)
	}
}
