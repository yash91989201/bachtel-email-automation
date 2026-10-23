package campaign_test

import (
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// New mapping tables carry no format choice, and a legacy table keeps its
// recorded format interpretation instead of being read as fallback text.
func TestMappingVersionSeparatesAuthoringFromHistory(t *testing.T) {
	legacy := []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
		{Variable: "candidate_name", Header: "Name", Required: true, Format: campaign.FormatName},
	}
	current := []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "candidate_name", Header: "Name", Required: true},
	}
	if err := campaign.ValidateMappingsForVersion(campaign.MappingVersionLegacy, legacy); err != nil {
		t.Fatalf("stored legacy table: %v", err)
	}
	if err := campaign.ValidateMappingsForVersion(campaign.CurrentMappingVersion, current); err != nil {
		t.Fatalf("current table: %v", err)
	}
	if err := campaign.ValidateMappingsForVersion(campaign.CurrentMappingVersion, legacy); err == nil {
		t.Fatal("a current table accepted a legacy format choice")
	}
	// The same text values render differently by version: legacy normalization
	// is historical evidence, current rendering preserves operator text.
	raw := map[string]string{"candidate_email": " ADA@EXAMPLE.COM ", "candidate_name": "  ada   lovelace  "}
	legacyTemplate, err := campaign.CompileForVersion(campaign.MappingVersionLegacy, "Hi {{candidate_name}}", "<p>{{candidate_name}}</p>", legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyRendered, err := legacyTemplate.Render(raw)
	if err != nil {
		t.Fatal(err)
	}
	if legacyRendered.Subject != "Hi Ada Lovelace" || legacyRendered.Recipient != "ada@example.com" {
		t.Fatalf("legacy render = %+v", legacyRendered)
	}
	currentTemplate, err := campaign.CompileForVersion(campaign.CurrentMappingVersion, "Hi {{candidate_name}}", "<p>{{candidate_name}}</p>", current)
	if err != nil {
		t.Fatal(err)
	}
	currentRendered, err := currentTemplate.Render(raw)
	if err != nil {
		t.Fatal(err)
	}
	if currentRendered.Subject != "Hi   ada   lovelace  " {
		t.Fatalf("current render rewrote the value: %q", currentRendered.Subject)
	}
	if currentRendered.Recipient != "ada@example.com" {
		t.Fatalf("recipient trim/lowercase = %q", currentRendered.Recipient)
	}
	// Version is part of the content identity; a fallback is literal text and
	// can never be confused with a stored format string.
	legacyHash := campaign.ContentHashForVersion(campaign.MappingVersionLegacy, "Hi {{candidate_name}}", []byte("doc"), current, "pandoc")
	currentHash := campaign.ContentHashForVersion(campaign.CurrentMappingVersion, "Hi {{candidate_name}}", []byte("doc"), current, "pandoc")
	if legacyHash == currentHash {
		t.Fatal("mapping versions shared a content identity")
	}
	optional := []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "role", Header: "Role", Fallback: "Team member"},
	}
	template, err := campaign.CompileForVersion(campaign.CurrentMappingVersion, "Hi", "<p>{{role}}</p>", optional)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := template.Render(map[string]string{"candidate_email": "ada@example.com", "role": "   "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.Body, "Team member") {
		t.Fatalf("literal fallback missing: %q", rendered.Body)
	}
}

// A current mapping table rejects a recipient fallback and requires exactly one
// recipient.
func TestCurrentMappingValidation(t *testing.T) {
	valid := []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "name", Header: "Name", Required: true},
	}
	if err := campaign.ValidateMappingsForVersion(campaign.CurrentMappingVersion, valid); err != nil {
		t.Fatal(err)
	}
	recipientFallback := append([]campaign.Mapping(nil), valid...)
	recipientFallback[0].Fallback = "fallback@example.com"
	if err := campaign.ValidateMappingsForVersion(campaign.CurrentMappingVersion, recipientFallback); err == nil {
		t.Fatal("recipient fallback was accepted")
	}
	noRecipient := append([]campaign.Mapping(nil), valid...)
	noRecipient[0].Recipient = false
	if err := campaign.ValidateMappingsForVersion(campaign.CurrentMappingVersion, noRecipient); err == nil {
		t.Fatal("a table without a recipient was accepted")
	}
}
