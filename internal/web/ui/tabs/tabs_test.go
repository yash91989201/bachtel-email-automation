package tabs

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
)

// A vertical Tabs root must drive the whole tree: the list announces the
// orientation the arrow keys use, and every trigger and panel carries it too.
// A horizontal root keeps its markup as before, with no aria-orientation.
func TestTabsRenderTheirOrientation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		orientation Orientation
		orient      string
		ariaOrient  string
	}{
		{name: "vertical", orientation: OrientationVertical, orient: "vertical", ariaOrient: "vertical"},
		{name: "horizontal", orientation: OrientationHorizontal, orient: "horizontal", ariaOrient: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var page strings.Builder
			if err := tabsFixture(tc.orientation).Render(context.Background(), &page); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(page.String()))
			if err != nil {
				t.Fatal(err)
			}
			root := nodeByAttr(doc, "data-slot", "tabs")
			list := nodeByAttr(doc, "data-slot", "tabs-list")
			if root == nil || list == nil {
				t.Fatal("missing tabs root or list")
			}
			if got := tabAttr(root, "data-orientation"); got != tc.orient {
				t.Errorf("root data-orientation = %q, want %q", got, tc.orient)
			}
			if got := tabAttr(list, "data-orientation"); got != tc.orient {
				t.Errorf("list data-orientation = %q, want %q", got, tc.orient)
			}
			if got := tabAttr(list, "aria-orientation"); got != tc.ariaOrient {
				t.Errorf("list aria-orientation = %q, want %q", got, tc.ariaOrient)
			}
			triggers, panels := 0, 0
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				switch tabAttr(n, "data-slot") {
				case "tabs-trigger":
					triggers++
					if got := tabAttr(n, "data-orientation"); got != tc.orient {
						t.Errorf("trigger data-orientation = %q, want %q", got, tc.orient)
					}
				case "tabs-content":
					panels++
					if got := tabAttr(n, "data-orientation"); got != tc.orient {
						t.Errorf("panel data-orientation = %q, want %q", got, tc.orient)
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
			if triggers != 2 || panels != 2 {
				t.Errorf("triggers/panels = %d/%d, want 2/2", triggers, panels)
			}
		})
	}
}

// tabsFixture composes the primitive children in Go, so the test needs no
// generated test-only template in the package.
func tabsFixture(o Orientation) templ.Component {
	list := withChildren(List(ListProps{}),
		withChildren(Trigger(TriggerProps{Value: "one"}), text("One")),
		withChildren(Trigger(TriggerProps{Value: "two"}), text("Two")),
	)
	return withChildren(
		Tabs(Props{ID: "workspace", Orientation: o}),
		list,
		withChildren(Content(ContentProps{Value: "one"}), text("One panel")),
		withChildren(Content(ContentProps{Value: "two"}), text("Two panel")),
	)
}

func withChildren(component templ.Component, children ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return component.Render(templ.WithChildren(ctx, join(children...)), w)
	})
}

func join(components ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		for _, component := range components {
			if err := component.Render(ctx, w); err != nil {
				return err
			}
		}
		return nil
	})
}

func text(value string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, value)
		return err
	})
}

func nodeByAttr(n *html.Node, key, value string) *html.Node {
	if tabAttr(n, key) == value {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := nodeByAttr(child, key, value); found != nil {
			return found
		}
	}
	return nil
}

func tabAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}
