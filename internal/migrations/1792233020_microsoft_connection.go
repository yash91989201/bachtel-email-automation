package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
)

// One record holds the single Microsoft connection. Its rules stay nil, so the
// record (and therefore the encrypted credential column) is reachable only to
// PocketBase superusers and to this application's server-side code.
func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection(microsoft.CollectionName)
		collection.Fields.Add(
			&core.TextField{Name: "status", Required: true, Max: 32},
			&core.TextField{Name: "account_id", Max: 255},
			&core.TextField{Name: "account_email", Max: 255},
			&core.TextField{Name: "tenant_id", Max: 255},
			&core.TextField{Name: "last_error", Max: 255},
			// Sealed credential material: versioned AES-256-GCM ciphertext.
			&core.TextField{Name: "credential"},
			&core.DateField{Name: "credential_expires_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId(microsoft.CollectionName)
		if err != nil {
			return err
		}
		return app.Delete(collection)
	})
}
