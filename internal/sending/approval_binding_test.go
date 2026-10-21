package sending_test

import (
	"context"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
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

// An approval is the operator's own evidence for this exact content and
// mailbox: a record bound to another operator's connection never authorizes
// sending, and same-mailbox credential rotation never resumes work by itself.
func TestApprovalIsOwnerBoundAndRotationNeverResumesWork(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	task := f.task(t, 2)
	_, foreignConnection := f.addOperator(t, "second@example.invalid", "second@example.invalid")
	d := f.dispatcher(t, &acceptanceMailbox{})
	f.approve(t, task, d)

	approvals, err := f.app.FindCollectionByNameOrId(sending.ApprovalsCollection)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := f.app.FindFirstRecordByFilter(sending.ApprovalsCollection, "owner = {:owner}", map[string]any{"owner": f.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	// Point the recorded evidence at a connection this operator does not own.
	if err = f.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(approvals.Id, valid.Id)
		if err != nil {
			return err
		}
		record.Set("connection", foreignConnection)
		record.Set("connection_generation", 1)
		return app.Save(record)
	}); err != nil {
		t.Fatal(err)
	}

	progress, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Approved {
		t.Fatal("an approval naming another operator's connection authorized sends")
	}
	if _, err := f.store.Act(ctx, f.ownerID, sending.ActionRequest{ActionID: "start-forged", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: progress.Eligible}); err == nil {
		t.Fatal("start was accepted with a foreign approval")
	}

	// The operator re-approves the same submitted test for their own mailbox.
	attempt, err := f.app.FindFirstRecordByFilter(
		sending.AttemptsCollection,
		"task = {:task} && kind = {:kind} && state = {:state}",
		map[string]any{"task": task, "kind": sending.KindTest, "state": sending.AttemptSubmitted},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Approve(ctx, f.ownerID, task, attempt.Id, "acceptance", "approve-again-"+task); err != nil {
		t.Fatal(err)
	}

	// Rotation for the same mailbox keeps the mailbox generation, so the
	// approval still authorizes work — and the task stays exactly where the
	// operator left it instead of resuming.
	store := mailbox.NewStore(f.app, func() ([]byte, error) { return testKey, nil })
	connection, err := store.Current(ctx, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconnect(ctx, connection, mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "rotated-password"}); err != nil {
		t.Fatal(err)
	}
	rotated, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if !rotated.Approved {
		t.Fatal("same-mailbox credential rotation invalidated the approval")
	}
	after, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	if after.GetString("status") != intake.TaskReady {
		t.Fatalf("rotation changed the task status to %q; it must not resume work", after.GetString("status"))
	}
}
