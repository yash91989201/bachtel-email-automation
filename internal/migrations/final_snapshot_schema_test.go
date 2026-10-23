package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// The final-snapshot schema is the server's only approval identity. A
// migration on a temporary database must keep the mapping version on existing
// revisions, allow snapshot/attempt/task links, and reject a repeated
// finalization action or a second approval for the same snapshot scope.
func TestFinalSnapshotSchemaKeepsIdentityAndConstraints(t *testing.T) {
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
	campaignRecord.Set("name", "Snapshot campaign")
	if err := app.Save(campaignRecord); err != nil {
		t.Fatal(err)
	}

	configurations, err := app.FindCollectionByNameOrId(campaign.ConfigurationsCollection)
	if err != nil {
		t.Fatal(err)
	}
	document, err := filesystem.NewFileFromBytes([]byte("document"), "body.docx")
	if err != nil {
		t.Fatal(err)
	}
	configuration := core.NewRecord(configurations)
	configuration.Set("campaign", campaignRecord.Id)
	configuration.Set("revision", 1)
	configuration.Set("subject", "Hello {{candidate_name}}")
	configuration.Set("docx", document)
	configuration.Set("content_hash", "hash-1")
	configuration.Set("converter", "pandoc")
	configuration.Set("mapping_version", campaign.CurrentMappingVersion)
	configuration.Set("mappings", []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "candidate_name", Header: "Name", Required: true},
	})
	if err := app.Save(configuration); err != nil {
		t.Fatalf("mapping-version revision: %v", err)
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
	task.Set("configuration", configuration.Id)
	task.Set("content_hash", "hash-1")
	task.Set("source", source)
	task.Set("source_digest", "source-digest")
	if err := app.Save(task); err != nil {
		t.Fatal(err)
	}

	snapshots, err := app.FindCollectionByNameOrId(finalization.SnapshotsCollection)
	if err != nil {
		t.Fatal(err)
	}
	newSnapshot := func(actionID string) *core.Record {
		record := core.NewRecord(snapshots)
		record.Set("owner", owner)
		record.Set("task", task.Id)
		record.Set("campaign", campaignRecord.Id)
		record.Set("configuration", configuration.Id)
		record.Set("kind", finalization.KindUpload)
		record.Set("action_id", actionID)
		record.Set("digest", "digest-"+actionID)
		record.Set("content_hash", "hash-1")
		return record
	}
	snapshot := newSnapshot("action-1")
	if err := app.Save(snapshot); err != nil {
		t.Fatalf("valid snapshot: %v", err)
	}
	if err := app.Save(newSnapshot("action-1")); err == nil {
		t.Fatal("a repeated finalization action ID was accepted for one task")
	}
	if err := app.Save(newSnapshot("action-2")); err != nil {
		t.Fatalf("a replacement must be able to create a new snapshot identity: %v", err)
	}

	task.Set("selected_final_snapshot", snapshot.Id)
	task.Set("final_snapshot_digest", "digest-action-1")
	if err := app.Save(task); err != nil {
		t.Fatalf("selecting the snapshot: %v", err)
	}
	reloaded, err := app.FindRecordById(intake.TasksCollection, task.Id)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetString("selected_final_snapshot") != snapshot.Id || reloaded.GetString("final_snapshot_digest") != "digest-action-1" {
		t.Fatal("task snapshot selection did not persist")
	}

	connections, err := app.FindCollectionByNameOrId("mail_connections")
	if err != nil {
		t.Fatal(err)
	}
	connection := core.NewRecord(connections)
	connection.Set("owner", owner)
	connection.Set("provider", "titan")
	connection.Set("sender", "sender@example.test")
	connection.Set("status", "connected")
	connection.Set("generation", 1)
	connection.Set("current", true)
	if err := app.Save(connection); err != nil {
		t.Fatalf("connection: %v", err)
	}
	attempts, err := app.FindCollectionByNameOrId(sending.AttemptsCollection)
	if err != nil {
		t.Fatal(err)
	}
	attempt := core.NewRecord(attempts)
	attempt.Set("owner", owner)
	attempt.Set("connection", connection.Id)
	attempt.Set("connection_generation", 1)
	attempt.Set("task", task.Id)
	attempt.Set("configuration", configuration.Id)
	attempt.Set("kind", sending.KindTest)
	attempt.Set("state", sending.AttemptSubmitted)
	attempt.Set("content_hash", "hash-1")
	attempt.Set("recipient", "test@example.test")
	attempt.Set("subject", "Hello Ada")
	attempt.Set("snapshot", snapshot.Id)
	attempt.Set("snapshot_digest", "digest-action-1")
	if err := app.Save(attempt); err != nil {
		t.Fatalf("snapshot-bound attempt: %v", err)
	}

	approvals, err := app.FindCollectionByNameOrId(sending.ApprovalsCollection)
	if err != nil {
		t.Fatal(err)
	}
	newApproval := func(snapshotID, digest string) *core.Record {
		record := core.NewRecord(approvals)
		record.Set("owner", owner)
		record.Set("task", task.Id)
		record.Set("snapshot", snapshotID)
		record.Set("snapshot_digest", digest)
		record.Set("connection", connection.Id)
		record.Set("connection_generation", 1)
		record.Set("configuration", configuration.Id)
		record.Set("content_hash", "hash-1")
		record.Set("attempt", attempt.Id)
		return record
	}
	if err := app.Save(newApproval(snapshot.Id, "digest-action-1")); err != nil {
		t.Fatalf("snapshot approval: %v", err)
	}
	if err := app.Save(newApproval(snapshot.Id, "digest-action-1")); err == nil {
		t.Fatal("a second approval for the same exact snapshot scope was accepted")
	}
}
