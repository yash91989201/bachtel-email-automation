package views

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Template & preview shows the current configuration, mapping guidance, the
// explicit revision form, the sample preview and the revision history in that
// order, with the later-revision form closed until the operator chooses it.
func TestCampaignTemplateSectionOrderAndGuidance(t *testing.T) {
	data := CampaignPageData{
		ID:              "camp1",
		Name:            "Test campaign",
		SourceHeaders:   "Candidate Name, Candidate Email",
		ExampleVariable: "candidate_name",
		ExampleHeader:   "Candidate Name",
		MappingDraft:    "candidate_name | Candidate Name | required",
		HasRevision:     true,
		Revision:        2,
		Subject:         "Hello {{candidate_name}}",
		Approved:        true,
		Variables:       []PreviewVariable{{Name: "candidate_name", Required: true}},
		Section:         CampaignSectionTemplate,
	}
	doc := parseView(t, CampaignPage(data))

	current := findCard(doc, "current-revision-heading")
	guidance := findCard(doc, "mapping-reference-heading")
	form := findCard(doc, "upload-heading")
	preview := findCard(doc, "preview-heading")
	history := findCard(doc, "revision-history-heading")
	for name, node := range map[string]*html.Node{
		"current configuration": current,
		"mapping guidance":      guidance,
		"revision form":         form,
		"sample preview":        preview,
		"revision history":      history,
	} {
		if node == nil {
			t.Fatalf("the campaign page has no %s section", name)
		}
	}
	if !before(doc, current, guidance) || !before(doc, guidance, form) || !before(doc, form, preview) || !before(doc, preview, history) {
		t.Fatal("template sections are out of the prescribed order")
	}

	// A later revision is explicit: the form's disclosure starts closed; a
	// campaign without a revision opens it.
	if got := workspaceAttr(form, "data-slot"); got != "accordion-item" {
		t.Fatalf("the revision form is a %q, want an explicit disclosure", got)
	}
	if !hasWorkspaceAttr(form, "data-closed") {
		t.Error("the later-revision form must not be open by default")
	}
	draft := parseView(t, CampaignPage(CampaignPageData{ID: "camp1", Name: "Draft", MappingDraft: data.MappingDraft, Section: CampaignSectionTemplate}))
	openForm := findCard(draft, "upload-heading")
	if !hasWorkspaceAttr(openForm, "data-open") {
		t.Error("the first-revision form must open by default")
	}
	if got := textOf(workspaceNode(draft, "id", "revision-mappings")); !strings.Contains(got, "candidate_name | Candidate Name | required") {
		t.Errorf("first-revision draft = %q, want the derived mapping draft", got)
	}
	// A later revision starts empty: only an operator's rejected draft is kept.
	later := parseView(t, CampaignPage(CampaignPageData{ID: "camp2", Name: "Later", HasRevision: true, Section: CampaignSectionTemplate}))
	if got := textOf(workspaceNode(later, "id", "revision-mappings")); got != "" {
		t.Errorf("later revision form carried %q, want an empty form", got)
	}
	rejected := parseView(t, CampaignPage(CampaignPageData{ID: "camp1", Name: "Edit", HasRevision: true, MappingDraft: "bad draft", Section: CampaignSectionTemplate}))
	if got := textOf(workspaceNode(rejected, "id", "revision-mappings")); got != "bad draft" {
		t.Errorf("rejected mapping draft = %q, want the supplied text preserved", got)
	}

	// Guidance stays outside placeholders and carries the worked example.
	guidanceText := textOf(guidance)
	for _, want := range []string{
		"variable | source header | role | fallback",
		"Mark exactly one row as recipient",
		"candidate_name | Candidate Name | required",
		"{{candidate_name}}",
		"Rendered value",
	} {
		if !strings.Contains(guidanceText, want) {
			t.Errorf("mapping guidance missing %q", want)
		}
	}
	for _, id := range []string{"revision-mappings-hint", "revision-mappings-help"} {
		if workspaceNode(doc, "id", id) == nil {
			t.Errorf("mapping help %q is missing", id)
		}
	}
	if got := workspaceAttr(workspaceNode(doc, "id", "revision-mappings"), "aria-describedby"); got != "revision-mappings-hint revision-mappings-help" {
		t.Errorf("mapping textarea described by %q", got)
	}
	if !strings.Contains(textOf(workspaceNode(doc, "id", "revision-document-help")), "selected again") {
		t.Error("DOCX reselection guidance is missing")
	}
}

