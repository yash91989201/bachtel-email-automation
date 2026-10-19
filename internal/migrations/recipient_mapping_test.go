package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

func TestRecipientMappingReplacesCampaignHeaderSchema(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	var original core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < "1792320000_recipient_mapping.go" {
			original.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, original).Up(); err != nil {
		t.Fatal(err)
	}
	collection, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("name", "Development campaign")
	record.Set("recipient_header", "Email, Name")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	collection, err = app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if collection.Fields.GetByName("recipient_header") != nil {
		t.Fatal("obsolete recipient_header field remains")
	}
	if _, ok := collection.Fields.GetByName("source_headers").(*core.JSONField); !ok {
		t.Fatal("source_headers is not a JSON list field")
	}
	if collection.ListRule != nil || collection.ViewRule != nil || collection.CreateRule != nil || collection.UpdateRule != nil || collection.DeleteRule != nil {
		t.Fatal("migration opened access to campaigns")
	}
	saved, err := app.FindRecordById(campaign.CampaignsCollection, record.Id)
	if err != nil || saved.GetString("name") != "Development campaign" {
		t.Fatalf("campaign unexpectedly removed: %v", err)
	}
	if len(saved.GetStringSlice("source_headers")) != 0 {
		t.Fatal("old recipient selection was converted to draft hints")
	}
}
