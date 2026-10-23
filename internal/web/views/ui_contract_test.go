package views

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

func TestLoginFormNativeContract(t *testing.T) {
	doc := parseView(t, LoginPage(""))
	form := workspaceNode(doc, "action", "/login")
	if form == nil || form.Data != "form" || workspaceAttr(form, "method") != "post" {
		t.Fatal("login form must post to /login")
	}

	for _, tc := range []struct {
		id, inputType, autocomplete string
	}{
		{id: "email", inputType: "email", autocomplete: "username"},
		{id: "password", inputType: "password", autocomplete: "current-password"},
	} {
		control := workspaceNode(form, "id", tc.id)
		if control == nil || control.Data != "input" {
			t.Errorf("missing %s input", tc.id)
			continue
		}
		if got := workspaceAttr(control, "type"); got != tc.inputType {
			t.Errorf("%s type = %q, want %q", tc.id, got, tc.inputType)
		}
		if got := workspaceAttr(control, "autocomplete"); got != tc.autocomplete {
			t.Errorf("%s autocomplete = %q, want %q", tc.id, got, tc.autocomplete)
		}
		if !hasViewAttr(control, "required") {
			t.Errorf("%s must be required", tc.id)
		}
		if workspaceNode(form, "for", tc.id) == nil {
			t.Errorf("%s input has no associated label", tc.id)
		}
	}
	assertSubmitButton(t, form, "Sign in")
}

func TestCampaignRevisionFormNativeContract(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{
		ID: "campaign-1", Name: "Synthetic campaign", MappingDraft: "recipient | Contact Email | recipient",
	}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/revisions#template")
	if form == nil || form.Data != "form" || workspaceAttr(form, "method") != "post" || workspaceAttr(form, "enctype") != "multipart/form-data" {
		t.Fatal("revision form must post multipart data to the campaign revision route")
	}

	file := workspaceNode(form, "id", "revision-document")
	if file == nil || file.Data != "input" || workspaceAttr(file, "type") != "file" || workspaceAttr(file, "name") != "document" {
		t.Fatal("revision upload must be a named file input")
	}
	if got, want := workspaceAttr(file, "accept"), ".docx,application/vnd.openxmlformats-officedocument.wordprocessingml.document"; got != want {
		t.Errorf("document accept = %q, want %q", got, want)
	}
	if !hasViewAttr(file, "required") {
		t.Error("document upload must be required")
	}

	subject := workspaceNode(form, "id", "revision-subject")
	if subject == nil || workspaceAttr(subject, "name") != "subject" || workspaceAttr(subject, "type") != "text" {
		t.Fatal("revision subject input is missing its native form contract")
	}
	if !hasViewAttr(subject, "required") || workspaceAttr(subject, "maxlength") != "998" {
		t.Error("revision subject must be required and capped at 998 characters")
	}
	if workspaceNode(form, "for", "revision-subject") == nil {
		t.Error("revision subject has no associated label")
	}

	mappings := workspaceNode(form, "id", "revision-mappings")
	if mappings == nil || mappings.Data != "textarea" || workspaceAttr(mappings, "name") != "mappings" {
		t.Fatal("revision mappings must remain a named textarea")
	}
	if got, want := strings.TrimSpace(viewText(mappings)), "recipient | Contact Email | recipient"; got != want {
		t.Errorf("mapping draft = %q, want %q", got, want)
	}
	if workspaceAttr(mappings, "aria-describedby") != "revision-mappings-hint revision-mappings-help" {
		t.Error("mapping textarea lost its help descriptions")
	}
	assertSubmitButton(t, form, "Create revision")
}

