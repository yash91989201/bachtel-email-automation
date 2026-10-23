package views

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

func TestCampaignWorkspaceSections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    CampaignPageData
		initial string
		ignore  bool
	}{
		{name: "campaign opens on tasks", data: CampaignPageData{ID: "camp1", Name: "Hiring"}, initial: CampaignSectionTasks},
		{name: "revision error opens template", data: CampaignPageData{ID: "camp1", Name: "Hiring", HasRevision: true, RevisionError: "Invalid mapping"}, initial: CampaignSectionTemplate, ignore: true},
		{name: "label error opens settings", data: CampaignPageData{ID: "camp1", Name: "Hiring", LabelsError: "Duplicate labels"}, initial: CampaignSectionSettings, ignore: true},
		{name: "delete error opens settings", data: CampaignPageData{ID: "camp1", Name: "Hiring", DeleteError: "Campaign is active"}, initial: CampaignSectionSettings, ignore: true},
		{name: "export error opens activity", data: CampaignPageData{ID: "camp1", Name: "Hiring", ExportError: "Generation failed"}, initial: CampaignSectionActivity, ignore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderWorkspace(t, CampaignPage(tc.data))
			assertWorkspaceTabs(t, page, "campaign", tc.initial, 4)
			doc, err := html.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			root := workspaceNode(doc, "data-slot", "tabs")
			if !hasWorkspaceAttr(root, "data-workspace-tabs") {
				t.Fatal("campaign workspace is not marked for the adapter")
			}
			if got := hasWorkspaceAttr(root, "data-workspace-ignore-hash"); got != tc.ignore {
				t.Errorf("ignore-hash = %t, want %t", got, tc.ignore)
			}
		})
	}
}

// The campaign header is identity only: breadcrumb, name and the section
// navigation. Revision and formatting approval live in Template & preview,
// where the operator acts on them.
func TestCampaignHeaderPrecedesSections(t *testing.T) {
	page := renderWorkspace(t, CampaignPage(CampaignPageData{ID: "camp1", Name: "Hiring", HasRevision: true, Revision: 2, Approved: true}))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	breadcrumb := workspaceNode(doc, "data-slot", "breadcrumb")
	heading := findViewNode(doc, func(n *html.Node) bool { return n.Data == "h1" })
	sections := workspaceNode(doc, "data-slot", "tabs")
	if breadcrumb == nil || heading == nil || sections == nil {
		t.Fatal("campaign header, headings or sections are missing")
	}
	if !before(doc, breadcrumb, heading) || !before(doc, heading, sections) {
		t.Fatal("campaign header must precede the section navigation")
	}
	header := viewText(workspaceNode(doc, "data-slot", "breadcrumb").Parent)
	for _, unwanted := range []string{"A campaign holds your email template", "Template: Revision", "Formatting:"} {
		if strings.Contains(header, unwanted) {
			t.Errorf("campaign header still shows %q", unwanted)
		}
	}
}

func TestTaskWorkspaceSections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    TaskPageData
		initial string
	}{
		{name: "staging opens overview", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "staging"}, initial: TaskSectionOverview},
		{name: "ready opens overview despite next action", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "ready", NextHref: "#recipients", NextLabel: "Choose sending time"}, initial: TaskSectionOverview},
		{name: "running opens overview", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "running"}, initial: TaskSectionOverview},
		{name: "finished opens overview", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "completed"}, initial: TaskSectionOverview},
		{name: "selection error keeps recipients", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", Section: TaskSectionRecipients, ValidationError: "Missing Email column"}, initial: TaskSectionRecipients},
		{name: "running next action is sending", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "running", Section: TaskSectionSending}, initial: TaskSectionSending},
		{name: "results error keeps results", data: TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "completed", Section: TaskSectionResults, IgnoreHash: true}, initial: TaskSectionResults},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderWorkspace(t, TaskPage(tc.data))
			assertWorkspaceTabs(t, page, "task", tc.initial, 5)
			if !strings.Contains(page, `data-workspace-default="`+tc.initial+`"`) {
				t.Fatal("task workspace must retain its initial section for Back navigation")
			}
		})
	}
}

