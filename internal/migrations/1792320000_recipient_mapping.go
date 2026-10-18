package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// Source headers only seed the authoring draft. Recipient selection belongs to
// each revision's mapping table; development data is not converted.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
		if err != nil {
			return err
		}
		collection.Fields.RemoveByName("recipient_header")
		collection.Fields.Add(&core.JSONField{Name: "source_headers", MaxSize: 64 << 10})
		return app.Save(collection)
	}, func(app core.App) error {
		return nil
	})
}