func TestTaskWorksheetSelectionNativeContract(t *testing.T) {
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign", StoredStatus: "staging",
		HasCurrentRevision: true, DefaultSheet: "Recipients", HeaderRow: 4,
		Sheets: []TaskSheetOption{{Name: "Archive"}, {Name: "Recipients", Selected: true}},
	}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/tasks/task-1/select#recipients")
	if form == nil || workspaceAttr(form, "method") != "post" {
		t.Fatal("worksheet selection form must post to its task route")
	}
	selectNode := workspaceNode(form, "id", "select-sheet")
	if selectNode == nil || selectNode.Data != "select" || workspaceAttr(selectNode, "name") != "sheet" {
		t.Fatal("worksheet choice must remain a native named select")
	}
	selected := workspaceNode(selectNode, "value", "Recipients")
	if selected == nil || selected.Data != "option" || !hasViewAttr(selected, "selected") {
		t.Error("the recorded worksheet must stay selected")
	}
	header := workspaceNode(form, "id", "select-header-row")
	if header == nil || header.Data != "input" || workspaceAttr(header, "name") != "header_row" || workspaceAttr(header, "type") != "number" {
		t.Fatal("header row must remain a native number input")
	}
	if workspaceAttr(header, "value") != "4" || workspaceAttr(header, "min") != "1" || !hasViewAttr(header, "required") {
		t.Error("header row must retain its selected value, minimum, and required constraint")
	}
	assertSubmitButton(t, form, "Validate workbook")
}

func TestCampaignDeleteConfirmationContract(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic campaign"}))
	form := workspaceNode(doc, "id", "campaign-delete-form")
	if form == nil || workspaceAttr(form, "method") != "post" {
		t.Fatal("campaign delete form is missing")
	}
	// The destructive dialog replaces the inline confirmation, but the
	// confirmation field and the same POST form still own the submission.
	trigger := workspaceNode(form, "aria-haspopup", "dialog")
	if trigger == nil || trigger.Data != "button" || workspaceAttr(trigger, "type") != "button" {
		t.Fatal("campaign deletion must open its confirmation dialog from a non-submitting trigger")
	}
	dialog := workspaceNode(form, "role", "alertdialog")
	if dialog == nil {
		t.Fatal("campaign deletion must render an alert dialog")
	}
	confirmation := workspaceNode(dialog, "name", "confirm")
	if confirmation == nil || workspaceAttr(confirmation, "type") != "checkbox" || workspaceAttr(confirmation, "value") != "delete" || workspaceAttr(confirmation, "form") != "campaign-delete-form" {
		t.Fatal("campaign deletion must keep its explicit confirmation field in the same POST form")
	}
	if !hasViewAttr(confirmation, "required") {
		t.Error("the confirmation field must stay required")
	}
	if hasViewAttr(confirmation, "checked") {
		t.Error("campaign deletion checkbox must start unchecked")
	}
	confirm := findViewNode(dialog, func(n *html.Node) bool {
		return n.Data == "button" && strings.Contains(viewText(n), "Yes, delete campaign")
	})
	if confirm == nil || workspaceAttr(confirm, "type") != "submit" || workspaceAttr(confirm, "form") != "campaign-delete-form" {
		t.Error("the dialog confirm control must submit the same POST form")
	}
	cancel := findViewNode(dialog, func(n *html.Node) bool { return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Cancel" })
	if cancel == nil || workspaceAttr(cancel, "type") != "button" || workspaceAttr(cancel, "data-templ-dialog-close") != "campaign-delete-dialog" {
		t.Error("cancel must close the dialog without submitting")
	}
}

func TestSendingDiagnosticTestFormContract(t *testing.T) {
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", Section: TaskSectionTest, ActionID: "test-key-",
		SavedRevision: 2, HasSavedRevision: true,
		Rows: []TaskAdmittedRow{{ID: "candidate-1", Row: 2, State: "eligible"}},
	}))
	form := workspaceNode(doc, "id", "sending-diagnostic-test")
	if form == nil || form.Data != "form" || workspaceAttr(form, "method") != "post" || workspaceAttr(form, "action") != "/campaigns/campaign-1/tasks/task-1/sending/test#test" || !hasViewAttr(form, "hx-preserve") {
		t.Fatal("diagnostic test form must retain its POST route and polling preservation")
	}
	selectNode := workspaceNode(form, "id", "sending-sample-row")
	if selectNode == nil || workspaceAttr(selectNode, "name") != "member_id" || !hasViewAttr(selectNode, "required") {
		t.Error("diagnostic test must require a sample row")
	}
	if workspaceNode(selectNode, "value", "candidate-1") == nil {
		t.Error("eligible sample row is missing from the native select")
	}
	address := workspaceNode(form, "id", "sending-test-address")
	if address == nil || workspaceAttr(address, "type") != "email" || workspaceAttr(address, "name") != "test_address" || !hasViewAttr(address, "required") {
		t.Error("diagnostic test must require an email recipient")
	}
	assertSubmitButton(t, form, "Send formatting test")
}

