package preparation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

// operationInput is one mapped sample: a recipient, a required name and an
// optional department, mirroring a campaign revision's captured fields.
func operationInput() Input {
	return Input{
		Version:     InputVersion,
		ContentHash: "content-hash",
		Fields: []Field{
			{ID: "candidate_email", Header: "Email", Recipient: true},
			{ID: "candidate_name", Header: "Applicant Full Name"},
			{ID: "department", Header: "Department"},
		},
		Rows: []InputRow{{ID: "1", Values: map[string]string{
			"candidate_email": "ada@example.com",
			"candidate_name":  "  ada   lovelace ",
			"department":      "",
		}}},
	}
}

// TestOperationPropagatesTheCapturedRequestContract proves the extracted
// operation is the only place the payload, prompt, instructions, inference
// options and session reach the provider.
func TestOperationPropagatesTheCapturedRequestContract(t *testing.T) {
	input := operationInput()
	captured := ai.Config{Provider: ai.ProviderCustom, APIRoot: "https://example.test/v1", APIFormat: ai.FormatChatCompletions, Model: "exact-model"}
	var gotConfig ai.Config
	var got ai.InferenceRequest
	op := operation{
		inference: func(_ context.Context, config ai.Config, request ai.InferenceRequest) (string, error) {
			gotConfig, got = config, request
			return `{"rows":[{"id":"1","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace","department":""},"corrections":[{"field":"candidate_name","note":"restored spacing"}],"findings":[{"field":"department","scope":"shared","note":"department is missing"}]}]}`, nil
		},
		config:       captured,
		prompt:       "base policy text",
		instructions: "address each candidate by first name",
		options:      RunOptions{BatchSize: 1, MaxTokens: 1234, Temperature: 0.25},
		session:      "session-1",
	}
	results, err := op.normalize(context.Background(), input, input.Rows)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if gotConfig != captured {
		t.Fatalf("inference config = %+v, want the captured %+v", gotConfig, captured)
	}
	if got.System != "base policy text" || got.MaxTokens != 1234 || got.Temperature != 0.25 || got.Session != "session-1" {
		t.Fatalf("inference request = %+v", got)
	}
	payload := got.User
	for _, want := range []string{
		`"instructions":"address each candidate by first name"`,
		`"id":"1"`,
		`"id":"candidate_email"`,
		`"header":"Applicant Full Name"`,
		`"recipient":true`,
		`"candidate_email":"ada@example.com"`,
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q: %s", want, payload)
		}
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	result := results[0]
	if result.SourceRow != 1 || result.Values["candidate_name"] != "Ada Lovelace" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Corrections) != 1 || result.Corrections[0].Field != "candidate_name" || result.Corrections[0].Header != "Applicant Full Name" {
		t.Fatalf("corrections = %+v, want the header resolved from the input", result.Corrections)
	}
	if len(result.Findings) != 1 || result.Findings[0].Header != "Department" {
		t.Fatalf("findings = %+v, want the header resolved from the input", result.Findings)
	}
	output := Derive(input, results)
	if output.Counts.Corrections != 1 || output.Counts.Findings != 1 {
		t.Fatalf("counts = %+v", output.Counts)
	}
	if len(output.Entries) != 1 || output.Entries[0].Outcome != OutcomeHeld {
		t.Fatalf("entries = %+v, want the shared finding to hold the sample row", output.Entries)
	}
}

// TestOperationRejectsInvalidResponses proves the shared operation keeps the
// complete response contract: omitted, duplicated, unknown and
// recipient-altering output never becomes prepared values.
func TestOperationRejectsInvalidResponses(t *testing.T) {
	input := operationInput()
	valid := `{"rows":[{"id":"1","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace","department":""}}]}`
	for _, tc := range []struct {
		name     string
		response string
	}{
		{name: "omitted row", response: `{"rows":[]}`},
		{name: "duplicated row", response: strings.TrimSuffix(valid, "]}}") + `,{"id":"1","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace","department":""}}]}`},
		{name: "unknown row", response: `{"rows":[{"id":"2","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace","department":""}}]}`},
		{name: "omitted field", response: `{"rows":[{"id":"1","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace"}}]}`},
		{name: "unknown field", response: `{"rows":[{"id":"1","values":{"candidate_email":"ada@example.com","candidate_name":"Ada Lovelace","department":"","status":"SUBMITTED"}}]}`},
		{name: "changed recipient", response: `{"rows":[{"id":"1","values":{"candidate_email":"grace@example.com","candidate_name":"Ada Lovelace","department":""}}]}`},
		{name: "fabricated recipient occurrence", response: `{"rows":[{"id":"1","values":{"candidate_email":"ada@example.com, grace@example.com","candidate_name":"Ada Lovelace","department":""}}]}`},
		{name: "prose instead of one object", response: "Here is the normalized data."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := operation{
				inference: func(context.Context, ai.Config, ai.InferenceRequest) (string, error) {
					return tc.response, nil
				},
				prompt: "base policy",
			}
			results, err := op.normalize(context.Background(), input, input.Rows)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err = %v, want ErrInvalidResponse", err)
			}
			if InvalidReason(err) == "" {
				t.Fatal("an invalid response must carry a fixed reason")
			}
			if results != nil {
				t.Fatalf("invalid response returned results: %+v", results)
			}
		})
	}
}

// TestOperationReturnsTheCallBoundaryError proves a provider failure stays
// classifiable and never becomes an invalid-response claim.
func TestOperationReturnsTheCallBoundaryError(t *testing.T) {
	input := operationInput()
	op := operation{
		inference: func(context.Context, ai.Config, ai.InferenceRequest) (string, error) {
			return "", ai.ErrThrottled
		},
		prompt: "base policy",
	}
	_, err := op.normalize(context.Background(), input, input.Rows)
	if !errors.Is(err, ai.ErrThrottled) {
		t.Fatalf("err = %v, want the provider error unchanged", err)
	}
	if errors.Is(err, ErrInvalidResponse) {
		t.Fatal("a provider failure must not be classified as an invalid response")
	}
}
