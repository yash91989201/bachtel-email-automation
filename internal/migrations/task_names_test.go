package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// The task-name migration adds one presentation field and fills it from the
// stored workbook filename. It must leave every other task fact alone, cover
// every lifecycle state, and be idempotent so a retried setup cannot overwrite
// a name an operator already chose.
func TestTaskNameMigrationBackfillsWithoutChangingTaskFacts(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	var before core.MigrationsList
	const migrationPrefix = "1793200000"
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < migrationPrefix {
			before.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, before).Up(); err != nil {
		t.Fatal(err)
	}

	owner := newOperator(t, app)
	campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	target := core.NewRecord(campaigns)
	target.Set("name", "Existing campaign")
	target.Set("owner", owner)
	target.Set("source_headers", []string{"Email"})
	if err = app.Save(target); err != nil {
		t.Fatal(err)
	}
	tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}

	type fixture struct {
		status     string
		sourceName string
		closed     bool
		want       string
	}
	fixtures := []fixture{
		{status: intake.TaskStaging, sourceName: "recipients.xlsx", want: "recipients.xlsx"},
		{status: intake.TaskReady, sourceName: "October follow-up.xlsx", want: "October follow-up.xlsx"},
		{status: "running", sourceName: "running.xlsx", want: "running.xlsx"},
		{status: "completed", sourceName: "past.xlsx", want: "past.xlsx"},
		{status: "completed", sourceName: "", closed: true, want: "task.xlsx"},
		{status: intake.TaskStaging, sourceName: "   ", want: "task.xlsx"},
		{status: intake.TaskStaging, sourceName: "\t", want: "task.xlsx"},
		{status: intake.TaskReady, sourceName: "\u00a0", want: "task.xlsx"},
	}
	type saved struct {
		id         string
		created    string
		sourceName string
		status     string
		content    string
		validation string
		closedAt   string
	}
	var created []saved
	for index, f := range fixtures {
		source, err := filesystem.NewFileFromBytes([]byte("workbook"), "book.xlsx")
		if err != nil {
			t.Fatal(err)
		}
		record := core.NewRecord(tasks)
		record.Set("campaign", target.Id)
		record.Set("status", f.status)
		record.Set("source", source)
		record.Set("source_name", f.sourceName)
		record.Set("schedule_state", intake.ScheduleNone)
		record.Set("content_hash", "hash-"+string(rune('a'+index)))
		record.Set("validation_error", "kept validation error")
		if f.closed {
			record.Set("closure_requested_at", "2026-10-20 10:00:00.000Z")
			record.Set("closed_at", "2026-10-21 10:00:00.000Z")
			record.Set("closure_actor", owner)
		}
		if err = app.Save(record); err != nil {
			t.Fatal(err)
		}
		created = append(created, saved{
			id:         record.Id,
			created:    record.GetString("created"),
			sourceName: f.sourceName,
			status:     f.status,
			content:    record.GetString("content_hash"),
			validation: record.GetString("validation_error"),
			closedAt:   record.GetString("closed_at"),
		})
	}

	if err = app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}

	tasks, err = app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	field, ok := tasks.Fields.GetByName("name").(*core.TextField)
	if !ok || field.Max != 255 {
		t.Fatalf("name field = %#v, want a 255-code-point text field", tasks.Fields.GetByName("name"))
	}

	assertTaskNames := func(stage string) {
		t.Helper()
		for index, f := range fixtures {
			got, err := app.FindRecordById(intake.TasksCollection, created[index].id)
			if err != nil {
				t.Fatalf("%s: load task %d: %v", stage, index, err)
			}
			if name := got.GetString("name"); name != f.want {
				t.Errorf("%s: task %d name = %q, want %q", stage, index, name, f.want)
			}
			if got.Id != created[index].id || got.GetString("created") != created[index].created {
				t.Errorf("%s: task %d identity or upload time changed", stage, index)
			}
			if source := got.GetString("source_name"); source != created[index].sourceName {
				t.Errorf("%s: task %d source_name = %q, want %q", stage, index, source, created[index].sourceName)
			}
			if status := got.GetString("status"); status != created[index].status {
				t.Errorf("%s: task %d status = %q, want %q", stage, index, status, created[index].status)
			}
			if hash := got.GetString("content_hash"); hash != created[index].content {
				t.Errorf("%s: task %d content_hash changed", stage, index)
			}
			if message := got.GetString("validation_error"); message != created[index].validation {
				t.Errorf("%s: task %d validation_error changed", stage, index)
			}
			if closedAt := got.GetString("closed_at"); closedAt != created[index].closedAt {
				t.Errorf("%s: task %d closed_at changed", stage, index)
			}
		}
	}
	assertTaskNames("after migration")

	// A retried setup must preserve a name an operator already chose.
	renamed, err := app.FindRecordById(intake.TasksCollection, created[0].id)
	if err != nil {
		t.Fatal(err)
	}
	renamed.Set("name", "Custom name")
	if err = app.Save(renamed); err != nil {
		t.Fatal(err)
	}
	if err = backfillTaskNames(app); err != nil {
		t.Fatal(err)
	}
	preserved, err := app.FindRecordById(intake.TasksCollection, created[0].id)
	if err != nil {
		t.Fatal(err)
	}
	if name := preserved.GetString("name"); name != "Custom name" {
		t.Fatalf("repeated backfill replaced a populated name with %q", name)
	}
	fixtures[0].want = "Custom name"
	assertTaskNames("after repeated backfill")
}
