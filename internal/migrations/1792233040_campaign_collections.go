package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// Campaigns own reusable email configuration. Rules stay nil on both
// collections, so every record is reachable only to PocketBase superusers and
// to this application's server-side code: candidate data, subject text and the
// uploaded DOCX never leave the process through the public API.
//
// Revisions are immutable by convention and by the absence of any update path:
// a change affecting personalized content writes a new campaign_configurations
// record. Only the campaign's current_configuration pointer and its output
// labels move forward. Output labels are deliberately not part of the content
// hash, so renaming a result column does not invalidate content approval.
func init() {
	m.Register(func(app core.App) error {
		// The campaign is saved first: PocketBase validates a relation target
		// when the collection holding the relation is saved, so each side of
		// the pair needs the other's generated id.
		campaigns := core.NewBaseCollection(campaign.CampaignsCollection)
		campaigns.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 200},
			// Expected source header for the recipient column. Change 3
			// compares it against the uploaded worksheet headers.
			&core.TextField{Name: "recipient_header", Required: true, Max: 255},
			// Output settings (downloadable column labels), snapshotted by a
			// task rather than covered by the content hash.
			&core.JSONField{Name: "result_labels", MaxSize: 4 << 10},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(campaigns); err != nil {
			return err
		}

		configurations := core.NewBaseCollection(campaign.ConfigurationsCollection)
		configurations.Fields.Add(
			&core.RelationField{Name: "campaign", Required: true, MaxSelect: 1, CollectionId: campaigns.Id},
			&core.NumberField{Name: "revision", Required: true},
			&core.TextField{Name: "subject", Required: true, Max: 998},
			// Private original document: a PocketBase-managed file that is
			// never publicly downloadable (collection rules are nil). The
			// uploaded package is verified by the converter's own inspection,
			// not by a sniffed mime type.
			&core.FileField{
				Name:      "docx",
				Required:  true,
				MaxSelect: 1,
				MaxSize:   campaign.MaxDocumentBytes,
			},
			// Sanitized semantic HTML, produced once per revision.
			&core.TextField{Name: "body_html"},
			// Compiled variable metadata and the mapping table.
			&core.JSONField{Name: "variables", MaxSize: 64 << 10},
			&core.JSONField{Name: "mappings", MaxSize: 64 << 10},
			// Identity of the personalized content: subject, document bytes,
			// mappings and converter identity. Never the output labels.
			&core.TextField{Name: "content_hash", Required: true, Max: 64},
			// Identity of the converter that produced body_html.
			&core.TextField{Name: "converter", Required: true, Max: 255},
			// Approval is bound to an exact content hash: an empty or different
			// hash means the revision is unapproved or was invalidated.
			&core.TextField{Name: "approved_hash", Max: 64},
			&core.DateField{Name: "approved_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(configurations); err != nil {
			return err
		}

		campaigns.Fields.Add(&core.RelationField{
			Name:         "current_configuration",
			MaxSelect:    1,
			CollectionId: configurations.Id,
		})
		return app.Save(campaigns)
	}, func(app core.App) error {
		// Revisions are referenced by later tasks; deleting them destroys
		// history. Rollback therefore restores a database backup taken before
		// the schema change instead of dropping collections, and this down
		// migration intentionally leaves the schema in place.
		return nil
	})
}