func TestTaskOverviewKeepsHeaderCompact(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1", CampaignName: "Hiring", TaskID: "task1", Reference: "abcd1234", Filename: "recipients.xlsx",
		StatusLabel: "Ready to send", StatusExplanation: "Sending requires explicit authorization.",
		UploadedAt: "22 Oct 2026, 11:12 AM +05:30", HasSavedRevision: true, SavedRevision: 2,
		Worksheet: "People", HeaderRow: 1, Sender: "hr@example.test", SenderStatus: "connected", Approved: true,
		NextLabel: "Choose sending time", NextHref: "/campaigns/camp1/tasks/task1#sending",
		Lifecycle: []LifecycleStep{
			{Name: "Upload", State: "Stored", Complete: true},
			{Name: "Recipients", State: "Validated", Complete: true},
			{Name: "Test & approve", State: "Approved", Complete: true},
			{Name: "Sending", State: "Not started"},
			{Name: "Results", State: "Not available yet"},
		},
		Blockers: []TaskBlocker{{Label: "Mailbox unavailable", Remedy: "Reconnect before sending", Href: "/connection"}},
	}
	page := renderWorkspace(t, TaskPage(data))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	heading := findViewNode(doc, func(n *html.Node) bool { return n.Data == "h1" })
	if heading == nil || viewText(heading) != data.Filename {
		t.Fatal("task heading must contain only the workbook name")
	}
	header := heading.Parent.Parent
	if badge := workspaceNode(header, "data-slot", "badge"); badge == nil || viewText(badge) != data.StatusLabel {
		t.Fatal("task status badge must appear beside its name")
	}
	if strings.Contains(viewText(header), data.Reference) || strings.Contains(viewText(header), "Task details") {
		t.Fatal("task references and detailed metadata must not occupy the persistent header")
	}
	overview := workspaceNode(doc, "id", "task-panel-overview")
	if overview == nil {
		t.Fatal("missing Overview content")
	}
	for _, want := range []string{data.NextLabel, data.StatusExplanation, "Mailbox unavailable", "Task details", data.UploadedAt, "Saved revision", data.Worksheet, data.Sender, "Approved for current mailbox", data.Reference, data.TaskID} {
		if !strings.Contains(viewText(overview), want) {
			t.Errorf("Overview is missing %q", want)
		}
	}
	if strings.Contains(viewText(overview), "Next steps") {
		t.Fatal("Overview must not render a separate next-steps card")
	}
	if workspaceNode(overview, "href", data.NextHref) == nil || workspaceNode(overview, "aria-label", "Task lifecycle") == nil {
		t.Fatal("Overview must contain the next-action button and lifecycle evidence")
	}
	list := workspaceNode(doc, "role", "tablist")
	first := workspaceNode(list, "data-slot", "tabs-trigger")
	if workspaceAttr(first, "data-templ-value") != TaskSectionOverview {
		t.Fatal("Overview must be first in task navigation")
	}
}