func TestCandidateControlsRequireTestApprovalWithoutReleaseGate(t *testing.T) {
	for _, approved := range []bool{false, true} {
		data := TaskPageData{
			CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
			StoredStatus: "ready", Section: TaskSectionSending, Remaining: 2, Approved: approved,
			Controls: TaskControls{Start: true}, ActionID: "actions-", SendingType: TaskSendingTypeDirect,
		}
		doc := parseView(t, TaskPage(data))
		if strings.Contains(viewText(doc), "release readiness") || strings.Contains(viewText(doc), "production release verification") {
			t.Fatal("obsolete release warning is still shown")
		}
		button := findViewNode(doc, func(n *html.Node) bool { return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Send now" })
		if button == nil || hasViewAttr(button, "disabled") == approved {
			t.Errorf("Send now with approved=%t: expected disabled=%t", approved, !approved)
		}
		data.SendingType, data.Controls = TaskSendingTypeScheduled, TaskControls{Schedule: true}
		doc = parseView(t, TaskPage(data))
		button = findViewNode(doc, func(n *html.Node) bool {
			return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Schedule start"
		})
		if button == nil || !hasViewAttr(button, "disabled") {
			t.Errorf("Schedule start with approved=%t: expected disabled until its confirmation is checked", approved)
		}
		bind := workspaceAttr(button, "x-bind:disabled")
		if !strings.Contains(bind, "!confirmed") || !strings.Contains(bind, "!eligible") {
			t.Errorf("Schedule start with approved=%t: confirmation/eligibility binding = %q", approved, bind)
		}
		form := button.Parent
		for form != nil && form.Data != "form" {
			form = form.Parent
		}
		wantEligible := map[bool]string{true: "eligible: true", false: "eligible: false"}[approved && data.Remaining > 0]
		if form == nil || !strings.Contains(workspaceAttr(form, "x-data"), wantEligible) {
			t.Errorf("Schedule start with approved=%t: x-data missing %q", approved, wantEligible)
		}
	}
}

func TestSendingStartAndRecoveryConfirmations(t *testing.T) {
	base := "/campaigns/campaign-1/tasks/task-1/sending"
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign",
		StoredStatus: "ready", SendingType: TaskSendingTypeDirect, Section: TaskSectionSending, ActionID: "actions-", Approved: false,
		Remaining: 2, Controls: TaskControls{Start: true},
	}))
	start := workspaceNode(doc, "action", base+"/start#sending")
	if start == nil || !hasViewAttr(start, "hx-preserve") {
		t.Fatal("candidate start form must preserve state across progress updates")
	}
	if checkbox := workspaceNode(start, "name", "confirmed"); checkbox == nil || workspaceAttr(checkbox, "type") != "checkbox" || workspaceAttr(checkbox, "value") != "yes" || !hasViewAttr(checkbox, "required") {
		t.Error("candidate start must require an explicit count confirmation")
	}
	startButton := findViewNode(start, func(n *html.Node) bool { return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Send now" })
	if startButton == nil || workspaceAttr(startButton, "type") != "submit" || !hasViewAttr(startButton, "disabled") {
		t.Error("candidate start must be an explicit submit button disabled until test formatting is approved")
	}

	for _, tc := range []struct {
		name, state, action, expectedButton string
	}{
		{name: "definite failure retry", state: "definite_failure", action: "/retry#sending", expectedButton: "Retry"},
		{name: "uncertain outcome resend", state: "review_required", action: "/resend#sending", expectedButton: "Send again — duplicate risk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderWorkspace(t, taskRecovery(TaskPageData{
				CampaignID: "campaign-1", TaskID: "task-1", StoredStatus: "stopped", ActionID: "row-key-",
				Approved: true, Held: 1, Controls: TaskControls{RetryRow: true, ResendRow: true, ResolveRow: true},
				Rows: []TaskAdmittedRow{{ID: "candidate-1", Row: 2, State: tc.state}},
			}))
			doc, err := html.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			form := findViewNode(doc, func(n *html.Node) bool {
				if n.Data != "form" || workspaceAttr(n, "action") != base+tc.action {
					return false
				}
				count := workspaceNode(n, "name", "expected_count")
				return count != nil && workspaceAttr(count, "value") == "1"
			})
			if form == nil || !hasViewAttr(form, "hx-preserve") {
				t.Fatal("recovery form must preserve state across progress updates")
			}
			confirm := workspaceNode(form, "name", "confirmed")
			if confirm == nil || workspaceAttr(confirm, "type") != "checkbox" || !hasViewAttr(confirm, "required") {
				t.Error("recovery action must require an explicit confirmation checkbox")
			}
			if tc.state == "review_required" {
				duplicateRisk := workspaceNode(form, "name", "duplicate_risk")
				if duplicateRisk == nil || !hasViewAttr(duplicateRisk, "required") {
					t.Error("uncertain resend must require the duplicate-risk acknowledgement")
				}
			}
			button := findViewNode(form, func(n *html.Node) bool {
				return n.Data == "button" && strings.TrimSpace(viewText(n)) == tc.expectedButton
			})
			if button == nil || workspaceAttr(button, "type") != "submit" {
				t.Errorf("recovery action %q must use an explicit submit button", tc.expectedButton)
			}
			if workspaceAttr(form, "hx-trigger") != "" || workspaceAttr(form, "hx-post") != "" || workspaceAttr(form, "x-on:submit") != "" {
				t.Error("recovery action must not auto-submit")
			}
		})
	}
}

