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
		ID: "campaign-1", Name: "Synthetic campaign", MappingDraft: "recipient | Contact Email | recipient | email",
	}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/revisions")
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
	if got, want := strings.TrimSpace(viewText(mappings)), "recipient | Contact Email | recipient | email"; got != want {
		t.Errorf("mapping draft = %q, want %q", got, want)
	}
	if workspaceAttr(mappings, "aria-describedby") != "revision-mappings-hint revision-mappings-help" {
		t.Error("mapping textarea lost its help descriptions")
	}
	assertSubmitButton(t, form, "Create revision")
}

func TestCampaignHintButtonsStayInertAndDescribed(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic campaign"}))
	for _, id := range []string{"revision-subject-help", "revision-document-help", "revision-mappings-help"} {
		button := workspaceNode(doc, "aria-describedby", id)
		if button == nil || button.Data != "button" || workspaceAttr(button, "type") != "button" {
			t.Errorf("hint %q must use a non-submitting button", id)
			continue
		}
		note := workspaceNode(doc, "id", id)
		if note == nil || workspaceAttr(note, "role") != "note" {
			t.Errorf("hint %q must describe a note with the same id", id)
		}
		if workspaceAttr(button, "hx-post") != "" || workspaceAttr(button, "hx-trigger") != "" || workspaceAttr(button, "x-on:click") != "" || workspaceAttr(button, "x-on:submit") != "" {
			t.Errorf("hint %q must not trigger a submission or action", id)
		}
	}
}

func TestTaskWorksheetSelectionNativeContract(t *testing.T) {
	doc := parseView(t, TaskPage(TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", CampaignName: "Synthetic campaign", Status: "staging",
		DefaultSheet: "Recipients", HeaderRow: 4,
		Sheets: []TaskSheetOption{{Name: "Archive"}, {Name: "Recipients", Selected: true}},
	}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/tasks/task-1/select")
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
	assertSubmitButton(t, form, "Validate task")
}

func TestCampaignDeleteConfirmationContract(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic campaign"}))
	form := workspaceNode(doc, "action", "/campaigns/campaign-1/delete#settings")
	if form == nil {
		t.Fatal("campaign delete form is missing")
	}
	confirmation := workspaceNode(form, "name", "confirm")
	if confirmation == nil || workspaceAttr(confirmation, "type") != "checkbox" || workspaceAttr(confirmation, "value") != "delete" || workspaceAttr(confirmation, "x-model") != "confirmed" {
		t.Fatal("campaign deletion must keep its explicit checkbox confirmation")
	}
	if hasViewAttr(confirmation, "checked") {
		t.Error("campaign deletion checkbox must start unchecked")
	}
	button := findViewNode(form, func(n *html.Node) bool { return n.Data == "button" && strings.Contains(viewText(n), "Delete campaign") })
	if button == nil || workspaceAttr(button, "type") != "submit" || workspaceAttr(button, "x-bind:disabled") != "!confirmed" {
		t.Error("delete submit must remain explicitly gated by the unchecked confirmation")
	}
}

func TestSendingDiagnosticTestFormContract(t *testing.T) {
	doc := parseView(t, SendingDiagnostics(SendingData{
		Base: "/campaigns/campaign-1/tasks/task-1/sending", ActionID: "test-key-", Rows: []SendingRow{{ID: "candidate-1", Number: 2, SampleEligible: true}},
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
	assertSubmitButton(t, form, "Send diagnostic test")
}

func TestSendingStartAndRecoveryConfirmations(t *testing.T) {
	base := "/campaigns/campaign-1/tasks/task-1/sending"
	controls := SendingControls(SendingData{Base: base, ActionID: "actions-", Enabled: false, Approved: true, Remaining: 2})
	doc := parseView(t, controls)
	start := workspaceNode(doc, "action", base+"/start#send")
	if start == nil || !hasViewAttr(start, "hx-preserve") {
		t.Fatal("candidate start form must preserve state across progress updates")
	}
	if checkbox := workspaceNode(start, "name", "confirmed"); checkbox == nil || workspaceAttr(checkbox, "type") != "checkbox" || workspaceAttr(checkbox, "value") != "yes" || !hasViewAttr(checkbox, "required") {
		t.Error("candidate start must require an explicit count confirmation")
	}
	startButton := findViewNode(start, func(n *html.Node) bool { return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Start" })
	if startButton == nil || workspaceAttr(startButton, "type") != "submit" || !hasViewAttr(startButton, "disabled") {
		t.Error("candidate start must be an explicit submit button disabled while release sending is closed")
	}

	for _, tc := range []struct {
		name, state, action, expectedButton string
	}{
		{name: "definite failure retry", state: "definite_failure", action: "/retry#recovery", expectedButton: "Retry"},
		{name: "uncertain outcome resend", state: "review_required", action: "/resend#recovery", expectedButton: "Send again — duplicate risk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseView(t, SendingRecovery(SendingData{Base: base, Enabled: true, Rows: []SendingRow{{ID: "candidate-1", State: tc.state, Attempt: SendingOutcome{ID: "attempt-1"}, ActionID: "row-key-"}}}))
			form := workspaceNode(doc, "action", base+tc.action)
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
