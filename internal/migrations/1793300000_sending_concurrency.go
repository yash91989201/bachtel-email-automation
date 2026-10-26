package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Add the mailbox-owned submission capacity beside the existing pacing rate.
// The value is a whole number from 1 through 10 and is independent of pacing:
// it bounds how many emails one connection may have in flight, while the rate
// still bounds how often starts occur.
//
// Existing mailbox rows receive the new default of 2. Their saved rate (the
// historical default 25 or an operator's own choice) and their pacing history,
// next start, cooldown and reason are left untouched: the stored row cannot
// distinguish an inherited rate from a chosen one, so an upgrade never
// overwrites it.
func init() {
	m.Register(func(app core.App) error {
		state, err := app.FindCollectionByNameOrId("dispatcher_state")
		if err != nil {
			return err
		}
		state.Fields.Add(&core.NumberField{Name: "concurrency", Required: true, OnlyInt: true, Min: new(1.0), Max: new(10.0)})
		if err := app.Save(state); err != nil {
			return err
		}
		// A new column holds 0 for every existing row. Fill only those so a
		// repeated run preserves an already saved valid value.
		_, err = app.DB().NewQuery("UPDATE dispatcher_state SET concurrency = 2 WHERE concurrency < 1 OR concurrency > 10").Execute()
		return err
	}, func(app core.App) error {
		state, err := app.FindCollectionByNameOrId("dispatcher_state")
		if err != nil {
			return err
		}
		// Rollback returns to the fixed-four dispatcher: only the new setting is
		// removed. Rates, pacing history, cooldowns and attempts stay valid.
		state.Fields.RemoveByName("concurrency")
		return app.Save(state)
	})
}
