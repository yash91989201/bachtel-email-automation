package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Add an optional task-level first-start preference. It is presentation state
// only; the scheduler and dispatcher continue to use their existing fields and
// actions. Existing records are normalized to the empty chooser preference.
func init() {
	m.Register(func(app core.App) error {
		tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
		if err != nil {
			return err
		}
		tasks.Fields.Add(&core.TextField{Name: "sending_type", Max: 16})
		if err := app.Save(tasks); err != nil {
			return err
		}
		_, err = app.DB().NewQuery("UPDATE send_tasks SET sending_type = ''").Execute()
		return err
	}, func(app core.App) error { return nil })
}
