package sending

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// AttemptCancelled marks a queued operation that explicit closure cancelled
// before it ran. It is never a provider outcome.
const AttemptCancelled = "cancelled"

// ClosedTaskMessage is the fixed refusal for every action on a closed task.
const ClosedTaskMessage = "this task is closed; upload a new task for future work"

// CloseTask performs the owner-authorized terminal closure. It records the
// durable actor and time, cancels pending schedules and queued operations,
// releases only reservations that are provably unsubmitted, and keeps the task
// pending until in-flight work settles. Repeated valid requests are idempotent;
// foreign requests are refused by authorizeTask.
func (s *Store) CloseTask(ctx context.Context, ownerID, taskID, actor, actionID string) (*ActionResult, error) {
	if strings.TrimSpace(actionID) == "" || len(actionID) > 128 {
		return nil, errors.New("an action ID is required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, errors.New("an actor is required")
	}
	var out *ActionResult
	err := s.app.RunInTransaction(func(app core.App) error {
		old, err := existingAction(app, actionID, taskID, "close")
		if err != nil {
			return err
		}
		if old != nil {
			out = old
			return nil
		}
		task, err := s.authorizeTask(app, ownerID, taskID)
		if err != nil {
			return err
		}
		out = &ActionResult{ActionID: actionID, TaskID: task.Id, Status: "closed"}
		if task.GetString("closed_at") == "" {
			now := s.options.Now()
			if task.GetString("closure_requested_at") == "" {
				nowField(task, "closure_requested_at", now)
				task.Set("closure_actor", actor)
			}
			// The stored start time stays as the recorded authorization; a
			// cancelled schedule never fires and no longer counts as pending.
			if task.GetString("schedule_state") == intake.ScheduleScheduled {
				task.Set("schedule_state", intake.ScheduleCancelled)
			}
			if err = cancelQueuedAttempts(app, task.Id, now); err != nil {
				return err
			}
			if err = cancelQueuedPreparation(app, task.Id, now); err != nil {
				return err
			}
			if err = releaseSafeReservations(app, task.Id); err != nil {
				return err
			}
			if err = app.Save(task); err != nil {
				return err
			}
			if err = s.finalizeClosure(app, task); err != nil {
				return err
			}
		}
		out.Status = closureStatus(task)
		return saveAction(app, ownerID, ActionRequest{ActionID: actionID, TaskID: task.Id, Kind: "close", Actor: actor}, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// closureStatus is the operator-facing closure state after one request.
func closureStatus(task *core.Record) string {
	if task.GetString("closed_at") != "" {
		return "closed"
	}
	return "closing"
}

// cancelQueuedPreparation cancels AI preparation that has not claimed its job.
// A running claim is left for its worker, which abandons it once the closure
// request is visible; nothing it returns can publish over the closure.
func cancelQueuedPreparation(app core.App, taskID string, now time.Time) error {
	if _, err := app.DB().NewQuery("UPDATE " + preparation.JobsCollection +
		" SET state = {:cancelled}, error = {:error}, finished_at = {:now}" +
		" WHERE task = {:task} AND state = {:queued}").
		Bind(dbx.Params{"cancelled": preparation.JobCancelled, "error": preparation.ErrCancelled.Error(), "now": now, "task": taskID, "queued": preparation.JobQueued}).
		Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery("UPDATE " + preparation.RunsCollection +
		" SET state = {:cancelled}, error = {:error}, finished_at = {:now}" +
		" WHERE task = {:task} AND state = {:queued}").
		Bind(dbx.Params{"cancelled": preparation.RunCancelled, "error": preparation.ErrCancelled.Error(), "now": now, "task": taskID, "queued": preparation.RunQueued}).
		Execute()
	return err
}

// SettleClosure finalizes a requested closure after an external operation of
// the task settled. It is safe to call repeatedly.
func (s *Store) SettleClosure(ctx context.Context, taskID string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		task, err := app.FindRecordById(intake.TasksCollection, taskID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return s.finalizeClosure(app, task)
	})
}

// cancelQueuedAttempts cancels every operation that has not started. An
// in-flight attempt is never rewritten: its provider outcome must settle.
func cancelQueuedAttempts(app core.App, taskID string, now time.Time) error {
	queued, err := records(app, AttemptsCollection, "task={:t} && state={:queued}", "", dbx.Params{"t": taskID, "queued": AttemptQueued})
	if err != nil {
		return err
	}
	for _, attempt := range queued {
		attempt.Set("state", AttemptCancelled)
		attempt.Set("reason", "task closed before this operation ran")
		attempt.Set("finished_at", now)
		if err = app.Save(attempt); err != nil {
			return err
		}
	}
	return nil
}

// releaseSafeReservations releases a candidate claim only when the existing
// safe-stop rules prove it unsubmitted: an eligible snapshot row, no submission
// history, no in-flight attempt, and the claim held by this exact task.
func releaseSafeReservations(app core.App, taskID string) error {
	members, err := records(app, intake.TaskCandidatesCollection, "task={:t} && dispatch_state='reserved'", "", dbx.Params{"t": taskID})
	if err != nil {
		return err
	}
	for _, member := range members {
		if member.GetString("state") != intake.RowEligible {
			continue
		}
		inFlight, err := app.FindFirstRecordByFilter(
			AttemptsCollection,
			"member={:member} && state={:state}",
			dbx.Params{"member": member.Id, "state": AttemptInFlight},
		)
		if err == nil && inFlight != nil {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		candidate, err := app.FindRecordById(intake.CandidatesCollection, member.GetString("candidate"))
		if err != nil {
			return err
		}
		if candidate.GetString("submission_state") != intake.SubmissionNone {
			continue
		}
		if candidate.GetString("claim_task") != taskID {
			continue
		}
		candidate.Set("claim_task", "")
		if err = app.Save(candidate); err != nil {
			return err
		}
		member.Set("dispatch_state", "")
		member.Set("retry_at", "")
		if err = app.Save(member); err != nil {
			return err
		}
	}
	return nil
}

// finalizeClosure marks a requested closure final once no queued or in-flight
// operation and no reservation remains for the task. It is safe to call
// repeatedly and does nothing for a task without a closure request.
func (s *Store) finalizeClosure(app core.App, task *core.Record) error {
	if task.GetString("closure_requested_at") == "" || task.GetString("closed_at") != "" {
		return nil
	}
	var pending struct {
		Active    bool `db:"active"`
		Reserved  bool `db:"reserved"`
		Preparing bool `db:"preparing"`
	}
	// A running preparation claim is bounded by the provider timeout and
	// abandons itself once the closure request is visible; closure waits for it
	// to settle instead of publishing under a closed task.
	query := "SELECT EXISTS(SELECT 1 FROM submission_attempts WHERE task = {:task} AND state IN ('queued', 'in_flight')) AS active," +
		" EXISTS(SELECT 1 FROM task_candidates WHERE task = {:task} AND dispatch_state IN ('reserved', 'in_flight')) AS reserved," +
		" EXISTS(SELECT 1 FROM " + preparation.JobsCollection + " WHERE task = {:task} AND state IN ({:queued}, {:running})) AS preparing"
	if err := app.DB().NewQuery(query).
		Bind(dbx.Params{"task": task.Id, "queued": preparation.JobQueued, "running": preparation.JobRunning}).
		One(&pending); err != nil {
		return err
	}
	if pending.Active || pending.Reserved || pending.Preparing {
		return nil
	}
	nowField(task, "closed_at", s.options.Now())
	task.Set("result_generation_needed", true)
	return app.Save(task)
}

// finalizeClosureForTask loads and finalizes one task. It is used after an
// attempt settles in a worker request owned by the task.
func (s *Store) finalizeClosureForTask(app core.App, taskID string) error {
	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		return err
	}
	return s.finalizeClosure(app, task)
}
