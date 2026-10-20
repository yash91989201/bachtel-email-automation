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

// The revision form leads with the fields the operator fills in and keeps the
// mapping reference behind a disclosure. The reference content must stay
// reachable, and every help panel an input points at must exist.
func TestCampaignRevisionFormRevealsItsReferenceOnDemand(t *testing.T) {
	data := CampaignPageData{
		ID:              "camp1",
		Name:            "Test campaign",
		SourceHeaders:   "Candidate Name, Candidate Email",
		ExampleVariable: "candidate_name",
		ExampleHeader:   "Candidate Name",
		MappingDraft:    "candidate_name | Candidate Name | required | text",
	}
	var page strings.Builder
	if err := CampaignPage(data).Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(page.String()))
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]*html.Node{}
	var describeTargets []string
	var inspect func(*html.Node)
	inspect = func(node *html.Node) {
		attrs := map[string]string{}
		for _, attr := range node.Attr {
			attrs[attr.Key] = attr.Val
		}
		if id, ok := attrs["id"]; ok {
			byID[id] = node
		}
		if described, ok := attrs["aria-describedby"]; ok {
			describeTargets = append(describeTargets, strings.Fields(described)...)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
	}
	inspect(doc)

	for _, id := range []string{"revision-document", "revision-subject", "revision-mappings", "revision-mappings-hint", "revision-mappings-help", "revision-document-help", "revision-subject-help"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing element %q", id)
		}
	}
	for _, id := range describeTargets {
		if _, ok := byID[id]; !ok {
			t.Errorf("aria-describedby points at missing element %q", id)
		}
	}

	// The draft still reaches the textarea.
	if got := textOf(byID["revision-mappings"]); got != data.MappingDraft {
		t.Errorf("mapping draft = %q, want %q", got, data.MappingDraft)
	}

	// The mapping rules stay reachable through the help panel.
	hint := textOf(byID["revision-mappings-help"])
	for _, want := range []string{"candidate_email | Candidate Email | recipient | email", "recipient"} {
		if !strings.Contains(hint, want) {
			t.Errorf("mapping help %q does not mention %q", hint, want)
		}
	}

	// The content fields show a placeholder example that matches the draft.
	for _, id := range []string{"revision-subject-help", "revision-document-help"} {
		panel := textOf(byID[id])
		if want := "{{" + data.ExampleVariable + "}}"; !strings.Contains(panel, want) {
			t.Errorf("help panel %q does not show the placeholder example %q", panel, want)
		}
	}

	// The template tab shows the current revision, the mapping reference and
	// the revision form in that order. The first two are collapsed
	// accordions; the form stays open.
	revision := findCard(doc, "revision-heading")
	reference := findCard(doc, "mapping-reference-heading")
	form := findCard(doc, "upload-heading")
	for name, card := range map[string]*html.Node{"current revision": revision, "mapping reference": reference, "revision form": form} {
		if card == nil {
			t.Fatalf("the campaign page has no %s card", name)
		}
	}
	if revision.Data != "details" {
		t.Errorf("the current revision card is a %q, want a collapsed accordion", revision.Data)
	}
	if reference.Data != "details" {
		t.Errorf("the mapping reference card is a %q, want a collapsed accordion", reference.Data)
	}
	if form.Data != "section" {
		t.Errorf("the revision form card is a %q, want an always-open card", form.Data)
	}
	if reference.Parent != form.Parent || revision.Parent != form.Parent {
		t.Errorf("the reference card is not a sibling of the revision form card")
	}
	for node := byID["upload-heading"]; node != nil; node = node.Parent {
		if node == reference {
			t.Fatalf("the mapping reference is inside the revision form card")
		}
	}
	order := map[*html.Node]int{}
	index := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		order[node] = index
		index++
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if order[revision] > order[reference] || order[reference] > order[form] {
		t.Errorf("cards are out of order: revision=%d reference=%d form=%d", order[revision], order[reference], order[form])
	}
	for node := byID["revision-mappings"]; node != nil; node = node.Parent {
		if node == reference {
			t.Fatalf("mapping table is inside the reference card")
		}
	}
	referenceText := textOf(reference)
	for _, want := range []string{
		"Mapping reference",
		data.SourceHeaders,
		"Column order",
		"candidate_name | Candidate Name | required | text",
		"Revisions are immutable",
	} {
		if !strings.Contains(referenceText, want) {
			t.Errorf("reference card does not contain %q", want)
		}
	}
}

// findCard returns the card that holds the heading with the given id.
func findCard(node *html.Node, headingID string) *html.Node {
	if node.Type == html.ElementNode && node.Data == "h2" {
		for _, attr := range node.Attr {
			if attr.Key == "id" && attr.Val == headingID {
				for parent := node.Parent; parent != nil; parent = parent.Parent {
					if parent.Type == html.ElementNode && (parent.Data == "section" || parent.Data == "details") {
						return parent
					}
				}
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
