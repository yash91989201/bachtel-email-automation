package retention_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/retention"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

type retentionFixture struct {
	app       core.App
	service   *retention.Service
	exports   *exports.Service
	store     *sending.Store
	intake    *intake.Service
	campaigns *campaign.Service
	now       time.Time
}

func newRetentionFixture(t *testing.T) *retentionFixture {
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
	stub := filepath.Join(t.TempDir(), "pandoc")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = \"--version\" ]; then echo 'pandoc retention'; exit 0; fi\ndone\nprintf '%s' '<p>Hello {{candidate_name}}</p>'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewServiceWithConverter(app, convert.Converter{Path: stub})
	now := time.Date(2026, 10, 18, 4, 0, 0, 0, time.UTC)
	cfg := config.Config{PandocPath: stub, Microsoft: config.Microsoft{SenderAddress: "hr@example.invalid"}}
	return &retentionFixture{
		app:       app,
		service:   retention.NewService(app),
		exports:   exports.NewService(app, cfg),
		store:     sending.NewStore(app, sending.Options{AllowCandidateSending: true, SenderAddress: "hr@example.invalid", Now: func() time.Time { return now }}),
		intake:    intake.NewService(app, campaigns, intake.Limits{}),
		campaigns: campaigns,
		now:       now,
	}
}

