package main

import (
	"log"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/web"
)

func main() {
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: "pb_data",
		DefaultDev:     osutils.IsProbablyGoRun(),
	})
	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Dir:         "internal/migrations",
		Automigrate: osutils.IsProbablyGoRun(),
	})

	// A malformed origin or callback URL fails startup. Missing Microsoft
	// connection settings do not: the connection reports the missing settings
	// in the UI instead of blocking the rest of the application.
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	microsoftService := microsoft.NewService(app, cfg)

	app.RootCmd.AddCommand(provisionOperatorCommand(app))

	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		// PocketBase keeps its API and dashboard routes; app handlers use net/http.
		event.Router.Any("/{path...}", apis.WrapStdHandler(web.NewHandler(app, cfg, microsoftService)))
		return event.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
