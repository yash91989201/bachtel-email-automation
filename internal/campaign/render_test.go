package campaign

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCompileValidation(t *testing.T) {
	t.Parallel()
	nameMapping := Mapping{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName}
	locationMapping := Mapping{Variable: "location", Header: "Location", Fallback: "Remote", Format: FormatText}

	tests := []struct {
		name     string
		subject  string
		body     string
		mappings []Mapping
		check    func(*testing.T, error)
	}{
		{
			name:     "unknown subject variable",
			subject:  "Update for {{candidate_name}} in {{office}}",
			body:     `<p>Hello {{candidate_name}}</p>`,
			mappings: []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, nameMapping},
			check: func(t *testing.T, err error) {
				var unknown *UnknownVariableError
				if !errors.As(err, &unknown) || len(unknown.Variables) != 1 || unknown.Variables[0] != "office" {
					t.Fatalf("error = %v, want an UnknownVariableError naming office", err)
				}
			},
		},
		{
			name:     "unknown body variable",
			subject:  "Update for {{candidate_name}}",
			body:     `<p>Hello {{candidate_name}}, you are {{rank}}</p>`,
			mappings: []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, nameMapping},
			check: func(t *testing.T, err error) {
				var unknown *UnknownVariableError
				if !errors.As(err, &unknown) || len(unknown.Variables) != 1 || unknown.Variables[0] != "rank" {
					t.Fatalf("error = %v, want an UnknownVariableError naming rank", err)
				}
			},
		},
		{
			name:     "configured variable used nowhere",
			subject:  "Update for {{candidate_name}}",
			body:     `<p>Hello {{candidate_name}}</p>`,
			mappings: []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, nameMapping, locationMapping},
			check: func(t *testing.T, err error) {
				var unused *UnusedVariableError
				if !errors.As(err, &unused) || len(unused.Variables) != 1 || unused.Variables[0] != "location" {
					t.Fatalf("error = %v, want an UnusedVariableError naming location", err)
				}
			},
		},
		{
			name:     "malformed placeholder in body",
			subject:  "Update for {{candidate_name}}",
			body:     `<p>Hello {{candidate name}}</p>`,
			mappings: []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, nameMapping},
			check: func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "malformed placeholder") {
					t.Fatalf("error = %v, want a malformed placeholder error", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			compiled, err := Compile(test.subject, test.body, test.mappings)
			if err == nil {
				t.Fatalf("Compile accepted an invalid configuration: %+v", compiled.Variables)
			}
			test.check(t, err)
		})
	}

	t.Run("subject only variable is valid", func(t *testing.T) {
		t.Parallel()
		compiled, err := Compile("Update for {{candidate_name}}", "<p>Dear candidate,</p>", []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, nameMapping})
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled.Variables) != 1 || !compiled.Variables[0].Subject || compiled.Variables[0].Body {
			t.Fatalf("variables = %+v, want a subject-only variable", compiled.Variables)
		}
	})
}

func TestRenderSubjectAndBody(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail},
		{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName},
		{Variable: "location", Header: "Location", Fallback: "Remote", Format: FormatText},
	}
	compiled, err := Compile(
		"Application for {{candidate_name}}",
		`<h1>Hello {{candidate_name}}</h1><p>Preferred location: <strong>{{location}}</strong>.</p>`,
		mappings,
	)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "  ada   lovelace ", "location": "London"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Subject != "Application for Ada Lovelace" {
		t.Fatalf("subject = %q", rendered.Subject)
	}
	for _, want := range []string{"<h1>Hello Ada Lovelace</h1>", "<strong>London</strong>"} {
		if !strings.Contains(rendered.Body, want) {
			t.Fatalf("body %q missing %q", rendered.Body, want)
		}
	}
	if len(rendered.Values) != 3 {
		t.Fatalf("values = %+v, want one entry per mapping", rendered.Values)
	}
	if rendered.Values[1].Raw != "  ada   lovelace " || rendered.Values[1].Normalized != "Ada Lovelace" {
		t.Fatalf("raw/normalized pair = %+v", rendered.Values[1])
	}
}

func TestRenderSplitRunPlaceholder(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName}}
	// Word splits the placeholder across two differently formatted runs.
	compiled, err := Compile(
		"Update for {{candidate_name}}",
		`<p>Dear <strong>{{cand</strong>idate_name}}, welcome.</p>`,
		mappings,
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.Body, "<strong>Ada</strong>") {
		t.Fatalf("body = %q, want the joined placeholder replaced inside its first run", rendered.Body)
	}
	if strings.Contains(rendered.Body, "idate_name") {
		t.Fatalf("body = %q, want the rest of the split placeholder removed", rendered.Body)
	}
}

