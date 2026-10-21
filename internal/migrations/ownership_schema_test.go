package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

// The ownership invariants are database constraints, not application checks:
// a fresh database must reject a campaign without an owner, a second current
// connection for one operator, and a mailbox address claimed twice, while
// keeping the campaign-scoped candidate identity unique.
func TestOwnershipSchemaEnforcesItsInvariants(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}

	first := newOperator(t, app)

	campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	unowned := core.NewRecord(campaigns)
	unowned.Set("name", "Unowned")
	if err := app.Save(unowned); err == nil {
		t.Fatal("a campaign without an owner was accepted")
	}

	owned := core.NewRecord(campaigns)
	owned.Set("owner", first)
	owned.Set("name", "Owned")
	if err := app.Save(owned); err != nil {
		t.Fatalf("an owned campaign was rejected: %v", err)
	}

	connections, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	newConnection := func(owner, sender string, current bool) *core.Record {
		record := core.NewRecord(connections)
		record.Set("owner", owner)
		record.Set("provider", string(mailbox.ProviderTitan))
		record.Set("sender", sender)
		record.Set("status", string(mailbox.StatusConnected))
		record.Set("generation", 1)
		record.Set("current", current)
		return record
	}
	if err := app.Save(newConnection(first, "first@example.test", true)); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	if err := app.Save(newConnection(first, "second@example.test", true)); err == nil {
		t.Fatal("a second current connection for one operator was accepted")
	}

	second := newOperator(t, app, "second@example.test")
	duplicate := newConnection(second, "first@example.test", true)
	if err := app.Save(duplicate); err == nil {
		t.Fatal("a mailbox address was claimed by a second operator")
	}

	candidates, err := app.FindCollectionByNameOrId(intake.CandidatesCollection)
	if err != nil {
		t.Fatal(err)
	}
	candidate := func(emailKey string) *core.Record {
		record := core.NewRecord(candidates)
		record.Set("campaign", owned.Id)
		record.Set("email_key", emailKey)
		record.Set("submission_state", intake.SubmissionNone)
		return record
	}
	if err := app.Save(candidate("recipient@example.test")); err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	if err := app.Save(candidate("recipient@example.test")); err == nil {
		t.Fatal("campaign-scoped candidate identity uniqueness was not preserved")
	}
}
