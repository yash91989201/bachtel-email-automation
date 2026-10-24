package views

import (
	"context"
	"io"
	"strings"
	"testing"

	"golang.org/x/net/html"
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

// preparedReviewPage is a completed preparation run whose backend review holds
// a few of many entries, so the page shows substantive review evidence.
func preparedReviewPage() TaskPageData {
	return TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", ActionID: "action-", Section: TaskSectionRecipients,
		RoleMapping: true, FinalizationRequired: true,
		Preparation: TaskPreparationView{
			Available: true, Configured: true, ConnectionLabel: "Confirmed AI connection", Endpoint: "https://ai.example.test",
			SelectedRunID: "run-1", EvidenceRunID: "run-1", EvidenceSelected: true,
			Counts: TaskPreparationCounts{SourceRows: 150, Entries: 151, Accepted: 138, Held: 13, Corrections: 210, Findings: 2},
			Runs:   []TaskPreparationRun{{ID: "run-1", State: "completed", Kind: "prepare", CreatedAt: "24 Oct 2026, 10:00 AM", Model: "web-model", Selected: true}},
			Corrections: []TaskPreparationNote{
				{Row: 2, Field: "Applicant Full Name", Note: "restored spacing"},
			},
			Findings: []TaskPreparationNote{
				{Row: 3, Position: 1, Field: "Department", Scope: "shared", Note: "compensation in a department column"},
			},
			Held: []TaskPreparationHeld{
				{Row: 5, Position: 1, Recipient: "provisional-held@example.com", Reasons: "Department: compensation in a department column"},
			},
		},
		Finalization: TaskFinalizationView{
			Available: true, Required: true, RoleMapping: true,
			CanFinalize: true, CanUpload: true, CanApprove: true,
			PreparedReady: true, PreparedReview: true, PreparedReviewSelected: true, PreparedAccepted: 138,
			PreparedCounts: TaskFinalReviewCounts{SourceRows: 150, Entries: 151, Accepted: 138, Excluded: 13},
			PreparedEntries: []TaskFinalReviewEntry{
				{Row: 2, Position: 1, Recipient: "accepted@example.com"},
				{Row: 3, Position: 1, Recipient: "second@example.com"},
			},
			PreparedExclusions: []TaskFinalReviewExclusion{
				{Row: 5, Position: 1, Recipient: "held@example.com", Reason: "missing mapped value"},
				{Row: 6, Position: 2, Recipient: "duplicate@example.com", Reason: "duplicate recipient address"},
			},
			PreparedHref:     "/campaigns/campaign-1/tasks/task-1/preparation/sending.xlsx",
			SanitizationHref: "/campaigns/campaign-1/tasks/task-1/preparation/sanitization.xlsx",
		},
	}
}