func (f *retentionFixture) campaign(t *testing.T, name string) string {
	t.Helper()
	created, err := f.campaigns.CreateCampaign(context.Background(), campaign.CreateCampaignInput{Name: name, SourceHeaders: []string{"Email", "Name"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc bytes.Buffer
	z := zip.NewWriter(&doc)
	part, err := z.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p/></w:body></w:document>`)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.campaigns.CreateRevision(context.Background(), created.ID, campaign.RevisionInput{
		Subject:      "Hello {{candidate_name}}",
		Document:     doc.Bytes(),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
			{Variable: "candidate_name", Header: "Name", Required: true, Format: campaign.FormatName},
		},
	}); err != nil {
		t.Fatal(err)
	}
	return created.ID
}

func (f *retentionFixture) task(t *testing.T, campaignID string, count int) string {
	t.Helper()
	book := excelize.NewFile()
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name"}); err != nil {
		t.Fatal(err)
	}
	for i := range count {
		row := []string{fmt.Sprintf("retention%02d@example.invalid", i), fmt.Sprintf("Candidate %02d", i)}
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", i+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), campaignID, "retention.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), task.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

// submit starts a task, approves a formatting test and completes every batch
// through the durable store so the campaign holds real history.
func (f *retentionFixture) submit(t *testing.T, task string, count int) {
	t.Helper()
	ctx := context.Background()
	attempt, err := f.store.QueueTest(ctx, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.DB().NewQuery("UPDATE submission_attempts SET state='submitted' WHERE id={:id}").Bind(map[string]any{"id": attempt.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Approve(ctx, task, attempt.ID, "operator", "approve-"+task); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Act(ctx, sending.ActionRequest{ActionID: "start-" + task, TaskID: task, Kind: "start", Actor: "operator", Confirmed: true, ExpectedCount: count}); err != nil {
		t.Fatal(err)
	}
	for {
		work, err := f.store.NextWork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if work == nil {
			break
		}
		a, err := f.store.BeginAttempt(ctx, work, f.now, f.now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err = f.store.CompleteAttempt(ctx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.store.SettleTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.exports.GenerateCampaign(ctx, taskCampaign(t, f, task), true); err != nil {
		t.Fatal(err)
	}
}

func taskCampaign(t *testing.T, f *retentionFixture, task string) string {
	t.Helper()
	record, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	return record.GetString("campaign")
}

func (f *retentionFixture) count(t *testing.T, collection, filter string) int {
	t.Helper()
	records, err := f.app.FindRecordsByFilter(collection, filter, "", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func TestDeleteCampaignRemovesScopedRecordsAndFiles(t *testing.T) {
	f := newRetentionFixture(t)
	campaignA := f.campaign(t, "Delete me")
	campaignB := f.campaign(t, "Keep me")
	taskA := f.task(t, campaignA, 3)
	taskB := f.task(t, campaignB, 2)
	f.submit(t, taskA, 3)
	f.submit(t, taskB, 2)

	artifacts, err := f.exports.CampaignArtifacts(context.Background(), campaignA)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected a report and a package before deletion, got %d", len(artifacts))
	}
	filePaths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.State != exports.StateReady {
			t.Fatalf("artifact %s is %s", artifact.Kind, artifact.State)
		}
		record, err := f.app.FindRecordById(exports.ArtifactsCollection, artifact.ID)
		if err != nil {
			t.Fatal(err)
		}
		filePaths = append(filePaths, record.BaseFilesPath()+"/"+record.GetString("file"))
	}

	if err = f.service.DeleteCampaign(context.Background(), campaignA); err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.FindRecordById(campaign.CampaignsCollection, campaignA); err == nil {
		t.Fatal("campaign record survived deletion")
	}
	for _, scope := range []struct {
		collection, filter string
	}{
		{intake.TasksCollection, "campaign='" + campaignA + "'"},
		{intake.TasksCollection, "campaign='" + campaignB + "'"},
		{intake.CandidatesCollection, "campaign='" + campaignA + "'"},
		{intake.CandidatesCollection, "campaign='" + campaignB + "'"},
		{exports.ArtifactsCollection, "campaign='" + campaignA + "'"},
		{exports.ArtifactsCollection, "campaign='" + campaignB + "'"},
		{campaign.ConfigurationsCollection, "campaign='" + campaignA + "'"},
	} {
		count := f.count(t, scope.collection, scope.filter)
		switch {
		case scope.collection == campaign.ConfigurationsCollection && scope.filter == "campaign='"+campaignA+"'":
			if count != 0 {
				t.Fatalf("%s kept %d records", scope.collection, count)
			}
		case scope.filter == "campaign='"+campaignA+"'":
			if count != 0 {
				t.Fatalf("%s kept %d records for the deleted campaign", scope.collection, count)
			}
		default:
			if count == 0 {
				t.Fatalf("%s lost the unrelated campaign's records", scope.collection)
			}
		}
	}
	if f.count(t, sending.AttemptsCollection, "task='"+taskA+"'") != 0 {
		t.Fatal("deleted campaign kept its attempts")
	}
	if f.count(t, sending.AttemptsCollection, "task='"+taskB+"'") == 0 {
		t.Fatal("unrelated campaign lost its attempts")
	}

	// A second deletion of the same campaign affects nothing else.
	if err = f.service.DeleteCampaign(context.Background(), campaignA); err == nil {
		t.Fatal("deleting an already removed campaign reported success")
	}
	if f.count(t, intake.CandidatesCollection, "campaign='"+campaignB+"'") == 0 {
		t.Fatal("retry cleanup touched the unrelated campaign")
	}

	// Stored generated files are removed with their records.
	fsys, err := f.app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fsys.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, path := range filePaths {
		if reader, err := fsys.GetReader(path); err == nil {
			if closeErr := reader.Close(); closeErr != nil {
				t.Error(closeErr)
			}
			t.Fatalf("deleted campaign left its file at %s", path)
		}
	}
}

func TestDeleteCampaignRejectsActiveWork(t *testing.T) {
	f := newRetentionFixture(t)
	ctx := context.Background()
	campaignID := f.campaign(t, "Active")
	task := f.task(t, campaignID, 2)
	attempt, err := f.store.QueueTest(ctx, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.DB().NewQuery("UPDATE submission_attempts SET state='submitted' WHERE id={:id}").Bind(map[string]any{"id": attempt.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Approve(ctx, task, attempt.ID, "operator", "approve-"+task); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Act(ctx, sending.ActionRequest{ActionID: "start-" + task, TaskID: task, Kind: "start", Actor: "operator", Confirmed: true, ExpectedCount: 2}); err != nil {
		t.Fatal(err)
	}
	if err = f.service.DeleteCampaign(ctx, campaignID); err != retention.ErrCampaignActive {
		t.Fatalf("running task allowed deletion: %v", err)
	}
	// An in-flight request also blocks deletion.
	work, err := f.store.NextWork(ctx)
	if err != nil || work == nil {
		t.Fatalf("next work: %v %v", work, err)
	}
	inflight, err := f.store.BeginAttempt(ctx, work, f.now, f.now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.DeleteCampaign(ctx, campaignID); err != retention.ErrCampaignActive {
		t.Fatalf("in-flight attempt allowed deletion: %v", err)
	}

	// Once stopped and settled, the campaign can be removed.
	if _, err = f.store.Act(ctx, sending.ActionRequest{ActionID: "stop-" + task, TaskID: task, Kind: "stop", Actor: "operator", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.CompleteAttempt(ctx, inflight.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.SettleTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.app.FindRecordById(campaign.CampaignsCollection, campaignID); err != nil {
		t.Fatalf("campaign vanished before the final delete: %v", err)
	}
	if err = f.service.DeleteCampaign(ctx, campaignID); err != nil {
		t.Fatalf("settled campaign could not be deleted: %v", err)
	}
}
