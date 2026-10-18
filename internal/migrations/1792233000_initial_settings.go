// Package migrations holds the PocketBase Go migrations compiled into the
// executable. They run automatically before the server starts.
package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		settings := app.Settings()
		settings.Meta.AppName = "Bachtel Email Automation"
		settings.RateLimits.Enabled = true
		return app.Save(settings)
	}, nil)
}
