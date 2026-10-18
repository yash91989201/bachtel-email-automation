package campaign

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateMappings(t *testing.T) {
	t.Parallel()
	valid := Mapping{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName}
	tests := []struct {
		name     string
		mappings []Mapping
		wantErr  string
	}{
		{name: "none", mappings: nil, wantErr: "at least one variable mapping"},
		{name: "valid", mappings: []Mapping{valid, {Variable: "candidate_email", Header: "Email", Recipient: true}}},
		{
			name:     "duplicate variable",
			mappings: []Mapping{valid, {Variable: "candidate_name", Header: "Other", Required: true, Format: FormatText}},
			wantErr:  "mapped more than once",
		},
		{
			name:     "invalid variable name",
			mappings: []Mapping{{Variable: "1name", Header: "Header", Required: true, Format: FormatText}},
			wantErr:  "must start with a letter",
		},
		{
			name:     "missing header",
			mappings: []Mapping{{Variable: "location", Header: "  ", Required: true, Format: FormatText}},
			wantErr:  "needs a source header",
		},
		{
			name:     "unsupported format",
			mappings: []Mapping{{Variable: "location", Header: "Location", Required: true, Format: "upper"}},
			wantErr:  "unsupported formatting",
		},
		{
			name:     "fallback on required variable",
			mappings: []Mapping{{Variable: "location", Header: "Location", Required: true, Fallback: "Remote", Format: FormatText}},
			wantErr:  "cannot have a fallback",
		},
		{
			name:     "optional with fallback",
			mappings: []Mapping{{Variable: "location", Header: "Location", Fallback: "Remote", Format: FormatText}, {Variable: "candidate_email", Header: "Email", Recipient: true}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMappings(test.mappings)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}

	t.Run("trims variable and header", func(t *testing.T) {
		t.Parallel()
		mappings := []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail}, {Variable: " candidate_name ", Header: " Applicant Full Name ", Required: true, Format: FormatName}}
		if err := ValidateMappings(mappings); err != nil {
			t.Fatal(err)
		}
		if mappings[1].Variable != "candidate_name" || mappings[1].Header != "Applicant Full Name" {
			t.Fatalf("not trimmed: %+v", mappings[1])
		}
	})
}

func TestValidateResultLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		labels  []string
		wantErr string
	}{
		{name: "empty", wantErr: "at least one result column label"},
		{name: "valid", labels: []string{"Email Status", "Submitted At"}},
		{name: "duplicate", labels: []string{"Status", "Status"}, wantErr: "more than once"},
		{name: "blank entry", labels: []string{"Status", " "}, wantErr: "must not be empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateResultLabels(test.labels)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()
		got := NormalizeResultLabels(nil)
		if len(got) != len(DefaultResultLabels) {
			t.Fatalf("labels = %v, want the defaults %v", got, DefaultResultLabels)
		}
		for i, label := range DefaultResultLabels {
			if got[i] != label {
				t.Fatalf("labels = %v, want the defaults %v", got, DefaultResultLabels)
			}
		}
	})
}

func TestValidateSubject(t *testing.T) {
	t.Parallel()
	if err := ValidateSubject("Application update for {{candidate_name}}"); err != nil {
		t.Fatal(err)
	}
	for name, subject := range map[string]string{
		"empty":      "",
		"whitespace": "   ",
		"newline":    "Hi\r\nBcc: evil@example.com",
		"null":       "Hi\x00there",
		"too long":   strings.Repeat("a", MaxSubjectLength+1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateSubject(subject); err == nil {
				t.Fatalf("ValidateSubject(%q) accepted an invalid subject", subject)
			}
		})
	}
}