func TestCampaignHintButtonsStayInertAndDescribed(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{ID: "campaign-1", Name: "Synthetic campaign", Section: CampaignSectionTemplate}))
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

// The Tasks section identifies each workbook, shows only the derived status
// and the eligible count, and offers the next action the workspace rule chose.
// Reference, upload time, saved revision, status explanation and the remaining
// recipient counts stay behind each cell's tooltip.
func TestCampaignTasksSectionShowsOperationalContext(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{
		ID: "camp1", Name: "Hiring", HasRevision: true, Revision: 2, Approved: true,
		Tasks: []CampaignTaskRow{
			{ID: "task-1", Filename: "recipients.xlsx", Reference: "abcd1234", UploadedAt: "22 Oct 2026, 11:12 AM +05:30", HasSavedRevision: true, SavedRevision: 2, Status: "Ready to send", StatusClass: "status-success", StatusDetail: "Validated and approved.", Recipients: "2 eligible", RecipientsDetail: []string{"Input rows: 3", "Submitted: 0", "Need attention: 1"}, NextLabel: "Choose sending time", NextSection: TaskSectionSending},
			{ID: "task-2", Filename: "recipients.xlsx", Reference: "efgh5678", UploadedAt: "22 Oct 2026, 12:40 PM +05:30", Status: "Needs worksheet selection", StatusClass: "status-warning", Recipients: "Not validated", NextLabel: "Validate workbook", NextSection: TaskSectionRecipients},
		},
	}))
	text := textOf(doc)
	for _, want := range []string{
		"recipients.xlsx",
		"Reference abcd1234",
		"Reference efgh5678",
		"Uploaded 22 Oct 2026, 11:12 AM +05:30",
		"Saved revision 2",
		"Ready to send",
		"Validated and approved.",
		"Needs worksheet selection",
		"2 eligible",
		"Input rows: 3",
		"Need attention: 1",
		"Not validated",
		"Choose sending time",
		"Validate workbook",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("tasks section is missing %q", want)
		}
	}
	for _, unwanted := range []string{"Header row", "Worksheet"} {
		if workspaceNode(doc, "data-slot", "table") != nil && strings.Contains(text, unwanted) {
			if columnsContain(doc, unwanted) {
				t.Errorf("the task list must not keep a %q column; keep it inside the task", unwanted)
			}
		}
	}
	// The workbook name is the task's identity: it opens the task at its
	// Overview, while only the next action deep-links into a section.
	if link := workspaceNode(doc, "href", "/campaigns/camp1/tasks/task-1"); link == nil || workspaceAttr(link, "href") == "/campaigns/camp1/tasks/task-1/sending" {
		t.Error("the workbook name does not open the task overview")
	}
	if link := workspaceNode(doc, "href", "/campaigns/camp1/tasks/task-1/sending"); link == nil {
		t.Error("the next action does not link to the task's Sending section")
	}
	if link := workspaceNode(doc, "href", "/campaigns/camp1/tasks/task-2/recipients"); link == nil {
		t.Error("the staging row does not link to Recipients")
	}
	// Each tooltip trigger describes content that exists in the page.
	for _, id := range []string{"task-workbook-detail-task-1", "task-status-detail-task-1", "task-recipients-detail-task-1"} {
		trigger := workspaceNode(doc, "aria-describedby", id)
		if trigger == nil {
			t.Errorf("tooltip %q has no trigger", id)
			continue
		}
		if note := workspaceNode(doc, "id", id); note == nil || workspaceAttr(note, "role") != "tooltip" {
			t.Errorf("tooltip %q has no describing content", id)
		}
	}
}