func TestTaskLifecycleEvidenceAndSectionLinks(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1", TaskID: "task1", SendingType: TaskSendingTypeDirect,
		NextLabel: "Choose sending time", NextHref: "/campaigns/camp1/tasks/task1#sending",
		StatusExplanation: "Fresh recipient count needs review.",
		Lifecycle: []LifecycleStep{
			{Name: "Upload", State: "Stored", Complete: true},
			{Name: "Recipients", State: "Validated", Complete: true},
			{Name: "Test & approve", State: "Approved", Complete: true},
			{Name: "Sending", State: "Not started"},
			{Name: "Results", State: "Not available yet"},
		},
	}
	doc, err := html.Parse(strings.NewReader(renderWorkspace(t, taskLifecycle(data))))
	if err != nil {
		t.Fatal(err)
	}
	list := workspaceNode(doc, "aria-label", "Task lifecycle")
	if list == nil {
		t.Fatal("missing lifecycle tracker")
	}
	sections := []string{TaskSectionOverview, TaskSectionRecipients, TaskSectionTest, TaskSectionSending, TaskSectionResults}
	index := 0
	for node := range list.Descendants() {
		if node.Data != "li" {
			continue
		}
		if index >= len(data.Lifecycle) {
			t.Fatal("unexpected lifecycle stage")
		}
		step := data.Lifecycle[index]
		if !strings.Contains(viewText(node), step.Name) || !strings.Contains(viewText(node), step.State) {
			t.Errorf("stage %d must keep its name and evidence-based status", index)
		}
		if got := workspaceAttr(node, "data-complete") == "true"; got != step.Complete {
			t.Errorf("stage %q completion must come from saved evidence", step.Name)
		}
		if got := workspaceAttr(node, "data-next") == "true"; got != (index == 3) {
			t.Errorf("stage %q has an incorrect next-action marker", step.Name)
		}
		localHref := "/campaigns/camp1/tasks/task1#" + sections[index]
		if index == 3 && workspaceNode(node, "href", localHref) == nil {
			t.Errorf("next stage %q must link without transient sending_type state", step.Name)
		}
		if index != 3 && workspaceNode(node, "role", "button") != nil {
			t.Errorf("stage %q must not show a navigation button before it is next", step.Name)
		}
		trigger := workspaceNode(node, "aria-label", "About "+step.Name)
		if trigger == nil || trigger.Data != "button" || workspaceAttr(trigger, "type") != "button" || !hasWorkspaceAttr(trigger, "data-templ-tooltip-open-on-press") || workspaceAttr(trigger, "aria-describedby") != "task-progress-help-"+sections[index] || workspaceAttr(trigger, "data-templ-tooltip-trigger") != "task-progress-help-"+sections[index] {
			t.Errorf("stage %q is missing its explicit, labelled About button", step.Name)
		}
		tooltip := workspaceNode(doc, "id", "task-progress-help-"+sections[index])
		if tooltip == nil || workspaceAttr(tooltip, "data-slot") != "tooltip-content" || workspaceAttr(tooltip, "role") != "tooltip" || (index != 3 && !strings.Contains(viewText(tooltip), taskLifecycleHelp(step.Name))) {
			t.Errorf("stage %q is missing its lifecycle explanation tooltip", step.Name)
		}
		if index == 3 {
			current := workspaceNode(node, "href", data.NextHref)
			if current == nil || workspaceAttr(current, "role") != "button" || !strings.Contains(viewText(current), data.NextLabel) {
				t.Error("current sending stage must contain the explicit next-action button")
			}
			if !strings.Contains(viewText(tooltip), data.StatusExplanation) {
				t.Error("current stage tooltip must include the current status explanation")
			}
		}
		if hasNestedInteractive(node) {
			t.Errorf("stage %q contains nested interactive controls", step.Name)
		}
		index++
	}
	if index != len(data.Lifecycle) {
		t.Fatalf("rendered %d stages, want %d", index, len(data.Lifecycle))
	}
	data.NextHref, data.NextLabel = "/campaigns/camp1#template", "Create template"
	doc, err = html.Parse(strings.NewReader(renderWorkspace(t, taskLifecycle(data))))
	if err != nil {
		t.Fatal(err)
	}
	list = workspaceNode(doc, "aria-label", "Task lifecycle")
	recipientsLink := workspaceNode(list, "href", "/campaigns/camp1/tasks/task1#recipients")
	recipients := viewAncestor(recipientsLink, "li")
	if recipients == nil {
		t.Fatal("an external #template next action must map to the Recipients stage")
	}
	primary := workspaceNode(recipients, "href", data.NextHref)
	if primary == nil || workspaceAttr(primary, "role") != "button" || workspaceNode(recipients, "href", "/campaigns/camp1/tasks/task1#recipients") == nil {
		t.Fatal("external template action must keep both the primary action and canonical Recipients link")
	}
	if tooltip := workspaceNode(list, "id", "task-progress-help-recipients"); tooltip == nil || !strings.Contains(viewText(tooltip), data.StatusExplanation) {
		t.Fatal("external #template action must place current status explanation in Recipients tooltip")
	}

	data.NextHref, data.NextLabel, data.StatusExplanation = "/connection", "Reconnect mailbox", "Connection must be restored before sending."
	data.Blockers = []TaskBlocker{
		{Label: "Mailbox unavailable", Remedy: "Reconnect before sending", Href: "/connection"},
		{Label: "Restore review", Remedy: "Reconcile submissions before sending", Href: "/restore-review"},
	}
	doc, err = html.Parse(strings.NewReader(renderWorkspace(t, taskLifecycle(data))))
	if err != nil {
		t.Fatal(err)
	}
	list = workspaceNode(doc, "aria-label", "Task lifecycle")
	sendingStep := workspaceNode(list, "data-next", "true")
	if sendingStep == nil || !strings.Contains(viewText(sendingStep), "Reconnect mailbox") {
		t.Fatal("external connection action must mark Sending as current and show its action button")
	}
	for _, label := range []string{"Mailbox unavailable", "Restore review"} {
		if !strings.Contains(viewText(sendingStep), label) {
			t.Errorf("sending blocker label %q is not visible", label)
		}
	}
	for _, href := range []string{"/connection", "/restore-review"} {
		if workspaceNode(sendingStep, "href", href) == nil {
			t.Errorf("sending remedy/next-action link %q is not visible", href)
		}
	}
	tooltip := workspaceNode(list, "id", "task-progress-help-sending")
	for _, want := range []string{data.StatusExplanation, "Reconnect before sending", "Reconcile submissions before sending"} {
		if tooltip == nil || !strings.Contains(viewText(tooltip), want) {
			t.Errorf("Sending tooltip is missing %q", want)
		}
	}
	if workspaceNode(sendingStep, "href", "/campaigns/camp1/tasks/task1#sending") == nil {
		t.Fatal("canonical Sending link contains transient sending_type state")
	}
}

func viewAncestor(node *html.Node, tag string) *html.Node {
	for ; node != nil; node = node.Parent {
		if node.Type == html.ElementNode && node.Data == tag {
			return node
		}
	}
	return nil
}

func hasNestedInteractive(root *html.Node) bool {
	var walk func(*html.Node, bool) bool
	walk = func(node *html.Node, inside bool) bool {
		interactive := node.Type == html.ElementNode && (node.Data == "button" || node.Data == "a" && workspaceAttr(node, "href") != "")
		if interactive && inside {
			return true
		}
		inside = inside || interactive
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if walk(child, inside) {
				return true
			}
		}
		return false
	}
	return walk(root, false)
}

