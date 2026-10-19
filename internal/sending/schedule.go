package sending

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// ParseSchedule maps the operator's 12-hour form fields to one absolute UTC
// instant. The wall-clock time is interpreted in the fixed +05:30 zone, never
// the process's local zone.
func ParseSchedule(date, hour, minute, meridiem string) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), ScheduleZone)
	if err != nil {
		return time.Time{}, &ScheduleRefusal{Reason: "choose a valid start date"}
	}
	h, err := strconv.Atoi(strings.TrimSpace(hour))
	if err != nil || h < 1 || h > 12 {
		return time.Time{}, &ScheduleRefusal{Reason: "choose an hour from 1 through 12"}
	}
	m, err := strconv.Atoi(strings.TrimSpace(minute))
	if err != nil || m < 0 || m > 59 {
		return time.Time{}, &ScheduleRefusal{Reason: "choose a minute from 0 through 59"}
	}
	switch strings.ToUpper(strings.TrimSpace(meridiem)) {
	case "AM":
		if h == 12 {
			h = 0
		}
	case "PM":
		if h < 12 {
			h += 12
		}
	default:
		return time.Time{}, &ScheduleRefusal{Reason: "choose AM or PM"}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, ScheduleZone).UTC(), nil
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
// the release gate, the pinned revision, the approved test and current
// candidate eligibility, but reserves nothing. A zero At clears a pending or
// missed schedule and leaves the manual Start action available.
func (s *Store) Schedule(ctx context.Context, in ScheduleRequest) (*ActionResult, error) {
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
		t, e := app.FindRecordById(intake.TasksCollection, in.TaskID)
		if e != nil {
			return e
		}
		out = &ActionResult{ActionID: in.ActionID, TaskID: t.Id}
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
			return saveAction(app, ActionRequest{ActionID: in.ActionID, TaskID: in.TaskID, Kind: "schedule", Actor: in.Actor}, out)
		}
		if !in.Confirmed {
			return &ScheduleRefusal{Reason: "explicit confirmation is required"}
		}
		if in.At.Before(s.options.Now().Add(ScheduleLead)) {
			return &ScheduleRefusal{Reason: "choose a start time at least 30 minutes from now"}
		}
		if !s.options.AllowCandidateSending {
			return ErrReleaseGate
		}
		if t.GetString("status") != intake.TaskReady || t.GetString("configuration") == "" {
			return &ScheduleRefusal{Reason: "validate the task before scheduling a start"}
		}
		isApproved, e := approved(app, t)
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
		rows, e := records(app, intake.TaskCandidatesCollection, "task={:t}", "source_row", dbx.Params{"t": t.Id})
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
		return saveAction(app, ActionRequest{ActionID: in.ActionID, TaskID: in.TaskID, Kind: "schedule", Actor: in.Actor}, out)
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
		miss := func(reason string) error {
			t.Set("schedule_state", intake.ScheduleMissed)
			t.Set("pause_reason", reason)
			if e := app.Save(t); e != nil {
				return e
			}
			return saveAction(app, ActionRequest{ActionID: id, TaskID: t.Id, Kind: "fire", Actor: "dispatcher"}, &ActionResult{ActionID: id, TaskID: t.Id, Status: intake.ScheduleMissed})
		}
		if !s.options.AllowCandidateSending {
			return miss(ErrReleaseGate.Error())
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
		isApproved, e := approved(app, t)
		if e != nil {
			return e
		}
		if !isApproved {
			return miss("approval for this exact configuration is missing; re-approve before sending")
		}
		rows, e := records(app, intake.TaskCandidatesCollection, "task={:t}", "source_row", dbx.Params{"t": t.Id})
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
		return saveAction(app, ActionRequest{ActionID: id, TaskID: t.Id, Kind: "fire", Actor: "dispatcher"}, &ActionResult{ActionID: id, TaskID: t.Id, Status: "running", Claimed: claimed, Excluded: len(rows) - claimed})
	})
	return fired, err
}

// fireActionID derives the audit identity of one scheduled start from the task
// and its stored instant.
func fireActionID(taskID string, at time.Time) string {
	return "scheduled-" + taskID + "-" + at.UTC().Format(time.RFC3339Nano)
}
