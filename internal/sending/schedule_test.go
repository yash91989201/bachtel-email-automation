package sending_test

import (
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// setClock moves the fixture's simulated clock, standing in for wall time.
func setClock(f *acceptanceFixture, at time.Time) {
	f.clock.mu.Lock()
	f.clock.now = at
	f.clock.mu.Unlock()
}

func storeSchedule(t *testing.T, f *acceptanceFixture, task, id string, at time.Time, count int) *sending.ActionResult {
	t.Helper()
	result, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: id, TaskID: task, Actor: "operator", At: at, Confirmed: true, ExpectedCount: count})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func scheduleTask(t *testing.T, f *acceptanceFixture, task string, at time.Time, count int) {
	t.Helper()
	storeSchedule(t, f, task, "schedule-"+task, at, count)
}

func taskRecord(t *testing.T, f *acceptanceFixture, task string) *intake.Task {
	t.Helper()
	loaded, err := f.intake.LoadTask(storeCtx, task)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func attemptCount(t *testing.T, f *acceptanceFixture) int {
	t.Helper()
	attempts, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "kind={:kind}", "", 0, 0, dbx.Params{"kind": sending.KindCandidate})
	if err != nil {
		t.Fatal(err)
	}
	return len(attempts)
}

func taskReserved(t *testing.T, f *acceptanceFixture, task string) int {
	t.Helper()
	rows, err := f.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t} && dispatch_state='reserved'", "", 0, 0, dbx.Params{"t": task})
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func TestStoreScheduleValidatesLeadApprovalRevisionAndEligibility(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	storeApprove(t, f, task)
	now := f.clock.Now()
	tooSoon := now.Add(sending.ScheduleLead - time.Second)
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "too-soon", TaskID: task, Actor: "operator", At: tooSoon, Confirmed: true, ExpectedCount: 1}); err == nil || !strings.Contains(err.Error(), "30 minutes") {
		t.Fatalf("too-soon schedule: %v", err)
	}
	camp := storeRecord(t, f, campaign.CampaignsCollection, f.campaignID)
	current := camp.GetString("current_configuration")
	camp.Set("current_configuration", "")
	storeSave(t, f, camp)
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "stale", TaskID: task, Actor: "operator", At: now.Add(time.Hour), Confirmed: true, ExpectedCount: 1}); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("stale revision schedule: %v", err)
	}
	camp.Set("current_configuration", current)
	storeSave(t, f, camp)

	other := f.task(t, 1)
	candidate := storeRecord(t, f, intake.CandidatesCollection, storeRecord(t, f, intake.TaskCandidatesCollection, storePreview(t, f, task).Members[0].ID).GetString("candidate"))
	candidate.Set("claim_task", other)
	storeSave(t, f, candidate)
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "zero", TaskID: task, Actor: "operator", At: now.Add(time.Hour), Confirmed: true, ExpectedCount: 0}); err == nil || !strings.Contains(err.Error(), "no eligible candidates") {
		t.Fatalf("zero eligible schedule: %v", err)
	}
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "changed", TaskID: task, Actor: "operator", At: now.Add(time.Hour), Confirmed: true, ExpectedCount: 1}); err == nil || !strings.Contains(err.Error(), "count changed") {
		t.Fatalf("changed count schedule: %v", err)
	}
	candidate.Set("claim_task", "")
	storeSave(t, f, candidate)

	first := now.Add(time.Hour)
	if result := storeSchedule(t, f, task, "schedule", first, 1); result.Status != intake.ScheduleScheduled {
		t.Fatalf("enable status = %q", result.Status)
	}
	loaded := taskRecord(t, f, task)
	if loaded.ScheduleState != intake.ScheduleScheduled || !loaded.ScheduleAt.Equal(first) {
		t.Fatalf("stored schedule = %s %s", loaded.ScheduleState, loaded.ScheduleAt)
	}
	replaced := now.Add(2 * time.Hour)
	storeSchedule(t, f, task, "schedule-replace", replaced, 1)
	if loaded = taskRecord(t, f, task); !loaded.ScheduleAt.Equal(replaced) {
		t.Fatalf("replaced schedule = %s", loaded.ScheduleAt)
	}
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "clear", TaskID: task, Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if loaded = taskRecord(t, f, task); loaded.ScheduleState != intake.ScheduleNone || !loaded.ScheduleAt.IsZero() {
		t.Fatalf("cleared schedule = %s %s", loaded.ScheduleState, loaded.ScheduleAt)
	}
	if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "clear-again", TaskID: task, Actor: "operator"}); err == nil {
		t.Fatal("empty schedule cleared twice")
	}
}

func TestStoreScheduleRequiresApproval(t *testing.T) {
	unapproved := newAcceptanceFixture(t)
	unapprovedTask := unapproved.task(t, 1)
	if _, err := unapproved.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "unapproved", TaskID: unapprovedTask, Actor: "operator", At: unapproved.clock.Now().Add(time.Hour), Confirmed: true, ExpectedCount: 1}); err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatalf("unapproved schedule: %v", err)
	}
	if _, err := unapproved.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "unconfirmed", TaskID: unapprovedTask, Actor: "operator", At: unapproved.clock.Now().Add(time.Hour), ExpectedCount: 1}); err == nil {
		t.Fatal("unconfirmed schedule accepted")
	}
}

