package preparation

import (
	"errors"
	"strings"
	"testing"
)

// promptFixture is the captured input shape used by the validation tests.
func inputFixture() Input {
	return Input{
		Version:         InputVersion,
		ConfigurationID: "config",
		ContentHash:     "hash",
		SourceDigest:    "digest",
		Fields: []Field{
			{ID: "candidate_name", Header: "Applicant Full Name"},
			{ID: "department", Header: "Department"},
			{ID: "candidate_email", Header: "Email", Recipient: true},
		},
		Rows: []InputRow{
			{ID: "2", Values: map[string]string{
				"candidate_name":  "H.P. R OY",
				"department":      "PROJECT MANGMENT",
				"candidate_email": "a@example.com / B@example.com",
			}},
			{ID: "3", Values: map[string]string{
				"candidate_name":  "",
				"department":      "6lpa",
				"candidate_email": "not-an-address",
			}},
		},
	}
}

func validResponse() string {
	return `{"rows":[
		{"id":"2","values":{"candidate_name":"H.P. Roy","department":"Project Management","candidate_email":"a@example.com, b@example.com"},"corrections":[{"field":"candidate_name","note":"restored spacing"},{"field":"department","note":"corrected spelling"}]},
		{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"},"findings":[{"field":"department","scope":"shared","note":"compensation in a department column"},{"field":"candidate_email","scope":"recipient","position":1,"note":"not a usable address"}]}
	]}`
}

func TestParseResponseAcceptsCompleteStructuredOutput(t *testing.T) {
	input := inputFixture()
	rows, err := ParseResponse(validResponse(), input)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].SourceRow != 2 || rows[1].SourceRow != 3 {
		t.Fatalf("row order = %d,%d", rows[0].SourceRow, rows[1].SourceRow)
	}
	if len(rows[0].Corrections) != 2 {
		t.Fatalf("corrections = %d, want 2", len(rows[0].Corrections))
	}
	if len(rows[1].Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(rows[1].Findings))
	}
}

func TestParseResponseAcceptsOneFence(t *testing.T) {
	text := "```json\n" + validResponse() + "\n```"
	if _, err := ParseResponse(text, inputFixture()); err != nil {
		t.Fatalf("a single fenced response should pass: %v", err)
	}
}

