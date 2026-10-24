package web

// Section-path routing: every campaign and task section is a directly
// loadable page behind the operator session, unknown sections stay 404, and
// the old roots keep their documented defaults.

import (
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// sectionNav returns the section navigation's link hrefs and its current one.
func sectionNav(t *testing.T, page, label string) (links []string, current string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var nav *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if nav != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "nav" {
			for _, attr := range n.Attr {
				if attr.Key == "aria-label" && attr.Val == label {
					nav = n
					return
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if nav == nil {
		t.Fatalf("no %q navigation", label)
	}
	for node := range nav.Descendants() {
		if node.Type != html.ElementNode || node.Data != "a" {
			continue
		}
		href, selected := "", false
		for _, attr := range node.Attr {
			switch attr.Key {
			case "href":
				href = attr.Val
			case "aria-current":
				selected = attr.Val == "page"
			}
		}
		links = append(links, href)
		if selected {
			if current != "" {
				t.Errorf("second current destination %q", href)
			}
			current = href
		}
	}
	return links, current
}

func TestCampaignSectionPathsRenderTheirOwnSection(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	markers := map[string]string{
		views.CampaignSectionTasks:    `id="campaign-tasks"`,
		views.CampaignSectionTemplate: `id="campaign-template"`,
		views.CampaignSectionActivity: `id="campaign-activity"`,
		views.CampaignSectionSettings: `id="campaign-settings-workspace"`,
	}
	for section, marker := range markers {
		path := views.CampaignSectionPath(id, section)
		page := fetchTaskPage(t, handler, cookie, path)
		if !strings.Contains(page, marker) {
			t.Errorf("%s does not render its own section", path)
		}
		if !strings.Contains(page, `href="`+path+`"`) {
			t.Errorf("%s is not linked from the section navigation", path)
		}
		if _, current := sectionNav(t, page, "Campaign sections"); current != path {
			t.Errorf("%s current = %q", path, current)
		}
		for other, otherMarker := range markers {
			if other != section && strings.Contains(page, otherMarker) {
				t.Errorf("%s also renders the %s section", path, other)
			}
		}
	}

	root := fetchTaskPage(t, handler, cookie, "/campaigns/"+id)
	if !strings.Contains(root, markers[views.CampaignSectionTasks]) {
		t.Fatal("the campaign root must show Tasks")
	}
	if !strings.Contains(root, `href="`+views.CampaignSectionPath(id, views.CampaignSectionTasks)+`"`) {
		t.Fatal("the campaign root must link Tasks by its explicit path")
	}
}

func TestTaskSectionPathsRenderTheirOwnSection(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	markers := map[string]string{
		views.TaskSectionOverview:   `id="overview-heading"`,
		views.TaskSectionRecipients: `id="task-recipients-summary"`,
		views.TaskSectionTest:       `id="task-test"`,
		views.TaskSectionSending:    `id="sending-live"`,
		views.TaskSectionResults:    `id="task-results"`,
	}
	for section, marker := range markers {
		path := views.TaskSectionPath(id, taskID, section)
		page := fetchTaskPage(t, handler, cookie, path)
		if !strings.Contains(page, marker) {
			t.Errorf("%s does not render its own section", path)
		}
		if _, current := sectionNav(t, page, "Task navigation"); current != path {
			t.Errorf("%s current = %q", path, current)
		}
		for other, otherMarker := range markers {
			if other != section && strings.Contains(page, otherMarker) {
				t.Errorf("%s also renders the %s section", path, other)
			}
		}
	}

	root := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/"+taskID)
	if !strings.Contains(root, markers[views.TaskSectionOverview]) {
		t.Fatal("the task root must show Overview")
	}
	// The forwarding entry points keep their section.
	for _, path := range []string{
		"/campaigns/" + id + "/tasks/" + taskID + "/sending",
	} {
		if page := fetchTaskPage(t, handler, cookie, path); !strings.Contains(page, markers[views.TaskSectionSending]) {
			t.Errorf("%s does not show the Sending section", path)
		}
	}
	// The polling fragment stays a section-local response without navigation.
	progress := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/"+taskID+"/sending/progress")
	if !strings.Contains(progress, `id="sending-live"`) || strings.Contains(progress, "Task navigation") {
		t.Fatal("the progress fragment must stay section-local without navigation")
	}
}

func TestSectionPathsRefuseUnknownForeignAndAnonymousNavigation(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	own := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, own, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, own, mixedWorkbook(t))
	otherCampaign := createCampaign(t, handler, cookie, "Contractors")

	unknown := []string{
		"/campaigns/" + own + "/bogus",
		"/campaigns/" + own + "/tasks/" + taskID + "/bogus",
		// A task id under the wrong campaign is not a section alias.
		"/campaigns/" + otherCampaign + "/tasks/" + taskID + "/overview",
		"/campaigns/" + otherCampaign + "/tasks/" + taskID + "/results",
	}
	for _, path := range unknown {
		if rec := do(t, handler, http.MethodGet, path, nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}

	// Another operator's campaign and task sections reveal nothing.
	provisionOperatorAs(t, app, "second@example.com", "second-password")
	foreign := loginAs(t, handler, "second@example.com", "second-password")
	for _, path := range []string{
		views.CampaignSectionPath(own, views.CampaignSectionTasks),
		views.CampaignSectionPath(own, views.CampaignSectionSettings),
		views.TaskSectionPath(own, taskID, views.TaskSectionOverview),
		views.TaskSectionPath(own, taskID, views.TaskSectionSending),
	} {
		if rec := do(t, handler, http.MethodGet, path, nil, withCookie(foreign)); rec.Code != http.StatusNotFound {
			t.Errorf("foreign %s = %d, want 404", path, rec.Code)
		}
	}

	// Every section path is behind the operator session.
	for _, path := range []string{
		views.CampaignSectionPath(own, views.CampaignSectionTemplate),
		views.CampaignSectionPath(own, views.CampaignSectionActivity),
		views.TaskSectionPath(own, taskID, views.TaskSectionRecipients),
		views.TaskSectionPath(own, taskID, views.TaskSectionTest),
	} {
		rec := do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Errorf("anonymous %s = %d %q, want a login redirect", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
