package migrations

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
)

func TestExpandMicrosoftCredentialOnExistingDatabase(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ClearBootstrap() })

	// Apply the original migrations first, just as on an existing deployment.
	const upgrade = "1792233030_expand_microsoft_credential.go"
	var original core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < upgrade {
			original.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, original).Up(); err != nil {
		t.Fatal(err)
	}
	collection, err := app.FindCollectionByNameOrId(microsoft.CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	if collection.Fields.GetByName("credential").(*core.TextField).Max != 0 {
		t.Fatal("original schema no longer reproduces the default 5000-character limit")
	}
	record := core.NewRecord(collection)
	record.Set("status", "connected")
	record.Set("credential", "v1.existing-ciphertext")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	collection, err = app.FindCollectionByNameOrId(microsoft.CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	if collection.Fields.GetByName("credential").(*core.TextField).Max != 1<<20 {
		t.Fatal("credential capacity was not expanded")
	}
	if collection.ListRule != nil || collection.ViewRule != nil || collection.CreateRule != nil || collection.UpdateRule != nil || collection.DeleteRule != nil {
		t.Fatal("migration opened access to the private connection collection")
	}
	saved, err := app.FindRecordById(microsoft.CollectionName, record.Id)
	if err != nil || saved.GetString("credential") != "v1.existing-ciphertext" {
		t.Fatalf("migration changed existing credentials (err=%v)", err)
	}
	saved.Set("credential", "v1."+strings.Repeat("x", 32000))
	if err := app.Save(saved); err != nil {
		t.Fatalf("expanded schema rejected a large credential: %v", err)
	}
	if err := app.RunAppMigrations(); err != nil {
		t.Fatalf("repeated migration run failed: %v", err)
	}
}
