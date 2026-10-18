package main

import (
	"log"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"

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
	app.RootCmd.AddCommand(provisionOperatorCommand(app))
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		// PocketBase keeps its API and dashboard routes; app handlers use net/http.
		event.Router.Any("/{path...}", apis.WrapStdHandler(web.NewHandler(app)))
		return event.Next()
	})
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
