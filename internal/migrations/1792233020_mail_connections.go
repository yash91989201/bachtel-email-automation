package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
)

// Mail connections bind one exclusive sending mailbox to one operator.
//
// Rules stay nil on both collections, so the encrypted credential column is
// reachable only to PocketBase superusers and this server's code. Three
// indexes carry the ownership invariants instead of application checks:
//
//   - one current connection per operator (a staged replacement target is not
//     current until the reset completes);
//   - one connection per canonical sender address, including disconnected and
//     reconnect-required ones;
//   - one connection per verified provider identity where the provider exposes
//     a stable one (Microsoft).
//
// A replacement/reset operation is its own durable record so an interrupted
// destructive reset is recovered rather than reported as completed.
func init() {
	m.Register(func(app core.App) error {
		operators, err := app.FindCollectionByNameOrId(operator.CollectionName)
		if err != nil {
			return err
		}

		connections := core.NewBaseCollection(mailbox.ConnectionsCollection)
		connections.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.TextField{Name: "provider", Required: true, Max: 32},
			&core.TextField{Name: "sender", Required: true, Max: 320},
			&core.TextField{Name: "identity", Max: 320},
			&core.TextField{Name: "status", Required: true, Max: 32},
			// Identity generation: bumped when a different mailbox replaces this
			// one, never by a same-identity credential refresh. Approvals bind to
			// it so a replacement invalidates them.
			&core.NumberField{Name: "generation", Required: true, OnlyInt: true, Min: new(1.0)},
			&core.BoolField{Name: "current"},
			// Sealed credential material: versioned AES-256-GCM ciphertext bound
			// to the owner, provider and connection id.
			&core.TextField{Name: "credential", Max: 1 << 20},
			&core.DateField{Name: "credential_expires_at"},
			&core.TextField{Name: "last_error", Max: 500},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		connections.AddIndex("idx_mail_connections_owner", true, "owner", "current = true")
		connections.AddIndex("idx_mail_connections_sender", true, "sender", "")
		connections.AddIndex("idx_mail_connections_identity", true, "identity", "identity != ''")
		if err = app.Save(connections); err != nil {
			return err
		}

		resets := core.NewBaseCollection(mailbox.ResetsCollection)
		resets.Fields.Add(
			&core.RelationField{Name: "owner", Required: true, MaxSelect: 1, CollectionId: operators.Id},
			&core.RelationField{Name: "from_connection", MaxSelect: 1, CollectionId: connections.Id},
			&core.RelationField{Name: "to_connection", Required: true, MaxSelect: 1, CollectionId: connections.Id},
			// staging | resetting | completed
			&core.TextField{Name: "status", Required: true, Max: 32},
			// Exact retained/deleted scope shown to the operator and used by the
			// idempotent cleanup manifest.
			&core.JSONField{Name: "manifest", MaxSize: 256 << 10},
			&core.TextField{Name: "error", Max: 500},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		// At most one unfinished reset per operator; completed rows stay as
		// history and cannot block a later replacement.
		resets.AddIndex("idx_connection_resets_active", true, "owner", "status != 'completed'")
		return app.Save(resets)
	}, func(app core.App) error {
		// Connections and reset history are audit state; rollback restores a
		// database backup taken before this schema change.
		return nil
	})
}
