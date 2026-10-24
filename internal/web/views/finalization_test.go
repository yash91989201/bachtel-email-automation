package views

import (
	"context"
	"io"
	"strings"
	"testing"
)

// Dataset finalization, snapshot formatting approval and send authorization are
// separate, labelled states in the task workspace.
func TestTaskFinalizationStatesAreDistinct(t *testing.T) {
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionRecipients,
		FinalizationRequired: true,
		Finalization: TaskFinalizationView{
			Available:        true,
			Required:         true,
			CanFinalize:      true,
			CanUpload:        true,
			CanApprove:       true,
			PreparedReady:    true,
			PreparedAccepted: 2,
			PreparedHref:     "/campaigns/campaign-1/tasks/task-1/preparation/sending.xlsx",
		},
		Counts: TaskCounts{Total: 2, Eligible: 2},
		Rows: []TaskAdmittedRow{
			{ID: "member-1", Row: 2, Position: 1, Recipient: "ada@example.com", State: "eligible"},
		},
	}))
	if workspaceNode(doc, "id", "task-finalization") == nil {
		t.Fatal("finalization section is missing")
	}
	if workspaceNode(doc, "id", "final-workbook") == nil {
		t.Fatal("edited final upload control is missing")
	}
	if workspaceNode(doc, "id", "finalize-approve") == nil {
		t.Fatal("approve-as-is confirmation is missing")
	}
	text := renderText(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionRecipients, FinalizationRequired: true,
		Finalization: TaskFinalizationView{Available: true, Required: true, CanFinalize: true, CanUpload: true, CanApprove: true, PreparedHref: "/prepared.xlsx", PreparedAccepted: 2},
	}))
	for _, want := range []string{
		"Approve prepared data as-is",
		"Upload and validate final sheet",
		"does not send a test, approve received formatting, or authorize candidate sending or scheduling",
		"Download prepared sending sheet",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("finalization copy missing %q", want)
		}
	}
	// Before finalization the test panel explains the separate prerequisite and
	// offers no formatting approval.
	body := renderText(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionTest, FinalizationRequired: true,
		Finalization: TaskFinalizationView{Available: true, Required: true, CanFinalize: true, CanUpload: true},
		Rows:         []TaskAdmittedRow{{ID: "member-1", Row: 2, Position: 1, Recipient: "ada@example.com", State: "eligible"}},
	}))
	if !strings.Contains(body, "Finalize the sending data before sending a test for approval") {
		t.Fatal("pre-finalization test prerequisite missing")
	}
	if strings.Contains(body, "Approve received formatting") {
		t.Fatal("formatting approval is offered before dataset finalization")
	}

	// After finalization the accepted snapshot is shown with its download and
	// the snapshot-bound test controls.
	finalized := TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionRecipients, FinalizationRequired: true, DatasetFinalized: true,
		FinalSnapshotRef: "abcd1234",
		Finalization: TaskFinalizationView{
			Available: true, Required: true, CanFinalize: true, CanUpload: true,
			HasSelected: true,
			Selected: TaskFinalSnapshot{
				ID: "snapshot-1", Reference: "abcd1234", Kind: "Edited final upload",
				Accepted: 1, Digest: "digest", Actor: "operator",
				SheetHref: "/campaigns/campaign-1/tasks/task-1/finalization/sending.xlsx?snapshot=snapshot-1",
			},
			History: []TaskFinalSnapshot{{ID: "snapshot-1", Reference: "abcd1234", Accepted: 1, SheetHref: "/sheet.xlsx"}},
		},
		Rows: []TaskAdmittedRow{{ID: "member-1", Row: 2, Position: 1, Recipient: "ada@example.com", State: "eligible"}},
	}
	recipientsPage := finalized
	recipientsPage.Section = TaskSectionRecipients
	body = renderText(t, TaskPage(recipientsPage))
	if !strings.Contains(body, "Download accepted final sheet") {
		t.Fatal("finalized Recipients section is missing the accepted snapshot download")
	}
	testPage := finalized
	testPage.Section = TaskSectionTest
	body = renderText(t, TaskPage(testPage))
	for _, want := range []string{
		"Final snapshot <strong>abcd1234</strong> is accepted",
		"Approve received formatting",
		"this exact final snapshot",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("finalized Test & approve section missing %q", want)
		}
	}
}

func TestRoleMappingTaskHidesIntakeRowsUntilPreparedReview(t *testing.T) {
	preflight := TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionRecipients,
		RoleMapping: true, FinalizationRequired: true,
		StatusLabel:       "Eligibility pending",
		StatusExplanation: "Run data preparation, select its output, and review accepted and held entries.",
		Summary:           TaskSummaryView{SourceRows: 4},
		Counts:            TaskCounts{Total: 9, Eligible: 8, Invalid: 1},
		Rows:              []TaskAdmittedRow{{ID: "old", Row: 2, Position: 1, Recipient: "stale@example.com", State: "eligible"}},
		HeldRows:          []TaskHeldRow{{Row: 4, Position: 1, Recipient: "held-stale@example.com", Reason: "old validation"}},
		Finalization:      TaskFinalizationView{Available: true, Required: true, CanFinalize: true, CanUpload: true},
	}
	body := renderText(t, TaskPage(preflight))
	for _, forbidden := range []string{"stale@example.com", "held-stale@example.com", "9 recipient entries", "8 eligible at validation"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("preflight page exposed raw intake value %q", forbidden)
		}
	}
	if !strings.Contains(body, "Validation is pending") || !strings.Contains(body, "4 source rows") {
		t.Fatal("role-mapping preflight does not explain pending validation and source rows")
	}
	for _, want := range []string{
		"Eligibility pending",
		"Workbook received",
		"after a preparation run completes and backend validation finishes",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("preflight page missing %q", want)
		}
	}
	// Before review there is no exclusion evidence to download.
	if strings.Contains(body, "/excluded.xlsx") {
		t.Fatal("preflight page offers an exclusion download without evidence")
	}

	prepared := preflight
	prepared.Finalization.PreparedReview = true
	prepared.Finalization.PreparedCounts = TaskFinalReviewCounts{SourceRows: 4, Entries: 3, Accepted: 2, Excluded: 1}
	prepared.Finalization.PreparedEntries = []TaskFinalReviewEntry{{Row: 2, Position: 1, Recipient: "accepted@example.com"}}
	prepared.Finalization.PreparedExclusions = []TaskFinalReviewExclusion{{Row: 3, Position: 2, Recipient: "held@example.com", Reason: "missing mapped value"}}
	body = renderText(t, TaskPage(prepared))
	for _, want := range []string{"2 accepted", "accepted@example.com", "held@example.com", "missing mapped value"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prepared backend review missing %q", want)
		}
	}
}

func renderText(t *testing.T, component interface {
	Render(ctx context.Context, w io.Writer) error
}) string {
	t.Helper()
	var builder strings.Builder
	if err := component.Render(context.Background(), &builder); err != nil {
		t.Fatal(err)
	}
	return builder.String()
}