func TestStoreScheduledFireClaimsOnlyFreeCandidatesAndIsIdempotent(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	at := f.clock.Now().Add(time.Hour)
	scheduleTask(t, f, task, at, 2)
	candidates, err := f.app.FindRecordsByFilter(intake.CandidatesCollection, "campaign={:c}", "email_key", 0, 0, dbx.Params{"c": f.campaignID})
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates: %v %v", candidates, err)
	}
	other := f.task(t, 1)
	taken := candidates[0]
	taken.Set("claim_task", other)
	storeSave(t, f, taken)

	setClock(f, at.Add(time.Second))
	fired, err := f.store.FireScheduled(storeCtx)
	if err != nil || !fired {
		t.Fatalf("fire = %t, %v", fired, err)
	}
	loaded := taskRecord(t, f, task)
	if loaded.Status != "running" || loaded.ScheduleState != intake.ScheduleFired {
		t.Fatalf("fired task = %s %s", loaded.Status, loaded.ScheduleState)
	}
	if reserved := taskReserved(t, f, task); reserved != 1 {
		t.Fatalf("fire reserved %d rows", reserved)
	}
	if p := storePreview(t, f, task); p.Eligible != 0 {
		t.Fatalf("fire left %d candidates eligible", p.Eligible)
	}
	if attemptCount(t, f) != 0 {
		t.Fatal("fire created a submission attempt before transmission")
	}
	actions, err := f.app.FindRecordsByFilter(sending.ActionsCollection, "task={:t} && kind='fire'", "", 0, 0, dbx.Params{"t": task})
	if err != nil || len(actions) != 1 {
		t.Fatalf("fire actions = %d, %v", len(actions), err)
	}
	repeated, err := f.store.FireScheduled(storeCtx)
	if err != nil || repeated {
		t.Fatalf("repeated fire = %t, %v", repeated, err)
	}
	actions, err = f.app.FindRecordsByFilter(sending.ActionsCollection, "task={:t} && kind='fire'", "", 0, 0, dbx.Params{"t": task})
	if err != nil || len(actions) != 1 {
		t.Fatalf("repeated fire created actions: %d, %v", len(actions), err)
	}
	if reserved := taskReserved(t, f, task); reserved != 1 {
		t.Fatalf("repeated fire changed reservations: %d", reserved)
	}

	_ = f.task(t, 1)
	if fired, err = f.store.FireScheduled(storeCtx); err != nil || fired {
		t.Fatalf("unscheduled task fired: %t, %v", fired, err)
	}
}

func TestStoreScheduledFireRefusalsMarkMissedWithoutSending(t *testing.T) {
	t.Run("restore review", func(t *testing.T) {
		f := newAcceptanceFixture(t)
		task := f.task(t, 1)
		storeApprove(t, f, task)
		at := f.clock.Now().Add(time.Hour)
		scheduleTask(t, f, task, at, 1)
		restored := sending.NewStore(f.app, sending.Options{RestoreReview: true, Now: f.clock.Now})
		setClock(f, at.Add(time.Second))
		if fired, err := restored.FireScheduled(storeCtx); err != nil || fired {
			t.Fatalf("restore-review fire = %t, %v", fired, err)
		}
		assertMissed(t, f, task, "restore review")
	})
	t.Run("changed revision", func(t *testing.T) {
		f := newAcceptanceFixture(t)
		task := f.task(t, 1)
		storeApprove(t, f, task)
		at := scheduleAndBreakRevision(t, f, task)
		setClock(f, at.Add(time.Second))
		if fired, err := f.store.FireScheduled(storeCtx); err != nil || fired {
			t.Fatalf("stale fire = %t, %v", fired, err)
		}
		assertMissed(t, f, task, "configuration changed")
	})
	t.Run("missing approval", func(t *testing.T) {
		f := newAcceptanceFixture(t)
		task := f.task(t, 1)
		storeApprove(t, f, task)
		at := f.clock.Now().Add(time.Hour)
		scheduleTask(t, f, task, at, 1)
		approvals, err := f.app.FindAllRecords(sending.ApprovalsCollection)
		if err != nil {
			t.Fatal(err)
		}
		for _, approval := range approvals {
			if err := f.app.Delete(approval); err != nil {
				t.Fatal(err)
			}
		}
		setClock(f, at.Add(time.Second))
		if fired, err := f.store.FireScheduled(storeCtx); err != nil || fired {
			t.Fatalf("unapproved fire = %t, %v", fired, err)
		}
		assertMissed(t, f, task, "approval")
	})
	t.Run("no free candidate", func(t *testing.T) {
		f := newAcceptanceFixture(t)
		task := f.task(t, 1)
		storeApprove(t, f, task)
		at := f.clock.Now().Add(time.Hour)
		scheduleTask(t, f, task, at, 1)
		other := f.task(t, 1)
		candidates, err := f.app.FindAllRecords(intake.CandidatesCollection)
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range candidates {
			candidate.Set("claim_task", other)
			storeSave(t, f, candidate)
		}
		setClock(f, at.Add(time.Second))
		if fired, err := f.store.FireScheduled(storeCtx); err != nil || fired {
			t.Fatalf("busy fire = %t, %v", fired, err)
		}
		assertMissed(t, f, task, "no candidate")
		// Clearing a refused schedule resets the reason and leaves the task
		// ready for a fresh schedule or manual start.
		if _, err := f.store.Schedule(storeCtx, sending.ScheduleRequest{ActionID: "clear-refused", TaskID: task, Actor: "operator"}); err != nil {
			t.Fatal(err)
		}
		cleared := taskRecord(t, f, task)
		if cleared.ScheduleState != intake.ScheduleNone || cleared.Status != intake.TaskReady {
			t.Fatalf("cleared refusal = %s %s", cleared.Status, cleared.ScheduleState)
		}
		if reason := storeRecord(t, f, intake.TasksCollection, task).GetString("pause_reason"); reason != "" {
			t.Fatalf("cleared refusal kept reason %q", reason)
		}
	})
}