// The completed review defaults to compact totals and count-bearing
// disclosures: backend-held entries open first, routine lists closed, and every
// long list in a bounded, named scroll area.
func TestPreparedReviewUsesCompactCountBearingDisclosures(t *testing.T) {
	data := preparedReviewPage()
	doc := parseView(t, TaskPage(data))
	body := renderText(t, TaskPage(data))
	text := viewText(doc)

	evidence := workspaceNode(doc, "id", "preparation-evidence")
	if evidence == nil {
		t.Fatal("preparation evidence summary is missing")
	}
	if !strings.Contains(viewText(workspaceNode(doc, "id", "summary-heading")), "Prepared review ready") {
		t.Fatal("the role-mapping summary does not reflect the completed review")
	}
	if strings.Contains(text, "are ready for data preparation") || strings.Contains(text, "appear after a preparation run completes") {
		t.Fatal("the reviewed workspace still shows pre-review summary copy")
	}
	if !strings.Contains(viewText(evidence), "Selected run · 210 corrections · 2 unresolved AI findings") {
		t.Fatalf("preparation summary is not the compact AI total: %.400s", viewText(evidence))
	}
	if !strings.Contains(viewText(evidence), "Backend acceptance and held counts appear once in the final review below.") {
		t.Fatal("preparation summary does not defer backend outcomes to the final review")
	}
	// The canonical backend-held list replaces the provisional AI-held rows.
	if workspaceNode(doc, "id", "preparation-held") != nil || strings.Contains(body, "provisional-held@example.com") {
		t.Fatal("provisional held entries are repeated beside the backend review")
	}

	for _, tc := range []struct {
		id    string
		open  string
		count string
	}{
		{id: "preparation-findings", open: "false", count: "Unresolved AI findings · 2"},
		{id: "preparation-corrections", open: "false", count: "Resolved corrections · 210"},
		{id: "preparation-settings", open: "false", count: "Preparation settings and run history · 1"},
		{id: "finalization-held", open: "true", count: "Held entries · 13"},
		{id: "finalization-accepted", open: "false", count: "Accepted recipients · 138"},
		{id: "finalization-reports", open: "false", count: "Download review reports · 2"},
		{id: "finalization-edit", open: "false", count: "Edit data before approval"},
	} {
		root := workspaceNode(doc, "id", tc.id)
		trigger := workspaceNode(doc, "id", tc.id+"-trigger")
		if root == nil || trigger == nil {
			t.Fatalf("disclosure %q is missing", tc.id)
		}
		if got := workspaceAttr(trigger, "aria-expanded"); got != tc.open {
			t.Errorf("%s aria-expanded = %q, want %q", tc.id, got, tc.open)
		}
		if got := hasWorkspaceAttr(workspaceNode(root, "id", tc.id+"-item"), "data-open"); got != (tc.open == "true") {
			t.Errorf("%s item data-open = %t, want %s", tc.id, got, tc.open)
		}
		if !strings.Contains(viewText(trigger), tc.count) {
			t.Errorf("%s trigger text %q does not carry %q", tc.id, viewText(trigger), tc.count)
		}
		if got := workspaceAttr(trigger, "type"); trigger.Data != "button" || got != "button" {
			t.Errorf("%s disclosure trigger is not a keyboard-native button", tc.id)
		}
	}

	// Held evidence precedes the routine accepted list.
	if !before(doc, workspaceNode(doc, "id", "finalization-held"), workspaceNode(doc, "id", "finalization-accepted")) {
		t.Fatal("held entries must appear before accepted recipients")
	}

	// Each long list is one named, bounded ScrollArea containing a shared table
	// with sticky headings; the viewport starts out of the tab order and is
	// promoted by scrollarea.js only when it overflows.
	heldViewport := reviewViewport(t, doc, "Prepared held entries")
	if got := workspaceAttr(heldViewport, "role"); got != "region" {
		t.Fatalf("held viewport role = %q, want region", got)
	}
	for _, label := range []string{"Prepared held entries", "Accepted recipients", "Unresolved AI findings", "Resolved corrections"} {
		viewport := reviewViewport(t, doc, label)
		if got := workspaceAttr(viewport, "tabindex"); got != "-1" {
			t.Errorf("%s viewport tabindex = %q, want the component's -1 default", label, got)
		}
		root := viewport.Parent
		if !strings.Contains(workspaceAttr(root, "class"), "max-h-[min(20rem,60vh)]") {
			t.Errorf("%s scroll area is not bounded by min(20rem, 60vh)", label)
		}
		table := findViewNode(viewport, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "table" })
		if table == nil || workspaceAttr(table, "data-slot") != "table" || !strings.Contains(workspaceAttr(table, "class"), "cn-table-sticky") {
			t.Errorf("%s does not use the sticky shared Table primitive", label)
		}
		if countViewNodes(root, func(n *html.Node) bool {
			return workspaceAttr(n, "data-slot") == "scroll-area-scrollbar" && workspaceAttr(n, "data-orientation") == "horizontal"
		}) != 1 {
			t.Errorf("%s has no horizontal scrollbar for wide tables", label)
		}
	}

	// Truncation is labelled with shown and total counts outside the viewport.
	for _, want := range []string{
		"Showing 2 of 13 held entries",
		"Showing 2 of 138 accepted recipients",
		"Showing 1 of 2 findings",
		"Showing 1 of 210 corrections",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("truncation guidance is missing %q", want)
		}
	}

	// One canonical location per report link, grouped behind the report area.
	for _, href := range []string{
		`href="/campaigns/campaign-1/tasks/task-1/preparation/sanitization.xlsx"`,
		`href="/campaigns/campaign-1/tasks/task-1/excluded.xlsx"`,
		`href="/campaigns/campaign-1/tasks/task-1/preparation/sending.xlsx"`,
	} {
		if got := strings.Count(body, href); got != 1 {
			t.Fatalf("%s appears %d times, want exactly one canonical location", href, got)
		}
	}
	if got := strings.Count(body, "Download sanitization report"); got != 1 {
		t.Fatalf("sanitization report label appears %d times, want one", got)
	}
	if !strings.Contains(text, "One row per prepared recipient entry: outcome, corrections, findings, exclusion reasons and original and prepared values.") {
		t.Fatal("sanitization report purpose is missing")
	}

	// The primary approve area stays outside the disclosures and states the
	// three separate approvals.
	primary := workspaceNode(doc, "id", "finalize-primary")
	if primary == nil || workspaceNode(primary, "id", "finalize-approve") == nil {
		t.Fatal("approve-as-is confirmation is not in the primary area")
	}
	for _, want := range []string{
		"Use prepared data",
		"does not send a test, approve received formatting, or authorize candidate sending or scheduling",
	} {
		if !strings.Contains(viewText(primary), want) {
			t.Fatalf("primary finalization area is missing %q", want)
		}
	}

	// The edit path holds the prepared sheet, edit guidance and the labeled
	// upload in that order.
	edit := workspaceNode(doc, "id", "finalization-edit")
	if !before(doc, workspaceNode(edit, "href", "/campaigns/campaign-1/tasks/task-1/preparation/sending.xlsx"), workspaceNode(edit, "id", "final-workbook")) {
		t.Fatal("the edit path must offer the prepared sheet before the upload")
	}
	if !strings.Contains(viewText(edit), "Edit recipients and mapped values in Excel. Keep the header row.") {
		t.Fatal("the edit path lacks edit guidance")
	}

	// A completed run that is not the selected evidence keeps its own label and
	// still asks for an explicit selection before finalization.
	unselected := preparedReviewPage()
	unselected.Preparation.EvidenceSelected = false
	unselected.Finalization.PreparedReviewSelected = false
	text = viewText(parseView(t, TaskPage(unselected)))
	if !strings.Contains(text, "Prepared run · 210 corrections") {
		t.Fatal("an unselected prepared run is labelled as selected")
	}
	if !strings.Contains(text, "Prepared data review · select this run to finalize") {
		t.Fatal("an unselected review does not ask for selection")
	}
}

