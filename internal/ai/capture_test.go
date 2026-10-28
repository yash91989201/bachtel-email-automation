package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

// confirmConnection stages and activates one connection for the owner.
func confirmConnection(t *testing.T, store *ai.Store, owner string, cfg ai.Config, apiKey string) *ai.Connection {
	t.Helper()
	ctx := context.Background()
	staged, err := store.StageValidation(ctx, owner, ai.Proposal{Config: cfg, APIKey: apiKey})
	if err != nil {
		t.Fatalf("stage connection: %v", err)
	}
	generation := 0
	if active, err := store.Connection(ctx, owner); err == nil {
		generation = active.ConfigurationGeneration
	}
	confirmed, err := store.Confirm(ctx, owner, staged.ID, generation)
	if err != nil {
		t.Fatalf("confirm connection: %v", err)
	}
	return confirmed
}

// TestCaptureKeepsConfigurationAndCredentialTogether proves one read yields the
// confirmed endpoint and its own key, so a request can never cross
// destinations.
func TestCaptureKeepsConfigurationAndCredentialTogether(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	first := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
	second := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	ctx := context.Background()

	confirmConnection(t, store, owner, customConfig(first.server.URL, "model-a", ai.FormatResponses), "first-key")
	connection, apiKey, err := store.Capture(ctx, owner)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if connection.Config.Model != "model-a" || connection.Config.APIRoot != first.server.URL+"/v1" || apiKey != "first-key" {
		t.Fatalf("captured %+v with key %q", connection.Config, apiKey)
	}
	if connection.ConfigurationGeneration == 0 || connection.CredentialGeneration == 0 {
		t.Fatalf("capture lost the generations: %+v", connection)
	}

	confirmConnection(t, store, owner, customConfig(second.server.URL, "model-b", ai.FormatResponses), "second-key")
	connection, apiKey, err = store.Capture(ctx, owner)
	if err != nil {
		t.Fatalf("capture after the switch: %v", err)
	}
	if connection.Config.Model != "model-b" || connection.Config.APIRoot != second.server.URL+"/v1" || apiKey != "second-key" {
		t.Fatalf("captured %+v with key %q after the switch", connection.Config, apiKey)
	}
	// The captured pair addresses exactly the new destination with its own key.
	firstRequests := first.requests.Load()
	if _, err := store.CompleteWith(ctx, connection.Config, apiKey, ai.InferenceRequest{System: "policy", User: "payload"}); err != nil {
		t.Fatalf("complete with the captured pair: %v", err)
	}
	if got := first.requests.Load(); got != firstRequests {
		t.Fatalf("the captured pair reached the previous endpoint: %d -> %d", firstRequests, got)
	}
	if last := second.auths[len(second.auths)-1]; last != "Bearer second-key" {
		t.Fatalf("new endpoint credential = %q, want the captured key", last)
	}
}

// TestCaptureRefusesMissingAndUnreadableCredentials proves an absent setup,
// another owner's setup and an unreadable secret never yield a usable pair.
func TestCaptureRefusesMissingAndUnreadableCredentials(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "operator@example.test")
	other := provision(t, app, "other@example.test")
	fake := newFakeEndpoint(t, respondProbe(t, ai.FormatResponses, probeOutput))
	store := newStore(t, app)
	ctx := context.Background()

	if _, _, err := store.Capture(ctx, other); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("capture for an unconfigured owner = %v, want ErrNotConfigured", err)
	}

	confirmConnection(t, store, owner, customConfig(fake.server.URL, "model-a", ai.FormatResponses), "test-key")
	if _, _, err := store.Capture(ctx, other); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("capture for another owner = %v, want ErrNotConfigured, not the configured owner's pair", err)
	}

	wrongKey := ai.NewStore(app, func() ([]byte, error) { return []byte("ffffffffffffffffffffffffffffffff"), nil })
	if _, _, err := wrongKey.Capture(ctx, owner); !errors.Is(err, ai.ErrCredentialUnreadable) {
		t.Fatalf("capture with the wrong deployment key = %v, want ErrCredentialUnreadable", err)
	}

	connection, err := store.Connection(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	record, err := app.FindRecordById(ai.ConnectionsCollection, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	tamper(t, app, record)
	if _, _, err := store.Capture(ctx, owner); !errors.Is(err, ai.ErrCredentialUnreadable) {
		t.Fatalf("capture of a tampered credential = %v, want ErrCredentialUnreadable", err)
	}
}

// tamper replaces the sealed credential with unusable ciphertext.
func tamper(t *testing.T, app core.App, record *core.Record) {
	t.Helper()
	record.Set("credential", "v1.zzzz")
	if err := app.Save(record); err != nil {
		t.Fatalf("tamper stored credential: %v", err)
	}
}
