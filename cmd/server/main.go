package main

import (
	"log"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/osutils"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/database"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
)

func main() {
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: "pb_data",
		DefaultDev:     osutils.IsProbablyGoRun(),
		DBConnect:      database.Open,
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

	bindSending(app, cfg, microsoftService)
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