// columnsContain reports whether a table header cell contains the label.
func columnsContain(doc *html.Node, label string) bool {
	var found bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Data == "th" && strings.TrimSpace(viewText(n)) == label {
			found = true
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

func TestCampaignTasksEmptyAndNoTemplateStates(t *testing.T) {
	empty := parseView(t, CampaignPage(CampaignPageData{ID: "camp1", Name: "Hiring", HasRevision: true}))
	text := textOf(empty)
	for _, want := range []string{"No tasks yet", "Each workbook creates a separate task", "+ New Task"} {
		if !strings.Contains(text, want) {
			t.Errorf("empty Tasks section is missing %q", want)
		}
	}
	noTemplate := parseView(t, CampaignPage(CampaignPageData{ID: "camp1", Name: "Hiring"}))
	text = textOf(noTemplate)
	for _, want := range []string{"Add an email template to validate recipient workbooks", "Add template", "You can upload a workbook now"} {
		if !strings.Contains(text, want) {
			t.Errorf("no-template Tasks section is missing %q", want)
		}
	}
	if workspaceNode(noTemplate, "href", "/campaigns/camp1/template") == nil {
		t.Error("the no-template guidance does not link to Template & preview")
	}
}

func TestCampaignActivityAndSettingsHaveDistinctPurposes(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{
		ID: "camp1", Name: "Hiring", HasRevision: true, Revision: 1, Section: CampaignSectionActivity,
		Attempts: []AttemptRow{{ID: "attempt-1", Kind: "candidate", Recipient: "a@example.test", State: "submitted", FinishedAt: "22 Oct 2026, 11:12 AM +05:30"}},
	}))
	activity := workspaceNode(doc, "id", "campaign-activity")
	if activity == nil {
		t.Fatal("activity section is missing")
	}
	activityText := textOf(activity)
	for _, want := range []string{"Activity & downloads", "Campaign report", "Campaign package", "Tests stay separate from candidate totals"} {
		if !strings.Contains(activityText, want) {
			t.Errorf("activity section is missing %q", want)
		}
	}
	if strings.Contains(activityText, "Revision history") {
		t.Error("revision history belongs to Template & preview, not activity")
	}
	doc = parseView(t, CampaignPage(CampaignPageData{
		ID: "camp1", Name: "Hiring", HasRevision: true, Revision: 1, Section: CampaignSectionSettings,
	}))
	settings := workspaceNode(doc, "id", "campaign-settings-workspace")
	if settings == nil {
		t.Fatal("settings section is missing")
	}
	settingsText := textOf(settings)
	for _, want := range []string{"Result column labels", "Delete campaign & all history", "duplicate-prevention history"} {
		if !strings.Contains(settingsText, want) {
			t.Errorf("settings section is missing %q", want)
		}
	}
	if strings.Contains(settingsText, "Campaign report") {
		t.Error("campaign downloads belong to activity, not settings")
	}
}

