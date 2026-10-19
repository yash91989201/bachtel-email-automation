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
	poll := workspaceNode(doc, "id", "sending-progress")
	if poll == nil {
		t.Fatal("missing polling region")
	}
	persistentState := false
	for n := poll.Parent; n != nil; n = n.Parent {
		if workspaceAttr(n, "x-data") != "" {
			persistentState = true
		}
	}
	if !persistentState {
		t.Fatal("tab state must outlive polling replacements")
	}
	if workspaceNode(poll, "x-bind:open", "guidanceOpen") == nil {
		t.Fatal("guidance disclosure must retain its state across polling")
	}
	if workspaceNode(poll, "x-data", "{tab:'test', guidanceOpen:false}") != nil {
		t.Fatal("polling must not reset the tab state")
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
}

func renderWorkspace(t *testing.T, component templ.Component) string {
	t.Helper()
	var page strings.Builder
	if err := component.Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	return page.String()
}

func assertWorkspaceTabs(t *testing.T, page, scope, initial string, count int) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	tabs, selected, visible := 0, 0, 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if workspaceAttr(n, "role") == "tab" {
			tabs++
			panel := workspaceNode(doc, "id", workspaceAttr(n, "aria-controls"))
			if panel == nil || workspaceAttr(panel, "aria-labelledby") != workspaceAttr(n, "id") {
				t.Errorf("tab %s has no matching labelled panel", workspaceAttr(n, "id"))
			}
			if workspaceAttr(n, "aria-selected") == "true" {
				selected++
				if workspaceAttr(n, "id") != scope+"-tab-"+initial {
					t.Error("incorrect initial tab")
				}
			}
		}
		if workspaceAttr(n, "role") == "tabpanel" {
			hidden := false
			for _, attr := range n.Attr {
				if attr.Key == "hidden" {
					hidden = true
				}
			}
			if !hidden {
				visible++
				if workspaceAttr(n, "id") != scope+"-panel-"+initial {
					t.Error("incorrect initial panel")
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