// Task identity, overview, next action and lifecycle stay outside the polling
// region, so a progress swap cannot reset navigation or the operator's place.
func TestTaskShellKeepsNavigationOutsidePolling(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1", TaskID: "task1", Filename: "recipients.xlsx", Reference: "abcd1234",
		StoredStatus: "running", StatusLabel: "Sending", StatusExplanation: "Current durable progress.",
		NextLabel: "View progress", NextHref: "/campaigns/camp1/tasks/task1#sending",
		Lifecycle: []LifecycleStep{
			{Name: "Upload", State: "Stored", Complete: true},
			{Name: "Recipients", State: "Validated", Complete: true},
			{Name: "Test & approve", State: "Approved", Complete: true},
			{Name: "Sending", State: "Sending"},
			{Name: "Results", State: "Not available yet"},
		},
		Controls: TaskControls{Stop: true}, Remaining: 2, Approved: true,
		Section: TaskSectionSending,
	}
	page := renderWorkspace(t, TaskPage(data))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	root := workspaceNode(doc, "data-slot", "tabs")
	live := workspaceNode(doc, "id", "sending-live")
	if root == nil || live == nil {
		t.Fatal("missing the tabs root or the live sending region")
	}
	if !isWorkspaceDescendant(root, live) {
		t.Fatal("the live region must stay inside the tabs root so its panel keeps the active state")
	}
	for n := live; n != nil; n = n.Parent {
		if workspaceAttr(n, "data-slot") == "tabs-trigger" {
			t.Fatal("section triggers must stay outside the polling region")
		}
	}
	// Identity and next action are outside the replaced region.
	for _, node := range []*html.Node{workspaceNode(doc, "aria-label", "Task lifecycle")} {
		if node == nil || isWorkspaceDescendant(live, node) {
			t.Fatal("lifecycle evidence must stay outside the live region")
		}
	}
	if got := workspaceAttr(live, "hx-get"); got != "/campaigns/camp1/tasks/task1/sending/progress" {
		t.Errorf("live region poll target = %q", got)
	}
	if got := workspaceAttr(live, "hx-swap"); got != "outerHTML" {
		t.Errorf("live region swap = %q, want outerHTML", got)
	}
	if workspaceNode(live, "data-slot", "tabs") != nil {
		t.Fatal("the live region must not own the tabs root")
	}
	// Disclosures inside the live region keep their open state across swaps.
	for _, id := range []string{"sending-settings"} {
		content := workspaceNode(live, "id", id)
		if content == nil {
			t.Fatalf("disclosure %q is missing from the live region", id)
		}
		if !hasWorkspaceAttr(content, "data-workspace-persist") {
			t.Errorf("disclosure %q is not marked for restoration", id)
		}
	}
}

// The polling fragment renders the Sending section only; it keeps the live
// region id and control identity without the tabs shell.
func TestTaskSendingLiveFragmentKeepsIdentity(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1", TaskID: "task1", StoredStatus: "ready", Approved: true,
		Remaining: 2, SendingType: TaskSendingTypeDirect, Controls: TaskControls{Start: true, Schedule: true}, ActionID: "actions-",
	}
	page := renderWorkspace(t, TaskSendingLive(data))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if workspaceNode(doc, "data-slot", "tabs") != nil {
		t.Fatal("the fragment must not render the tabs root")
	}
	if workspaceNode(doc, "id", "sending-live") == nil {
		t.Fatal("the fragment lost its live region id")
	}
	if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1/sending/start#sending") == nil {
		t.Fatal("the fragment lost the start control")
	}
}

func TestTaskSendingLivePollsOnlyActiveWork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   string
		schedule string
		inFlight int
		wantPoll bool
	}{
		{"completed", "completed", "", 0, false},
		{"completed with active request", "completed", "", 1, true},
		{"running with no remaining rows", "running", "", 0, true},
		{"waiting", "waiting", "", 0, true},
		{"stopping", "stopping", "", 1, true},
		{"ready for scheduled start", "ready", "scheduled", 0, true},
		{"idle ready", "ready", "none", 0, false},
		{"paused", "paused", "", 0, false},
		{"stopped", "stopped", "", 0, false},
		{"interrupted", "interrupted", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: tc.status, ScheduleState: tc.schedule, InFlight: tc.inFlight}
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(data))))
			if err != nil {
				t.Fatal(err)
			}
			live := workspaceNode(doc, "id", "sending-live")
			if live == nil {
				t.Fatal("missing live fragment")
			}
			for _, attr := range []string{"hx-get", "hx-trigger", "hx-swap"} {
				if got := hasWorkspaceAttr(live, attr); got != tc.wantPoll {
					t.Errorf("%s present = %t, want %t", attr, got, tc.wantPoll)
				}
			}
		})
	}
}

