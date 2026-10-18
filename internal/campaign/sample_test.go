package campaign

import (
	"errors"
	"strings"
	"testing"
)

// TestDocumentedSampleRenders pins the complete worked example in
// docs/campaign-templates.md: if the docs' sample or the documented
// normalization changes, this test must fail.
func TestDocumentedSampleRenders(t *testing.T) {
	const subject = `Welcome aboard, {{first_name}}`
	const body = `<h1>Welcome {{first_name}}</h1>
<p>Dear {{first_name}}, your title is {{job_title}}.</p>
<ul>
<li>Team: {{team}}</li>
<li>Reply to this email with questions</li>
</ul>
<p><strong>{{first_name}}</strong>, contact your manager for access.</p>`
	mappings := []Mapping{
		{Variable: "first_name", Header: "First Name", Required: true, Format: FormatName},
		{Variable: "job_title", Header: "Title", Required: true, Format: FormatDesignation},
		{Variable: "team", Header: "Team", Required: false, Fallback: "our team", Format: FormatText},
	}

	tmpl, err := Compile(subject, body, mappings)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	rendered, err := tmpl.Render(map[string]string{
		"first_name": "  ada   lovelace ",
		"job_title":  "head of qa",
		"team":       "",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	const wantSubject = "Welcome aboard, Ada Lovelace"
	if rendered.Subject != wantSubject {
		t.Errorf("subject = %q, want %q", rendered.Subject, wantSubject)
	}
	const wantBody = "<h1>Welcome Ada Lovelace</h1>\n" +
		"<p>Dear Ada Lovelace, your title is Head Of QA.</p>\n" +
		"<ul>\n" +
		"<li>Team: our team</li>\n" +
		"<li>Reply to this email with questions</li>\n" +
		"</ul>\n" +
		"<p><strong>Ada Lovelace</strong>, contact your manager for access.</p>"
	if rendered.Body != wantBody {
		t.Errorf("body = %q, want %q", rendered.Body, wantBody)
	}

	// Every replaced value appears in the body, and each raw value is
	// preserved next to its normalized replacement.
	for _, value := range []string{"Ada Lovelace", "Head Of QA", "our team"} {
		if !strings.Contains(rendered.Body, value) {
			t.Errorf("body does not contain replaced value %q", value)
		}
	}
	rawByVariable := map[string]string{}
	normalizedByVariable := map[string]string{}
	for _, value := range rendered.Values {
		rawByVariable[value.Variable] = value.Raw
		normalizedByVariable[value.Variable] = value.Normalized
	}
	if rawByVariable["first_name"] != "  ada   lovelace " {
		t.Errorf("raw first_name = %q, want the uploaded value preserved", rawByVariable["first_name"])
	}
	if normalizedByVariable["first_name"] != "Ada Lovelace" {
		t.Errorf("normalized first_name = %q, want %q", normalizedByVariable["first_name"], "Ada Lovelace")
	}
	// The empty optional value uses the documented fallback.
	if normalizedByVariable["team"] != "our team" {
		t.Errorf("normalized team = %q, want fallback %q", normalizedByVariable["team"], "our team")
	}

	// An empty required value holds only that row and names the variable.
	var missing *MissingRequiredError
	_, err = tmpl.Render(map[string]string{
		"first_name": "",
		"job_title":  "head of qa",
		"team":       "",
	})
	if !errors.As(err, &missing) {
		t.Fatalf("Render with empty first_name: err = %v, want MissingRequiredError", err)
	}
	if len(missing.Variables) != 1 || missing.Variables[0] != "first_name" {
		t.Errorf("missing variables = %v, want [first_name]", missing.Variables)
	}
}