func TestParseResponseRejectsMalformedOutput(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		reason string
	}{
		{
			name:   "unknown row",
			reason: "unknown row",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, B@example.com"}},
				{"id":"9","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "omitted row",
			reason: "omits row 3",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, B@example.com"}}]}`,
		},
		{
			name:   "repeated row",
			reason: "repeats row 2",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, B@example.com"}},
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, B@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "unknown field",
			reason: "unknown field",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com","extra":"x"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "omitted field",
			reason: "every mapped field",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "repeated field key",
			reason: "repeats a key",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com","candidate_email":"a@example.com, b@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "number value",
			reason: "not a string",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":42,"department":"B","candidate_email":"a@example.com, b@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "null value",
			reason: "missing or null",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":null,"department":"B","candidate_email":"a@example.com, b@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "unsupported scope",
			reason: "unsupported scope",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"},"findings":[{"field":"department","scope":"row","note":"x"}]},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "shared finding with position",
			reason: "must not name an address position",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"},"findings":[{"field":"department","scope":"shared","position":1,"note":"x"}]},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "recipient finding on another field",
			reason: "must name the recipient field",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"},"findings":[{"field":"department","scope":"recipient","position":1,"note":"x"}]},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "recipient position outside cell",
			reason: "outside the recipient cell",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"},"findings":[{"field":"candidate_email","scope":"recipient","position":3,"note":"x"}]},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "changed address",
			reason: "changed, fabricated or dropped",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, c@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "fabricated extra address",
			reason: "changed, fabricated or dropped",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com, c@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "dropped duplicate sibling",
			reason: "changed, fabricated or dropped",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com / B@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "dropped invalid sibling",
			reason: "changed, fabricated or dropped",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "dropped empty sibling",
			reason: "changed, fabricated or dropped",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"}},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":""}}]}`,
		},
		{
			name:   "unknown row property",
			reason: "unknown property",
			text: `{"rows":[
				{"id":"2","values":{"candidate_name":"A","department":"B","candidate_email":"a@example.com, b@example.com"},"rows":[]},
				{"id":"3","values":{"candidate_name":"","department":"6lpa","candidate_email":"not-an-address"}}]}`,
		},
		{
			name:   "prose around the object",
			reason: "not valid JSON",
			text:   "Here is the result:\n" + validResponse(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseResponse(test.text, inputFixture())
			if err == nil {
				t.Fatal("invalid response was accepted")
			}
			if !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("error = %q, want reason %q", err.Error(), test.reason)
			}
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("error %q does not wrap ErrInvalidResponse", err.Error())
			}
		})
	}
}

func TestRecipientOccurrencesPreserveOrderAndEmpties(t *testing.T) {
	tests := []struct {
		original   string
		normalized string
		want       bool
	}{
		{"a@example.com / B@example.com", "a@example.com, b@example.com", true},
		{"a@example.com, b@example.com", "A@example.com , B@example.com", true},
		{"a@example.com, a@example.com", "A@example.com, a@example.com", true},
		{"a@example.com, not-an-address", "a@example.com, Not-An-Address", true},
		{"a@example.com,,b@example.com", "a@example.com, , b@example.com", true},
		{"a@example.com; not-an-address", "a@example.com, not-an-address", true},
		{"a@example.com\nB@example.com", "a@example.com, b@example.com", true},
		{"a@example.com / B@example.com", "a@example.com, c@example.com", false},
		{"a@example.com, b@example.com", "a@example.com", false},
		{"a@example.com", "a@example.com, b@example.com", false},
		{"a@example.com / B@example.com", "a@example.com / B@example.com", false},
	}
	for _, test := range tests {
		if got := occurrencesPreserved(test.original, test.normalized); got != test.want {
			t.Fatalf("occurrencesPreserved(%q, %q) = %v, want %v", test.original, test.normalized, got, test.want)
		}
	}
}

func TestDeriveHoldsSharedAndRecipientFindings(t *testing.T) {
	input := inputFixture()
	rows, err := ParseResponse(validResponse(), input)
	if err != nil {
		t.Fatal(err)
	}
	output := Derive(input, rows)
	if output.Counts.SourceRows != 2 || output.Counts.Entries != 3 {
		t.Fatalf("counts = %+v", output.Counts)
	}
	if output.Counts.Accepted != 2 || output.Counts.Held != 1 {
		t.Fatalf("accepted/held = %d/%d, want 2/1", output.Counts.Accepted, output.Counts.Held)
	}
	byRow := map[int][]Entry{}
	for _, entry := range output.Entries {
		byRow[entry.SourceRow] = append(byRow[entry.SourceRow], entry)
	}
	row2 := byRow[2]
	if len(row2) != 2 || row2[0].Outcome != OutcomeAccepted || row2[1].Outcome != OutcomeAccepted {
		t.Fatalf("row 2 entries = %+v, want two accepted", row2)
	}
	row3 := byRow[3]
	if len(row3) != 1 {
		t.Fatalf("row 3 entries = %+v", row3)
	}
	if row3[0].Outcome != OutcomeHeld || len(row3[0].Reasons) != 2 {
		t.Fatalf("row 3 entry = %+v, want held with both reasons", row3[0])
	}
	if output.Counts.Corrections != 2 || output.Counts.Findings != 2 {
		t.Fatalf("note counts = %+v", output.Counts)
	}
}

func TestDeriveSharedFindingHoldsEverySibling(t *testing.T) {
	input := inputFixture()
	rows := []RowResult{{
		SourceRow: 2,
		Values: map[string]string{
			"candidate_name":  "H.P. Roy",
			"department":      "6lpa",
			"candidate_email": "a@example.com, b@example.com",
		},
		Findings: []Finding{{Row: 2, Field: "department", Header: "Department", Scope: ScopeShared, Note: "does not fit"}},
	}}
	output := Derive(input, rows)
	if output.Counts.Held != 2 || output.Counts.Accepted != 0 {
		t.Fatalf("counts = %+v, want both siblings held", output.Counts)
	}
}

func TestDeriveRecipientFindingHoldsOnlyAffectedAddress(t *testing.T) {
	input := inputFixture()
	rows := []RowResult{{
		SourceRow: 2,
		Values: map[string]string{
			"candidate_name":  "H.P. Roy",
			"department":      "Project Management",
			"candidate_email": "a@example.com, not-an-address",
		},
		Findings: []Finding{{Row: 2, Field: "candidate_email", Header: "Email", Scope: ScopeRecipient, Position: 2, Note: "uncertain address"}},
	}}
	output := Derive(input, rows)
	if output.Counts.Accepted != 1 || output.Counts.Held != 1 {
		t.Fatalf("counts = %+v, want one accepted and one held", output.Counts)
	}
	if output.Entries[0].Outcome != OutcomeAccepted || output.Entries[1].Outcome != OutcomeHeld {
		t.Fatalf("entries = %+v", output.Entries)
	}
}
