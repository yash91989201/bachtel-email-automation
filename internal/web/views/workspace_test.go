package views

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

func TestTaskWorkspaceTabs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    TaskPageData
		tabs    int
		sending bool
	}{
		{name: "worksheet setup", data: TaskPageData{Status: "staging"}, tabs: 1},
		{name: "validated recipients", data: TaskPageData{Status: "ready", Counts: TaskCounts{Eligible: 1}, Rows: []TaskAdmittedRow{{ID: "member", Row: 2}}}, tabs: 4, sending: true},
		{name: "all held", data: TaskPageData{Status: "ready"}, tabs: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := renderWorkspace(t, TaskPage(tc.data))
			assertWorkspaceTabs(t, page, "task", "review", tc.tabs)
			if got := strings.Contains(page, "Continue to test & approval"); got != tc.sending {
				t.Errorf("sending action present=%t, want %t", got, tc.sending)
			}
		})
	}
}

func TestSendingWorkspaceKeepsTabStateOutsidePolling(t *testing.T) {
	page := renderWorkspace(t, SendingPage(SendingData{Base: "/sending", Status: "ready", Remaining: 2, Enabled: true, Approved: true}))
	assertWorkspaceTabs(t, page, "sending", "test", 6)
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	root := workspaceNode(doc, "data-slot", "tabs")
	poll := workspaceNode(doc, "id", "sending-progress")
	if root == nil || poll == nil {
		t.Fatal("missing the tabs root or the polling region")
	}
	if !isWorkspaceDescendant(root, poll) {
		t.Fatal("the polling region must stay inside the tabs root so its panels keep the active state")
	}
	// The triggers and the active state live outside the replaced region, so
	// a poll cannot reset the selected tab.
	for n := poll; n != nil; n = n.Parent {
		if workspaceAttr(n, "data-slot") == "tabs-trigger" {
			t.Fatal("triggers must stay outside the polling region")
		}
	}
	for _, id := range []string{"sending-panel-test", "sending-panel-send", "sending-panel-recovery", "sending-panel-recent"} {
		if workspaceNode(poll, "id", id) == nil {
			t.Errorf("dynamic panel %s missing from polling region", id)
		}
	}
	for _, id := range []string{"sending-panel-results", "sending-panel-pacing"} {
		if workspaceNode(poll, "id", id) != nil {
			t.Errorf("management panel %s must remain outside polling", id)
		}
	}
	// The guidance disclosure is owned by the collapsible primitive with a
	// stable id the adapter restores after each swap.
	guidance := workspaceNode(poll, "data-slot", "collapsible")
	trigger := workspaceNode(poll, "data-templ-controls", "sending-guidance")
	content := workspaceNode(poll, "id", "sending-guidance")
	if !hasWorkspaceAttr(guidance, "data-workspace-persist") {
		t.Fatal("guidance disclosure must be a collapsible root marked for restoration")
	}
	if trigger == nil || trigger.Data != "button" || workspaceAttr(trigger, "aria-expanded") != "false" {
		t.Fatal("guidance must use the collapsible trigger")
	}
	if content == nil || !hasWorkspaceAttr(content, "data-closed") {
		t.Fatal("guidance content must start closed")
	}
	if workspaceNode(poll, "x-bind:open", "guidanceOpen") != nil || workspaceNode(poll, "x-data", "{tab:'test', guidanceOpen:false}") != nil {
		t.Fatal("polling must not own tab or disclosure state")
	}
	if workspaceNode(poll, "data-workspace-hash-link", "") == nil {
		t.Error("the refresh link must keep the selected section through the adapter")
	}
}

// The polling fragment renders only the panels. It must keep the full page's
// panel ids, label pairing and orientation without rendering the tabs root;
// hidden panels are expected until the adapter re-applies the active tab.
func TestSendingProgressFragmentKeepsPanelIdentity(t *testing.T) {
	page := renderWorkspace(t, SendingProgress(SendingData{Base: "/sending", Status: "ready"}))
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"test", "send", "recovery", "recent"} {
		panel := workspaceNode(doc, "id", "sending-panel-"+value)
		if panel == nil {
			t.Fatalf("fragment panel %q lost its id", value)
		}
		if got := workspaceAttr(panel, "aria-labelledby"); got != "sending-tab-"+value {
			t.Errorf("panel %q labelled by %q", value, got)
		}
		if got := workspaceAttr(panel, "data-orientation"); got != "vertical" {
			t.Errorf("panel %q orientation = %q, want vertical", value, got)
		}
		if !hasWorkspaceAttr(panel, "data-hidden") {
			t.Errorf("panel %q must stay hidden until the adapter re-applies the active tab", value)
		}
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
