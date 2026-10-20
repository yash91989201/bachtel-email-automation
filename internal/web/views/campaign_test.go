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

// The sample preview shows its result only after the operator renders one, and
// the render button stays disabled while a required sample value is missing.
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
				ID: "camp1", Name: "Test campaign", HasRevision: true, Variables: tc.variables,
			}))
			form := workspaceNode(doc, "action", "/campaigns/camp1/preview")
			if form == nil || form.Data != "form" {
				t.Fatal("the campaign page has no sample preview form")
			}
			if workspaceAttr(form, "x-data") != previewReadyState || workspaceAttr(form, "x-init") != "sync()" || workspaceAttr(form, "x-on:input") != "sync()" {
				t.Errorf("preview form lost its required-value gate: x-data=%q x-init=%q input=%q", workspaceAttr(form, "x-data"), workspaceAttr(form, "x-init"), workspaceAttr(form, "x-on:input"))
			}
			if workspaceAttr(form, "hx-target") != "#preview-result" || workspaceAttr(form, "hx-swap") != "innerHTML" {
				t.Error("the preview must stay an in-place swap into its own result region")
			}

			// Nothing renders before the operator asks for a preview.
			result := workspaceNode(doc, "id", "preview-result")
			if result == nil {
				t.Fatal("preview result region is missing")
			}
			if result.FirstChild != nil {
				t.Errorf("preview result must start empty, got %q", viewText(result))
			}

			// Every required input says so and carries the constraint the gate reads.
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
				if label := viewText(fieldLabel); variable.Required && !strings.Contains(label, "(required)") {
					t.Errorf("label %q does not mark the variable required", label)
				}
			}

			render := findViewNode(form, func(n *html.Node) bool {
				return n.Data == "button" && strings.TrimSpace(viewText(n)) == "Render preview"
			})
			if render == nil || workspaceAttr(render, "type") != "submit" {
				t.Fatal("missing the render preview submit button")
			}
			if got := workspaceAttr(render, "x-bind:disabled"); got != "!ready" {
				t.Errorf("render button x-bind:disabled = %q, want %q", got, "!ready")
			}
			if got := hasViewAttr(render, "disabled"); got != tc.wantDisabled {
				t.Errorf("render button disabled=%t, want %t", got, tc.wantDisabled)
			}
		})
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
	// Closing dismisses the rendered content too, not only the heading.
	if workspaceNode(card, "class", "email-body rounded-card border border-border p-4") == nil {
		t.Error("the rendered body sits outside the dismissible card")
	}
	for _, want := range []string{"Preview result", "candidate@example.com", "Hello", "Hi there"} {
		if !strings.Contains(viewText(card), want) {
			t.Errorf("preview card is missing %q", want)
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
