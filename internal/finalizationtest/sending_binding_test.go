package finalizationtest

import (
	"context"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

func (f *fixture) connectMailbox(t *testing.T) string {
	t.Helper()
	connection, err := f.app.FindCollectionByNameOrId("mail_connections")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(connection)
	record.Set("owner", f.owner)
	record.Set("provider", "titan")
	record.Set("sender", "sender@example.test")
	record.Set("status", "connected")
	record.Set("generation", 1)
	record.Set("current", true)
	if err := f.app.Save(record); err != nil {
		t.Fatalf("save connection: %v", err)
	}
	return record.Id
}

func (f *fixture) eligibleMember(t *testing.T) string {
	t.Helper()
	record, err := f.app.FindFirstRecordByFilter("task_candidates", "task = {:task} && state = 'eligible'", dbx.Params{"task": f.task})
	if err != nil {
		t.Fatalf("eligible member: %v", err)
	}
	return record.Id
}

func (f *fixture) submitTest(t *testing.T, store *sending.Store, actionID, memberID string) string {
	t.Helper()
	attempt, err := store.QueueTest(context.Background(), f.owner, sending.TestRequest{
		ActionID:  actionID,
		TaskID:    f.task,
		Actor:     f.owner,
		Recipient: "test@example.test",
		MemberID:  memberID,
	})
	if err != nil {
		t.Fatalf("queue test: %v", err)
	}
	if _, err := f.app.DB().NewQuery("UPDATE submission_attempts SET state={:state} WHERE id={:id}").
		Bind(dbx.Params{"state": sending.AttemptSubmitted, "id": attempt.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	return attempt.ID
}

// Approval belongs to one task and one selected final snapshot: it cannot be
// reused by a replacement snapshot or by another task with identical content.
func TestApprovalIsBoundToTaskAndFinalSnapshot(t *testing.T) {
	f := newFixture(t)
	f.connectMailbox(t)
	rows := []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada Lovelace"}}}
	first, err := f.upload(t, "approval-first-123456", rows)
	if err != nil {
		t.Fatal(err)
	}
	store := sending.NewStore(f.app, sending.StoreOptions{})
	attempt := f.submitTest(t, store, "approval-test-123456", f.eligibleMember(t))
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "approval-approve-123456"); err != nil {
		t.Fatal(err)
	}
	progress, err := store.Preview(context.Background(), f.owner, f.task)
	if err != nil || !progress.Approved || progress.SnapshotID != first.ID {
		t.Fatalf("approved progress = %+v %v", progress, err)
	}

	// An arbitrary sample test formally submitted for this task still cannot
	// approve the finalized snapshot.
	diagnostic, err := store.QueueTest(context.Background(), f.owner, sending.TestRequest{
		ActionID: "approval-arbitrary-1", TaskID: f.task, Actor: f.owner, Recipient: "test@example.test",
		Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada Lovelace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.DB().NewQuery("UPDATE submission_attempts SET state={:state} WHERE id={:id}").
		Bind(dbx.Params{"state": sending.AttemptSubmitted, "id": diagnostic.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := store.Approve(context.Background(), f.owner, f.task, diagnostic.ID, f.owner, "approval-arbitrary-approve"); err == nil {
		t.Fatal("an arbitrary sample test approved a final snapshot")
	}

	// A replacement snapshot has no inherited approval.
	second, err := f.upload(t, "approval-second-123456", rows)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("replacement reused the snapshot identity")
	}
	replaced, err := store.Preview(context.Background(), f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Approved {
		t.Fatal("a replacement snapshot inherited the old approval")
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "approval-start-stale-1", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: replaced.Eligible}); err == nil {
		t.Fatal("candidate start was accepted for a snapshot without approval")
	}
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "approval-late-123456"); err == nil {
		t.Fatal("a test of the replaced snapshot approved the new one")
	}

	// A second task with the same revision and the same values has its own
	// snapshot and cannot reuse the first task's submitted test or approval.
	otherTask := f.createTask(t)
	otherSnapshot, err := f.service.AcceptUpload(context.Background(), f.owner, otherTask, "approval-other-123456", f.owner, "other.xlsx", mustSheet(t, f, rows))
	if err != nil {
		t.Fatal(err)
	}
	if otherSnapshot.ID == first.ID {
		t.Fatal("another task reused a snapshot identity")
	}
	if err := store.Approve(context.Background(), f.owner, otherTask, attempt, f.owner, "approval-cross-task-1"); err == nil {
		t.Fatal("another task's submitted test was accepted")
	}
	otherProgress, err := store.Preview(context.Background(), f.owner, otherTask)
	if err != nil {
		t.Fatal(err)
	}
	if otherProgress.Approved {
		t.Fatal("another task inherited a matching-content approval")
	}
}

// A task whose pinned revision uses the snapshot workflow cannot start or
// schedule before finalization, and a snapshot-bound member test is required.
func TestSnapshotWorkflowRequiresFinalizationBeforeSend(t *testing.T) {
	f := newFixture(t)
	f.connectMailbox(t)
	store := sending.NewStore(f.app, sending.StoreOptions{})
	// Before finalization the task has no candidate rows: an explicit start is
	// refused before any reservation.
	progress, err := store.Preview(context.Background(), f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Finalized {
		t.Fatal("a task without a snapshot reported finalization")
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "unfinalized-start-1234", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 0}); err == nil {
		t.Fatal("unfinalized start was accepted")
	}
	if _, err := store.Schedule(context.Background(), f.owner, sending.ScheduleRequest{ActionID: "unfinalized-schedule-1", TaskID: f.task, At: storeTime(t), Confirmed: true, ExpectedCount: 0}); err == nil {
		t.Fatal("unfinalized schedule was accepted")
	}

	rows := []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada Lovelace"}}}
	snapshot, err := f.upload(t, "workflow-final-123456", rows)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.Accepted != 1 {
		t.Fatalf("final snapshot = %+v", snapshot.Counts)
	}
	// Finalized but unapproved: start remains refused.
	progress, err = store.Preview(context.Background(), f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if !progress.Finalized || progress.Approved {
		t.Fatalf("progress after finalization = %+v", progress)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "finalized-unapproved-1", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 1}); err == nil {
		t.Fatal("start without snapshot approval was accepted")
	}
	attempt := f.submitTest(t, store, "workflow-test-123456", f.eligibleMember(t))
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "workflow-approve-123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "finalized-approved-1", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatalf("approved start: %v", err)
	}
}

// Resume, retry and resend recheck the exact snapshot approval and pinned
// revision; a stale or missing approval cannot reserve work again.
func TestResumeRechecksSnapshotApprovalAndRevision(t *testing.T) {
	f := newFixture(t)
	f.connectMailbox(t)
	rows := []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}}}
	if _, err := f.upload(t, "resume-final-123456", rows); err != nil {
		t.Fatal(err)
	}
	store := sending.NewStore(f.app, sending.StoreOptions{})
	attempt := f.submitTest(t, store, "resume-test-123456", f.eligibleMember(t))
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "resume-approve-123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "resume-start-123456", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("status", "paused")
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	// Removing the exact approval makes a resume stick to evidence rules.
	if _, err := f.app.DB().NewQuery("DELETE FROM test_approvals WHERE task = {:task}").Bind(dbx.Params{"task": f.task}).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "resume-no-approval-1", TaskID: f.task, Kind: "resume", Confirmed: true, ExpectedCount: 1}); err == nil {
		t.Fatal("resume without the snapshot approval was accepted")
	}
	// Restoring approval but moving the campaign revision still refuses.
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "resume-reapprove-12345"); err != nil {
		t.Fatal(err)
	}
	config, err := f.app.FindRecordById(campaign.ConfigurationsCollection, f.config)
	if err != nil {
		t.Fatal(err)
	}
	document, err := filesystem.NewFileFromBytes([]byte("changed"), "changed.docx")
	if err != nil {
		t.Fatal(err)
	}
	second := core.NewRecord(config.Collection())
	second.Set("campaign", f.campaign)
	second.Set("revision", 2)
	second.Set("subject", "Changed")
	second.Set("docx", document)
	second.Set("body_html", "<p>Changed</p>")
	second.Set("content_hash", "content-hash-2")
	second.Set("converter", "test")
	second.Set("mapping_version", campaign.CurrentMappingVersion)
	second.Set("mappings", config.Get("mappings"))
	if err := f.app.Save(second); err != nil {
		t.Fatal(err)
	}
	campaignRecord, err := f.app.FindRecordById(campaign.CampaignsCollection, f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	campaignRecord.Set("current_configuration", second.Id)
	if err := f.app.Save(campaignRecord); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "resume-stale-config-1", TaskID: f.task, Kind: "resume", Confirmed: true, ExpectedCount: 1}); err == nil {
		t.Fatal("resume under a changed campaign revision was accepted")
	}
}

// mustSheet renders the fixture's final sheet for another task upload.
func mustSheet(t *testing.T, f *fixture, rows []sheetRow) []byte {
	t.Helper()
	configuration, err := f.campaigns.LoadConfiguration(context.Background(), f.owner, f.config)
	if err != nil {
		t.Fatal(err)
	}
	data, err := sendingSheet(configuration, rows)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// storeTime returns a future instant acceptable to the schedule lead.
func storeTime(t *testing.T) time.Time {
	t.Helper()
	return time.Now().UTC().Add(2 * time.Hour)
}
