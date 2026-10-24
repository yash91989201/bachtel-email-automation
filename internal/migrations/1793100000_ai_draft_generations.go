package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
)

// Older installations required nonzero draft generations. An initial connection
// has no confirmed configuration or credential, so both base generations are
// legitimately zero. Updating the original migration only fixed fresh databases.
func init() {
	m.Register(func(app core.App) error {
		drafts, err := app.FindCollectionByNameOrId(ai.DraftsCollection)
		if err != nil {
			return err
		}
		for _, name := range []string{"base_configuration_generation", "base_credential_generation"} {
			field, ok := drafts.Fields.GetByName(name).(*core.NumberField)
			if !ok {
				return fmt.Errorf("AI draft field %q is missing or is not numeric", name)
			}
			// PocketBase's Required check treats zero as blank. Keep the
			// existing field identity, integer constraint and minimum value.
			field.Required = false
		}
		return app.Save(drafts)
	}, func(app core.App) error {
		// Requiring nonzero values again would invalidate initial drafts.
		// Restore a pre-upgrade database backup with its matching binary instead.
		return nil
	})
}
