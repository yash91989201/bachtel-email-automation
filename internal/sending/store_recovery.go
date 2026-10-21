package sending

import (
	"context"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

func (s *Store) Recover(ctx context.Context) error {
	return s.app.RunInTransaction(func(app core.App) error {
		unfinished, e := records(app, AttemptsCollection, "state='in_flight'", "", nil)
		if e != nil {
			return e
		}
		for _, r := range unfinished {
			task, e := app.FindRecordById(intake.TasksCollection, r.GetString("task"))
			if e != nil {
				return e
			}
			task.Set("result_generation_needed", true)
			if e = app.Save(task); e != nil {
				return e
			}
			r.Set("state", AttemptReview)
			r.Set("reason", "server interrupted before a durable definite outcome")
			nowField(r, "finished_at", s.options.Now())
			if e = app.Save(r); e != nil {
				return e
			}
			if r.GetString("kind") == KindTest {
				continue
			}
			m, e := app.FindRecordById(intake.TaskCandidatesCollection, r.GetString("member"))
			if e != nil {
				return e
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, r.GetString("candidate"))
			if e != nil {
				return e
			}
			m.Set("dispatch_state", AttemptReview)
			c.Set("submission_state", intake.SubmissionUncertain)
			c.Set("claim_task", "")
			if e = app.Save(m); e != nil {
				return e
			}
			if e = app.Save(c); e != nil {
				return e
			}
		}
		tasks, e := records(app, intake.TasksCollection, "status='running' || status='waiting' || status='stopping'", "", nil)
		if e != nil {
			return e
		}
		for _, t := range tasks {
			t.Set("status", "interrupted")
			t.Set("pause_reason", "server restarted; review held attempts and explicitly resume safe remaining candidates")
			t.Set("result_generation_needed", true)
			if e = app.Save(t); e != nil {
				return e
			}
		}
		// A stored start time that passed while no dispatcher owned this data
		// is a downtime miss: nothing was started, so no attempt is created and
		// the operator must review the remainder before it can send.
		now := s.options.Now().UTC().Format(types.DefaultDateLayout)
		missed, e := records(app, intake.TasksCollection, "status={:ready} && schedule_state={:scheduled} && schedule_at <= {:now}", "", dbx.Params{"ready": intake.TaskReady, "scheduled": intake.ScheduleScheduled, "now": now})
		if e != nil {
			return e
		}
		for _, t := range missed {
			t.Set("status", "paused")
			t.Set("schedule_state", intake.ScheduleMissed)
			t.Set("pause_reason", "the scheduled start time passed while the server was not running; review the safe remainder and resume explicitly")
			if e = app.Save(t); e != nil {
				return e
			}
		}
		// Only claims without an intent are safe to release. Uncertainty has already
		// been made campaign-scoped above and is never turned back into eligibility.
		members, e := records(app, intake.TaskCandidatesCollection, "dispatch_state='reserved'", "", nil)
		if e != nil {
			return e
		}
		for _, m := range members {
			c, e := app.FindRecordById(intake.CandidatesCollection, m.GetString("candidate"))
			if e != nil {
				return e
			}
			if c.GetString("submission_state") == intake.SubmissionNone && c.GetString("claim_task") == m.GetString("task") {
				c.Set("claim_task", "")
				if e = app.Save(c); e != nil {
					return e
				}
				m.Set("dispatch_state", "")
				if e = app.Save(m); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) SettleTasks(ctx context.Context) error {
	return s.app.RunInTransaction(func(app core.App) error {
		tasks, e := records(app, intake.TasksCollection, "status='running' || status='waiting' || status='stopping'", "", nil)
		if e != nil {
			return e
		}
		for _, t := range tasks {
			var pending struct {
				Active   bool `db:"active"`
				Reserved bool `db:"reserved"`
			}
			// Settlement needs only presence, so stop at the first matching
			// index entry instead of recounting the task after every outcome.
			if e = app.DB().NewQuery("SELECT EXISTS(SELECT 1 FROM task_candidates WHERE task = {:task} AND dispatch_state = 'in_flight') AS active, EXISTS(SELECT 1 FROM task_candidates WHERE task = {:task} AND dispatch_state = 'reserved') AS reserved").Bind(dbx.Params{"task": t.Id}).One(&pending); e != nil {
				return e
			}

			if pending.Active {
				continue
			}
			if t.GetString("status") == "stopping" {
				if e = releaseReservations(app, t.Id); e != nil {
					return e
				}
				t.Set("status", "stopped")
			} else {
				if pending.Reserved {
					continue
				}
				t.Set("status", "completed")
			}
			t.Set("result_generation_needed", true)
			if e = app.Save(t); e != nil {
				return e
			}
		}
		return nil
	})
}
