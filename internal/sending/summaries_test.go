package sending_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// CampaignSummaries must agree with the single-task preview for the same facts,
// stay owner-scoped and write nothing (no attempt, action or artifact).
func TestCampaignSummariesMatchPreviewWithoutWrites(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	task := f.task(t, 3)

	attemptsBefore := recordCount(t, f.app, sending.AttemptsCollection)
	actionsBefore := recordCount(t, f.app, sending.ActionsCollection)
	artifactsBefore := recordCount(t, f.app, exports.ArtifactsCollection)

	summaries, err := f.store.CampaignSummaries(ctx, f.ownerID, f.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := summaries[task]
	if !ok {
		t.Fatalf("summaries = %+v, want the campaign's task", summaries)
	}
	preview, err := f.store.Preview(ctx, f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Eligible != preview.Eligible || summary.Remaining != preview.Remaining ||
		summary.Submitted != preview.Submitted || summary.InFlight != preview.InFlight ||
		summary.Reserved != preview.Reserved || summary.Approved != preview.Approved ||
		summary.Status != preview.Status || summary.Invalid != preview.Invalid ||
		summary.Duplicate != preview.Duplicate || summary.Ineligible != preview.Ineligible {
		t.Fatalf("summary = %+v, preview = %+v", summary, preview)
	}
	if summary.Eligible != 3 {
		t.Fatalf("eligible = %d, want the three fresh candidates", summary.Eligible)
	}
	if recordCount(t, f.app, sending.AttemptsCollection) != attemptsBefore ||
		recordCount(t, f.app, sending.ActionsCollection) != actionsBefore ||
		recordCount(t, f.app, exports.ArtifactsCollection) != artifactsBefore {
		t.Fatal("loading campaign summaries wrote an attempt, action or artifact")
	}

	// Another operator cannot load this campaign's summaries at all.
	other := provisionOperator(t, f.app, "other-summary@example.invalid")
	if _, err := f.store.CampaignSummaries(ctx, other, f.campaignID); !errors.Is(err, sending.ErrNotFound) {
		t.Fatalf("foreign summaries error = %v, want ErrNotFound", err)
	}
}

// A campaign-level approval is scoped to the current mailbox generation:
// replacing the mailbox invalidates it without touching the stored revision.
func TestCampaignSummariesUseMailboxScopedApproval(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	task := f.task(t, 2)
	dispatcher := f.dispatcher(t, &acceptanceMailbox{})
	f.approve(t, task, dispatcher)

	summaries, err := f.store.CampaignSummaries(ctx, f.ownerID, f.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if !summaries[task].Approved {
		t.Fatal("approved task summary reports no approval")
	}
	configuration := taskConfiguration(t, f.app, task)
	approved, err := f.store.ApprovedFor(ctx, f.ownerID, configuration.GetString("configuration"), configuration.GetString("content_hash"))
	if err != nil || !approved {
		t.Fatalf("campaign approval = %t, %v", approved, err)
	}

	// A replacement connection generation is a different mailbox scope.
	connection, err := f.app.FindRecordById(mailbox.ConnectionsCollection, f.connectionID)
	if err != nil {
		t.Fatal(err)
	}
	connection.Set("generation", connection.GetInt("generation")+1)
	if err := f.app.Save(connection); err != nil {
		t.Fatal(err)
	}
	approved, err = f.store.ApprovedFor(ctx, f.ownerID, configuration.GetString("configuration"), configuration.GetString("content_hash"))
	if err != nil || approved {
		t.Fatalf("approval survived a mailbox generation change: %t, %v", approved, err)
	}
	summaries, err = f.store.CampaignSummaries(ctx, f.ownerID, f.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if summaries[task].Approved {
		t.Fatal("task summary kept approval across a mailbox generation change")
	}
}

func recordCount(t *testing.T, app core.App, collection string) int {
	t.Helper()
	records, err := app.FindRecordsByFilter(collection, "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func taskConfiguration(t *testing.T, app core.App, taskID string) *core.Record {
	t.Helper()
	record, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// Approval is reusable across tasks with the same saved configuration and
// mailbox generation, without requiring another test for the second task.
func TestApprovalReusedAcrossMatchingTasks(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	first := f.task(t, 2)
	second := f.task(t, 2)
	f.approve(t, first, f.dispatcher(t, &acceptanceMailbox{}))

	progress, err := f.store.Preview(ctx, f.ownerID, second)
	if err != nil {
		t.Fatal(err)
	}
	if !progress.Approved {
		t.Fatal("a matching task did not reuse the current mailbox approval")
	}
	summaries, err := f.store.CampaignSummaries(ctx, f.ownerID, f.campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if !summaries[first].Approved || !summaries[second].Approved {
		t.Fatalf("summaries did not reuse approval: %+v", summaries)
	}
}