func TestContentHashScope(t *testing.T) {
	t.Parallel()
	document := []byte("docx-bytes")
	mappings := []Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatName}}
	base := ContentHash("Subject {{candidate_name}}", document, mappings, "pandoc 3.10")

	t.Run("stable", func(t *testing.T) {
		t.Parallel()
		again := ContentHash("Subject {{candidate_name}}", document, mappings, "pandoc 3.10")
		if base != again {
			t.Fatalf("hash is not stable: %q != %q", base, again)
		}
	})

	t.Run("mapping order is irrelevant", func(t *testing.T) {
		t.Parallel()
		reordered := []Mapping{
			{Variable: "location", Header: "Location", Fallback: "Remote", Format: FormatText},
			mappings[0],
		}
		if ContentHash("Subject {{candidate_name}}", document, reordered, "pandoc 3.10") == base {
			t.Fatal("adding a mapping did not change the hash")
		}
	})

	for name, changed := range map[string]string{
		"subject":    ContentHash("Other subject", document, mappings, "pandoc 3.10"),
		"document":   ContentHash("Subject {{candidate_name}}", []byte("other"), mappings, "pandoc 3.10"),
		"converter":  ContentHash("Subject {{candidate_name}}", document, mappings, "pandoc 3.11"),
		"formatting": ContentHash("Subject {{candidate_name}}", document, []Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: FormatText}}, "pandoc 3.10"),
		"fallback":   ContentHash("Subject {{candidate_name}}", document, []Mapping{{Variable: "candidate_name", Header: "Applicant Full Name", Fallback: "Ada", Format: FormatName}}, "pandoc 3.10"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if changed == base {
				t.Fatalf("changing the %s did not change the content hash", name)
			}
		})
	}
}

func TestSanitizerAllowlist(t *testing.T) {
	t.Parallel()
	policy := NewSanitizer()
	cleaned := policy.Sanitize(`<h1>Heading</h1><p><strong>Bold</strong> <em>Italic</em>
	<a href="https://example.com">ok</a> <a href="javascript:alert(1)">bad</a>
	<img src="https://example.com/x.png"><table><tr><td>cell</td></tr></table>
	<script>alert(1)</script></p>`)
	for _, want := range []string{"<h1>Heading</h1>", "<strong>Bold</strong>", "<em>Italic</em>", `href="https://example.com"`, "cell"} {
		if !strings.Contains(cleaned, want) {
			t.Fatalf("sanitized output %q dropped %q", cleaned, want)
		}
	}
	for _, unwanted := range []string{"<img", "<script", "javascript:", "<table", "<td"} {
		if strings.Contains(cleaned, unwanted) {
			t.Fatalf("sanitized output %q kept %q", cleaned, unwanted)
		}
	}
}

func TestApprovalInvalidated(t *testing.T) {
	t.Parallel()
	configuration := &Configuration{ContentHash: "hash-1"}
	if configuration.ApprovalCurrent() {
		t.Fatal("an unapproved revision reported a current approval")
	}
	configuration.ApprovedHash = "hash-0"
	if configuration.ApprovalCurrent() {
		t.Fatal("a stale approval hash reported a current approval")
	}
	configuration.ApprovedHash = "hash-1"
	if !configuration.ApprovalCurrent() {
		t.Fatal("a matching approval hash was not recognized")
	}
}

func TestSanitizerUnwrapsUnsupportedElements(t *testing.T) {
	t.Parallel()
	got := NewSanitizer().Sanitize("<div><p>kept</p><span>unwrapped</span></div>")
	if strings.Contains(got, "<div") || strings.Contains(got, "<span") {
		t.Fatalf("non-allowlisted elements survived: %q", got)
	}
	if !strings.Contains(got, "kept") || !strings.Contains(got, "unwrapped") {
		t.Fatalf("content was dropped instead of unwrapped: %q", got)
	}
}

