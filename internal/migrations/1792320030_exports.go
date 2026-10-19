package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Generated downloads carry their own durable state so a failed or interrupted
// generation can never masquerade as a finished file. One artifact exists per
// download role: a task-results workbook, a campaign report or a campaign ZIP.
// The unique (kind, campaign, task) index makes regeneration an update of the
// same record instead of a second competing download.
//
// The attempt sender is added here because historical HTML must show the
// sender recorded when the attempt was queued, not whatever mailbox is
// configured when the operator downloads it later.
func init() {
	m.Register(func(app core.App) error {
		attempts, err := app.FindCollectionByNameOrId("submission_attempts")
		if err != nil {
			return err
		}
		attempts.Fields.Add(&core.TextField{Name: "sender", Max: 320})
		if err = app.Save(attempts); err != nil {
			return err
		}

		campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
		if err != nil {
			return err
		}
		tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
		if err != nil {
			return err
		}

		artifacts := core.NewBaseCollection(exports.ArtifactsCollection)
		artifacts.Fields.Add(
			// task_results | campaign_report | campaign_package
			&core.TextField{Name: "kind", Required: true, Max: 32},
			&core.RelationField{Name: "campaign", Required: true, MaxSelect: 1, CollectionId: campaigns.Id},
			// Empty for campaign-level downloads, set for a task export.
			&core.RelationField{Name: "task", MaxSelect: 1, CollectionId: tasks.Id},
			// pending (interrupted recovery) | generating | ready | failed
			&core.TextField{Name: "state", Required: true, Max: 32},
			// Consistent snapshot time shown in the generated document.
			&core.DateField{Name: "as_of"},
			// Start of the current generation attempt, used to recover a
			// generation interrupted without a clean failure record.
			&core.DateField{Name: "generation_started"},
			// SHA-256 of the published file bytes.
			&core.TextField{Name: "digest", Max: 64},
			// Fixed operator-facing failure text.
			&core.TextField{Name: "error", Max: 500},
			// Private generated file. The collection rules stay nil, so only
			// this server's session-gated handlers can read it.
			&core.FileField{Name: "file", MaxSelect: 1, MaxSize: exports.MaxArtifactBytes},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		artifacts.AddIndex("idx_export_artifact_role", true, "kind, campaign, task", "")
		return app.Save(artifacts)
	}, func(app core.App) error {
		// Exports are derived data, but the schema is additive and a rollback
		// restores a database backup instead of deleting collections.
		return nil
	})
}
