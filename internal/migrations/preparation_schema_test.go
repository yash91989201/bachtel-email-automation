package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// A preparation run is owner-scoped audit evidence with one execution claim.
// The schema must refuse an unowned run and a second job for one run.
func TestPreparationSchemaEnforcesOwnershipAndSingleClaim(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}

	owner := newOperator(t, app)
	campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	campaignRecord := core.NewRecord(campaigns)
	campaignRecord.Set("owner", owner)
	campaignRecord.Set("name", "Campaign")
	if err := app.Save(campaignRecord); err != nil {
		t.Fatal(err)
	}
	tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	source, err := filesystem.NewFileFromBytes([]byte("workbook"), "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	task := core.NewRecord(tasks)
	task.Set("campaign", campaignRecord.Id)
	task.Set("status", intake.TaskReady)
	task.Set("schedule_state", intake.ScheduleNone)
	task.Set("source", source)
	if err := app.Save(task); err != nil {
		t.Fatal(err)
	}

	runs, err := app.FindCollectionByNameOrId(preparation.RunsCollection)
	if err != nil {
		t.Fatal(err)
	}
	newRun := func() *core.Record {
		record := core.NewRecord(runs)
		record.Set("owner", owner)
		record.Set("task", task.Id)
		record.Set("kind", preparation.KindInitial)
		record.Set("state", preparation.RunQueued)
		record.Set("provider", "custom")
		record.Set("api_root", "https://example.test/v1")
		record.Set("api_format", "responses")
		record.Set("model", "test-model")
		record.Set("configuration_generation", 1)
		record.Set("credential_generation", 1)
		record.Set("base_prompt_version", preparation.BasePromptVersion)
		record.Set("base_prompt", preparation.CurrentBasePrompt())
		return record
	}
	unowned := newRun()
	unowned.Set("owner", "")
	if err := app.Save(unowned); err == nil {
		t.Fatal("a preparation run without an owner was accepted")
	}
	run := newRun()
	if err := app.Save(run); err != nil {
		t.Fatalf("valid run: %v", err)
	}
	secondRun := newRun()
	if err := app.Save(secondRun); err != nil {
		t.Fatalf("a task may keep multiple run evidence records: %v", err)
	}

	jobs, err := app.FindCollectionByNameOrId(preparation.JobsCollection)
	if err != nil {
		t.Fatal(err)
	}
	newJob := func(runID string) *core.Record {
		record := core.NewRecord(jobs)
		record.Set("owner", owner)
		record.Set("task", task.Id)
		record.Set("run", runID)
		record.Set("state", preparation.JobQueued)
		record.Set("attempt", "attempt")
		record.Set("batch_total", 1)
		return record
	}
	if err := app.Save(newJob(run.Id)); err != nil {
		t.Fatalf("first job: %v", err)
	}
	if err := app.Save(newJob(run.Id)); err == nil {
		t.Fatal("a second job for one run was accepted")
	}
	if err := app.Save(newJob(secondRun.Id)); err != nil {
		t.Fatalf("a job for another run: %v", err)
	}

	// Selection stores only the chosen completed run on the owning task.
	task.Set("selected_preparation_run", run.Id)
	task.Set("preparation_instructions", "Expand abbreviations.")
	if err := app.Save(task); err != nil {
		t.Fatalf("save task selection: %v", err)
	}
	reloaded, err := app.FindRecordById(intake.TasksCollection, task.Id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetString("selected_preparation_run") != run.Id {
		t.Fatal("task selection did not persist")
	}
	if reloaded.GetString("preparation_instructions") != "Expand abbreviations." {
		t.Fatal("instruction draft did not persist")
	}
}
