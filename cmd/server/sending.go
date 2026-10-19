package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web"
)

// bindSending attaches the sole dispatcher to the server's lifetime. Neither
// request cancellation nor logout owns its context. The release gate has no
// environment or browser override: change 5 must complete release verification
// before production candidate dispatch can be enabled in code.
func bindSending(app core.App, cfg config.Config, connections *microsoft.Service) {
	var mu sync.Mutex
	var dispatcher *sending.Dispatcher
	var terminating bool
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		mu.Lock()
		defer mu.Unlock()
		if terminating {
			return errors.New("application is shutting down")
		}
		store := sending.NewStore(app, sending.Options{})
		dispatcher = sending.NewDispatcher(store, sending.NewGraph(connections, sending.GraphOptions{
			BaseURL: cfg.Microsoft.GraphBaseURL,
			Timeout: 20 * time.Second,
		}), sending.WorkerOptions{DataDir: app.DataDir(), RequestTimeout: 20 * time.Second})
		if err := dispatcher.Start(); err != nil {
			return err
		}
		connections.SetDispatchGuard(dispatcher)
		event.Router.Any("/{path...}", apis.WrapStdHandler(web.NewHandlerWithDispatcher(app, cfg, connections, store, dispatcher)))
		return event.Next()
	})
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id:       "drain-mail-dispatcher",
		Priority: -10000,
		Func: func(event *core.TerminateEvent) error {
			mu.Lock()
			terminating = true
			active := dispatcher
			mu.Unlock()
			if active != nil {
				// Compose grants 30 seconds; requests have a 20-second deadline.
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				err := active.Shutdown(ctx)
				cancel()
				if err != nil {
					app.Logger().Error("drain email submissions", "error", err)
				}
			}
			return event.Next()
		},
	})
}