func TestTaskSendingTypeChooserAndSelectedControls(t *testing.T) {
	base := TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "ready", Remaining: 2, Controls: TaskControls{Start: true, Schedule: true}}
	t.Run("chooser is UI only", func(t *testing.T) {
		doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(base))))
		if err != nil {
			t.Fatal(err)
		}
		form := workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1/sending-type")
		if form == nil || workspaceAttr(form, "method") != "post" {
			t.Fatal("chooser must persist the selection through the sending-type POST")
		}
		for _, typ := range []string{TaskSendingTypeDirect, TaskSendingTypeScheduled} {
			button := workspaceNode(doc, "value", typ)
			if button == nil || workspaceAttr(button, "name") != "sending_type" || button.Data != "button" || workspaceAttr(button, "type") != "submit" {
				t.Errorf("missing POST choice button for %q", typ)
			}
		}
		for _, action := range []string{"/sending/start#sending", "/sending/schedule#sending"} {
			if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1"+action) != nil {
				t.Errorf("chooser exposes POST action %q", action)
			}
		}
	})
	t.Run("invalid selection returns to chooser", func(t *testing.T) {
		data := base
		data.SendingType = "unexpected"
		doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(data))))
		if err != nil {
			t.Fatal(err)
		}
		if workspaceNode(doc, "value", TaskSendingTypeDirect) == nil || workspaceNode(doc, "value", TaskSendingTypeScheduled) == nil {
			t.Fatal("invalid sending type must leave the operator at the explicit chooser")
		}
		if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1/sending/start#sending") != nil || workspaceNode(doc, "id", "sending-schedule-"+sendingControlKey(data)) != nil {
			t.Fatal("invalid sending type must not select a first-start action")
		}
	})
	for _, tc := range []struct{ mode, shown, hidden string }{
		{TaskSendingTypeDirect, "/sending/start#sending", "/sending/schedule#sending"},
		{TaskSendingTypeScheduled, "/sending/schedule#sending", "/sending/start#sending"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			data := base
			data.SendingType = tc.mode
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(data))))
			if err != nil {
				t.Fatal(err)
			}
			if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1"+tc.shown) == nil {
				t.Errorf("%s mode is missing selected action", tc.mode)
			}
			if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1"+tc.hidden) != nil {
				t.Errorf("%s mode exposes other action", tc.mode)
			}
			if workspaceNode(doc, "href", "/campaigns/camp1/tasks/task1?sending_type=choose#sending") == nil {
				t.Errorf("%s mode is missing change-type link", tc.mode)
			}
		})
	}
}

func TestTaskCanChooseSendingType(t *testing.T) {
	for _, tc := range []struct {
		name string
		data TaskPageData
		want bool
	}{
		{"ready with both first-start paths", TaskPageData{StoredStatus: "ready", Controls: TaskControls{Start: true, Schedule: true}}, true},
		{"ready with direct only", TaskPageData{StoredStatus: "ready", Controls: TaskControls{Start: true}}, true},
		{"ready with schedule only", TaskPageData{StoredStatus: "ready", Controls: TaskControls{Schedule: true}}, true},
		{"ready without action", TaskPageData{StoredStatus: "ready"}, false},
		{"running", TaskPageData{StoredStatus: "running", Controls: TaskControls{Start: true, Schedule: true}}, false},
		{"ready with saved schedule", TaskPageData{StoredStatus: "ready", ScheduleState: "scheduled", Controls: TaskControls{SendNowInstead: true, EditSchedule: true, ClearSchedule: true}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskCanChooseSendingType(tc.data); got != tc.want {
				t.Errorf("taskCanChooseSendingType = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestTaskSendingTypeSavedScheduleAndStartedStateLimits(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		data                               TaskPageData
		wantStart, wantSchedule, wantClear bool
	}{
		{name: "saved schedule defaults to schedule", data: TaskPageData{StoredStatus: "ready", ScheduleState: "scheduled", Controls: TaskControls{SendNowInstead: true, EditSchedule: true, ClearSchedule: true}}, wantSchedule: true, wantClear: true},
		{name: "missed schedule defaults to schedule", data: TaskPageData{StoredStatus: "ready", ScheduleState: "missed", Controls: TaskControls{Start: true, ClearSchedule: true}}, wantClear: true},
		{name: "explicit direct wins", data: TaskPageData{StoredStatus: "ready", ScheduleState: "scheduled", SendingType: TaskSendingTypeDirect, Controls: TaskControls{SendNowInstead: true, EditSchedule: true, ClearSchedule: true}}, wantStart: true, wantClear: true},
		{name: "explicit choose has no first-start actions", data: TaskPageData{StoredStatus: "ready", SendingType: TaskSendingTypeChoose, Controls: TaskControls{Start: true, Schedule: true}}},
		{name: "started forged mode cannot show chooser", data: TaskPageData{StoredStatus: "running", SendingType: TaskSendingTypeChoose, Controls: TaskControls{Stop: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.data.CampaignID, tc.data.TaskID = "camp1", "task1"
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(tc.data))))
			if err != nil {
				t.Fatal(err)
			}
			start := workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1/sending/start#sending") != nil
			schedule := workspaceNode(doc, "id", "sending-schedule-"+sendingControlKey(tc.data)) != nil
			clear := findViewNode(doc, func(n *html.Node) bool {
				return n.Data == "form" && strings.HasPrefix(workspaceAttr(n, "id"), "sending-schedule-clear-")
			}) != nil
			if start != tc.wantStart || schedule != tc.wantSchedule {
				t.Errorf("first-start forms start=%t schedule=%t", start, schedule)
			}
			if tc.wantClear && !clear {
				t.Error("saved schedule needs a clear control")
			}
			if tc.name == "explicit direct wins" {
				if workspaceNode(doc, "action", "/campaigns/camp1/tasks/task1/sending/start#sending") == nil || !strings.Contains(viewText(doc), "Send now instead") {
					t.Error("direct action must replace the stored scheduled start")
				}
				if strings.Contains(viewText(doc), "Schedule start") {
					t.Error("direct mode must not expose schedule editing")
				}
			}
			if tc.data.StoredStatus != "ready" && workspaceNode(doc, "name", "sending_type") != nil {
				t.Error("started task exposes forged first-start chooser")
			}
			if tc.name == "explicit choose has no first-start actions" && workspaceNode(doc, "value", TaskSendingTypeDirect) == nil {
				t.Error("explicit choose must render the chooser")
			}
		})
	}
}

func TestTaskSendingLivePollURLHasNoTypeQuery(t *testing.T) {
	data := TaskPageData{CampaignID: "camp1", TaskID: "task1", StoredStatus: "ready", ScheduleState: "scheduled", SendingType: TaskSendingTypeScheduled}
	doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskSendingLive(data))))
	if err != nil {
		t.Fatal(err)
	}
	live := workspaceNode(doc, "id", "sending-live")
	if live == nil || workspaceAttr(live, "hx-get") != "/campaigns/camp1/tasks/task1/sending/progress" {
		t.Fatalf("poll URL = %q", workspaceAttr(live, "hx-get"))
	}
}

