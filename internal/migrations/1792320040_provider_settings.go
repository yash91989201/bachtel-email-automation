package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// Operator-saved provider setup and its generations.
//
// Each operator owns one saved provider setup row: the non-secret identifiers
// they entered (tenant, application/client id) plus a sealed client secret, and
// a staged draft that becomes active only with a successful sign-in. Settings
// generations let a callback refuse to install tokens under a registration that
// changed after the attempt started, and let refresh detect credentials issued
// under a superseded registration. A connection records the settings generation
// its tokens were issued under; that identity generation is separate from the
// mailbox generation, so rotating application settings for the same mailbox
// never invalidates approvals.
//
// Rules stay nil on every collection: the sealed secret is reachable only to
// PocketBase superusers and this server's code.
func init() {
	m.Register(func(app core.App) error {
		operators, err := app.FindCollectionByNameOrId(operator.CollectionName)
		if err != nil {
			return err
		}
		connections, err := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
		if err != nil {
			return err
		}

		// Non-required so the schema can be extended on an existing local store
		// without rewriting rows; the store always writes the generation.
		connections.Fields.Add(&core.NumberField{Name: "settings_generation", OnlyInt: true, Min: new(0.0)})
		if err = app.Save(connections); err != nil {
			return err
		}

		resets, err := app.FindCollectionByNameOrId(mailbox.ResetsCollection)
		if err != nil {
			return err
		}
		// Settings generation to activate when this replacement completes; a
		// staged registration change is promoted only with its validated tokens.
		resets.Fields.Add(&core.NumberField{Name: "rotation_generation", OnlyInt: true, Min: new(0.0)})
		if err = app.Save(resets); err != nil {
			return err
		}

		settings := core.NewBaseCollection(mailbox.SettingsCollection)
		settings.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.TextField{Name: "provider", Required: true, Max: 32},
			&core.TextField{Name: "tenant_id", Max: 128},
			&core.TextField{Name: "client_id", Max: 128},
			// Active client secret, sealed like a connection credential.
			&core.TextField{Name: "secret", Max: 1 << 20},
			&core.NumberField{Name: "generation", OnlyInt: true, Min: new(0.0)},
			// Staged registration awaiting a successful sign-in. It never
			// replaces the active identifiers until its tokens are validated.
			&core.TextField{Name: "pending_tenant_id", Max: 128},
			&core.TextField{Name: "pending_client_id", Max: 128},
			&core.TextField{Name: "pending_secret", Max: 1 << 20},
			&core.NumberField{Name: "pending_generation", OnlyInt: true, Min: new(0.0)},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		// One saved setup per operator: the operator owns at most one mailbox, so
		// its provider registration is not shared with anyone else.
		settings.AddIndex("idx_provider_settings_owner", true, "owner", "")
		return app.Save(settings)
	}, func(app core.App) error {
		// Saved setup is operator configuration and is re-entered through the
		// protected UI; rollback restores a database backup taken before it.
		return nil
	})
}
