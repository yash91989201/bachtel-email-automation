package views

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// The campaign list, task heading and breadcrumb identify a task by its saved
// name, while the original workbook filename stays available as source detail.
func TestCampaignTaskRowsShowNameAndWorkbook(t *testing.T) {
	data := CampaignPageData{
		ID: "camp1", Name: "Hiring", Section: CampaignSectionTasks,
		Tasks: []CampaignTaskRow{
			{ID: "task-1", Name: "October outreach", Filename: "first.xlsx", Reference: "abcd1234",
				UploadedAt: "22 Oct 2026, 11:12 AM +05:30", Status: "Ready to send", Recipients: "2 eligible",
				NextLabel: "Choose sending time", NextSection: TaskSectionSending},
			{ID: "task-2", Name: "October outreach", Filename: "second.xlsx", Reference: "efgh5678",
				UploadedAt: "22 Oct 2026, 12:40 PM +05:30", Status: "Ready to send", Recipients: "1 eligible",
				NextLabel: "Choose sending time", NextSection: TaskSectionSending},
		},
	}
	page := renderWorkspace(t, CampaignPage(data))
	if !strings.Contains(page, `<th class="min-w-56">Task</th>`) {
		t.Error("the task column must be labelled Task")
	}
	if count := strings.Count(page, `>October outreach</a>`); count != 2 {
		t.Errorf("task name link appears %d times, want 2", count)
	}
	for _, want := range []string{
		`href="/campaigns/camp1/tasks/task-1"`,
		`href="/campaigns/camp1/tasks/task-2"`,
		"Workbook first.xlsx",
		"Workbook second.xlsx",
		"Reference abcd1234",
		"Reference efgh5678",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("campaign task row is missing %q", want)
		}
	}
	if strings.Contains(page, `<th class="min-w-56">Workbook</th>`) {
		t.Error("the workbook label must no longer head the task column")
	}
}

// A name is text: markup-like input reaches the page escaped, and the task
// heading and breadcrumb both use the name instead of the source filename.
func TestTaskPageHeadingAndBreadcrumbUseSavedName(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1", CampaignName: "Hiring", TaskID: "task1",
		Name: "<b>October</b>", Filename: "recipients.xlsx", Section: TaskSectionOverview,
	}
	page := renderWorkspace(t, TaskPage(data))
	if strings.Contains(page, "<b>October</b>") {
		t.Fatal("the task name supplied active markup")
	}
	if !strings.Contains(page, "&lt;b&gt;October&lt;/b&gt;") {
		t.Fatal("the task name must be displayed as escaped text")
	}
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	heading := findViewNode(doc, func(n *html.Node) bool { return n.Data == "h1" })
	if heading == nil || viewText(heading) != data.Name {
		t.Errorf("task heading = %q, want the saved name", viewText(heading))
	}
	breadcrumb := workspaceNode(doc, "data-slot", "breadcrumb")
	if breadcrumb == nil || !strings.Contains(viewText(breadcrumb), data.Name) {
		t.Error("the final breadcrumb must show the saved task name")
	}
	overview := workspaceNode(doc, "id", "task-panel-overview")
	if overview == nil || !strings.Contains(viewText(overview), "Workbook") || !strings.Contains(viewText(overview), data.Filename) {
		t.Error("Overview Task details must show the original workbook filename")
	}
}

// Settings keeps a refused draft with safe feedback, shows save confirmation
// after the redirect, and offers no processing control.
func TestTaskSettingsDraftFeedbackAndControls(t *testing.T) {
	refused := TaskPageData{
		CampaignID: "camp1", CampaignName: "Hiring", TaskID: "task1",
		Name: "October outreach", NameDraft: "Draft name", Filename: "recipients.xlsx",
		Section: TaskSectionSettings, Error: "The task name could not be saved; check the name and try again.",
	}
	doc := parseView(t, TaskPage(refused))
	panel := workspaceNode(doc, "id", "task-settings")
	if panel == nil {
		t.Fatal("missing Settings panel")
	}
	nameInput := workspaceNode(panel, "id", "task-settings-name")
	if nameInput == nil || workspaceAttr(nameInput, "value") != refused.NameDraft {
		t.Error("Settings must keep the submitted draft")
	}
	if !strings.Contains(viewText(doc), "could not be saved") {
		t.Error("Settings must show the refusal reason")
	}
	for _, control := range []string{"Start sending", "Schedule sending", "Stop sending", "Retry all"} {
		if strings.Contains(viewText(panel), control) {
			t.Errorf("Settings exposes the processing control %q", control)
		}
	}
	if workspaceNode(doc, "aria-label", "Task lifecycle") != nil {
		t.Error("Settings must stay outside the lifecycle tracker")
	}

	saved := TaskPageData{
		CampaignID: "camp1", CampaignName: "Hiring", TaskID: "task1",
		Name: "November follow-up", NameDraft: "November follow-up", Filename: "recipients.xlsx",
		Section: TaskSectionSettings, NameSaved: true,
	}
	savedPage := renderWorkspace(t, TaskPage(saved))
	if !strings.Contains(savedPage, "Task name saved") || !strings.Contains(savedPage, `value="November follow-up"`) {
		t.Error("a successful save must show feedback with the new name")
	}

	long := TaskPageData{
		CampaignID: "camp1", CampaignName: "Hiring", TaskID: "task1",
		Name: strings.Repeat("Long name ", 28), NameDraft: strings.Repeat("Long name ", 28),
		Filename: "recipients.xlsx", Section: TaskSectionSettings,
	}
	longDoc := parseView(t, TaskPage(long))
	heading := findViewNode(longDoc, func(n *html.Node) bool { return n.Data == "h1" })
	if heading == nil || !strings.Contains(workspaceAttr(heading, "class"), "wrap-anywhere") {
		t.Error("a long task name must wrap in the shared heading")
	}
}
