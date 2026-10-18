package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId(microsoft.CollectionName)
		if err != nil {
			return err
		}
		field, ok := collection.Fields.GetByName("credential").(*core.TextField)
		if !ok {
			return fmt.Errorf("microsoft_connection.credential must be a text field")
		}
		// Max=0 means 5000 characters in PocketBase, not unlimited. Allow a
		// bounded 1 MiB envelope for encrypted, base64-encoded OAuth tokens.
		field.Max = 1 << 20
		return app.Save(collection)
	}, nil) // Keep the larger capacity on rollback to preserve usable credentials.
}