// reviewViewport returns the scroll-area viewport named by label.
func reviewViewport(t *testing.T, doc *html.Node, label string) *html.Node {
	t.Helper()
	viewport := findViewNode(doc, func(n *html.Node) bool {
		return workspaceAttr(n, "data-slot") == "scroll-area-viewport" && workspaceAttr(n, "aria-label") == label
	})
	if viewport == nil {
		t.Fatalf("no named scroll-area viewport %q", label)
	}
	return viewport
}

func countViewNodes(n *html.Node, match func(*html.Node) bool) int {
	if n == nil {
		return 0
	}
	count := 0
	if match(n) {
		count++
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		count += countViewNodes(child, match)
	}
	return count
}

// Empty and pending states keep the same disclosures without inventing counts.
func TestPreparedReviewEmptyAndPendingStates(t *testing.T) {
	empty := preparedReviewPage()
	empty.Finalization.PreparedAccepted = 0
	empty.Finalization.CanApprove = false
	empty.Finalization.PreparedCounts = TaskFinalReviewCounts{SourceRows: 4, Entries: 3}
	empty.Finalization.PreparedEntries = nil
	empty.Finalization.PreparedExclusions = nil
	empty.Finalization.PreparedHref = ""
	doc := parseView(t, TaskPage(empty))
	text := viewText(doc)
	for _, want := range []string{
		"No entries are held in this review.",
		"No entries are accepted in this review. Correct the data and upload an edited final sheet.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("empty review is missing %q", want)
		}
	}
	if workspaceNode(doc, "id", "finalization-held") != nil || workspaceNode(doc, "id", "finalization-accepted") != nil {
		t.Fatal("empty review renders empty disclosures")
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-edit-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("zero-accepted edit remedy expanded = %q, want true", got)
	}

	pending := preparedReviewPage()
	pending.Finalization = TaskFinalizationView{
		Available: true, Required: true, RoleMapping: true,
		CanFinalize: true, CanUpload: true,
	}
	pending.Preparation.EvidenceSelected = false
	doc = parseView(t, TaskPage(pending))
	text = viewText(doc)
	for _, want := range []string{
		"Validation is pending. Run data preparation to review accepted entries and held exclusions.",
		"Backend validation is pending. Accepted and held recipient counts appear once the backend review of the prepared run completes.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("pending review is missing %q", want)
		}
	}
	if workspaceNode(doc, "id", "finalization-reports") != nil {
		t.Fatal("pending review offers report downloads before the backend review")
	}
	provisional := workspaceNode(doc, "id", "preparation-held-trigger")
	if provisional == nil || !strings.Contains(viewText(provisional), "Prepared entries held by findings · 13") {
		t.Fatal("pending review hides the provisional preparation-held evidence")
	}
	if got := workspaceAttr(provisional, "aria-expanded"); got != "true" {
		t.Fatalf("provisional held disclosure expanded = %q, want true", got)
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-edit-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("missing-review edit remedy expanded = %q, want true", got)
	}
}