// Control identity changes when any fact relevant to a confirmation changes,
// even when the numeric count is identical.
func TestSendingControlKeyInvalidatesOnScopeChange(t *testing.T) {
	base := TaskPageData{StoredStatus: "ready", Approved: true, Remaining: 2, Sender: "hr@example.test", SenderStatus: "connected", Hash: "sha256:a"}
	key := sendingControlKey(base)
	for name, mutate := range map[string]func(*TaskPageData){
		"mailbox":            func(d *TaskPageData) { d.SenderStatus = "disconnected" },
		"sender punctuation": func(d *TaskPageData) { d.Sender = "h.r@example.test" },
		"approval":           func(d *TaskPageData) { d.Approved = false },
		"schedule":           func(d *TaskPageData) { d.ScheduleState = "scheduled" },
		"configuration":      func(d *TaskPageData) { d.Hash = "sha256:b" },
		"status":             func(d *TaskPageData) { d.StoredStatus = "paused" },
		"sending type":       func(d *TaskPageData) { d.SendingType = TaskSendingTypeDirect },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if sendingControlKey(changed) == key {
				t.Fatal("changing a confirmation scope fact did not change the control identity")
			}
		})
	}
}

func TestPreservedSendingFormsHaveSelectorSafeIDs(t *testing.T) {
	selectorSafe := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	for name, data := range map[string]TaskPageData{
		"direct":            {StoredStatus: "ready", SendingType: TaskSendingTypeDirect, Controls: TaskControls{Start: true}},
		"scheduled":         {StoredStatus: "ready", SendingType: TaskSendingTypeScheduled, Controls: TaskControls{Schedule: true}},
		"enabled schedule":  {StoredStatus: "ready", ScheduleState: "scheduled", Controls: TaskControls{SendNowInstead: true, EditSchedule: true, ClearSchedule: true}},
		"recovery and stop": {StoredStatus: "paused", Controls: TaskControls{Resume: true, Stop: true, RetryAll: true, RetryRow: true, ResendRow: true, ResolveRow: true}},
	} {
		t.Run(name, func(t *testing.T) {
			data.CampaignID, data.TaskID = "camp1", "task1"
			data.Hash, data.Sender = "sha256:0123456789", "hr+recruiting@example.test"
			data.Approved, data.HasSavedRevision, data.Remaining = true, true, 2
			data.Rows = []TaskAdmittedRow{{ID: "row1", State: "definite_failure"}, {ID: "row2", State: "review_required"}}
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, TaskPage(data))))
			if err != nil {
				t.Fatal(err)
			}
			preserved := 0
			for node := range doc.Descendants() {
				if !hasWorkspaceAttr(node, "hx-preserve") {
					continue
				}
				preserved++
				id := workspaceAttr(node, "id")
				if !selectorSafe.MatchString(id) {
					t.Errorf("preserved form ID %q cannot be restored by htmx's ID selector", id)
				}
			}
			if preserved == 0 {
				t.Fatal("fixture must render preserved forms")
			}
		})
	}
}

// Settled rows stay out of the attention table: already submitted candidates
// and eligible rows without a reason need no operator action.
func TestTaskRowReasonsExcludesSettledRows(t *testing.T) {
	data := TaskPageData{
		CampaignID: "camp1",
		TaskID:     "task1",
		Rows: []TaskAdmittedRow{
			{ID: "sent", Row: 2, State: "submitted"},
			{ID: "clean", Row: 3, State: "eligible"},
			{ID: "review", Row: 4, State: "review_required", Reason: "outcome unknown"},
			{ID: "short", Row: 5, State: "ineligible", Reason: "missing required value"},
		},
	}
	doc, err := html.Parse(strings.NewReader(renderWorkspace(t, taskRowReasons(data))))
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]bool{}
	for node := range doc.Descendants() {
		if node.Type != html.ElementNode || node.Data != "code" {
			continue
		}
		states[strings.TrimSpace(node.FirstChild.Data)] = true
	}
	if states["submitted"] || states["eligible"] {
		t.Fatalf("settled states rendered in attention table: %v", states)
	}
	if !states["review_required"] || !states["ineligible"] {
		t.Fatalf("attention states missing from table: %v", states)
	}
}

