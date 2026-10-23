package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Immutable final sending snapshots and exact-snapshot approvals.
//
//   - campaign_configurations records the mapping-table version, so a stored
//     legacy format value can never be misread as current fallback text.
//   - final_snapshots holds one immutable accepted dataset per finalization:
//     the accepted entries, their deterministic sending sheet, the exclusion
//     evidence, the original edited upload and the digest that binds approvals.
//   - send_tasks gains the selected snapshot pointer and its digest. A
//     replacement upload always creates a new snapshot identity, even for
//     identical bytes.
//   - submission_attempts and test_approvals record the exact snapshot and
//     digest they observed, so approval can never be reused across tasks or
//     snapshots. The legacy approval index is replaced; old records stay as
//     historical evidence.
//
// Rules stay nil: snapshot evidence and approvals are reachable only to
// PocketBase superusers and this server's code.
func init() {
	m.Register(func(app core.App) error {
		operators, err := app.FindCollectionByNameOrId(operator.CollectionName)
		if err != nil {
			return err
		}
		tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
		if err != nil {
			return err
		}
		campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
		if err != nil {
			return err
		}
		configurations, err := app.FindCollectionByNameOrId(campaign.ConfigurationsCollection)
		if err != nil {
			return err
		}
		runs, err := app.FindCollectionByNameOrId(preparation.RunsCollection)
		if err != nil {
			return err
		}

		configurations.Fields.Add(
			// 0/1 are legacy format-bearing tables; 2 is the current
			// role/fallback table.
			&core.NumberField{Name: "mapping_version", OnlyInt: true, Min: new(0.0)},
		)
		if err := app.Save(configurations); err != nil {
			return err
		}

		snapshots := core.NewBaseCollection(finalization.SnapshotsCollection)
		snapshots.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			&core.RelationField{Name: "campaign", Required: true, MaxSelect: 1, CollectionId: campaigns.Id},
			&core.RelationField{Name: "configuration", Required: true, MaxSelect: 1, CollectionId: configurations.Id},
			// The completed preparation run for an Approve as-is snapshot;
			// empty for an edited final upload.
			&core.RelationField{Name: "run", MaxSelect: 1, CollectionId: runs.Id},
			// prepared | upload.
			&core.TextField{Name: "kind", Required: true, Max: 16},
			// Idempotency identity of the finalization action.
			&core.TextField{Name: "action_id", Required: true, Max: 128},
			&core.TextField{Name: "actor", Max: 255},
			&core.TextField{Name: "content_hash", Max: 64},
			// Digest of the accepted mapped values. Approvals bind to it.
			&core.TextField{Name: "digest", Required: true, Max: 64},
			// Digest of the source run input or uploaded final workbook.
			&core.TextField{Name: "source_digest", Max: 64},
			&core.JSONField{Name: "counts", MaxSize: 4 << 10},
			&core.FileField{Name: "entries", MaxSelect: 1, MaxSize: finalization.MaxArtifactBytes},
			&core.FileField{Name: "final_sheet", MaxSelect: 1, MaxSize: finalization.MaxArtifactBytes},
			&core.FileField{Name: "exclusions", MaxSelect: 1, MaxSize: finalization.MaxArtifactBytes},
			&core.FileField{Name: "exclusion_entries", MaxSelect: 1, MaxSize: finalization.MaxArtifactBytes},
			&core.FileField{Name: "upload", MaxSelect: 1, MaxSize: finalization.MaxArtifactBytes},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		snapshots.AddIndex("idx_final_snapshots_action", true, "task, action_id", "")
		snapshots.AddIndex("idx_final_snapshots_task", false, "task, created", "")
		snapshots.AddIndex("idx_final_snapshots_owner", false, "owner", "")
		if err := app.Save(snapshots); err != nil {
			return err
		}

		tasks.Fields.Add(
			// The accepted dataset that authorizes preview, test and sending.
			&core.RelationField{Name: "selected_final_snapshot", MaxSelect: 1, CollectionId: snapshots.Id},
			&core.TextField{Name: "final_snapshot_digest", Max: 64},
			&core.DateField{Name: "finalized_at"},
			&core.TextField{Name: "finalization_error", Max: 1000},
		)
		if err := app.Save(tasks); err != nil {
			return err
		}

		attempts, err := app.FindCollectionByNameOrId(sending.AttemptsCollection)
		if err != nil {
			return err
		}
		attempts.Fields.Add(
			&core.RelationField{Name: "snapshot", MaxSelect: 1, CollectionId: snapshots.Id},
			&core.TextField{Name: "snapshot_digest", Max: 64},
		)
		if err := app.Save(attempts); err != nil {
			return err
		}

		approvals, err := app.FindCollectionByNameOrId(sending.ApprovalsCollection)
		if err != nil {
			return err
		}
		// The legacy identity allowed reuse across tasks with matching
		// content. Exact task and snapshot identity replace it.
		approvals.RemoveIndex("idx_test_approval_identity")
		approvals.Fields.Add(
			&core.RelationField{Name: "task", MaxSelect: 1, CollectionId: tasks.Id},
			&core.RelationField{Name: "snapshot", MaxSelect: 1, CollectionId: snapshots.Id},
			&core.TextField{Name: "snapshot_digest", Max: 64},
		)
		approvals.AddIndex("idx_test_approval_snapshot", true, "task, snapshot, snapshot_digest, configuration, content_hash, connection, connection_generation", "")
		approvals.AddIndex("idx_test_approval_task", false, "task, snapshot", "")
		return app.Save(approvals)
	}, func(app core.App) error {
		// Final snapshots, exact approval evidence and closure markers are
		// audit records. Roll back through a database backup taken before this
		// change and the matching binary.
		return nil
	})
}