// A finalized task shows the selected outcome, keeps preparation evidence
// labelled as history and reports the final snapshot's own exclusions.
func TestFinalizedReviewSeparatesHistoryAndFinalOutcome(t *testing.T) {
	data := preparedReviewPage()
	data.DatasetFinalized = true
	data.FinalSnapshotRef = "abcd1234"
	data.Finalization = TaskFinalizationView{
		Available: true, Required: true, RoleMapping: true,
		CanFinalize: true, CanUpload: true, HasSelected: true,
		Selected: TaskFinalSnapshot{
			ID: "snapshot-1", Reference: "abcd1234", Kind: "Prepared data", CreatedAt: "24 Oct 2026, 11:00 AM",
			Actor: "operator", Accepted: 138, Excluded: 13, Digest: "digest",
			SheetHref:  "/campaigns/campaign-1/tasks/task-1/finalization/sending.xlsx?snapshot=snapshot-1",
			ReportHref: "/campaigns/campaign-1/tasks/task-1/finalization/exclusions.xlsx?snapshot=snapshot-1",
			Exclusions: []TaskFinalReviewExclusion{
				{Row: 5, Position: 1, Recipient: "final-held@example.com", Reason: "already submitted in this campaign"},
			},
		},
	}
	body := renderText(t, TaskPage(data))
	doc := parseView(t, TaskPage(data))
	text := viewText(doc)
	evidence := workspaceNode(doc, "id", "preparation-evidence")
	if !strings.Contains(viewText(evidence), "Preparation history") {
		t.Fatal("preparation evidence is not labelled as history after finalization")
	}
	for _, want := range []string{
		"Selected final snapshot",
		"138 accepted · 13 excluded",
		"Download accepted final sheet",
		"Download final exclusions",
		"already submitted in this campaign",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("finalized review is missing %q", want)
		}
	}
	if workspaceNode(doc, "id", "prepared-review") != nil {
		t.Fatal("finalized review repeats the prepared review")
	}
	if strings.Contains(text, "Approve as-is becomes available") {
		t.Fatal("finalized review still offers pre-review approval guidance")
	}
	if got := strings.Count(body, `href="/campaigns/campaign-1/tasks/task-1/finalization/exclusions.xlsx?snapshot=snapshot-1"`); got != 1 {
		t.Fatalf("final exclusion report link appears %d times, want one grouped location", got)
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-excluded-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("final exclusion list expanded = %q, want true", got)
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-edit-trigger"), "aria-expanded"); got != "false" {
		t.Fatalf("replacement edit path expanded = %q, want false", got)
	}
	if !strings.Contains(viewText(workspaceNode(doc, "id", "finalization-edit-trigger")), "Edit and upload a replacement") {
		t.Fatal("finalized edit path is not clearly labelled as a replacement")
	}
}

