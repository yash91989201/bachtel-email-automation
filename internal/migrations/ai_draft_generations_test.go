package migrations

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

func TestAIDraftGenerationUpgrade(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})

	var original core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < "1793100000_ai_draft_generations.go" {
			original.Add(migration)
		}
	}
	if _, err := core.NewMigrationsRunner(app, original).Up(); err != nil {
		t.Fatal(err)
	}

	drafts, err := app.FindCollectionByNameOrId(ai.DraftsCollection)
	if err != nil {
		t.Fatal(err)
	}
	configurationField, ok := drafts.Fields.GetByName("base_configuration_generation").(*core.NumberField)
	if !ok {
		t.Fatal("base_configuration_generation is not a number field")
	}
	credentialField, ok := drafts.Fields.GetByName("base_credential_generation").(*core.NumberField)
	if !ok {
		t.Fatal("base_credential_generation is not a number field")
	}
	configurationField.Required = true
	credentialField.Required = true
	configurationFieldID := configurationField.Id
	credentialFieldID := credentialField.Id
	if err := app.Save(drafts); err != nil {
		t.Fatal(err)
	}

	ownerA := newOperator(t, app, "draft-a@example.test")
	ownerB := newOperator(t, app, "draft-b@example.test")
	prior := newAIDraft(t, app, ownerA, 4, 7)
	if err := app.Save(prior); err != nil {
		t.Fatal(err)
	}
	zeroDraft := newAIDraft(t, app, ownerB, 0, 0)
	if err := app.Save(zeroDraft); err == nil {
		t.Fatal("old required generation fields accepted an initial zero-generation draft")
	}

	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	drafts, err = app.FindCollectionByNameOrId(ai.DraftsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if drafts.ListRule != nil || drafts.ViewRule != nil || drafts.CreateRule != nil || drafts.UpdateRule != nil || drafts.DeleteRule != nil {
		t.Fatal("draft collection API rules changed")
	}
	indexes := strings.Join(drafts.Indexes, "\n")
	if !strings.Contains(indexes, "CREATE UNIQUE INDEX `idx_ai_connection_drafts_owner` ON `ai_connection_drafts` (owner)") {
		t.Fatalf("unique owner index was not preserved: %s", indexes)
	}

	for name, wantID := range map[string]string{
		"base_configuration_generation": configurationFieldID,
		"base_credential_generation":    credentialFieldID,
	} {
		field, ok := drafts.Fields.GetByName(name).(*core.NumberField)
		if !ok {
			t.Fatalf("%s is not a number field after upgrade", name)
		}
		if field.Id != wantID || field.Required || !field.OnlyInt || field.Min == nil || *field.Min != 0 {
			t.Fatalf("%s field changed unexpectedly: %+v", name, field)
		}
	}

	zeroDraft = newAIDraft(t, app, ownerB, 0, 0)
	if err := app.Save(zeroDraft); err != nil {
		t.Fatalf("zero-generation initial draft rejected after upgrade: %v", err)
	}
	for label, values := range map[string][2]float64{
		"negative configuration generation":   {-1, 0},
		"fractional configuration generation": {0.5, 0},
		"negative credential generation":      {0, -1},
		"fractional credential generation":    {0, 0.5},
	} {
		owner := newOperator(t, app, strings.ReplaceAll(label, " ", "-")+"@example.test")
		invalid := newAIDraft(t, app, owner, values[0], values[1])
		if err := app.Save(invalid); err == nil {
			t.Errorf("%s was accepted", label)
		}
	}

	savedPrior, err := app.FindRecordById(ai.DraftsCollection, prior.Id)
	if err != nil {
		t.Fatal(err)
	}
	if savedPrior.Id != prior.Id || savedPrior.GetString("credential") != "sealed-fixture-ciphertext" ||
		savedPrior.GetString("provider") != string(ai.ProviderOpenCodeGo) || savedPrior.GetString("api_root") != "https://provider.example.test/v1" ||
		savedPrior.GetString("api_format") != "responses" || savedPrior.GetString("model") != "fixture-model" ||
		savedPrior.GetFloat("base_configuration_generation") != 4 || savedPrior.GetFloat("base_credential_generation") != 7 {
		t.Fatal("existing nonzero AI draft changed during upgrade")
	}
}

func newAIDraft(t *testing.T, app core.App, owner string, configurationGeneration, credentialGeneration float64) *core.Record {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(ai.DraftsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("owner", owner)
	record.Set("provider", string(ai.ProviderOpenCodeGo))
	record.Set("api_root", "https://provider.example.test/v1")
	record.Set("api_format", "responses")
	record.Set("model", "fixture-model")
	record.Set("credential", "sealed-fixture-ciphertext")
	record.Set("base_configuration_generation", configurationGeneration)
	record.Set("base_credential_generation", credentialGeneration)
	record.Set("fingerprint", "fixture-fingerprint")
	return record
}
