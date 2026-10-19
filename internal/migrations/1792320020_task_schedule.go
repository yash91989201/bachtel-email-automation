package migrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// One additive migration stores the operator's single-shot start schedule on
// the task itself: one schedule per task, so a second collection would only
// add joins. schedule_at is the absolute UTC instant and stays after a fire as
// history; schedule_state is the source of truth for whether a fire is still
// pending. Existing tasks are backfilled to none and behave exactly as before.
//
// There is no down migration that drops the fields, matching the existing
// sending migrations: rollback restores a backup taken before deployment.
func init() {
	m.Register(func(app core.App) error {
		tasks, e := app.FindCollectionByNameOrId(intake.TasksCollection)
		if e != nil {
			return e
		}
		tasks.Fields.Add(
			&core.DateField{Name: "schedule_at"},
			&core.TextField{Name: "schedule_state", Max: 32},
		)
		if e = app.Save(tasks); e != nil {
			return e
		}
		// A text field has no schema-level default, so existing rows and rows
		// written outside the intake service are normalized here once.
		_, e = app.DB().NewQuery("UPDATE send_tasks SET schedule_state = {:none} WHERE schedule_state = ''").
			Bind(dbx.Params{"none": intake.ScheduleNone}).
			Execute()
		return e
	}, func(app core.App) error { return nil })
}