// A refused upload opens the edit path and keeps the prior snapshot and the
// visible error; blocked states never offer an upload.
func TestFinalizationEditRemedyAndBlockedStates(t *testing.T) {
	rejected := preparedReviewPage()
	rejected.Finalization.Error = "The final worksheet is missing the recipient column."
	doc := parseView(t, TaskPage(rejected))
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-edit-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("rejected-upload edit path expanded = %q, want true", got)
	}
	confirmation := workspaceNode(doc, "id", "finalize-approve")
	if confirmation == nil || !hasWorkspaceAttr(confirmation, "required") {
		t.Fatal("a refused upload removed the explicit approval confirmation")
	}
	errorText := viewText(workspaceNode(doc, "data-slot", "alert"))
	if !strings.Contains(errorText, "The final worksheet is missing the recipient column.") {
		t.Fatal("refused upload error is not visible outside the collapsed evidence")
	}
	if strings.Contains(viewText(workspaceNode(doc, "id", "finalization-edit")), "The final worksheet is missing the recipient column.") {
		t.Fatal("the refused upload error is hidden inside the edit disclosure")
	}

	started := preparedReviewPage()
	started.Finalization.CanFinalize = false
	started.Finalization.CanUpload = false
	started.Finalization.CanApprove = false
	started.Finalization.BlockedReason = "This task has started; the final sending data is frozen."
	doc = parseView(t, TaskPage(started))
	if workspaceNode(doc, "id", "final-workbook") != nil || workspaceNode(doc, "id", "finalize-approve") != nil {
		t.Fatal("a started task still offers finalization controls")
	}
	body := renderText(t, TaskPage(started))
	for _, want := range []string{
		"This task has started; the final sending data is frozen.",
		`href="/campaigns/campaign-1/tasks/task-1/preparation/sending.xlsx"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("started task is missing %q", want)
		}
	}

	closed := preparedReviewPage()
	closed.StoredStatus = "closed"
	closed.Closed = true
	closed.Finalization.CanFinalize = false
	closed.Finalization.CanUpload = false
	closed.Finalization.CanApprove = false
	closed.Finalization.BlockedReason = "This task is closed; its sending data is frozen."
	doc = parseView(t, TaskPage(closed))
	if workspaceNode(doc, "id", "final-workbook") != nil || workspaceNode(doc, "id", "finalize-approve") != nil {
		t.Fatal("a closed task still offers finalization controls")
	}
	if !strings.Contains(viewText(doc), "This task is closed; its sending data is frozen.") {
		t.Fatal("closed task does not explain why finalization is unavailable")
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "finalization-edit-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("closed-task edit remedy expanded = %q, want true", got)
	}
}

// A failed run keeps its state, error and retry outside the collapsed settings,
// and the instruction editor stays inline before any completed evidence.
func TestPreparationFailureStaysVisibleOutsideHistory(t *testing.T) {
	data := preparedReviewPage()
	data.Preparation.EvidenceRunID = ""
	data.Preparation.EvidenceSelected = false
	data.Preparation.CanRetry = true
	data.Preparation.Runs = []TaskPreparationRun{
		{ID: "run-2", State: "failed", Kind: "prepare", CreatedAt: "24 Oct 2026, 10:05 AM", Model: "web-model", Error: "provider refused the request"},
	}
	data.Finalization = TaskFinalizationView{Available: true, Required: true, RoleMapping: true, CanFinalize: true, CanUpload: true}
	doc := parseView(t, TaskPage(data))
	latest := workspaceNode(doc, "id", "preparation-latest-run")
	if latest == nil || !strings.Contains(viewText(latest), "provider refused the request") {
		t.Fatal("the failed run and its error are not visible outside the run history")
	}
	if workspaceNode(doc, "id", "preparation-settings") != nil {
		t.Fatal("settings are collapsed before the first completed run")
	}
	text := viewText(doc)
	for _, want := range []string{"Retry failed preparation", "Save instructions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("first-run guidance is missing %q", want)
		}
	}
}

// Callers that pass no viewport attributes keep the component's presentation
// viewport and its single vertical scrollbar.
func TestScrollAreaKeepsDefaultViewportPresentation(t *testing.T) {
	doc := parseView(t, taskAttemptHistory(TaskPageData{}))
	viewport := workspaceNode(doc, "data-slot", "scroll-area-viewport")
	if viewport == nil {
		t.Fatal("history scroll area is missing its viewport")
	}
	if got := workspaceAttr(viewport, "role"); got != "presentation" {
		t.Fatalf("default viewport role = %q, want presentation", got)
	}
	if hasWorkspaceAttr(viewport, "aria-label") {
		t.Fatal("default viewport gained an accessible name")
	}
	if got := countViewNodes(viewport.Parent, func(n *html.Node) bool {
		return workspaceAttr(n, "data-slot") == "scroll-area-scrollbar" && workspaceAttr(n, "data-orientation") == "horizontal"
	}); got != 0 {
		t.Fatalf("default scroll area renders %d horizontal scrollbars", got)
	}
}

// The legacy preparation state keeps provisional held evidence and never labels
// it backend accepted or held.
func TestLegacyPreparationKeepsProvisionalHeldEvidence(t *testing.T) {
	data := preparedReviewPage()
	data.RoleMapping = false
	data.Finalization = TaskFinalizationView{Available: true, CanFinalize: true, CanUpload: true}
	doc := parseView(t, TaskPage(data))
	text := viewText(doc)
	for _, want := range []string{
		"provisionally accepted",
		"held by findings",
		"Prepared entries held by findings · 13",
		"Showing 1 of 13 held entries",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("legacy preparation is missing %q", want)
		}
	}
	if strings.Contains(text, "backend accepted") || strings.Contains(text, "backend held") {
		t.Fatal("legacy preparation labels provisional counts as backend outcomes")
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "preparation-held-trigger"), "aria-expanded"); got != "true" {
		t.Fatalf("legacy provisional held disclosure expanded = %q, want true", got)
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