func renderWorkspace(t *testing.T, component templ.Component) string {
	t.Helper()
	var page strings.Builder
	if err := component.Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	return page.String()
}

// before reports whether first precedes second in document order.
func before(root, first, second *html.Node) bool {
	order := map[*html.Node]int{}
	index := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		order[n] = index
		index++
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return order[first] < order[second]
}

// assertWorkspaceTabs checks the owned tabs primitive contract: one tablist
// with a vertical orientation, one trigger marked active, its panel labelled
// by it, and one visible panel. Inactive panels carry data-hidden, not the
// native hidden attribute.
func assertWorkspaceTabs(t *testing.T, page, scope, initial string, count int) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	list := workspaceNode(doc, "role", "tablist")
	if list == nil {
		t.Fatal("workspace has no tablist")
	}
	if got := workspaceAttr(list, "aria-orientation"); got != "vertical" {
		t.Errorf("tablist aria-orientation = %q, want vertical", got)
	}
	tabs, selected, visible := 0, 0, 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if workspaceAttr(n, "data-slot") == "tabs-trigger" {
			tabs++
			value := workspaceAttr(n, "data-templ-value")
			panel := workspaceNode(doc, "id", scope+"-panel-"+value)
			if panel == nil || workspaceAttr(panel, "role") != "tabpanel" || workspaceAttr(panel, "aria-labelledby") != workspaceAttr(n, "id") {
				t.Errorf("trigger %s has no matching labelled panel", workspaceAttr(n, "id"))
			}
			if workspaceAttr(n, "id") != scope+"-tab-"+value {
				t.Errorf("trigger id = %q, want %q", workspaceAttr(n, "id"), scope+"-tab-"+value)
			}
			active := hasWorkspaceAttr(n, "data-active")
			if active != (workspaceAttr(n, "aria-selected") == "true") {
				t.Errorf("trigger %s data-active and aria-selected disagree", workspaceAttr(n, "id"))
			}
			if active {
				selected++
				if value != initial {
					t.Errorf("initial trigger = %q, want %q", value, initial)
				}
				if workspaceAttr(n, "aria-controls") != scope+"-panel-"+initial {
					t.Errorf("active trigger controls %q", workspaceAttr(n, "aria-controls"))
				}
			}
		}
		if workspaceAttr(n, "role") == "tabpanel" {
			class := " " + workspaceAttr(n, "class") + " "
			hidden := hasWorkspaceAttr(n, "data-hidden") || strings.Contains(class, " hidden ")
			if !hidden {
				visible++
				if workspaceAttr(n, "id") != scope+"-panel-"+initial {
					t.Errorf("visible panel = %q, want %q", workspaceAttr(n, "id"), scope+"-panel-"+initial)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if tabs != count || selected != 1 || visible != 1 {
		t.Errorf("tabs/selected/visible=%d/%d/%d, want %d/1/1", tabs, selected, visible, count)
	}
}

func isWorkspaceDescendant(root, target *html.Node) bool {
	for n := target; n != nil; n = n.Parent {
		if n == root {
			return true
		}
	}
	return false
}

func hasWorkspaceAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func workspaceAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func workspaceNode(n *html.Node, key, value string) *html.Node {
	if workspaceAttr(n, key) == value {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := workspaceNode(child, key, value); found != nil {
			return found
		}
	}
	return nil
}

// Approved formatting is quiet by default, with an explicit, keyboard-native
// disclosure for retesting or following the existing template revision flow.
func TestApprovedFormattingControlsRequireDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		approved bool
		message  string
		wantOpen bool
	}{
		{name: "approved", approved: true},
		{name: "review error remains visible", approved: true, message: "Test rejected", wantOpen: true},
		{name: "approval required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := TaskPageData{CampaignID: "campaign", TaskID: "task", StoredStatus: "ready", Approved: tc.approved, Error: tc.message, Rows: []TaskAdmittedRow{{ID: "row", State: "eligible"}}}
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, taskTest(data))))
			if err != nil {
				t.Fatal(err)
			}
			review := workspaceNode(doc, "id", "formatting-review")
			if tc.approved {
				if review == nil {
					t.Fatal("approved task has no explicit formatting disclosure")
				}
				if hasWorkspaceAttr(review, "open") != tc.wantOpen {
					t.Fatalf("formatting disclosure open = %t, want %t", hasWorkspaceAttr(review, "open"), tc.wantOpen)
				}
				for _, id := range []string{"sending-diagnostic-test", approvalFormID(data)} {
					if workspaceNode(review, "id", id) == nil {
						t.Errorf("form %s is outside the gated disclosure", id)
					}
				}
				if workspaceNode(review, "href", "/campaigns/campaign#template") == nil {
					t.Error("missing template change path")
				}
			} else {
				if review != nil {
					t.Fatal("unapproved task must show required controls directly")
				}
				if workspaceNode(doc, "id", "sending-diagnostic-test") == nil {
					t.Fatal("required test form missing")
				}
			}
		})
	}
}

func TestConnectedMailboxProviderSetupIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      ConnectionData
		wantSetup bool
	}{
		{name: "connected", data: ConnectionData{Status: "connected", SenderAddress: "hr@example.test"}},
		{name: "change provider", data: ConnectionData{Status: "connected", SenderAddress: "hr@example.test", ShowProviderChooser: true}, wantSetup: true},
		{name: "selected provider", data: ConnectionData{Status: "connected", SenderAddress: "hr@example.test", SelectedProvider: "godaddy-titan"}, wantSetup: true},
		{name: "disconnected", data: ConnectionData{Status: "disconnected"}, wantSetup: true},
		{name: "reconnect required", data: ConnectionData{Status: "reconnect_required", SenderAddress: "hr@example.test"}, wantSetup: true},
		{name: "replacement blocked", data: ConnectionData{Status: "connected", SenderAddress: "hr@example.test", ShowProviderChooser: true, ResetBlocked: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, ConnectionPage(tc.data))))
			if err != nil {
				t.Fatal(err)
			}
			if got := workspaceNode(doc, "id", "provider-setup") != nil; got != tc.wantSetup {
				t.Fatalf("provider setup present = %t, want %t", got, tc.wantSetup)
			}
			if tc.data.Status == "connected" && !tc.data.ResetBlocked {
				if workspaceNode(doc, "id", "mailbox-disconnect-form") == nil || workspaceNode(doc, "href", "/connection?change_provider=1#provider-setup") == nil {
					t.Fatal("connected mailbox needs Disconnect and Change provider actions")
				}
			}
		})
	}
}

func TestSendingSettingsUsesAccordion(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(renderWorkspace(t, taskPacing(TaskPageData{Rate: 25}))))
	if err != nil {
		t.Fatal(err)
	}
	root := workspaceNode(doc, "id", "sending-settings")
	if root == nil || workspaceAttr(root, "data-slot") != "accordion" || !hasWorkspaceAttr(root, "data-workspace-persist") {
		t.Fatal("settings must use a persistent accordion")
	}
	trigger := workspaceNode(root, "data-slot", "accordion-trigger")
	if trigger == nil || trigger.Data != "button" || workspaceAttr(trigger, "aria-expanded") != "false" {
		t.Fatal("settings need a collapsed accordion button")
	}
	if workspaceNode(trigger, "data-slot", "accordion-trigger-icon") == nil {
		t.Fatal("accordion needs its disclosure indicator")
	}
	if workspaceNode(root, "id", "sending-rate") == nil {
		t.Fatal("pacing control missing from accordion")
	}
}

func TestTaskHistoryUsesAccordion(t *testing.T) {
	for _, tc := range []struct {
		name string
		data TaskPageData
		want []string
	}{
		{name: "empty", want: []string{"No attempts recorded for this task yet."}},
		{
			name: "attempts and resolutions",
			data: TaskPageData{
				Recent: []TaskAttemptRow{
					{Kind: "candidate", Recipient: "candidate@example.test", State: "submitted", Time: "22 Oct 2026, 07:24 PM +05:30"},
					{Kind: "test", Recipient: "test@example.test", State: "uncertain", Reason: "Review required"},
				},
				Resolutions: []TaskResolutionRow{{Row: 3, Basis: "Confirmed in sent mail", Actor: "Operator"}},
			},
			want: []string{"candidate@example.test", "test@example.test", "submitted", "uncertain", "Review required", "22 Oct 2026, 07:24 PM +05:30", "Worksheet row 3 marked already submitted", "Confirmed in sent mail", "Operator"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(renderWorkspace(t, taskAttemptHistory(tc.data))))
			if err != nil {
				t.Fatal(err)
			}
			root := workspaceNode(doc, "id", "task-results-history")
			if root == nil || workspaceAttr(root, "data-slot") != "accordion" || !hasWorkspaceAttr(root, "data-workspace-persist") {
				t.Fatal("history must use a persistent accordion at the legacy bookmark target")
			}
			trigger := workspaceNode(root, "data-slot", "accordion-trigger")
			if trigger == nil || trigger.Data != "button" || workspaceAttr(trigger, "aria-expanded") != "false" || !strings.Contains(viewText(trigger), "Task history") {
				t.Fatal("history needs a collapsed accordion button")
			}
			if workspaceNode(trigger, "data-slot", "accordion-trigger-icon") == nil {
				t.Fatal("history needs its disclosure indicator")
			}
			panel := workspaceNode(root, "data-slot", "accordion-content")
			if panel == nil || !hasWorkspaceAttr(panel, "hidden") || workspaceAttr(panel, "aria-labelledby") != workspaceAttr(trigger, "id") {
				t.Fatal("history panel must start hidden and be labelled by its trigger")
			}
			text := viewText(panel)
			for _, want := range append(tc.want, "Times use India time (+05:30)", "does not confirm delivery", "Tests are kept separate from candidate totals") {
				if !strings.Contains(text, want) {
					t.Errorf("history panel missing %q", want)
				}
			}
		})
	}
}