// The recovery table selects rows client-side but submits one native request
// per bulk action, so a stale selection can only be refused, never partially
// applied.
func TestRecoveryTableBulkSelectionContract(t *testing.T) {
	base := "/campaigns/campaign-1/tasks/task-1/sending"
	page := renderWorkspace(t, taskRecovery(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", StoredStatus: "stopped", ActionID: "row-key-",
		Approved: true, Held: 1, Controls: TaskControls{RetryRow: true, ResendRow: true, ResolveRow: true},
		Rows: []TaskAdmittedRow{
			{ID: "candidate-1", Row: 2, State: "definite_failure", Reason: "Titan rejected the message"},
			{ID: "candidate-2", Row: 3, State: "review_required", Reason: "lost response"},
		},
	}))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	rows := workspaceNode(doc, "aria-label", "Recovery rows")
	if rows == nil || rows.Data != "table" {
		t.Fatal("recovery rows must render in a labelled table")
	}
	for _, model := range []string{"retry", "review"} {
		selectable := findViewNode(rows, func(n *html.Node) bool {
			return n.Data == "input" && workspaceAttr(n, "type") == "checkbox" && workspaceAttr(n, "x-model") == model && workspaceAttr(n, "value") != ""
		})
		if selectable == nil {
			t.Errorf("recovery row for %q must expose a named selection checkbox", model)
		}
	}
	for _, tc := range []struct{ action, submit string }{
		{action: "/retry#sending", submit: "Retry selected"},
		{action: "/resend#sending", submit: "Send again selected"},
		{action: "/resolve#sending", submit: "Mark selected already submitted"},
	} {
		form := workspaceNode(doc, "action", base+tc.action)
		if form == nil || !hasViewAttr(form, "hx-preserve") {
			t.Fatalf("bulk %s form must preserve state across progress updates", tc.action)
		}
		if workspaceAttr(workspaceNode(form, "name", "expected_count"), "x-bind:value") == "" {
			t.Errorf("bulk %s form must confirm the live selected count", tc.action)
		}
		if findViewNode(form, func(n *html.Node) bool {
			return n.Data == "template" && workspaceAttr(n, "x-for") != ""
		}) == nil {
			t.Errorf("bulk %s form must submit the selected member ids", tc.action)
		}
		if confirm := workspaceNode(form, "name", "confirmed"); confirm == nil || !hasViewAttr(confirm, "required") {
			t.Errorf("bulk %s form must require an explicit confirmation", tc.action)
		}
		button := findViewNode(form, func(n *html.Node) bool {
			return n.Data == "button" && strings.TrimSpace(viewText(n)) == tc.submit
		})
		if button == nil || workspaceAttr(button, "type") != "submit" {
			t.Errorf("bulk action %q must use an explicit submit button", tc.submit)
		}
	}
}