// The sample preview shows its result only after the operator renders one, and
// both actions stay disabled while a required sample value is missing.
func TestCampaignSamplePreviewGatesOnRequiredValues(t *testing.T) {
	for _, tc := range []struct {
		name         string
		variables    []PreviewVariable
		wantDisabled bool
	}{
		{name: "required values start empty", variables: []PreviewVariable{{Name: "recipient", Required: true}, {Name: "first_name", Required: true}}, wantDisabled: true},
		{name: "optional values only", variables: []PreviewVariable{{Name: "nickname"}}, wantDisabled: false},
		{name: "no variables", wantDisabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseView(t, CampaignPage(CampaignPageData{
				ID: "camp1", Name: "Test campaign", HasRevision: true, Revision: 2,
				AIReady: true, AIEndpoint: "https://ai.example.test/v1", AIModel: "exact-model",
				PromptVersion: 1, Prompt: "Normalize the supplied mapped values.",
				Variables: tc.variables, Section: CampaignSectionTemplate,
			}))
			scope := workspaceNode(doc, "x-data", sampleFormState)
			if scope == nil || workspaceAttr(scope, "x-init") != "sync()" {
				t.Fatal("the sample preview lost its Alpine state scope")
			}
			form := workspaceNode(scope, "action", "/campaigns/camp1/preview")
			if form == nil || form.Data != "form" {
				t.Fatal("the campaign page has no sample preview form")
			}
			if workspaceAttr(form, "x-on:submit") != "submit($event)" || workspaceAttr(form, "x-on:input") != "sync(); invalidate()" {
				t.Errorf("preview form lost its submit/gate wiring: submit=%q input=%q", workspaceAttr(form, "x-on:submit"), workspaceAttr(form, "x-on:input"))
			}
			revision := workspaceNode(form, "name", "revision")
			if revision == nil || workspaceAttr(revision, "value") != "2" {
				t.Fatalf("the form does not pin the revision identity it was filled for: %+v", revision)
			}
			result := workspaceNode(scope, "id", "preview-result")
			if result == nil {
				t.Fatal("preview result region is missing")
			}
			if result.FirstChild != nil {
				t.Errorf("preview result must start empty, got %q", viewText(result))
			}
			for _, variable := range tc.variables {
				sample := workspaceNode(form, "id", "sample-"+variable.Name)
				if sample == nil || sample.Data != "input" || workspaceAttr(sample, "name") != variable.Name {
					t.Fatalf("missing sample input for %s", variable.Name)
				}
				if got := hasViewAttr(sample, "required"); got != variable.Required {
					t.Errorf("sample %s required=%t, want %t", variable.Name, got, variable.Required)
				}
				fieldLabel := workspaceNode(form, "for", "sample-"+variable.Name)
				if fieldLabel == nil {
					t.Fatalf("sample input for %s has no label", variable.Name)
				}
				if label := viewText(fieldLabel); !strings.Contains(label, "Sample value for "+variable.Name) {
					t.Errorf("label %q does not name the variable", label)
				}
			}
			for _, action := range []struct{ label, formaction string }{
				{label: "Render preview", formaction: ""},
				{label: "Normalize with AI & preview", formaction: "/campaigns/camp1/preview/normalize"},
			} {
				button := findViewNode(form, func(n *html.Node) bool {
					return n.Data == "button" && strings.TrimSpace(viewText(n)) == action.label
				})
				if button == nil || workspaceAttr(button, "type") != "submit" {
					t.Fatalf("missing the %q submit button", action.label)
				}
				if got := workspaceAttr(button, "formaction"); got != action.formaction {
					t.Errorf("%q formaction = %q, want %q", action.label, got, action.formaction)
				}
				if got := workspaceAttr(button, "x-bind:disabled"); got != "busy || !ready" {
					t.Errorf("%q x-bind:disabled = %q", action.label, got)
				}
				if got := hasViewAttr(button, "disabled"); got != tc.wantDisabled {
					t.Errorf("%q disabled=%t, want %t", action.label, got, tc.wantDisabled)
				}
			}
			// The AI action discloses its destination and exposes the policy.
			text := viewText(doc)
			for _, want := range []string{"https://ai.example.test/v1", "exact-model", "version 1", "Normalize the supplied mapped values."} {
				if !strings.Contains(text, want) {
					t.Errorf("sample disclosure is missing %q", want)
				}
			}
		})
	}
}

// Without a confirmed AI connection the AI action is absent and the remedy
// points at Account settings, while direct rendering stays usable.
func TestCampaignSamplePreviewOffersAISetupRemedy(t *testing.T) {
	doc := parseView(t, CampaignPage(CampaignPageData{
		ID: "camp1", Name: "Test campaign", HasRevision: true, Revision: 1,
		Variables: []PreviewVariable{{Name: "nickname"}}, Section: CampaignSectionTemplate,
	}))
	if findViewNode(doc, func(n *html.Node) bool { return n.Data == "a" && workspaceAttr(n, "href") == "/account/ai" }) == nil {
		t.Fatal("an unconfigured campaign sample form must offer the Account settings remedy")
	}
	if findViewNode(doc, func(n *html.Node) bool {
		return n.Data == "button" && workspaceAttr(n, "formaction") == "/campaigns/camp1/preview/normalize"
	}) != nil {
		t.Fatal("the AI sample action must be absent without a confirmed connection")
	}
	if findViewNode(doc, func(n *html.Node) bool {
		return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Render preview"
	}) == nil {
		t.Fatal("direct rendering must stay usable without AI")
	}
}

