package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Account connection settings and terminal task closure.
//
//   - ai_connections holds at most one confirmed AI configuration per operator:
//     the non-secret selection, its configuration generation, and a separately
//     versioned credential sealed under the deployment key.
//   - ai_connection_drafts holds the single staged proposal. It records the
//     configuration and credential generations it was validated against, so a
//     stale confirmation cannot overwrite a newer confirmed configuration.
//   - send_tasks records closure separately from execution status. A closed
//     task keeps its stored status and history; closed_at is the terminal
//     marker, and closure_requested_at marks a close that still waits for
//     in-flight work to settle.
//
// The attempt owner/state index answers the account-wide pending-operation
// question without scanning every attempt.
//
// Rules stay nil: credentials, endpoint configuration and task history are
// reachable only to PocketBase superusers and this server's code.
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
		tasks.Fields.Add(
			// Durable closure request: set when the owner confirms Close and
			// kept until every in-flight operation has settled.
			&core.DateField{Name: "closure_requested_at"},
			&core.TextField{Name: "closure_actor", Max: 255},
			// Terminal closure marker. A closed task never reopens.
			&core.DateField{Name: "closed_at"},
		)
		if err = app.Save(tasks); err != nil {
			return err
		}

		attempts, err := app.FindCollectionByNameOrId(sending.AttemptsCollection)
		if err != nil {
			return err
		}
		attempts.AddIndex("idx_sending_attempt_owner_state", false, "owner, state", "")
		if err = app.Save(attempts); err != nil {
			return err
		}

		connections := core.NewBaseCollection(ai.ConnectionsCollection)
		connections.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.TextField{Name: "provider", Required: true, Max: 32},
			&core.TextField{Name: "api_root", Required: true, Max: 2048},
			&core.TextField{Name: "api_format", Required: true, Max: 32},
			&core.TextField{Name: "model", Required: true, Max: 200},
			// Non-secret configuration generation: a switch bumps it, a key
			// repair does not. A run captures it with the configuration.
			&core.NumberField{Name: "configuration_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			// Credential version, bumped only when the key changes.
			&core.NumberField{Name: "credential_generation", Required: true, OnlyInt: true, Min: new(1.0)},
			// Sealed API key. It is never returned to a browser or logged.
			&core.TextField{Name: "credential", Required: true, Max: 1 << 20},
			&core.DateField{Name: "validated_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		connections.AddIndex("idx_ai_connections_owner", true, "owner", "")
		if err = app.Save(connections); err != nil {
			return err
		}

		drafts := core.NewBaseCollection(ai.DraftsCollection)
		drafts.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.TextField{Name: "provider", Required: true, Max: 32},
			&core.TextField{Name: "api_root", Required: true, Max: 2048},
			&core.TextField{Name: "api_format", Required: true, Max: 32},
			&core.TextField{Name: "model", Required: true, Max: 200},
			// Sealed replacement key, staged only.
			&core.TextField{Name: "credential", Required: true, Max: 1 << 20},
			// Generations this proposal was validated against. Confirmation
			// refuses when the confirmed connection moved past them.
			&core.NumberField{Name: "base_configuration_generation", OnlyInt: true, Min: new(0.0)},
			&core.NumberField{Name: "base_credential_generation", OnlyInt: true, Min: new(0.0)},
			&core.TextField{Name: "fingerprint", Required: true, Max: 2048},
			&core.DateField{Name: "validated_at"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		drafts.AddIndex("idx_ai_connection_drafts_owner", true, "owner", "")
		return app.Save(drafts)
	}, func(app core.App) error {
		// Confirmed AI configuration and closure markers are audit evidence;
		// rollback restores a database backup taken before this change.
		return nil
	})
}
