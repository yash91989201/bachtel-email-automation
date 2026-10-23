package reset_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
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
	return app
}

func provision(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

func create(t *testing.T, app core.App, collection string, values map[string]any) *core.Record {
	t.Helper()
	model, err := app.FindCollectionByNameOrId(collection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(model)
	for key, value := range values {
		record.Set(key, value)
	}
	if err := app.Save(record); err != nil {
		t.Fatalf("create %s: %v", collection, err)
	}
	return record
}

// ownedWorkspace creates one operator's campaign, revision, task, candidate,
// attempt, approval, action, export artifact and pacing row.
func ownedWorkspace(t *testing.T, app core.App, ownerID, name string) (campaignID, taskID string) {
	t.Helper()
	target := create(t, app, campaign.CampaignsCollection, map[string]any{"owner": ownerID, "name": name})
	revision := create(t, app, campaign.ConfigurationsCollection, map[string]any{
		"campaign": target.Id, "revision": 1, "subject": "Subject", "body_html": "<p>Body</p>",
		"content_hash": "hash-" + name, "converter": "test",
		"docx": mustFile(t, "body.docx", "docx-bytes"),
	})
	target.Set("current_configuration", revision.Id)
	if err := app.Save(target); err != nil {
		t.Fatal(err)
	}
	task := create(t, app, intake.TasksCollection, map[string]any{
		"campaign": target.Id, "configuration": revision.Id, "status": "completed",
		"source": mustFile(t, "source.xlsx", "workbook-bytes"), "schedule_state": intake.ScheduleNone,
	})
	candidate := create(t, app, intake.CandidatesCollection, map[string]any{
		"campaign": target.Id, "email_key": name + "@example.test", "submission_state": intake.SubmissionNone,
	})
	member := create(t, app, intake.TaskCandidatesCollection, map[string]any{
		"task": task.Id, "candidate": candidate.Id, "source_row": 2, "recipient_index": 1,
		"original_recipient": name + "@example.test", "state": intake.RowEligible,
	})
	connection := create(t, app, mailbox.ConnectionsCollection, map[string]any{
		"owner": ownerID, "provider": string(mailbox.ProviderTitan), "sender": name + "@example.test",
		"status": string(mailbox.StatusConnected), "generation": 1, "current": true,
	})
	attempt := create(t, app, sending.AttemptsCollection, map[string]any{
		"owner": ownerID, "connection": connection.Id, "connection_generation": 1,
		"task": task.Id, "member": member.Id, "candidate": candidate.Id, "configuration": revision.Id,
		"kind": sending.KindCandidate, "state": sending.AttemptSubmitted, "content_hash": "hash-" + name,
		"recipient": candidate.GetString("email_key"), "sender": name + "@example.test",
		"subject": "Subject", "body": "<p>Body</p>",
	})
	create(t, app, sending.ApprovalsCollection, map[string]any{
		"owner": ownerID, "connection": connection.Id, "connection_generation": 1,
		"configuration": revision.Id, "attempt": attempt.Id, "content_hash": "hash-" + name, "actor": "test",
	})
	create(t, app, sending.ActionsCollection, map[string]any{
		"action_id": "action-" + name, "owner": ownerID, "task": task.Id, "kind": "start", "actor": "test",
	})
	create(t, app, sending.ResolutionsCollection, map[string]any{
		"member": member.Id, "action_id": "resolve-" + name, "actor": "test", "basis": "external verification",
	})
	create(t, app, exports.ArtifactsCollection, map[string]any{
		"owner": ownerID, "kind": "task_results", "campaign": target.Id, "task": task.Id,
		"state": "ready", "file": mustFile(t, "results.xlsx", "artifact-bytes"),
	})
	create(t, app, sending.DispatcherCollection, map[string]any{
		"connection": connection.Id, "owner": ownerID, "rate": 25,
	})
	return target.Id, task.Id
}

func mustFile(t *testing.T, name, content string) *filesystem.File {
	t.Helper()
	file, err := filesystem.NewFileFromBytes([]byte(content), name)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func count(t *testing.T, app core.App, collection, filter string, params map[string]any) int {
	t.Helper()
	records, err := app.FindRecordsByFilter(collection, filter, "", 0, 0, dbx.Params(params))
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func TestReplacementResetDeletesSendingDataAndPreservesTemplates(t *testing.T) {
	app := newApp(t)
	store := mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
	service := reset.NewService(app, store)
	ctx := context.Background()

	first := provision(t, app, "first@example.test")
	second := provision(t, app, "second@example.test")
	firstCampaign, firstTask := ownedWorkspace(t, app, first, "first")
	_, secondTaskID := ownedWorkspace(t, app, second, "second")

	// The reset targets the current first workspace connection.
	current, err := store.Current(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.StageReplacement(ctx, mailbox.Candidate{
		OwnerID: first, Provider: mailbox.ProviderTitan, Sender: "new@example.test", Identity: "",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Confirm(ctx, first, operation.ID); err != nil {
		t.Fatal(err)
	}

	// Deleted for the replaced operator.
	for _, spec := range []struct{ collection, filter string }{
		{intake.CandidatesCollection, "campaign.owner = {:o}"},
		{intake.TasksCollection, "campaign.owner = {:o}"},
		{intake.TaskCandidatesCollection, "task.campaign.owner = {:o}"},
		{sending.AttemptsCollection, "owner = {:o}"},
		{sending.ApprovalsCollection, "owner = {:o}"},
		{sending.ActionsCollection, "owner = {:o}"},
		{sending.ResolutionsCollection, "member.task.campaign.owner = {:o}"},
		{exports.ArtifactsCollection, "owner = {:o}"},
	} {
		if n := count(t, app, spec.collection, spec.filter, map[string]any{"o": first}); n != 0 {
			t.Fatalf("%s kept %d replaced-operator records", spec.collection, n)
		}
	}

	// Retained: campaign, revision and the original template document.
	if _, err := app.FindRecordById(campaign.CampaignsCollection, firstCampaign); err != nil {
		t.Fatalf("campaign was not preserved: %v", err)
	}
	revision, err := app.FindFirstRecordByFilter(campaign.ConfigurationsCollection, "campaign = {:c}", map[string]any{"c": firstCampaign})
	if err != nil {
		t.Fatalf("configuration revision was not preserved: %v", err)
	}
	if revision.GetString("docx") == "" {
		t.Fatal("template document was deleted with the sending data")
	}
	if _, err := app.FindRecordById(intake.TasksCollection, firstTask); err == nil {
		t.Fatal("task survived the reset")
	}

	// The other operator keeps everything.
	if _, err := app.FindRecordById(intake.TasksCollection, secondTaskID); err != nil {
		t.Fatalf("second operator's task was deleted: %v", err)
	}
	if n := count(t, app, intake.TasksCollection, "campaign.owner = {:o}", map[string]any{"o": second}); n != 1 {
		t.Fatalf("second operator tasks = %d, want unchanged 1", n)
	}

	// The target connection is current and the old claim released.
	now, err := store.Current(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if now.Sender != "new@example.test" || now.Generation != current.Generation+1 {
		t.Fatalf("current connection after reset = %+v", now)
	}
	if _, err := app.FindRecordById(mailbox.ConnectionsCollection, current.ID); err == nil {
		t.Fatal("the replaced connection still holds its claim")
	}
}

// A confirmed replacement blocks the owner's sending-data writes and launches
// before anything is deleted, so competing actions cannot start old work or
// claim the target while the reset runs.
func TestConfirmedResetBlocksOwnerWritesAndLaunches(t *testing.T) {
	app := newApp(t)
	store := mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
	ctx := context.Background()

	ownerID := provision(t, app, "owner@example.test")
	_, taskID := ownedWorkspace(t, app, ownerID, "owner")
	operation, err := store.StageReplacement(ctx, mailbox.Candidate{
		OwnerID: ownerID, Provider: mailbox.ProviderTitan, Sender: "target@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reset.Guard(app)(ctx, ownerID); err != nil {
		t.Fatalf("a staged proposal must not block writes: %v", err)
	}
	if _, err := store.ConfirmReset(ctx, ownerID, operation.ID); err != nil {
		t.Fatal(err)
	}

	blocked := reset.Guard(app)
	if err := blocked(ctx, ownerID); err == nil {
		t.Fatal("new sending-data writes were accepted during the reset")
	}
	sendingStore := sending.NewStore(app, sending.Options{})
	if _, err := sendingStore.Act(ctx, ownerID, sending.ActionRequest{ActionID: "start-during-reset", TaskID: taskID, Kind: "start", Confirmed: true, ExpectedCount: 1}); err == nil {
		t.Fatal("a start action was accepted during the reset")
	}
	if _, err := store.StageReplacement(ctx, mailbox.Candidate{
		OwnerID: ownerID, Provider: mailbox.ProviderTitan, Sender: "other@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "password"},
	}); !errors.Is(err, mailbox.ErrResetInProgress) {
		t.Fatalf("competing replacement = %v, want ErrResetInProgress", err)
	}

	// Completing the reset releases the workspace.
	if err := reset.NewService(app, store).Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := blocked(ctx, ownerID); err != nil {
		t.Fatalf("completed reset still blocks writes: %v", err)
	}
}

func TestInterruptedResetRecoversWithoutRepublishing(t *testing.T) {
	app := newApp(t)
	store := mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
	service := reset.NewService(app, store)
	ctx := context.Background()

	ownerID := provision(t, app, "owner@example.test")
	campaignID, taskID := ownedWorkspace(t, app, ownerID, "owner")
	current, err := store.Current(ctx, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.StageReplacement(ctx, mailbox.Candidate{
		OwnerID: ownerID, Provider: mailbox.ProviderTitan, Sender: "target@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmReset(ctx, ownerID, operation.ID); err != nil {
		t.Fatal(err)
	}
	// The server stops here, before any deletion.

	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	taskFiles := filepath.Join(app.DataDir(), "storage", task.BaseFilesPath())
	if _, err := os.Stat(taskFiles); err != nil {
		t.Fatalf("task storage directory is missing before reset: %v", err)
	}
	artifact, err := app.FindFirstRecordByFilter(exports.ArtifactsCollection, "owner = {:o}", dbx.Params{"o": ownerID})
	if err != nil {
		t.Fatal(err)
	}
	artifactFiles := filepath.Join(app.DataDir(), "storage", artifact.BaseFilesPath())

	if err := service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(taskFiles); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted task files survived recovery: %v", err)
	}
	if _, err := os.Stat(artifactFiles); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted export files survived recovery: %v", err)
	}
	if _, err := app.FindRecordById(intake.TasksCollection, taskID); err == nil {
		t.Fatal("the recovered reset left a task record")
	}
	if n := count(t, app, exports.ArtifactsCollection, "owner = {:o}", map[string]any{"o": ownerID}); n != 0 {
		t.Fatalf("artifacts after recovery = %d", n)
	}
	if _, err := app.FindRecordById(campaign.CampaignsCollection, campaignID); err != nil {
		t.Fatalf("campaign was not preserved by recovery: %v", err)
	}
	now, err := store.Current(ctx, ownerID)
	if err != nil || now.Sender != "target@example.test" {
		t.Fatalf("recovered connection = %+v, err = %v", now, err)
	}
	if _, err := app.FindRecordById(mailbox.ConnectionsCollection, current.ID); err == nil {
		t.Fatal("recovered reset kept the old claim")
	}

	// Recovery is idempotent: a completed reset is never repeated.
	if err := service.Recover(ctx); err != nil {
		t.Fatalf("second recovery: %v", err)
	}
	if err := service.Confirm(ctx, ownerID, operation.ID); err == nil {
		t.Fatal("a completed replacement accepted a second confirmation")
	}
}
