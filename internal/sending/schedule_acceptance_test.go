package sending_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// TestAcceptanceScheduleMissedAcrossRestartAndResume restarts the dispatcher
// after a stored schedule instant passes. Recovery must pause the task with a
// missed-schedule reason and no attempt, and resume must send exactly the
// candidates still free, once, then settle to completion.
func TestAcceptanceScheduleMissedAcrossRestartAndResume(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)
	f.approve(t, task, d)

	at := f.clock.Now().Add(time.Hour)
	scheduleTask(t, f, task, at, 2)
	loaded := taskRecord(t, f, task)
	if loaded.Status != intake.TaskReady || loaded.ScheduleState != intake.ScheduleScheduled || !loaded.ScheduleAt.Equal(at) {
		t.Fatalf("scheduled task = %s %s %s", loaded.Status, loaded.ScheduleState, loaded.ScheduleAt)
	}

	// Clean restart: shutdown releases process ownership, then the clock moves
	// past the stored instant while no dispatcher owns the data directory.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := d.Shutdown(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	setClock(f, at.Add(time.Minute))
	d2 := f.dispatcher(t, mailbox)

	// Startup recovery pauses the missed schedule without sending anything.
	loaded = taskRecord(t, f, task)
	if loaded.Status != "paused" || loaded.ScheduleState != intake.ScheduleMissed {
		t.Fatalf("recovered task = %s %s", loaded.Status, loaded.ScheduleState)
	}
	if reason := storeRecord(t, f, intake.TasksCollection, task).GetString("pause_reason"); !strings.Contains(reason, "scheduled start time passed") {
		t.Fatalf("pause reason = %q", reason)
	}
	if attemptCount(t, f) != 0 {
		t.Fatal("restart over a missed schedule created a candidate attempt")
	}
	if reserved := taskReserved(t, f, task); reserved != 0 {
		t.Fatalf("restart reserved %d candidates", reserved)
	}
	if mailbox.count() != 1 {
		t.Fatalf("restart requests = %d, want only the diagnostic test", mailbox.count())
	}

	// Another task claims candidate0000 before resume, so only candidate0001
	// is free when the operator resumes the remainder.
	other := f.task(t, 1)
	candidates, err := f.app.FindRecordsByFilter(intake.CandidatesCollection, "campaign={:c}", "email_key", 0, 0, dbx.Params{"c": f.campaignID})
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates: %v %v", candidates, err)
	}
	if candidates[0].GetString("email_key") != "candidate0000@example.invalid" {
		t.Fatalf("first candidate = %q", candidates[0].GetString("email_key"))
	}
	candidates[0].Set("claim_task", other)
	storeSave(t, f, candidates[0])

	storeAct(t, f, task, "resume", "resume-schedule", 1)
	loaded = taskRecord(t, f, task)
	if loaded.Status != "running" || loaded.ScheduleState != intake.ScheduleNone || !loaded.ScheduleAt.IsZero() {
		t.Fatalf("resumed task = %s %s %s", loaded.Status, loaded.ScheduleState, loaded.ScheduleAt)
	}
	if reserved := taskReserved(t, f, task); reserved != 1 {
		t.Fatalf("resume reserved %d candidates", reserved)
	}

	d2.Wake()
	candidateMessages := func() []sending.Message {
		mailbox.mu.Lock()
		defer mailbox.mu.Unlock()
		var seen []sending.Message
		for _, m := range mailbox.messages {
			if strings.HasPrefix(m.Recipient, "candidate") {
				seen = append(seen, m)
			}
		}
		return seen
	}
	acceptanceEventually(t, func() bool { return len(candidateMessages()) == 1 })
	if got := candidateMessages(); got[0].Recipient != "candidate0001@example.invalid" {
		t.Fatalf("resume sent to %q, want the free candidate0001", got[0].Recipient)
	}
	// The mailbox holds the diagnostic test plus the single resumed candidate.
	if mailbox.count() != 2 {
		t.Fatalf("requests = %d, want 2 (test + one candidate)", mailbox.count())
	}

	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(intake.TasksCollection, task)
		return e == nil && r.GetString("status") == "completed"
	})
	// Settling must not re-fire the schedule or launch another candidate.
	time.Sleep(100 * time.Millisecond)
	if mailbox.count() != 2 || attemptCount(t, f) != 1 {
		t.Fatalf("after settle: requests = %d, candidate attempts = %d", mailbox.count(), attemptCount(t, f))
	}
}
