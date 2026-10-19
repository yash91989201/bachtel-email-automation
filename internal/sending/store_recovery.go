package sending

import (
	"context"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
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
			var counts []struct {
				State string `db:"dispatch_state"`
				Count int    `db:"count"`
			}
			if e = app.DB().NewQuery("SELECT dispatch_state, COUNT(*) AS count FROM task_candidates WHERE task = {:task} AND dispatch_state IN ('in_flight','reserved') GROUP BY dispatch_state").Bind(dbx.Params{"task": t.Id}).All(&counts); e != nil {
				return e
			}
			active, reserved := 0, 0
			for _, c := range counts {
				if c.State == AttemptInFlight {
					active = c.Count
				}
				if c.State == "reserved" {
					reserved = c.Count
				}
			}

			if active > 0 {
				continue
			}
			if t.GetString("status") == "stopping" {
				if e = releaseReservations(app, t.Id); e != nil {
					return e
				}
				t.Set("status", "stopped")
			} else {
				if reserved > 0 {
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
