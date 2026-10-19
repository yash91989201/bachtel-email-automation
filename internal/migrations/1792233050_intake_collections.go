package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Task intake owns three collections: the uploaded task with its private
// validation artifacts, the campaign-scoped candidate identity, and the
// immutable membership snapshot of each admitted row.
//
// Rules stay nil on every collection, so candidate addresses, uploaded
// workbooks, manifests and exclusion reports are reachable only to PocketBase
// superusers and to this application's server-side code: nothing here is
// downloadable through the public API, and the operator downloads go through
// the session-gated routes in internal/web.
//
// Two indexes carry invariants rather than speed alone:
//
//   - campaign_candidates(campaign, email_key) is unique, so one campaign can
//     never hold two identities for the same recipient: that uniqueness is what
//     makes the reservation compare-and-swap in internal/intake atomic.
//   - task_candidates(task, source_row) is unique, so a task snapshot can never
//     hold two rows for one source row, even after a revalidation rewrite.
func init() {
	m.Register(func(app core.App) error {
		campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
		if err != nil {
			return err
		}
		configurations, err := app.FindCollectionByNameOrId(campaign.ConfigurationsCollection)
		if err != nil {
			return err
		}

		tasks := core.NewBaseCollection(intake.TasksCollection)
		tasks.Fields.Add(
			&core.RelationField{Name: "campaign", Required: true, MaxSelect: 1, CollectionId: campaigns.Id},
			// The revision this task was validated against. It is pinned so a
			// later campaign edit cannot change this task's personalized
			// content, and compared against the current revision to detect a
			// stale preview before a start is accepted.
			&core.RelationField{Name: "configuration", MaxSelect: 1, CollectionId: configurations.Id},
			// staging: uploaded, not validated, never sendable.
			// ready: validated; waiting for a later change to start it.
			&core.TextField{Name: "status", Required: true, Max: 32},
			// Private original workbook. The bytes are stored unmodified and
			// their digest is recorded, so results can reproduce the source
			// cells and row coordinates exactly.
			&core.FileField{
				Name:      "source",
				Required:  true,
				MaxSelect: 1,
				MaxSize:   intake.MaxUploadBytes,
			},
			// Original upload name and content digest (sha256 hex).
			&core.TextField{Name: "source_name", Max: 255},
			&core.TextField{Name: "source_digest", Max: 64},
			// Worksheet inventory captured at staging, so the selection form
			// can offer every worksheet without re-uploading.
			&core.JSONField{Name: "sheets", MaxSize: 16 << 10},
			&core.TextField{Name: "sheet", Max: 255},
			&core.NumberField{Name: "header_row"},
			// Source header labels of the selected worksheet.
			&core.JSONField{Name: "headers", MaxSize: 64 << 10},
			// Result labels this task snapshots, with collisions resolved
			// against the source columns (see intake.ResolveResultLabels).
			&core.JSONField{Name: "result_labels", MaxSize: 4 << 10},
			// Task-level counts only: excluded row details stay in the private
			// manifest and report artifacts, never in candidate records.
			&core.JSONField{Name: "counts", MaxSize: 4 << 10},
			// Private validation manifest, keyed by original sheet row.
			&core.FileField{Name: "manifest", MaxSelect: 1, MaxSize: 8 << 20},
			// Private exclusion report (XLSX) offered immediately after
			// validation: every excluded row with its original columns, row
			// number and reason.
			&core.FileField{Name: "excluded", MaxSelect: 1, MaxSize: 32 << 20},
			&core.TextField{Name: "content_hash", Max: 64},
			// Fixed operator-facing reason the last validation attempt failed.
			// A staging task with an error is never sendable.
			&core.TextField{Name: "validation_error", Max: 500},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		if err := app.Save(tasks); err != nil {
			return err
		}

		candidates := core.NewBaseCollection(intake.CandidatesCollection)
		candidates.Fields.Add(
			&core.RelationField{Name: "campaign", Required: true, MaxSelect: 1, CollectionId: campaigns.Id},
			// Trimmed lowercase recipient address: the campaign-scoped identity.
			&core.TextField{Name: "email_key", Required: true, Max: 320},
			// none | submitted | uncertain. Submission and uncertainty are
			// campaign-scoped history: a later upload can never clear them.
			&core.TextField{Name: "submission_state", Required: true, Max: 32},
			// Active reservation owner. Empty means the candidate is free; the
			// reservation is a single compare-and-swap update on this field.
			&core.RelationField{Name: "claim_task", MaxSelect: 1, CollectionId: tasks.Id},
			&core.DateField{Name: "claimed_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		candidates.AddIndex("idx_campaign_candidates_identity", true, "campaign, email_key", "")
		candidates.AddIndex("idx_campaign_candidates_claim", false, "claim_task", "")
		if err := app.Save(candidates); err != nil {
			return err
		}

		taskCandidates := core.NewBaseCollection(intake.TaskCandidatesCollection)
		taskCandidates.Fields.Add(
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			&core.RelationField{Name: "candidate", Required: true, MaxSelect: 1, CollectionId: candidates.Id},
			// Original 1-based worksheet row this snapshot came from.
			&core.NumberField{Name: "source_row", Required: true},
			// eligible | ineligible | already_submitted | already_scheduled |
			// review_required. Only an eligible row can ever be claimed.
			&core.TextField{Name: "state", Required: true, Max: 32},
			// Fixed operator-facing explanation of the state.
			&core.TextField{Name: "reason", Max: 500},
			// Immutable membership snapshot: the raw cells and the normalized
			// replacement values, sufficient to reproduce this row's
			// personalized content with the task's pinned revision.
			&core.JSONField{Name: "raw", MaxSize: 64 << 10},
			&core.JSONField{Name: "normalized", MaxSize: 64 << 10},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		taskCandidates.AddIndex("idx_task_candidates_row", true, "task, source_row", "")
		taskCandidates.AddIndex("idx_task_candidates_candidate", false, "candidate", "")
		return app.Save(taskCandidates)
	}, func(app core.App) error {
		// Task snapshots and campaign candidate identity are audit and
		// deduplication history: dropping them could make a recipient eligible
		// again. Rollback therefore restores a database backup taken before the
		// schema change, and this down migration intentionally leaves the
		// schema in place.
		return nil
	})
}