func scheduleAndBreakRevision(t *testing.T, f *acceptanceFixture, task string) time.Time {
	t.Helper()
	at := f.clock.Now().Add(time.Hour)
	scheduleTask(t, f, task, at, 1)
	camp := storeRecord(t, f, campaign.CampaignsCollection, f.campaignID)
	camp.Set("current_configuration", "")
	storeSave(t, f, camp)
	return at
}

func assertMissed(t *testing.T, f *acceptanceFixture, task, reason string) {
	t.Helper()
	loaded := taskRecord(t, f, task)
	if loaded.Status != intake.TaskReady || loaded.ScheduleState != intake.ScheduleMissed {
		t.Fatalf("refused fire = %s %s", loaded.Status, loaded.ScheduleState)
	}
	record := storeRecord(t, f, intake.TasksCollection, task)
	if !strings.Contains(record.GetString("pause_reason"), reason) {
		t.Fatalf("missed reason = %q, want %q", record.GetString("pause_reason"), reason)
	}
	if attemptCount(t, f) != 0 {
		t.Fatal("refused fire created an attempt")
	}
	if reserved := taskReserved(t, f, task); reserved != 0 {
		t.Fatalf("refused fire reserved %d candidates", reserved)
	}
}

func TestStoreRecoveryPausesMissedScheduleAndResumeUsesSafeRemainder(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	at := f.clock.Now().Add(time.Hour)
	scheduleTask(t, f, task, at, 2)

	// A restart before the stored instant preserves the pending schedule.
	restarted := sending.NewStore(f.app, sending.Options{Now: f.clock.Now})
	if err := restarted.Recover(storeCtx); err != nil {
		t.Fatal(err)
	}
	if loaded := taskRecord(t, f, task); loaded.Status != intake.TaskReady || loaded.ScheduleState != intake.ScheduleScheduled || !loaded.ScheduleAt.Equal(at) {
		t.Fatalf("early restart changed schedule: %+v", loaded)
	}

	// A restart after the instant pauses the task instead of starting it.
	setClock(f, at.Add(2*time.Hour))
	restarted = sending.NewStore(f.app, sending.Options{Now: f.clock.Now})
	if err := restarted.Recover(storeCtx); err != nil {
		t.Fatal(err)
	}
	loaded := taskRecord(t, f, task)
	if loaded.Status != "paused" || loaded.ScheduleState != intake.ScheduleMissed {
		t.Fatalf("recovery over schedule = %s %s", loaded.Status, loaded.ScheduleState)
	}
	if !strings.Contains(storeRecord(t, f, intake.TasksCollection, task).GetString("pause_reason"), "scheduled start time passed") {
		t.Fatal("missed schedule reason missing")
	}
	if attemptCount(t, f) != 0 {
		t.Fatal("recovery over schedule created an attempt")
	}

	// One candidate was taken elsewhere; Resume starts only the remainder and
	// clears the schedule so it can never fire again.
	candidates, err := f.app.FindRecordsByFilter(intake.CandidatesCollection, "campaign={:c}", "email_key", 0, 0, dbx.Params{"c": f.campaignID})
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidates: %v %v", candidates, err)
	}
	other := f.task(t, 1)
	candidates[0].Set("claim_task", other)
	storeSave(t, f, candidates[0])
	if _, err := f.store.Act(storeCtx, sending.ActionRequest{ActionID: "resume", TaskID: task, Kind: "resume", Actor: "operator", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	loaded = taskRecord(t, f, task)
	if loaded.Status != "running" || loaded.ScheduleState != intake.ScheduleNone || !loaded.ScheduleAt.IsZero() {
		t.Fatalf("resume state = %s %s %s", loaded.Status, loaded.ScheduleState, loaded.ScheduleAt)
	}
	if reserved := taskReserved(t, f, task); reserved != 1 {
		t.Fatalf("resume reserved %d", reserved)
	}
	if fired, err := f.store.FireScheduled(storeCtx); err != nil || fired {
		t.Fatalf("resumed schedule fired again: %t, %v", fired, err)
	}
}
