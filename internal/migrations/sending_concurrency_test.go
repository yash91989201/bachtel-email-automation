package migrations

import (
	"context"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

type legacyMailbox struct {
	owner, sender, connection string
	rate                      int
	starts                    []time.Time
	cooldown                  time.Time
}

// The concurrency migration adds one whole-number setting to existing mailbox
// state. An upgrade must keep every saved rate, pacing start, next eligible
// time, cooldown and reason, and fill only the new field with the default 2. A
// fresh database creates new rows with concurrency 2 and pacing 15. Rollback
// removes only the new field.
func TestSendingConcurrencyMigrationPreservesSavedPacing(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	var before core.MigrationsList
	const migrationPrefix = "1793300000"
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < migrationPrefix {
			before.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, before).Up(); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	fixtures := []*legacyMailbox{
		{
			owner:    newOperator(t, app, "first@example.test"),
			sender:   "first@example.test",
			rate:     25,
			starts:   []time.Time{now.Add(-2 * time.Minute), now.Add(-90 * time.Second)},
			cooldown: now.Add(7 * time.Minute),
		},
		{
			owner:  newOperator(t, app, "second@example.test"),
			sender: "second@example.test",
			rate:   7,
			starts: []time.Time{now.Add(-30 * time.Second)},
		},
	}
	connections, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.FindCollectionByNameOrId("dispatcher_state")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		connection := core.NewRecord(connections)
		connection.Set("owner", fixture.owner)
		connection.Set("provider", string(mailbox.ProviderTitan))
		connection.Set("sender", fixture.sender)
		connection.Set("status", string(mailbox.StatusConnected))
		connection.Set("generation", 1)
		connection.Set("current", true)
		if err = app.Save(connection); err != nil {
			t.Fatalf("connection %s: %v", fixture.sender, err)
		}
		fixture.connection = connection.Id
		record := core.NewRecord(state)
		record.Set("connection", connection.Id)
		record.Set("owner", fixture.owner)
		record.Set("rate", fixture.rate)
		record.Set("starts", fixture.starts)
		record.Set("next_eligible", now.Add(time.Minute))
		record.Set("cooldown_until", fixture.cooldown)
		record.Set("reason", "Provider throttling or safe retry wait")
		if err = app.Save(record); err != nil {
			t.Fatalf("dispatcher state %s: %v", fixture.sender, err)
		}
	}

	if err = app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}

	state, err = app.FindCollectionByNameOrId("dispatcher_state")
	if err != nil {
		t.Fatal(err)
	}
	field, ok := state.Fields.GetByName("concurrency").(*core.NumberField)
	if !ok || !field.Required || !field.OnlyInt || field.Min == nil || *field.Min != 1 || field.Max == nil || *field.Max != 10 {
		t.Fatalf("concurrency field = %#v, want a required integer from 1 through 10", state.Fields.GetByName("concurrency"))
	}

	store := sending.NewStore(app, sending.Options{Now: func() time.Time { return now }})
	for _, fixture := range fixtures {
		saved, stateErr := store.DispatcherState(context.Background(), fixture.connection, fixture.owner)
		if stateErr != nil {
			t.Fatalf("upgraded state %s: %v", fixture.owner, stateErr)
		}
		if saved.Rate != fixture.rate {
			t.Fatalf("upgrade changed a saved rate: got %d want %d", saved.Rate, fixture.rate)
		}
		if saved.Concurrency != sending.DefaultConcurrency {
			t.Fatalf("upgraded concurrency = %d, want %d", saved.Concurrency, sending.DefaultConcurrency)
		}
		if len(saved.Starts) != len(fixture.starts) || !saved.Starts[0].Equal(fixture.starts[0].UTC()) {
			t.Fatalf("upgrade lost pacing history: %+v", saved.Starts)
		}
		if !saved.CooldownUntil.Equal(fixture.cooldown) || !saved.NextEligible.Equal(now.Add(time.Minute)) {
			t.Fatalf("upgrade lost a wait: %+v", saved)
		}
		if saved.Reason != "Provider throttling or safe retry wait" {
			t.Fatalf("upgrade lost the cooldown reason: %q", saved.Reason)
		}
	}

	// Rollback removes only the new setting; rates and history stay readable.
	if _, err = core.NewMigrationsRunner(app, core.AppMigrations).Down(1); err != nil {
		t.Fatal(err)
	}
	state, err = app.FindCollectionByNameOrId("dispatcher_state")
	if err != nil {
		t.Fatal(err)
	}
	if state.Fields.GetByName("concurrency") != nil {
		t.Fatal("rollback kept the concurrency field")
	}
	for _, fixture := range fixtures {
		var kept struct {
			Rate   int    `db:"rate"`
			Starts string `db:"starts"`
		}
		if err = app.DB().NewQuery("SELECT rate, starts FROM dispatcher_state WHERE connection = {:connection}").
			Bind(dbx.Params{"connection": fixture.connection}).
			One(&kept); err != nil {
			t.Fatal(err)
		}
		if kept.Rate != fixture.rate || kept.Starts == "" {
			t.Fatalf("rollback changed saved state: %+v", kept)
		}
	}
}

// A fresh database creates mailbox state with the new defaults instead of
// inheriting a backfilled value.
func TestSendingConcurrencyDefaultsOnFreshDatabase(t *testing.T) {
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

	owner := newOperator(t, app)
	connections, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
	if err != nil {
		t.Fatal(err)
	}
	connection := core.NewRecord(connections)
	connection.Set("owner", owner)
	connection.Set("provider", string(mailbox.ProviderTitan))
	connection.Set("sender", "fresh@example.test")
	connection.Set("status", string(mailbox.StatusConnected))
	connection.Set("generation", 1)
	connection.Set("current", true)
	if err = app.Save(connection); err != nil {
		t.Fatal(err)
	}

	saved, err := sending.NewStore(app, sending.Options{}).DispatcherState(context.Background(), connection.Id, owner)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Concurrency != 2 || saved.Rate != 15 {
		t.Fatalf("fresh settings = %d concurrent, %d per minute; want 2 and 15", saved.Concurrency, saved.Rate)
	}
}