func TestRenderIsInertData(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatText}}
	compiled, err := Compile(
		"Update for {{candidate_name}}",
		`<p>Hello {{candidate_name}}, location {{location}} and { braces }.</p>`,
		append(mappings, Mapping{Variable: "location", Header: "Location", Required: true, Format: FormatText}),
	)
	if err != nil {
		t.Fatal(err)
	}

	rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com",
		"candidate_name": `<script>alert(1)</script>`,
		"location":       `{{candidate_name}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.Body, "<script") {
		t.Fatalf("body executed uploaded markup: %q", rendered.Body)
	}
	if !strings.Contains(rendered.Body, "&lt;script&gt;") {
		t.Fatalf("body does not contain the escaped value: %q", rendered.Body)
	}
	if !strings.Contains(rendered.Body, "{{candidate_name}}") {
		t.Fatalf("body re-evaluated an uploaded template expression: %q", rendered.Body)
	}
	if !strings.Contains(rendered.Body, "{ braces }") {
		t.Fatalf("body lost literal braces: %q", rendered.Body)
	}
}

func TestRenderLiteralBracesSurvive(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatText}}
	compiled, err := Compile(
		"Update for {{candidate_name}}: cost is 5 {{ dollars",
		`<p>Braces {{ here, and closing braces }} here.</p>`,
		mappings,
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Subject != "Update for Ada: cost is 5 {{ dollars" {
		t.Fatalf("subject = %q, want the literal braces preserved", rendered.Subject)
	}
	if !strings.Contains(rendered.Body, "Braces {{ here, and closing braces }} here.") {
		t.Fatalf("body = %q, want literal braces preserved", rendered.Body)
	}
}

func TestRenderEligibility(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail},
		{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName},
		{Variable: "location", Header: "Location", Fallback: "Remote", Format: FormatText},
		{Variable: "team", Header: "Team", Format: FormatText},
	}
	compiled, err := Compile("Update for {{candidate_name}} {{location}}", "<p>{{candidate_name}} {{location}} {{team}}</p>", mappings)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("missing required value holds only this row", func(t *testing.T) {
		t.Parallel()
		_, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "   ", "location": "London"})
		var missing *MissingRequiredError
		if !errors.As(err, &missing) || len(missing.Variables) != 1 || missing.Variables[0] != "candidate_name" {
			t.Fatalf("error = %v, want a MissingRequiredError naming candidate_name", err)
		}
	})

	t.Run("optional fallback then empty string", func(t *testing.T) {
		t.Parallel()
		rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"})
		if err != nil {
			t.Fatal(err)
		}
		if rendered.Subject != "Update for Ada Remote" {
			t.Fatalf("subject = %q, want the configured fallback", rendered.Subject)
		}
		byVariable := map[string]string{}
		for _, value := range rendered.Values {
			byVariable[value.Variable] = value.Normalized
		}
		if byVariable["team"] != "" {
			t.Fatalf("optional value without fallback = %q, want an empty string", byVariable["team"])
		}
	})
}

func TestRenderSubjectControls(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatText}}
	compiled, err := Compile("Update for {{candidate_name}}", "<p>Hello {{candidate_name}}</p>", mappings)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("header controls make the row ineligible", func(t *testing.T) {
		t.Parallel()
		_, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada\x00Bcc: evil@example.com"})
		var controls *SubjectControlError
		if !errors.As(err, &controls) {
			t.Fatalf("error = %v, want a SubjectControlError", err)
		}
	})

	t.Run("line breaks are normalized away", func(t *testing.T) {
		t.Parallel()
		rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada\r\n  Lovelace"})
		if err != nil {
			t.Fatal(err)
		}
		if rendered.Subject != "Update for Ada Lovelace" {
			t.Fatalf("subject = %q", rendered.Subject)
		}
	})
}

func TestCompilePlaceholderInAttribute(t *testing.T) {
	t.Parallel()
	mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatText}}
	compiled, err := Compile(
		"Update for {{candidate_name}}",
		`<p><a href="https://example.com/{{candidate_name}}">link</a> Hello {{candidate_name}}</p>`,
		mappings,
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := compiled.Render(map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.Body, "Hello Ada") {
		t.Fatalf("body = %q, want the text placeholder replaced", rendered.Body)
	}
	if !strings.Contains(rendered.Body, "example.com/") {
		t.Fatalf("body = %q, want the link preserved", rendered.Body)
	}
	if strings.Contains(rendered.Body, `example.com/Ada`) {
		t.Fatalf("body = %q, want attribute placeholders left unsubstituted", rendered.Body)
	}
}

func TestRecipientMappingRendering(t *testing.T) {
	t.Parallel()
	for _, includePlaceholder := range []bool{false, true} {
		t.Run(fmt.Sprint(includePlaceholder), func(t *testing.T) {
			body := "<p>Welcome</p>"
			if includePlaceholder {
				body = "<p>Welcome {{candidate_email}}</p>"
			}
			mappings := []Mapping{{Variable: "candidate_email", Header: "Candidate Email", Recipient: true}}
			compiled, err := Compile("Welcome", body, mappings)
			if err != nil {
				t.Fatal(err)
			}
			if !mappings[0].Required || mappings[0].Format != FormatEmail {
				t.Fatalf("recipient defaults = %+v", mappings[0])
			}
			rendered, err := compiled.Render(map[string]string{"candidate_email": " ADA@EXAMPLE.COM "})
			if err != nil {
				t.Fatal(err)
			}
			if rendered.Recipient != "ada@example.com" || strings.Contains(rendered.Body, "ada@example.com") != includePlaceholder {
				t.Fatalf("recipient/content = %+v", rendered)
			}
			if _, ok := compiled.VarKeys()[""]; ok {
				t.Fatal("unused recipient created an empty template key")
			}
			for _, invalid := range []string{"not-email", "a@example.com,b@example.com", "Ada <ada@example.com>", "ada@exam\nple.com"} {
				_, err := compiled.Render(map[string]string{"candidate_email": invalid})
				if !errors.Is(err, ErrRecipientInvalid) {
					t.Fatalf("Render(%q) error = %v", invalid, err)
				}
			}
			_, err = compiled.Render(nil)
			var missing *MissingRequiredError
			if !errors.As(err, &missing) || strings.Join(missing.Variables, ",") != "candidate_email" {
				t.Fatalf("missing recipient error = %v", err)
			}
		})
	}
}