// A recovery row whose action is gated off must say why: an empty action cell
// reads as a missing feature rather than a withheld operation.
func TestRecoveryRowsExplainUnavailableActions(t *testing.T) {
	base := TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", ActionID: "row-key-", Approved: true,
		Rows: []TaskAdmittedRow{{ID: "candidate-1", Row: 2, State: "definite_failure", Reason: "Titan rejected"}},
	}
	for _, tc := range []struct {
		name       string
		status     string
		restore    bool
		want       string
		wantAction bool
	}{
		{name: "active work", status: "running", want: "Available after the active sending work settles."},
		{name: "restore review", status: "paused", restore: true, want: "Restore review is active"},
		{name: "row no longer retryable", status: "paused", want: "no longer retryable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := base
			data.StoredStatus = tc.status
			data.RestoreReview = tc.restore
			page := renderWorkspace(t, taskRecovery(data))
			if !strings.Contains(page, tc.want) {
				t.Fatalf("recovery hint missing %q", tc.want)
			}
			if strings.Contains(page, `data-slot="dialog-trigger"`) {
				t.Error("gated recovery row must not render an action")
			}
		})
	}
	// A settled task with a retryable failure shows the action and no hint.
	data := base
	data.StoredStatus = "paused"
	data.Controls = TaskControls{RetryRow: true}
	page := renderWorkspace(t, taskRecovery(data))
	if !strings.Contains(page, `data-slot="dialog-trigger"`) {
		t.Fatal("settled retryable row must render its action")
	}
	if strings.Contains(page, "no longer retryable") || strings.Contains(page, "active sending work settles") {
		t.Error("available action must not carry an unavailable hint")
	}
}

