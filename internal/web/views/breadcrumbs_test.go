package views

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

func TestPageBreadcrumbs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		page    templ.Component
		current string
		links   []string
	}{
		{"campaign list", CampaignList(CampaignListData{}), "Campaigns", []string{"/"}},
		{"campaign", CampaignPage(CampaignPageData{ID: "camp1", Name: "Hiring"}), "Hiring", []string{"/campaigns"}},
		{"task workspace", TaskPage(TaskPageData{CampaignID: "camp1", CampaignName: "Hiring", Name: "October outreach", Filename: "recipients.xlsx", Reference: "abcd1234", StoredStatus: "ready"}), "October outreach", []string{"/campaigns", "/campaigns/camp1", "/campaigns/camp1/tasks"}},
		{"upload", TaskUpload(TaskUploadData{CampaignID: "camp1", CampaignName: "Hiring"}), "Upload workbook", []string{"/campaigns", "/campaigns/camp1", "/campaigns/camp1/tasks"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rendered strings.Builder
			if err := tc.page.Render(context.Background(), &rendered); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(rendered.String()))
			if err != nil {
				t.Fatal(err)
			}
			nav := workspaceNode(doc, "data-slot", "breadcrumb")
			if nav == nil || nav.Data != "nav" || workspaceAttr(nav, "aria-label") != "breadcrumb" {
				t.Fatal("missing labelled breadcrumb navigation")
			}
			current := workspaceNode(nav, "aria-current", "page")
			if current == nil || current.FirstChild == nil || strings.TrimSpace(current.FirstChild.Data) != tc.current {
				t.Fatalf("missing current breadcrumb %q", tc.current)
			}
			var links []string
			for n := range nav.Descendants() {
				if n.Type == html.ElementNode && n.Data == "a" {
					links = append(links, workspaceAttr(n, "href"))
				}
			}
			if strings.Join(links, "|") != strings.Join(tc.links, "|") {
				t.Errorf("breadcrumb links = %v, want %v", links, tc.links)
			}
			if strings.Index(rendered.String(), `data-slot="breadcrumb"`) > strings.Index(rendered.String(), "<h1") {
				t.Error("breadcrumbs must precede the page heading")
			}
		})
	}
}
