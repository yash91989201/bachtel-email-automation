package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// A task created before the schedule migration must read back as unscheduled:
// the additive fields exist on the fresh schema and the backfill records none.
func TestTaskScheduleMigrationAddsFieldsAndDefaultsExistingTask(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	var original core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < "1792320020" {
			original.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, original).Up(); err != nil {
		t.Fatal(err)
	}

	campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	target := core.NewRecord(campaigns)
	target.Set("name", "Existing campaign")
	target.Set("owner", newOperator(t, app))
	target.Set("source_headers", []string{"Email"})
	if err = app.Save(target); err != nil {
		t.Fatal(err)
	}
	tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	source, err := filesystem.NewFileFromBytes([]byte("legacy workbook"), "legacy.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	existing := core.NewRecord(tasks)
	existing.Set("campaign", target.Id)
	existing.Set("status", intake.TaskReady)
	existing.Set("source", source)
	if err = app.Save(existing); err != nil {
		t.Fatal(err)
	}

	if err = app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	tasks, err = app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tasks.Fields.GetByName("schedule_at").(*core.DateField); !ok {
		t.Fatal("schedule_at is not a date field")
	}
	if _, ok := tasks.Fields.GetByName("schedule_state").(*core.TextField); !ok {
		t.Fatal("schedule_state is not a text field")
	}
	saved, err := app.FindRecordById(intake.TasksCollection, existing.Id)
	if err != nil {
		t.Fatal(err)
	}
	if state := saved.GetString("schedule_state"); state != intake.ScheduleNone {
		t.Fatalf("existing task schedule_state = %q, want none", state)
	}
	if !saved.GetDateTime("schedule_at").Time().IsZero() {
		t.Fatal("existing task gained a schedule instant")
	}
}