// Every card is a labelled landmark and every labelled control resolves its
// label, across all pages, so the primitive migration kept the structure the
// operator navigates by.
func TestPagesKeepLabelledRegionsAndControls(t *testing.T) {
	pages := map[string]templ.Component{
		"login":                   LoginPage(""),
		"overview":                Home(),
		"connection":              ConnectionPage(ConnectionData{Status: "connected", SenderAddress: "hr@example.test"}),
		"campaign list":           CampaignList(CampaignListData{}),
		"campaign form":           CampaignForm(CampaignFormData{}),
		"campaign page":           CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic", HasRevision: true, Variables: []PreviewVariable{{Name: "recipient", Required: true}}}),
		"campaign tasks":          CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic", HasRevision: true, Section: CampaignSectionTasks, Tasks: []CampaignTaskRow{{ID: "task-1", Filename: "recipients.xlsx", Reference: "abcd1234", Status: "Ready to send", Recipients: "1 eligible", RecipientsDetail: []string{"Input rows: 1", "Submitted: 0", "Need attention: 0"}, NextLabel: "Choose sending time", NextSection: TaskSectionSending}}}),
		"task upload":             TaskUpload(TaskUploadData{CampaignID: "campaign-1", CampaignName: "Synthetic", HasRevision: true, MappedColumns: []string{"Candidate Email"}}),
		"task staging":            TaskPage(TaskPageData{CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic", Filename: "recipients.xlsx", Reference: "abcd1234", StoredStatus: "staging", HasCurrentRevision: true, DefaultSheet: "People", Sheets: []TaskSheetOption{{Name: "People", Selected: true}}, HeaderRow: 1}),
		"task recipients":         TaskPage(TaskPageData{CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic", Filename: "recipients.xlsx", Reference: "abcd1234", StoredStatus: "ready", HasSavedRevision: true, SavedRevision: 1, Hash: "sha256:abc", ActionID: "actions-", Rows: []TaskAdmittedRow{{ID: "member", Row: 2, State: "eligible"}}}),
		"task sending":            TaskPage(TaskPageData{CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic", Filename: "recipients.xlsx", Reference: "abcd1234", StoredStatus: "ready", SendingType: TaskSendingTypeDirect, Section: TaskSectionSending, ActionID: "actions-", Remaining: 2, Controls: TaskControls{Start: true}, Rows: []TaskAdmittedRow{{ID: "member", Row: 2, State: "review_required"}}}),
		"task results":            TaskPage(TaskPageData{CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic", Filename: "recipients.xlsx", Reference: "abcd1234", StoredStatus: "completed", Section: TaskSectionResults, ResultsReady: true, ResultsAsOf: "22 Oct 2026, 11:12 AM +05:30", ResultEligible: true}),
		"sending fragment":        TaskSendingLive(TaskPageData{CampaignID: "campaign-1", TaskID: "task-1", StoredStatus: "ready", Approved: true, Remaining: 2, Controls: TaskControls{Start: true, Schedule: true}, ActionID: "actions-"}),
		"campaign list with rows": CampaignList(CampaignListData{Campaigns: []CampaignListRow{{ID: "campaign-1", Name: "Synthetic", HasRevision: true, Approved: true, TaskCount: 2, AttentionCount: 1}}}),
	}
	for name, component := range pages {
		t.Run(name, func(t *testing.T) {
			doc := parseView(t, component)
			ids := map[string]bool{}
			var collect func(*html.Node)
			collect = func(n *html.Node) {
				if id := workspaceAttr(n, "id"); id != "" {
					ids[id] = true
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					collect(child)
				}
			}
			collect(doc)
			var inspect func(*html.Node)
			inspect = func(n *html.Node) {
				if workspaceAttr(n, "role") == "region" {
					labelledBy := workspaceAttr(n, "aria-labelledby")
					if labelledBy == "" && workspaceAttr(n, "aria-label") == "" {
						t.Errorf("region %q has no accessible name", workspaceAttr(n, "data-slot"))
					}
					for id := range strings.FieldsSeq(labelledBy) {
						if !ids[id] {
							t.Errorf("region is labelled by missing id %q", id)
						}
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					inspect(child)
				}
			}
			inspect(doc)
			// A control with a for/id label pair keeps its association.
			var labels func(*html.Node)
			labels = func(n *html.Node) {
				if n.Data == "label" {
					if target := workspaceAttr(n, "for"); target != "" && !ids[target] {
						t.Errorf("label points at missing control %q", target)
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					labels(child)
				}
			}
			labels(doc)
		})
	}
}

func parseView(t *testing.T, component templ.Component) *html.Node {
	t.Helper()
	page := renderWorkspace(t, component)
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func hasViewAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func findViewNode(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n != nil && match(n) {
		return n
	}
	if n != nil {
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if found := findViewNode(child, match); found != nil {
				return found
			}
		}
	}
	return nil
}

func viewText(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(viewText(child))
	}
	return text.String()
}

func assertSubmitButton(t *testing.T, form *html.Node, label string) {
	t.Helper()
	button := findViewNode(form, func(n *html.Node) bool {
		return n.Data == "button" && strings.TrimSpace(viewText(n)) == label
	})
	if button == nil || workspaceAttr(button, "type") != "submit" {
		t.Errorf("%q must be an explicit submit button", label)
	}
}

func TestTaskUploadSendingTypeSelection(t *testing.T) {
	for _, mode := range []string{TaskSendingTypeDirect, TaskSendingTypeScheduled} {
		t.Run(mode, func(t *testing.T) {
			doc := parseView(t, TaskUpload(TaskUploadData{CampaignID: "campaign-1", CampaignName: "Synthetic", SendingType: mode}))
			control := workspaceNode(doc, "id", "task-sending-type")
			if control == nil || control.Data != "select" || workspaceAttr(control, "name") != "sending_type" || !hasViewAttr(control, "required") {
				t.Fatal("upload must offer a required native sending type selection")
			}
			selected := findViewNode(control, func(node *html.Node) bool {
				return node.Type == html.ElementNode && node.Data == "option" && hasViewAttr(node, "selected")
			})
			if selected == nil || workspaceAttr(selected, "value") != mode {
				t.Fatal("upload error must retain the selected sending type")
			}
		})
	}
}
