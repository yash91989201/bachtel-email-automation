package migrations

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// Durable sending state is operator- and mailbox-scoped:
//
//   - every attempt records its owning operator and the exact mailbox
//     connection/generation that transmitted it;
//   - a test approval covers one configuration's content on one connection
//     generation, so replacing the mailbox invalidates it;
//   - pacing, cooldowns and service pauses live per connection instead of on a
//     singleton mailbox row.
//
// Rules stay nil: attempts carry personalized content and failure reasons, and
// only this server's gated handlers may read them.
func init() {
	m.Register(func(app core.App) error {
		operators, e := app.FindCollectionByNameOrId(operator.CollectionName)
		if e != nil {
			return e
		}
		connections, e := app.FindCollectionByNameOrId(mailbox.ConnectionsCollection)
		if e != nil {
			return e
		}
		tasks, e := app.FindCollectionByNameOrId("send_tasks")
		if e != nil {
			return e
		}
		tasks.Fields.Add(&core.TextField{Name: "pause_reason", Max: 500}, &core.BoolField{Name: "result_generation_needed"})
		if e = app.Save(tasks); e != nil {
			return e
		}
		members, e := app.FindCollectionByNameOrId("task_candidates")
		if e != nil {
			return e
		}
		members.Fields.Add(&core.TextField{Name: "dispatch_state", Max: 32}, &core.NumberField{Name: "retry_count"}, &core.DateField{Name: "retry_at"})
		if e = app.Save(members); e != nil {
			return e
		}
		configs, e := app.FindCollectionByNameOrId("campaign_configurations")
		if e != nil {
			return e
		}
		bodyField := configs.Fields.GetByName("body_html")
		body, ok := bodyField.(*core.TextField)
		if !ok {
			return fmt.Errorf("campaign body_html field has type %T", bodyField)
		}
		body.Max = 2 * 1024 * 1024
		if e = app.Save(configs); e != nil {
			return e
		}
		candidates, e := app.FindCollectionByNameOrId("campaign_candidates")
		if e != nil {
			return e
		}
		attempts := core.NewBaseCollection("submission_attempts")
		attempts.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "connection", Required: true, MaxSelect: 1, CollectionId: connections.Id},
			&core.NumberField{Name: "connection_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			&core.RelationField{Name: "member", MaxSelect: 1, CollectionId: members.Id},
			&core.RelationField{Name: "candidate", MaxSelect: 1, CollectionId: candidates.Id},
			&core.RelationField{Name: "configuration", Required: true, MaxSelect: 1, CollectionId: configs.Id},
			&core.TextField{Name: "kind", Required: true, Max: 32},
			&core.TextField{Name: "state", Required: true, Max: 32},
			&core.TextField{Name: "content_hash", Required: true, Max: 64},
			&core.TextField{Name: "recipient", Required: true, Max: 320},
			&core.TextField{Name: "sender", Max: 320},
			&core.TextField{Name: "subject", Required: true, Max: 998},
			&core.TextField{Name: "body", Max: 2 * 1024 * 1024},
			&core.TextField{Name: "reason", Max: 500},
			&core.DateField{Name: "started_at"},
			&core.DateField{Name: "finished_at"},
			&core.DateField{Name: "retry_at"},
			&core.NumberField{Name: "retry_count"},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		attempts.AddIndex("idx_sending_attempt_state", false, "state, kind", "")
		attempts.AddIndex("idx_sending_attempt_connection", false, "connection, state", "")
		if e = app.Save(attempts); e != nil {
			return e
		}
		approvals := core.NewBaseCollection("test_approvals")
		approvals.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "connection", Required: true, MaxSelect: 1, CollectionId: connections.Id},
			&core.NumberField{Name: "connection_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			&core.RelationField{Name: "configuration", Required: true, MaxSelect: 1, CollectionId: configs.Id},
			&core.RelationField{Name: "attempt", Required: true, MaxSelect: 1, CollectionId: attempts.Id},
			&core.TextField{Name: "content_hash", Required: true, Max: 64},
			&core.TextField{Name: "actor", Max: 255},
			&core.DateField{Name: "approved_at"},
		)
		approvals.AddIndex("idx_test_approval_identity", true, "configuration, content_hash, connection", "")
		if e = app.Save(approvals); e != nil {
			return e
		}
		actions := core.NewBaseCollection("sending_actions")
		actions.Fields.Add(
			&core.TextField{Name: "action_id", Required: true, Max: 128},
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "task", Required: true, MaxSelect: 1, CollectionId: tasks.Id},
			&core.TextField{Name: "kind", Required: true, Max: 32},
			&core.TextField{Name: "actor", Max: 255},
			&core.JSONField{Name: "result", MaxSize: 4096},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		actions.AddIndex("idx_sending_action_id", true, "action_id", "")
		if e = app.Save(actions); e != nil {
			return e
		}
		resolutions := core.NewBaseCollection("manual_resolutions")
		resolutions.Fields.Add(
			&core.RelationField{Name: "member", Required: true, MaxSelect: 1, CollectionId: members.Id},
			&core.TextField{Name: "action_id", Required: true, Max: 128},
			&core.TextField{Name: "actor", Required: true, Max: 255},
			&core.TextField{Name: "basis", Required: true, Max: 500},
			&core.DateField{Name: "resolved_at"},
		)
		if e = app.Save(resolutions); e != nil {
			return e
		}
		// Per-mailbox pacing and cooldown state. A row is created for a
		// connection when it first sends, so a new connection starts at the
		// documented default rate instead of inheriting another mailbox's
		// rolling history.
		state := core.NewBaseCollection("dispatcher_state")
		state.Fields.Add(
			&core.RelationField{Name: "connection", Required: true, MaxSelect: 1, CollectionId: connections.Id},
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.NumberField{Name: "rate", Required: true, OnlyInt: true, Min: new(1.0), Max: new(30.0)},
			&core.JSONField{Name: "starts", MaxSize: 32768},
			&core.DateField{Name: "next_eligible"},
			&core.DateField{Name: "cooldown_until"},
			&core.TextField{Name: "reason", Max: 500},
		)
		state.AddIndex("idx_dispatcher_connection", true, "connection", "")
		return app.Save(state)
	}, func(app core.App) error { return nil })
}
