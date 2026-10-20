package sending_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// TestRestoreReviewBlocksSendingAndScheduling models an old backup restore:
// candidate submission history is absent from the restored database, but the
// restore-review flag keeps those recipients from silently becoming eligible.
func TestRestoreReviewBlocksSendingAndScheduling(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	restored := sending.NewStore(f.app, sending.Options{
		RestoreReview: true,
		SenderAddress: "hr@example.invalid",
		Now:           f.clock.Now,
	})
	ctx := context.Background()

	_, err := restored.Act(ctx, sending.ActionRequest{ActionID: "restore-start", TaskID: task, Kind: "start", Actor: "operator", Confirmed: true, ExpectedCount: 2})
	var refusal *sending.ScheduleRefusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "restore review") {
		t.Fatalf("restore review did not refuse start: %v", err)
	}
	work, err := restored.NextWork(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if work != nil {
		t.Fatal("restore review still selected candidate work")
	}
	_, err = restored.Schedule(ctx, sending.ScheduleRequest{ActionID: "restore-schedule", TaskID: task, Actor: "operator", At: f.clock.Now().Add(time.Hour), Confirmed: true, ExpectedCount: 2})
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "restore review") {
		t.Fatalf("restore review did not refuse scheduling: %v", err)
	}
	if status, err := restored.Preview(ctx, task); err != nil || !status.RestoreReview {
		t.Fatalf("restore review not reported in progress: %+v %v", status, err)
	}

	// The flag is explicit configuration, not a data mutation: the restored
	// candidates remain untouched and no attempt was created.
	attempts, err := f.app.CountRecords(sending.AttemptsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("restore review created %d attempts", attempts)
	}
	members, err := f.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t}", "", 0, 0, map[string]any{"t": task})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("restored rows changed: %d", len(members))
	}
	for _, member := range members {
		if member.GetString("dispatch_state") != "" {
			t.Fatalf("restore review reserved a row: %q", member.GetString("dispatch_state"))
		}
	}
}

// TestAttemptStoresSenderForHistoricalHTML verifies the recorded sender is the
// configured mailbox at queue time, independent of later configuration.
func TestAttemptStoresSenderForHistoricalHTML(t *testing.T) {
	f := newAcceptanceFixture(t)
	f.store = sending.NewStore(f.app, sending.Options{
		SenderAddress: "hr@example.invalid",
		Now:           f.clock.Now,
	})
	task := f.task(t, 1)
	ctx := context.Background()
	test, err := f.store.QueueTest(ctx, sending.TestRequest{
		ActionID:  "sender-test-123456",
		TaskID:    task,
		Recipient: "operator@example.invalid",
		Values:    map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"},
	})
	if err != nil {
		t.Fatal(err)
	}
	work, err := f.store.NextWork(ctx)
	if err != nil || work == nil {
		t.Fatalf("next test work: %v %v", work, err)
	}
	attempt, err := f.store.BeginAttempt(ctx, work, f.clock.Now(), f.clock.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.CompleteAttempt(ctx, attempt.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Approve(ctx, task, attempt.ID, "operator", "sender-approve"); err != nil {
		t.Fatal(err)
	}

	if _, err = f.store.Act(ctx, sending.ActionRequest{ActionID: "sender-start", TaskID: task, Kind: "start", Actor: "operator", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	work, err = f.store.NextWork(ctx)
	if err != nil || work == nil {
		t.Fatalf("next candidate work: %v %v", work, err)
	}
	candidate, err := f.store.BeginAttempt(ctx, work, f.clock.Now(), f.clock.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.app.FindRecordById(sending.AttemptsCollection, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.GetString("sender"); got != "hr@example.invalid" {
		t.Fatalf("attempt sender = %q, want the configured mailbox", got)
	}
	testRecord, err := f.app.FindRecordById(sending.AttemptsCollection, test.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := testRecord.GetString("sender"); got != "hr@example.invalid" {
		t.Fatalf("test attempt sender = %q", got)
	}
}
