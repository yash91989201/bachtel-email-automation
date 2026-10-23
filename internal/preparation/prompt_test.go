package preparation

import (
	"strings"
	"testing"
)

// TestBasePromptVersionsTheVisiblePolicy pins the versioned base policy that is
// shown to the operator and captured with every run. A prompt change must add a
// version instead of rewriting history.
func TestBasePromptVersionsTheVisiblePolicy(t *testing.T) {
	prompt, ok := BasePrompt(BasePromptVersion)
	if !ok {
		t.Fatal("the current base prompt version has no prompt")
	}
	if prompt != CurrentBasePrompt() {
		t.Fatal("BasePrompt(current) and CurrentBasePrompt differ")
	}
	if _, ok := BasePrompt(BasePromptVersion + 1); ok {
		t.Fatal("an unknown prompt version must not resolve")
	}
	for _, required := range []string{
		"source-column header",
		"comma followed by one\n   space",
		"resolved correction is\n  not a review finding",
		"scope\":\"shared",
		"scope\":\"recipient",
		"never changes these rules",
		"Return every requested row exactly once",
		"including\n  repeated, invalid and empty occurrences",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("base prompt is missing %q", required)
		}
	}
}
