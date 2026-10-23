package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// Task data preparation evidence.
//
//   - preparation_runs holds one immutable normalization run per explicit
//     preparation, retry or rerun: the exact source digest, campaign revision,
//     base prompt version/text, task instructions, confirmed AI configuration
//     and inference options, plus the captured input and published output.
//   - preparation_jobs holds the durable server-owned execution claim of one
//     run. A job carries an attempt token so a response from an older claim can
//     never publish over a newer one.
//   - send_tasks gains the editable instruction draft and the explicitly
//     selected completed run. Selection is preparation evidence only: it never
//     writes candidates, memberships or attempts.
//
// Rules stay nil: run inputs, outputs, findings and instructions are reachable
// only to PocketBase superusers and this server's code, and every operator read
// goes through the owner-scoped service. Both collections keep audit evidence,
// so rollback restores a database backup instead of dropping records.
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
		configurations, err := app.FindCollectionByNameOrId("campaign_configurations")
		if err != nil {
			return err
		}

		runs := core.NewBaseCollection(preparation.RunsCollection)
		runs.Fields.Add(
			// The owner is stored explicitly so an isolation query never
			// depends on a join through the task.
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			// initial | retry | rerun.
			&core.TextField{Name: "kind", Required: true, Max: 16},
			// queued | running | completed | failed | incomplete | cancelled.
			&core.TextField{Name: "state", Required: true, Max: 16},
			// Exact confirmed AI configuration captured at creation. Every
			// batch of this run uses only this selection.
			&core.TextField{Name: "provider", Required: true, Max: 32},
			&core.TextField{Name: "api_root", Required: true, Max: 2048},
			&core.TextField{Name: "api_format", Required: true, Max: 32},
			&core.TextField{Name: "model", Required: true, Max: 200},
			&core.NumberField{Name: "configuration_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			&core.NumberField{Name: "credential_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			// The pinned campaign revision whose mappings define the fields.
			&core.RelationField{Name: "configuration", MaxSelect: 1, CollectionId: configurations.Id},
			&core.TextField{Name: "content_hash", Max: 64},
			&core.TextField{Name: "source_digest", Max: 64},
			// Complete visible base prompt and its version, so a historical run
			// can always be compared with the policy that produced it.
			&core.NumberField{Name: "base_prompt_version", Required: true, OnlyInt: true, Min: new(1.0)},
			&core.TextField{Name: "base_prompt", Required: true, Max: 65536},
			// Task-specific instructions captured for this run.
			&core.TextField{Name: "instructions", Max: 16384},
			// Inference settings (batch size, output bound, temperature) and the
			// canonical input digest prove what was sent.
			&core.JSONField{Name: "options", MaxSize: 4 << 10},
			&core.TextField{Name: "input_digest", Max: 64},
			&core.TextField{Name: "output_digest", Max: 64},
			// Prepared counts: source rows, entries, accepted, held, findings.
			&core.JSONField{Name: "counts", MaxSize: 4 << 10},
			// Fixed operator-facing failure text. It never carries a provider
			// response, request or credential.
			&core.TextField{Name: "error", Max: 1000},
			// Private captured input and published output. The input is stored
			// so a restart or retry never re-reads the workbook or re-derives
			// what was sent.
			&core.FileField{Name: "input", MaxSelect: 1, MaxSize: preparation.MaxArtifactBytes},
			&core.FileField{Name: "output", MaxSelect: 1, MaxSize: preparation.MaxArtifactBytes},
			&core.DateField{Name: "started_at"},
			&core.DateField{Name: "finished_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		runs.AddIndex("idx_preparation_runs_task", false, "task, created", "")
		runs.AddIndex("idx_preparation_runs_owner", false, "owner", "")
		if err := app.Save(runs); err != nil {
			return err
		}

		jobs := core.NewBaseCollection(preparation.JobsCollection)
		jobs.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			&core.RelationField{Name: "run", Required: true, MaxSelect: 1, CollectionId: runs.Id},
			// queued | running | done | failed | cancelled.
			&core.TextField{Name: "state", Required: true, Max: 16},
			// Random claim token. A late response is published only when its
			// token still owns the job.
			&core.TextField{Name: "attempt", Max: 64},
			&core.NumberField{Name: "batch_total", OnlyInt: true},
			&core.NumberField{Name: "batch_done", OnlyInt: true},
			&core.TextField{Name: "error", Max: 1000},
			&core.DateField{Name: "started_at"},
			&core.DateField{Name: "finished_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		jobs.AddIndex("idx_preparation_jobs_run", true, "run", "")
		jobs.AddIndex("idx_preparation_jobs_task", false, "task, state", "")
		if err := app.Save(jobs); err != nil {
			return err
		}

		tasks.Fields.Add(
			// The editable task-instruction draft. Editing it never changes a
			// completed run; a new run captures the current text.
			&core.TextField{Name: "preparation_instructions", Max: preparation.MaxInstructions},
			// The completed run whose values the operator selected for review.
			// Selection stays preparation evidence: it never writes candidate,
			// membership or attempt records.
			&core.RelationField{Name: "selected_preparation_run", MaxSelect: 1, CollectionId: runs.Id},
		)
		return app.Save(tasks)
	}, func(app core.App) error {
		// Preparation runs, outputs and selection are audit evidence. Roll
		// back through a database backup taken before this change and the
		// matching binary.
		return nil
	})
}
