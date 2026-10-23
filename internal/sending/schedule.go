package sending

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// ParseSchedule maps native date and time fields to one UTC instant. The
// wall-clock time always uses India time, regardless of browser/server zone.
func ParseSchedule(date, clock string) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), ScheduleZone)
	if err != nil {
		return time.Time{}, &ScheduleRefusal{Reason: "choose a valid start date"}
	}
	clock = strings.TrimSpace(clock)
	at, err := time.Parse("15:04", clock)
	if err != nil || at.Format("15:04") != clock {
		return time.Time{}, &ScheduleRefusal{Reason: "choose a valid start time"}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), at.Hour(), at.Minute(), 0, 0, ScheduleZone).UTC(), nil
}

// DisplayDate renders the operator-facing calendar date of a stored instant.
func DisplayDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(ScheduleZone).Format("2006-01-02")
}

// Schedule enables or clears a task's single-shot start schedule in one
// transaction. Enabling is the task's explicit start authorization: it rechecks
// the pinned revision, the approved test and current
// candidate eligibility, but reserves nothing. A zero At clears a pending or
// missed schedule and leaves the manual Start action available.
func (s *Store) Schedule(ctx context.Context, ownerID string, in ScheduleRequest) (*ActionResult, error) {
	if strings.TrimSpace(in.ActionID) == "" || len(in.ActionID) > 128 {
		return nil, errors.New("an action ID is required")
	}
	var out *ActionResult
	err := s.app.RunInTransaction(func(app core.App) error {
		old, e := existingAction(app, in.ActionID, in.TaskID, "schedule")
		if e != nil {
			return e
		}
		if old != nil {
			out = old
			return nil
		}
		t, e := s.authorizeTask(app, ownerID, in.TaskID)
		if e != nil {
			return e
		}
		out = &ActionResult{ActionID: in.ActionID, TaskID: t.Id}
		if t.GetString("closed_at") != "" || t.GetString("closure_requested_at") != "" {
			return &ScheduleRefusal{Reason: ClosedTaskMessage}
		}
		if in.At.IsZero() {
			switch t.GetString("schedule_state") {
			case intake.ScheduleScheduled, intake.ScheduleMissed:
			default:
				return &ScheduleRefusal{Reason: "this task has no pending schedule to clear"}
			}
			t.Set("schedule_at", "")
			t.Set("schedule_state", intake.ScheduleNone)
			// A never-started refusal recorded its reason on a still-ready
			// task; clearing the schedule also clears that stale reason. A
			// downtime miss keeps its reason because the paused task still
			// needs the operator's resume decision.
			if t.GetString("status") == intake.TaskReady {
				t.Set("pause_reason", "")
			}
			out.Status = intake.ScheduleNone
			if e = app.Save(t); e != nil {
				return e
			}
			return saveAction(app, ownerID, ActionRequest{ActionID: in.ActionID, TaskID: in.TaskID, Kind: "schedule", Actor: in.Actor}, out)
		}
		if !in.Confirmed {
			return &ScheduleRefusal{Reason: "explicit confirmation is required"}
		}
		if s.options.RestoreReview {
			return &ScheduleRefusal{Reason: "restore review is active: reconcile submissions made after the backup and restart without APP_RESTORE_REVIEW before scheduling"}
		}
		if in.At.Before(s.options.Now().Add(ScheduleLead)) {
			return &ScheduleRefusal{Reason: "choose a start time at least 30 minutes from now"}
		}
		if t.GetString("status") != intake.TaskReady || t.GetString("configuration") == "" {
			return &ScheduleRefusal{Reason: "validate the task before scheduling a start"}
		}
		if requiresFinalSnapshot(app, t) && t.GetString("selected_final_snapshot") == "" {
			return &ScheduleRefusal{Reason: "approve the prepared data as-is or upload an edited final sheet before scheduling"}
		}
		if _, e := s.usableConnection(ownerID); e != nil {
			return &ScheduleRefusal{Reason: "connect a usable sending mailbox before scheduling a start"}
		}
		isApproved, e := s.approvalFor(app, t, ownerID)
		if e != nil {
			return e
		}
		if !isApproved {
			return &ScheduleRefusal{Reason: "approve a successfully submitted formatting test first"}
		}
		target, e := app.FindRecordById(campaign.CampaignsCollection, t.GetString("campaign"))
		if e != nil {
			return e
		}
		if target.GetString("current_configuration") != t.GetString("configuration") {
			return &ScheduleRefusal{Reason: "the configuration changed; revalidate this task"}
		}
		rows, e := records(app, intake.TaskCandidatesCollection, "task={:t}", "source_row,recipient_index", dbx.Params{"t": t.Id})
		if e != nil {
			return e
		}
		eligible := 0
		for _, m := range rows {
			if m.GetString("state") != intake.RowEligible {
				continue
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, m.GetString("candidate"))
			if e != nil {
				return e
			}
			if startable(t.Id, m, c) {
				eligible++
			}
		}
		out.Eligible = eligible
		if eligible != in.ExpectedCount {
			return &ScheduleRefusal{Reason: "eligible count changed; refresh and confirm the current count"}
		}
		if eligible == 0 {
			return &ScheduleRefusal{Reason: "no eligible candidates remain; refresh the counts"}
		}
		t.Set("schedule_at", in.At.UTC())
		t.Set("schedule_state", intake.ScheduleScheduled)
		t.Set("pause_reason", "")
		out.Status = intake.ScheduleScheduled
		if e = app.Save(t); e != nil {
			return e
		}
		return saveAction(app, ownerID, ActionRequest{ActionID: in.ActionID, TaskID: in.TaskID, Kind: "schedule", Actor: in.Actor}, out)
	})
	return out, err
}

