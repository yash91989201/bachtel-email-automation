package sending_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// closedTask reads the durable closure markers.
func closedTask(t *testing.T, f *acceptanceFixture, taskID string) (closedAt, requestedAt, scheduleState string) {
	t.Helper()
	record, err := f.app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("closed_at"), record.GetString("closure_requested_at"), record.GetString("schedule_state")
}

func TestCloseReadyTaskIsTerminalAndIdempotent(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	// Record a stored schedule authorization directly: closure must cancel it
	// without deleting the recorded start time.
	future := types.NowDateTime().Add(2 * time.Hour).Time()
	if _, err := f.app.DB().NewQuery("UPDATE send_tasks SET schedule_state = 'scheduled', schedule_at = {:at} WHERE id = {:id}").
		Bind(dbx.Params{"at": future.UTC().Format(types.DefaultDateLayout), "id": task}).Execute(); err != nil {
		t.Fatal(err)
	}

	result, err := f.store.CloseTask(context.Background(), f.ownerID, task, "operator", "close-ready")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if result.Status != "closed" {
		t.Fatalf("close status = %q, want closed", result.Status)
	}
	closedAt, requestedAt, scheduleState := closedTask(t, f, task)
	if closedAt == "" || requestedAt == "" {
		t.Fatalf("closure markers missing: closed=%q requested=%q", closedAt, requestedAt)
	}
	if scheduleState != intake.ScheduleCancelled {
		t.Fatalf("schedule state = %q, want cancelled", scheduleState)
	}

	// Every processing, test and sending action is refused.
	for _, kind := range []string{"start", "resume", "retry", "retry_all"} {
		_, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "after-" + kind, TaskID: task, Kind: kind, Actor: "operator", Confirmed: true, ExpectedCount: 2})
		if err == nil || err.Error() != sending.ClosedTaskMessage {
			t.Fatalf("%s on a closed task = %v", kind, err)
		}
	}
	if _, err := f.store.QueueTest(context.Background(), f.ownerID, sending.TestRequest{ActionID: "closed-test", TaskID: task, Recipient: "operator@example.invalid", Actor: "operator", Values: map[string]string{"candidate_email": "a@b.test", "candidate_name": "A"}}); err == nil || err.Error() != sending.ClosedTaskMessage {
		t.Fatalf("test on closed task = %v", err)
	}
	if _, err := f.store.Schedule(context.Background(), f.ownerID, sending.ScheduleRequest{ActionID: "closed-schedule", TaskID: task, At: future, Confirmed: true, ExpectedCount: 2}); err == nil {
		t.Fatal("schedule on closed task succeeded")
	}

	// A repeated valid request is idempotent.
	repeated, err := f.store.CloseTask(context.Background(), f.ownerID, task, "operator", "close-ready-2")
	if err != nil || repeated.Status != "closed" {
		t.Fatalf("repeated close = %+v, %v", repeated, err)
	}
	// A foreign operator cannot close it.
	if _, err := f.store.CloseTask(context.Background(), f.ownerID+"x", task, "intruder", "close-foreign"); !errors.Is(err, sending.ErrNotFound) {
		t.Fatalf("foreign close = %v", err)
	}
}

func TestCloseActiveTaskSettlesInFlightWorkBeforeReleasing(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	hold := make(chan struct{})
	mailbox := &acceptanceMailbox{hold: hold}
	d := f.dispatcher(t, mailbox)
	f.approve(t, task, d)
	f.act(t, task, "start", "start-close", 2)
	d.Wake()
	acceptanceEventually(t, func() bool {
		rows, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "task = {:t} && kind = 'candidate' && state = 'in_flight'", "", 0, 0, dbx.Params{"t": task})
		return err == nil && len(rows) > 0
	})

	result, err := f.store.CloseTask(context.Background(), f.ownerID, task, "operator", "close-active")
	if err != nil {
		t.Fatalf("close active: %v", err)
	}
	if result.Status != "closing" {
		t.Fatalf("active close status = %q, want closing", result.Status)
	}
	if closedAt, _, _ := closedTask(t, f, task); closedAt != "" {
		t.Fatal("closure reported final while a request was still in flight")
	}
	// The unresolved in-flight attempt is never rewritten as cancelled.
	if _, err := f.app.FindFirstRecordByFilter(sending.AttemptsCollection, "task = {:t} && state = 'cancelled'", dbx.Params{"t": task}); err == nil {
		t.Fatal("in-flight attempt was rewritten as cancelled")
	}

	close(hold)
	acceptanceEventually(t, func() bool {
		closedAt, _, _ := closedTask(t, f, task)
		return closedAt != ""
	})
	// The submitted outcome and its attempt history remain.
	progress, err := f.store.Preview(context.Background(), f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Submitted+progress.Held == 0 || len(progress.Recent) == 0 {
		t.Fatalf("closure erased attempt evidence: submitted=%d held=%d recent=%d", progress.Submitted, progress.Held, len(progress.Recent))
	}
	// A new start is refused once closure is final.
	if _, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "restart-after-close", TaskID: task, Kind: "resume", Actor: "operator", Confirmed: true, ExpectedCount: 1}); err == nil || err.Error() != sending.ClosedTaskMessage {
		t.Fatalf("resume after close = %v", err)
	}
}