// TestDefaultMappings covers the draft table derived from the expected
// recipient header: one required text row per header, underscore variable
// names, and no row a draft must not invent.
func TestDefaultMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   []string
	}{
		{
			name:   "delivered header list",
			header: "Candidate Name, Candidate Email, Expected Profile, Expected Department/Division, Expected Location",
			want: []string{
				"candidate_name | Candidate Name",
				"candidate_email | Candidate Email",
				"expected_profile | Expected Profile",
				"expected_department_division | Expected Department/Division",
				"expected_location | Expected Location",
			},
		},
		{
			name:   "surrounding and repeated separators",
			header: " ,  Full Name ,, Applicant Email  , ",
			want:   []string{"full_name | Full Name", "applicant_email | Applicant Email"},
		},
		{
			name:   "duplicate headers kept once",
			header: "Full Name, full  name, FULL NAME",
			want:   []string{"full_name | Full Name"},
		},
		{
			name:   "header without usable characters",
			header: "----, 2024 Region, /",
			want:   nil,
		},
		{
			name:   "empty header field",
			header: "",
			want:   nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mappings := DefaultMappings(strings.Split(test.header, ","))
			var got []string
			for _, mapping := range mappings {
				if mapping.Recipient || !mapping.Required || mapping.Format != FormatText || mapping.Fallback != "" {
					t.Fatalf("draft row %+v is not a required text mapping", mapping)
				}
				got = append(got, mapping.Variable+" | "+mapping.Header)
			}
			if strings.Join(got, "\n") != strings.Join(test.want, "\n") {
				t.Fatalf("DefaultMappings(%q) = %q, want %q", test.header, got, test.want)
			}
			if len(mappings) > 0 {
				if err := ValidateMappings(mappings); err == nil {
					t.Fatalf("draft from %q must require explicit recipient selection", test.header)
				}
			}
		})
	}
}

// TestUnknownVariableErrorNamesRemedy pins the operator-facing remedy in the
// unmapped-placeholder error: the document and the mapping table may disagree,
// so the message must say which side the operator can change.
func TestUnknownVariableErrorNamesRemedy(t *testing.T) {
	t.Parallel()
	_, err := Compile("Hello {{full_name}}", "<p>Hi</p>", []Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Required: true, Format: FormatEmail},
		{Variable: "candidate_name", Header: "Candidate Name", Required: true, Format: FormatText},
	})
	var unknown *UnknownVariableError
	if !errors.As(err, &unknown) {
		t.Fatalf("Compile error = %v, want UnknownVariableError", err)
	}
	message := err.Error()
	for _, want := range []string{
		"no mapping for variable(s): full_name",
		"Add a mapping row for each name",
		"rename the placeholder in the subject or body",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
}

// TestUnusedVariableErrorNamesRemedy pins the operator-facing remedy in the
// unused-mapping error: a row whose variable never appears in the content is
// the operator's to remove.
func TestUnusedVariableErrorNamesRemedy(t *testing.T) {
	t.Parallel()
	_, err := Compile("Hello {{candidate_name}}", "<p>Hi</p>", []Mapping{{Variable: "recipient", Header: "Email", Recipient: true, Format: FormatEmail},
		{Variable: "candidate_name", Header: "Candidate Name", Required: true, Format: FormatText},
		{Variable: "candidate_email", Header: "Candidate Email", Required: true, Format: FormatText},
	})
	var unused *UnusedVariableError
	if !errors.As(err, &unused) {
		t.Fatalf("Compile error = %v, want UnusedVariableError", err)
	}
	message := err.Error()
	for _, want := range []string{
		"configured variable(s) missing from the subject and body: candidate_email",
		"Remove each mapping row",
		"add its placeholder to the subject or body",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
}

func TestRecipientMappingValidationAndHash(t *testing.T) {
	t.Parallel()
	for name, mappings := range map[string][]Mapping{
		"missing":  {{Variable: "name", Header: "Name", Format: FormatText}},
		"multiple": {{Variable: "to", Header: "Email", Recipient: true}, {Variable: "other", Header: "Other", Recipient: true}},
		"format":   {{Variable: "to", Header: "Email", Recipient: true, Format: FormatText}},
		"fallback": {{Variable: "to", Header: "Email", Recipient: true, Fallback: "other@example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMappings(mappings); err == nil {
				t.Fatalf("accepted invalid recipient mappings: %+v", mappings)
			}
		})
	}
	mappings := []Mapping{{Variable: "to", Header: "Email", Required: true, Format: FormatEmail}}
	before := ContentHash("Welcome", []byte("docx"), mappings, "pandoc")
	mappings[0].Recipient = true
	if after := ContentHash("Welcome", []byte("docx"), mappings, "pandoc"); after == before {
		t.Fatal("recipient selection did not change content approval hash")
	}
}