// FireScheduled starts every scheduled task whose stored instant is due. Each
// task is fired inside one transaction; a refusal records the reason and marks
// the schedule missed without starting anything.
func (s *Store) FireScheduled(ctx context.Context) (bool, error) {
	now := s.options.Now().UTC().Format(types.DefaultDateLayout)
	due, err := s.app.FindRecordsByFilter(
		intake.TasksCollection,
		"status={:ready} && schedule_state={:scheduled} && schedule_at <= {:now}",
		"schedule_at",
		0,
		0,
		dbx.Params{"ready": intake.TaskReady, "scheduled": intake.ScheduleScheduled, "now": now},
	)
	if err != nil {
		return false, err
	}
	fired := false
	for _, r := range due {
		started, err := s.fire(ctx, r.Id)
		if err != nil {
			return fired, err
		}
		fired = fired || started
	}
	return fired, nil
}

// fire starts one due schedule. The deterministic action ID makes a repeated
// fire, or a retried transaction, a no-op instead of a second start.
func (s *Store) fire(_ context.Context, taskID string) (bool, error) {
	fired := false
	err := s.app.RunInTransaction(func(app core.App) error {
		t, e := app.FindRecordById(intake.TasksCollection, taskID)
		if e != nil {
			return e
		}
		if t.GetString("status") != intake.TaskReady || t.GetString("schedule_state") != intake.ScheduleScheduled {
			return nil
		}
		at := stamp(t, "schedule_at")
		if at.IsZero() || at.After(s.options.Now()) {
			return nil
		}
		id := fireActionID(t.Id, at)
		old, e := existingAction(app, id, t.Id, "fire")
		if e != nil {
			return e
		}
		if old != nil {
			return nil
		}
		owner, e := ownerOfTask(app, t)
		if e != nil {
			return e
		}
		miss := func(reason string) error {
			t.Set("schedule_state", intake.ScheduleMissed)
			t.Set("pause_reason", reason)
			if e := app.Save(t); e != nil {
				return e
			}
			return saveAction(app, owner, ActionRequest{ActionID: id, TaskID: t.Id, Kind: "fire", Actor: "dispatcher"}, &ActionResult{ActionID: id, TaskID: t.Id, Status: intake.ScheduleMissed})
		}
		if s.options.RestoreReview {
			return miss("restore review is active: reconcile submissions made after the backup and restart without APP_RESTORE_REVIEW before sending")
		}
		if t.GetString("configuration") == "" {
			return miss("this task has no pinned configuration; revalidate and approve it")
		}
		target, e := app.FindRecordById(campaign.CampaignsCollection, t.GetString("campaign"))
		if e != nil {
			return e
		}
		if target.GetString("current_configuration") != t.GetString("configuration") {
			return miss("the campaign configuration changed after this task was scheduled; revalidate and schedule again")
		}
		if requiresFinalSnapshot(app, t) && t.GetString("selected_final_snapshot") == "" {
			return miss("this scheduled task has no finalized sending dataset; finalize it again before sending")
		}
		connection, connectionErr := s.usableConnection(owner)
		if connectionErr != nil {
			return miss("the mailbox connection is not ready; reconnect it and schedule this task again")
		}
		isApproved, e := approved(app, t, connection.Id, connection.GetInt("generation"))
		if e != nil {
			return e
		}
		if !isApproved {
			return miss("approval for this exact configuration and mailbox is missing; re-approve before sending")
		}
		rows, e := records(app, intake.TaskCandidatesCollection, "task={:t}", "source_row,recipient_index", dbx.Params{"t": t.Id})
		if e != nil {
			return e
		}
		claimed := 0
		for _, m := range rows {
			if m.GetString("state") != intake.RowEligible {
				continue
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, m.GetString("candidate"))
			if e != nil {
				return e
			}
			if !startable(t.Id, m, c) {
				continue
			}
			if e = s.reserve(app, t, m, c); e != nil {
				return e
			}
			claimed++
		}
		if claimed == 0 {
			return miss("no candidate was free at the scheduled time; review the exclusions and schedule or start again")
		}
		t.Set("status", "running")
		t.Set("pause_reason", "")
		t.Set("schedule_state", intake.ScheduleFired)
		if e = app.Save(t); e != nil {
			return e
		}
		fired = true
		return saveAction(app, owner, ActionRequest{ActionID: id, TaskID: t.Id, Kind: "fire", Actor: "dispatcher"}, &ActionResult{ActionID: id, TaskID: t.Id, Status: "running", Claimed: claimed, Excluded: len(rows) - claimed})
	})
	return fired, err
}

// fireActionID derives the audit identity of one scheduled start from the task
// and its stored instant.
func fireActionID(taskID string, at time.Time) string {
	return "scheduled-" + taskID + "-" + at.UTC().Format(time.RFC3339Nano)
}
