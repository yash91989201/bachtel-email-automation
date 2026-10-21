package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		members, err := app.FindCollectionByNameOrId("task_candidates")
		if err != nil {
			return err
		}
		// Select pending rows in worksheet order without scanning earlier
		// outcomes; the same prefix covers settlement's per-state counts.
		members.AddIndex("idx_task_candidates_dispatch", false, "task, dispatch_state, source_row", "")
		return app.Save(members)
	}, func(app core.App) error {
		members, err := app.FindCollectionByNameOrId("task_candidates")
		if err != nil {
			return err
		}
		members.RemoveIndex("idx_task_candidates_dispatch")
		return app.Save(members)
	})
}
