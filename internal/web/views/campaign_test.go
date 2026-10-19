package views

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestCampaignTabsExposeTheRelevantPanel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    CampaignPageData
		visible string
		tabs    int
	}{
		{name: "draft has no preview tab", visible: "template", tabs: 3},
		{name: "revision error opens template", data: CampaignPageData{HasRevision: true, RevisionError: "Invalid mapping"}, visible: "template", tabs: 4},
		{name: "label error opens settings", data: CampaignPageData{HasRevision: true, LabelsError: "Duplicate labels"}, visible: "settings", tabs: 4},
		{name: "deletion error opens settings", data: CampaignPageData{HasRevision: true, DeleteError: "Campaign is active"}, visible: "settings", tabs: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var page strings.Builder
			if err := CampaignPage(tc.data).Render(context.Background(), &page); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(page.String()))
			if err != nil {
				t.Fatal(err)
			}
			tabs, selected, visible := 0, 0, 0
			var inspect func(*html.Node)
			inspect = func(node *html.Node) {
				attrs := make(map[string]string, len(node.Attr))
				for _, attr := range node.Attr {
					attrs[attr.Key] = attr.Val
				}
				if attrs["role"] == "tab" {
					tabs++
					if attrs["aria-selected"] == "true" {
						selected++
						if attrs["aria-controls"] != "campaign-panel-"+tc.visible {
							t.Errorf("selected tab controls %q", attrs["aria-controls"])
						}
					}
				}
				if _, hidden := attrs["hidden"]; attrs["role"] == "tabpanel" && !hidden {
					visible++
					if attrs["id"] != "campaign-panel-"+tc.visible || attrs["aria-labelledby"] != "campaign-tab-"+tc.visible {
						t.Errorf("wrong visible panel: %v", attrs)
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					inspect(child)
				}
			}
			inspect(doc)
			if tabs != tc.tabs || selected != 1 || visible != 1 {
				t.Fatalf("tabs=%d selected=%d visible=%d, want %d/1/1", tabs, selected, visible, tc.tabs)
			}
		})
	}
}
