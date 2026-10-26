package views

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// findCampaignForm returns the form in the campaign Settings workspace whose
// action matches the given path suffix.
func findCampaignForm(doc *html.Node, action string) *html.Node {
	return findViewNode(doc, func(n *html.Node) bool {
		return n.Data == "form" && strings.HasSuffix(workspaceAttr(n, "action"), action)
	})
}

// The Settings General card names the campaign form: heading, action, the
// required name input prefilled from the draft, its help text and the submit
// button. It precedes the result-column labels card and the delete form stays
// a separate form with its own action.
func TestCampaignNameSettingsGeneralCard(t *testing.T) {
	data := CampaignPageData{
		ID: "camp1", Name: "Hiring", NameDraft: "Hiring",
		Section: CampaignSectionSettings,
	}
	doc := parseView(t, CampaignPage(data))
	settings := workspaceNode(doc, "id", "campaign-settings-workspace")
	if settings == nil {
		t.Fatal("missing Settings workspace")
	}

	general := findViewNode(settings, func(n *html.Node) bool {
		return n.Data == "h3" && workspaceAttr(n, "id") == "campaign-name-heading"
	})
	if general == nil || viewText(general) != "General" {
		t.Fatal("the Settings card must be headed General")
	}
	labels := findViewNode(settings, func(n *html.Node) bool {
		return n.Data == "h3" && workspaceAttr(n, "id") == "labels-heading"
	})
	if labels == nil {
		t.Fatal("missing the result-column labels card")
	}
	if !before(settings, general, labels) {
		t.Error("the General card must precede the result-column labels card")
	}

	form := findCampaignForm(doc, "/campaigns/"+data.ID+"/name")
	if form == nil {
		t.Fatal("missing the campaign name form")
	}
	nameInput := workspaceNode(form, "id", "campaign-name-input")
	if nameInput == nil {
		t.Fatal("missing the campaign name input")
	}
	if workspaceAttr(nameInput, "name") != "name" {
		t.Errorf("input name = %q, want name", workspaceAttr(nameInput, "name"))
	}
	if workspaceAttr(nameInput, "value") != data.NameDraft {
		t.Errorf("input value = %q, want the draft %q", workspaceAttr(nameInput, "value"), data.NameDraft)
	}
	if !hasViewAttr(nameInput, "required") {
		t.Error("the campaign name input must be required")
	}
	helpID := workspaceAttr(nameInput, "aria-describedby")
	if helpID == "" {
		t.Fatal("the campaign name input must point at its help text")
	}
	help := workspaceNode(form, "id", helpID)
	if help == nil || !strings.Contains(viewText(help), "200") {
		t.Error("the help text must state the 200-character bound")
	}
	assertSubmitButton(t, form, "Save name")

	deleteForm := findCampaignForm(doc, "/campaigns/"+data.ID+"/delete")
	if deleteForm == nil || deleteForm == form {
		t.Error("the delete form must be a separate form with its own action")
	}
	labelsForm := findCampaignForm(doc, "/campaigns/"+data.ID+"/labels")
	if labelsForm == nil || labelsForm == form {
		t.Error("the labels form target must differ from the name form target")
	}
	for _, control := range []string{"Start sending", "Schedule sending", "Stop sending", "Pause sending"} {
		if strings.Contains(viewText(settings), control) {
			t.Errorf("Settings exposes the processing control %q", control)
		}
	}
}

// A refused save keeps the submitted draft in the input and shows the refusal
// reason, while the heading and final breadcrumb still carry the saved name.
func TestCampaignNameRefusedSaveKeepsDraftAndSavedIdentity(t *testing.T) {
	data := CampaignPageData{
		ID: "camp1", Name: "Hiring", NameDraft: "Draft name",
		NameError: "a campaign name is required", Section: CampaignSectionSettings,
	}
	doc := parseView(t, CampaignPage(data))
	form := findCampaignForm(doc, "/campaigns/"+data.ID+"/name")
	if form == nil {
		t.Fatal("missing the campaign name form")
	}
	nameInput := workspaceNode(form, "id", "campaign-name-input")
	if nameInput == nil || workspaceAttr(nameInput, "value") != data.NameDraft {
		t.Error("a refused save must keep the submitted draft")
	}
	if !strings.Contains(viewText(doc), data.NameError) {
		t.Error("a refused save must show the refusal reason")
	}
	heading := findViewNode(doc, func(n *html.Node) bool { return n.Data == "h1" })
	if heading == nil || viewText(heading) != "Hiring" {
		t.Errorf("campaign heading = %q, want the saved name Hiring", viewText(heading))
	}
	breadcrumb := workspaceNode(doc, "data-slot", "breadcrumb")
	if breadcrumb == nil || !strings.Contains(viewText(breadcrumb), "Hiring") {
		t.Error("the final breadcrumb must show the saved name")
	}
	if strings.Contains(viewText(breadcrumb), "Draft name") {
		t.Error("the draft must never reach the breadcrumb")
	}
}

// A name is text: markup-like drafts reach the page escaped, never as active
// markup.
func TestCampaignNameDraftIsEscaped(t *testing.T) {
	data := CampaignPageData{
		ID: "camp1", Name: "Hiring", NameDraft: "<b>October</b>",
		NameError: "a campaign name is required", Section: CampaignSectionSettings,
	}
	page := renderWorkspace(t, CampaignPage(data))
	if strings.Contains(page, `<b>October</b>`) {
		t.Fatal("the name draft supplied active markup")
	}
	if !strings.Contains(page, "&lt;b&gt;October&lt;/b&gt;") {
		t.Fatal("the name draft must be displayed as escaped text")
	}
}

// A successful save renders the confirmation and keeps the saved value
// prefilled in the input.
func TestCampaignNameSavedFeedback(t *testing.T) {
	data := CampaignPageData{
		ID: "camp1", Name: "November follow-up", NameDraft: "November follow-up",
		NameSaved: true, Section: CampaignSectionSettings,
	}
	doc := parseView(t, CampaignPage(data))
	if !strings.Contains(viewText(doc), "Campaign name saved") {
		t.Error("a successful save must show the confirmation")
	}
	form := findCampaignForm(doc, "/campaigns/"+data.ID+"/name")
	if form == nil {
		t.Fatal("missing the campaign name form")
	}
	nameInput := workspaceNode(form, "id", "campaign-name-input")
	if nameInput == nil || workspaceAttr(nameInput, "value") != "November follow-up" {
		t.Error("the saved value must stay prefilled after the save")
	}
}

// A long campaign name wraps in the shared heading instead of overflowing the
// page.
func TestCampaignNameLongNameWrapsInHeading(t *testing.T) {
	long := strings.Repeat("Long name ", 30)
	data := CampaignPageData{
		ID: "camp1", Name: long, NameDraft: long, Section: CampaignSectionSettings,
	}
	doc := parseView(t, CampaignPage(data))
	heading := findViewNode(doc, func(n *html.Node) bool { return n.Data == "h1" })
	if heading == nil || !strings.Contains(workspaceAttr(heading, "class"), "wrap-anywhere") {
		t.Error("a long campaign name must wrap in the shared heading")
	}
}
