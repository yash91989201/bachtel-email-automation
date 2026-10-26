package migrations

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Add the editable task name as presentation metadata separate from the
// immutable source filename. The name is capped at 255 code points like the
// upload rule. Every unnamed existing task receives its own stored workbook
// filename, or task.xlsx when that filename is missing or whitespace-only, so
// lists and headings keep a usable identity without changing source evidence,
// upload time or workflow state.
func init() {
	m.Register(func(app core.App) error {
		tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
		if err != nil {
			return err
		}
		tasks.Fields.Add(&core.TextField{Name: "name", Max: 255})
		if err := app.Save(tasks); err != nil {
			return err
		}
		return backfillTaskNames(app)
	}, func(app core.App) error { return nil })
}

// backfillTaskNames fills the name of every task that has none, without
// touching a populated name. It is safe to run again: a second pass finds no
// blank names and writes nothing. Names are checked with Unicode whitespace
// trimming, matching the creation rule.
func backfillTaskNames(app core.App) error {
	records, err := app.FindRecordsByFilter(intake.TasksCollection, "", "", 0, 0)
	if err != nil {
		return err
	}
	for _, record := range records {
		if strings.TrimSpace(record.GetString("name")) != "" {
			continue
		}
		name := record.GetString("source_name")
		if strings.TrimSpace(name) == "" {
			name = "task.xlsx"
		}
		if _, err := app.DB().NewQuery("UPDATE send_tasks SET name = {:name} WHERE id = {:id}").
			Bind(dbx.Params{"name": name, "id": record.Id}).
			Execute(); err != nil {
			return err
		}
	}
	return nil
}
