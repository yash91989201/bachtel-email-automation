package main

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

func resolverFixture(t *testing.T) (core.App, *connectionResolver, string) {
	t.Helper()
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
	if err := operator.Provision(app, "operator@example.test", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, "operator@example.test")
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	mailboxes := mailbox.NewStore(app, func() ([]byte, error) { return key, nil })
	cfg := config.Config{Origin: "http://127.0.0.1:8090"}
	return app, &connectionResolver{cfg: cfg, connections: mailboxes, microsoft: microsoft.NewService(app, cfg, mailboxes)}, record.Id
}

// An unsupported provider is refused at resolution time: no sender is built and
// no connection or queue state changes.
func TestResolverFailsClosedForUnknownProvider(t *testing.T) {
	app, resolver, ownerID := resolverFixture(t)
	collection, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("owner", ownerID)
	record.Set("provider", "unknown-provider")
	record.Set("sender", "sender@example.test")
	record.Set("status", string(mailbox.StatusConnected))
	record.Set("generation", 1)
	record.Set("current", true)
	if err = app.Save(record); err != nil {
		t.Fatal(err)
	}

	if _, err = resolver.Sender(context.Background(), record.Id); !errors.Is(err, mailbox.ErrUnsupportedProvider) {
		t.Fatalf("resolver error = %v, want ErrUnsupportedProvider", err)
	}
	count, err := app.CountRecords(sending.AttemptsCollection)
	if err != nil || count != 0 {
		t.Fatalf("attempts = %d, %v; a refused provider must not queue work", count, err)
	}
}

// Titan readiness is independent of Microsoft application configuration.
func TestTitanResolutionDoesNotRequireMicrosoftConfiguration(t *testing.T) {
	app, resolver, ownerID := resolverFixture(t)
	mailboxes := mailbox.NewStore(app, func() ([]byte, error) {
		return []byte("0123456789abcdef0123456789abcdef"), nil
	})
	connection, err := mailboxes.Install(context.Background(), mailbox.Candidate{
		OwnerID:    ownerID,
		Provider:   mailbox.ProviderTitan,
		Sender:     "owner@example.test",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.cfg.Microsoft.Missing()) == 0 {
		t.Fatal("fixture must leave Microsoft settings incomplete")
	}
	sender, err := resolver.Sender(context.Background(), connection.ID)
	if err != nil {
		t.Fatalf("titan resolution with missing Microsoft settings: %v", err)
	}
	if sender == nil {
		t.Fatal("titan sender was not built")
	}
}