// The rendered preview is dismissible from the same page, without a reload.
func TestPreviewResultCanBeClosed(t *testing.T) {
	doc := parseView(t, PreviewResult(PreviewData{
		HasRevision: true, Revision: 3, Recipient: "candidate@example.com", Subject: "Hello", Body: "<p>Hi there</p>",
	}))
	card := workspaceNode(doc, "x-data", "{ open: true }")
	if card == nil || workspaceAttr(card, "x-show") != "open" {
		t.Fatal("the preview result card must own the state that dismisses it")
	}
	closeButton := findViewNode(card, func(n *html.Node) bool {
		return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Close"
	})
	if closeButton == nil || workspaceAttr(closeButton, "type") != "button" {
		t.Fatal("the preview result card has no non-submitting close button")
	}
	if got, want := workspaceAttr(closeButton, "x-on:click"), "open = false"; got != want {
		t.Errorf("close click = %q, want %q", got, want)
	}
	if findViewNode(card, func(n *html.Node) bool {
		return strings.Contains(workspaceAttr(n, "class"), "rounded-card border border-border p-4")
	}) == nil {
		t.Error("the rendered body sits outside the dismissible card")
	}
	for _, want := range []string{"Preview result", "candidate@example.com", "Hello", "Hi there"} {
		if !strings.Contains(viewText(card), want) {
			t.Errorf("preview card is missing %q", want)
		}
	}
}

// An AI-prepared sample identifies its provenance, labels the result as
// illustrative, and separates corrections from unresolved findings.
func TestPreviewResultShowsAIProvenanceAndReview(t *testing.T) {
	doc := parseView(t, PreviewResult(PreviewData{
		HasRevision: true, Revision: 4, ContentHash: "hash-4",
		AIPrepared: true, Provider: "Custom compatible endpoint",
		Endpoint: "https://ai.example.test/v1", Model: "exact-model", PromptVersion: 2,
		NeedsReview: true, Findings: []string{"Department (address 1): ambiguous value"},
		Corrections: []string{"Applicant Full Name: restored spacing"},
		Recipient:   "ada@example.com", Subject: "Hi Ada", Body: "<p>Hello</p>",
		Values: []PreviewValue{{
			Variable: "candidate_name", Header: "Applicant Full Name",
			Raw: "ada   lovelace ", Prepared: "Ada Lovelace", Normalized: "Ada Lovelace",
		}},
	}))
	text := viewText(doc)
	for _, want := range []string{
		"AI-prepared sample",
		"Custom compatible endpoint",
		"https://ai.example.test/v1",
		"exact-model",
		"version 2",
		"Illustrative only",
		"Needs review",
		"Department (address 1): ambiguous value",
		"Corrections applied",
		"Applicant Full Name: restored spacing",
		"Original",
		"AI-prepared",
		"Effective",
		"ada   lovelace",
		"Ada Lovelace",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("AI sample result is missing %q", want)
		}
	}
	if strings.Contains(text, "Preview result") {
		t.Error("an AI-prepared sample must not present itself as a plain preview result")
	}
}

// A refused render keeps the validated comparison visible and never claims a
// successful email preview.
func TestPreviewResultKeepsAIComparisonWhenRenderingRefuses(t *testing.T) {
	doc := parseView(t, PreviewResult(PreviewData{
		AIPrepared: true, NeedsReview: false,
		Error: "missing required value(s): candidate_name",
		Values: []PreviewValue{{
			Variable: "candidate_name", Header: "Applicant Full Name",
			Raw: "", Prepared: "",
		}},
	}))
	text := viewText(doc)
	if !strings.Contains(text, "missing required value(s): candidate_name") {
		t.Fatal("the backend validation error is not shown")
	}
	if !strings.Contains(text, "candidate_name") || !strings.Contains(text, "Effective") {
		t.Fatal("the validated comparison must stay visible beside a refused render")
	}
	for _, absent := range []string{"Hi Ada", "ada@example.com"} {
		if strings.Contains(text, absent) {
			t.Errorf("a refused render showed %q as if it were a rendered email", absent)
		}
	}
}

// findCard returns the disclosure or card that holds the element with the
// given id.
func findCard(node *html.Node, headingID string) *html.Node {
	if node.Type == html.ElementNode && workspaceAttr(node, "id") == headingID {
		for parent := node.Parent; parent != nil; parent = parent.Parent {
			if parent.Type != html.ElementNode {
				continue
			}
			if slot := workspaceAttr(parent, "data-slot"); slot == "accordion-item" || slot == "card" || workspaceAttr(parent, "role") == "region" {
				return parent
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findCard(child, headingID); found != nil {
			return found
		}
	}
	return nil
}

// textOf collects the text content of a node and its descendants.
func textOf(node *html.Node) string {
	if node == nil {
		return ""
	}
	var out strings.Builder
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(node)
	return strings.Join(strings.Fields(out.String()), " ")
}
