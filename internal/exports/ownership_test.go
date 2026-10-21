package exports_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// zipEntryText returns one entry's contents from a generated package.
func zipEntryText(t *testing.T, data []byte, name string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				t.Error(err)
			}
		}()
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(contents)
	}
	t.Fatalf("package entry %q missing", name)
	return ""
}

// ownedTask stages and fully sends one task for ownerID inside campaignID, so
// the campaign records that operator's attempt history.
func (f *exportFixture) ownedTask(t *testing.T, ownerID, campaignID string, rows [][]string) string {
	t.Helper()
	task := f.stageFor(t, ownerID, campaignID, rows)
	f.approveAs(t, ownerID, task)
	if act := f.act(t, ownerID, task, "start", "start-"+task, len(rows)); act.Claimed != len(rows) {
		t.Fatalf("claimed %d, want %d", act.Claimed, len(rows))
	}
	ctx := context.Background()
	for {
		w, err := f.store.NextWork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if w == nil {
			break
		}
		a, err := f.store.BeginAttempt(ctx, w, f.now, f.now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err = f.store.CompleteAttempt(ctx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.SettleTasks(ctx); err != nil {
		t.Fatal(err)
	}
	return task
}

// approveAs queues, completes and approves one formatting test as ownerID,
// which the durable store requires before candidate sending starts.
func (f *exportFixture) approveAs(t *testing.T, ownerID, task string) {
	t.Helper()
	ctx := context.Background()
	a, err := f.store.QueueTest(ctx, ownerID, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	for {
		w, err := f.store.NextWork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if w == nil {
			t.Fatalf("no work for %s", a.ID)
		}
		attempt, err := f.store.BeginAttempt(ctx, w, f.now, f.now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if attempt.ID == a.ID {
			if err = f.store.CompleteAttempt(ctx, attempt.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
				t.Fatal(err)
			}
			break
		}
		if err = f.store.CompleteAttempt(ctx, attempt.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.store.Approve(ctx, ownerID, task, a.ID, "operator", "approve-"+task); err != nil {
		t.Fatal(err)
	}
}

// TestBackgroundGenerationsStayWithinOneOperator proves a shared worker never
// mixes operators: each task workbook and campaign report/package contains only
// its owning operator's tasks and attempts, and every artifact carries the
// owner derived from its campaign.
func TestBackgroundGenerationsStayWithinOneOperator(t *testing.T) {
	f := newExportFixture(t)
	otherOwner, otherCampaign := f.addOperator(t, "second@example.invalid")

	mine := f.ownedTask(t, f.ownerID, f.campaignID, [][]string{
		{"mine-a@example.invalid", "Mine A"},
		{"mine-b@example.invalid", "Mine B"},
	})
	theirs := f.ownedTask(t, otherOwner, otherCampaign, [][]string{
		{"theirs-a@example.invalid", "Theirs A"},
		{"theirs-b@example.invalid", "Theirs B"},
	})
	setTask(t, f, mine, "stopped", true)
	setTask(t, f, theirs, "stopped", true)

	// One background worker serves every operator.
	startExports(t, f.service)
	waitForArtifactState(t, f, mine, exports.StateReady)
	waitForArtifactState(t, f, theirs, exports.StateReady)

	for task, owner := range map[string]string{mine: f.ownerID, theirs: otherOwner} {
		artifact := waitForArtifactState(t, f, task, exports.StateReady)
		if artifact.GetString("owner") != owner {
			t.Fatalf("artifact %s owner = %q, want %q", artifact.Id, artifact.GetString("owner"), owner)
		}
	}

	// Each workbook contains only its own operator's recipients.
	mineBook := f.taskWorkbook(t, f.ownerID, mine)
	for _, foreign := range []string{"theirs-a@example.invalid", "theirs-b@example.invalid"} {
		if strings.Contains(mineBook, foreign) {
			t.Fatalf("my workbook leaked recipient %s", foreign)
		}
	}
	if !strings.Contains(mineBook, "mine-a@example.invalid") {
		t.Fatal("my workbook lost my own recipient")
	}
	theirBook := f.taskWorkbook(t, otherOwner, theirs)
	if strings.Contains(theirBook, "mine-a@example.invalid") || strings.Contains(theirBook, "mine-b@example.invalid") {
		t.Fatal("their workbook leaked my recipients")
	}

	// Each campaign report contains only its own attempts and tasks.
	if err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateCampaign(context.Background(), otherOwner, otherCampaign, true); err != nil {
		t.Fatal(err)
	}
	mineReport := f.campaignSheet(t, f.ownerID, f.campaignID, "Attempts")
	if strings.Contains(mineReport, "theirs-a@example.invalid") {
		t.Fatal("my campaign report leaked another operator's attempts")
	}
	if !strings.Contains(mineReport, "mine-a@example.invalid") {
		t.Fatal("my campaign report lost my own attempts")
	}
	theirReport := f.campaignSheet(t, otherOwner, otherCampaign, "Attempts")
	if strings.Contains(theirReport, "mine-a@example.invalid") || strings.Contains(theirReport, "mine-b@example.invalid") {
		t.Fatal("their campaign report leaked my attempts")
	}

	// Each ZIP index maps only its own attempts to files.
	mineIndex := f.packageIndex(t, f.ownerID, f.campaignID)
	if strings.Contains(mineIndex, theirs) {
		t.Fatal("my package indexed another operator's task")
	}
	theirIndex := f.packageIndex(t, otherOwner, otherCampaign)
	if strings.Contains(theirIndex, mine) {
		t.Fatal("their package indexed my task")
	}
}

// taskWorkbook downloads the ready workbook and joins every cell value.
func (f *exportFixture) taskWorkbook(t *testing.T, ownerID, task string) string {
	t.Helper()
	data, _, err := f.service.DownloadTask(context.Background(), ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	return sheetText(t, data, "Candidates")
}

// campaignSheet downloads one ready campaign report and joins one sheet.
func (f *exportFixture) campaignSheet(t *testing.T, ownerID, campaignID, sheet string) string {
	t.Helper()
	data, _, err := f.service.DownloadCampaign(context.Background(), ownerID, campaignID, exports.KindCampaignReport)
	if err != nil {
		t.Fatal(err)
	}
	return sheetText(t, data, sheet)
}

// packageIndex downloads the ready ZIP and returns its index.csv contents.
func (f *exportFixture) packageIndex(t *testing.T, ownerID, campaignID string) string {
	t.Helper()
	data, _, err := f.service.DownloadCampaign(context.Background(), ownerID, campaignID, exports.KindCampaignPackage)
	if err != nil {
		t.Fatal(err)
	}
	return zipEntryText(t, data, "index.csv")
}

func sheetText(t *testing.T, data []byte, sheet string) string {
	t.Helper()
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, row := range rows {
		out.WriteString(strings.Join(row, " "))
		out.WriteByte('\n')
	}
	return out.String()
}

// TestForeignExportIDsRefusedWithTheAbsentIDError proves every owner-scoped
// exports method returns the identical fixed error for a foreign id and an
// absent id, and that a refused call leaves no side effect.
func TestForeignExportIDsRefusedWithTheAbsentIDError(t *testing.T) {
	f := newExportFixture(t)
	otherOwner, otherCampaign := f.addOperator(t, "second@example.invalid")
	task := f.ownedTask(t, f.ownerID, f.campaignID, [][]string{{"alice@example.invalid", "Alice"}})
	setTask(t, f, task, "stopped", true)
	if err := f.service.GenerateTask(context.Background(), f.ownerID, task, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	ready := waitForArtifactState(t, f, task, exports.StateReady)
	before := ready.GetString("digest")
	attemptsBefore := f.attemptsFingerprint(t)
	ctx := context.Background()

	absentTask := "absenttask00000"
	absentCampaign := "absentcampaign0"
	absentAttempt := "absentattempt00"
	if len(absentTask) != 15 || len(absentCampaign) != 15 || len(absentAttempt) != 15 {
		t.Fatal("fixture ids must be pocketbase-shaped")
	}

	// Task-scoped refusals: foreign and absent produce the same fixed error.
	foreignTaskErr := f.service.GenerateTask(ctx, otherOwner, task, true)
	if !errors.Is(foreignTaskErr, intake.ErrNotFound) {
		t.Fatalf("foreign generation = %v, want intake.ErrNotFound", foreignTaskErr)
	}
	absentTaskErr := f.service.GenerateTask(ctx, otherOwner, absentTask, true)
	if foreignTaskErr.Error() != absentTaskErr.Error() {
		t.Fatalf("foreign and absent generation errors differ: %q vs %q", foreignTaskErr, absentTaskErr)
	}
	for name, call := range map[string]func(string) error{
		"download": func(id string) error { _, _, err := f.service.DownloadTask(ctx, otherOwner, id); return err },
		"artifact": func(id string) error { _, err := f.service.TaskArtifact(ctx, otherOwner, id); return err },
		"delete":   func(id string) error { return f.service.DeleteTask(ctx, otherOwner, id) },
	} {
		foreignErr := call(task)
		if !errors.Is(foreignErr, intake.ErrNotFound) {
			t.Fatalf("%s foreign = %v, want intake.ErrNotFound", name, foreignErr)
		}
		absentErr := call(absentTask)
		if foreignErr.Error() != absentErr.Error() {
			t.Fatalf("%s: foreign and absent errors differ: %q vs %q", name, foreignErr, absentErr)
		}
	}

	// Campaign-scoped refusals.
	foreignCampaignErr := f.service.GenerateCampaign(ctx, otherOwner, f.campaignID, true)
	if !errors.Is(foreignCampaignErr, campaign.ErrNotFound) {
		t.Fatalf("foreign campaign generation = %v, want campaign.ErrNotFound", foreignCampaignErr)
	}
	absentCampaignErr := f.service.GenerateCampaign(ctx, otherOwner, absentCampaign, true)
	if foreignCampaignErr.Error() != absentCampaignErr.Error() {
		t.Fatalf("foreign and absent campaign errors differ: %q vs %q", foreignCampaignErr, absentCampaignErr)
	}
	for name, call := range map[string]func(string) error{
		"campaign download": func(id string) error {
			_, _, err := f.service.DownloadCampaign(ctx, otherOwner, id, exports.KindCampaignReport)
			return err
		},
		"campaign artifacts": func(id string) error { _, err := f.service.CampaignArtifacts(ctx, otherOwner, id); return err },
		"campaign delete":    func(id string) error { return f.service.DeleteCampaign(ctx, otherOwner, id) },
	} {
		foreignErr := call(f.campaignID)
		if !errors.Is(foreignErr, campaign.ErrNotFound) {
			t.Fatalf("%s foreign = %v, want campaign.ErrNotFound", name, foreignErr)
		}
		absentErr := call(absentCampaign)
		if foreignErr.Error() != absentErr.Error() {
			t.Fatalf("%s: foreign and absent errors differ: %q vs %q", name, foreignErr, absentErr)
		}
	}

	// No side effects: my artifact is unchanged and still downloadable by me,
	// attempts were not touched, and no foreign artifact was minted.
	after := waitForArtifactState(t, f, task, exports.StateReady)
	if after.GetString("digest") != before {
		t.Fatal("a refused foreign download changed my artifact")
	}
	if after.GetString("owner") != f.ownerID {
		t.Fatal("a refused foreign call changed the artifact owner")
	}
	if _, _, err := f.service.DownloadTask(ctx, f.ownerID, task); err != nil {
		t.Fatalf("my own download broke after foreign refusals: %v", err)
	}
	if got := f.attemptsFingerprint(t); got != attemptsBefore {
		t.Fatal("foreign refusals changed recorded attempts")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "owner='"+otherOwner+"'"); len(rows) != 0 {
		t.Fatalf("foreign refusals minted %d artifacts", len(rows))
	}

	// Attempt downloads follow the same boundary.
	attempts := f.records(t, sending.AttemptsCollection, "kind='candidate'")
	if len(attempts) == 0 {
		t.Fatal("fixture produced no candidate attempts")
	}
	attemptID := attempts[0].Id
	if _, _, err := f.service.AttemptHTML(ctx, otherOwner, attemptID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign attempt html = %v, want intake.ErrNotFound", err)
	}
	absentAttemptErr := func() error { _, _, err := f.service.AttemptHTML(ctx, otherOwner, "absentattempt00"); return err }()
	foreignAttemptErr := func() error { _, _, err := f.service.AttemptHTML(ctx, otherOwner, attemptID); return err }()
	if foreignAttemptErr.Error() != absentAttemptErr.Error() {
		t.Fatalf("foreign and absent attempt errors differ: %q vs %q", foreignAttemptErr, absentAttemptErr)
	}
	belongs, err := f.service.AttemptBelongsTo(ctx, otherOwner, attemptID, otherCampaign)
	if err != nil {
		t.Fatal(err)
	}
	if belongs {
		t.Fatal("another operator's attempt was reported as belonging to their campaign")
	}
	if _, err := f.service.AttemptBelongsTo(ctx, f.ownerID, attemptID, f.campaignID); err != nil {
		t.Fatalf("owner lost access to their own attempt: %v", err)
	}
}

// TestLateGenerationForDeletedTaskPublishesNothing proves a generation whose
// task disappeared (mailbox reset) retires the claim instead of publishing a
// file: the artifact fails with fixed text and no download exists.
func TestLateGenerationForDeletedTaskPublishesNothing(t *testing.T) {
	f := newExportFixture(t)
	task := f.stage(t, [][]string{{"late@example.invalid", "Late"}})
	setTask(t, f, task, "stopped", true)
	if err := f.service.GenerateTask(context.Background(), f.ownerID, task, false); err != nil {
		t.Fatal(err)
	}
	artifact := waitForArtifactState(t, f, task, exports.StateReady)
	// Simulate a generation in flight when its task is deleted.
	artifact.Set("state", exports.StateGenerating)
	artifact.Set("file", "")
	artifact.Set("digest", "")
	if err := f.app.Save(artifact); err != nil {
		t.Fatal(err)
	}
	taskRecord, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	// The reset deletes the task together with its rows; the export worker
	// only ever observes the missing task.
	for _, member := range f.records(t, intake.TaskCandidatesCollection, "task='"+task+"'") {
		if err := f.app.Delete(member); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.app.Delete(taskRecord); err != nil {
		t.Fatal(err)
	}

	startExports(t, f.service)
	var failed *core.Record
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := f.app.FindRecordById(exports.ArtifactsCollection, artifact.Id)
		if err == nil && record.GetString("state") == exports.StateFailed {
			failed = record
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failed == nil {
		t.Fatal("late generation for a deleted task did not retire its claim")
	}
	if failed.GetString("file") != "" {
		t.Fatal("late generation for a deleted task published a file")
	}
	if failed.GetString("digest") != "" {
		t.Fatal("late generation for a deleted task published a digest")
	}
	if failed.GetString("error") == "" {
		t.Fatal("retired artifact recorded no operator-facing error")
	}
	if _, _, err := f.service.DownloadTask(context.Background(), f.ownerID, task); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("deleted task download = %v, want intake.ErrNotFound", err)
	}
}
