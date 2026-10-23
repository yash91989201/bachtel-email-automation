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
		StoredStatus: "ready", ActionID: "action-",
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
		StoredStatus: "ready", ActionID: "action-", FinalizationRequired: true,
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
		StoredStatus: "ready", ActionID: "action-", FinalizationRequired: true,
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
		StoredStatus: "ready", ActionID: "action-", FinalizationRequired: true, DatasetFinalized: true,
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
	body = renderText(t, TaskPage(finalized))
	for _, want := range []string{
		"Final snapshot <strong>abcd1234</strong> is accepted",
		"Download accepted final sheet",
		"Approve received formatting",
		"this exact final snapshot",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("finalized task page missing %q", want)
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
