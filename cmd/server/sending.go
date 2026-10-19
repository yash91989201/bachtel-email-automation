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
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
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
	var exportWorker func()
	var terminating bool
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		mu.Lock()
		defer mu.Unlock()
		if terminating {
			return errors.New("application is shutting down")
		}
		store := sending.NewStore(app, sending.Options{
			SenderAddress: cfg.Microsoft.SenderAddress,
			RestoreReview: cfg.RestoreReview,
		})
		dispatcher = sending.NewDispatcher(store, sending.NewGraph(connections, sending.GraphOptions{
			BaseURL: cfg.Microsoft.GraphBaseURL,
			Timeout: 20 * time.Second,
		}), sending.WorkerOptions{DataDir: app.DataDir(), RequestTimeout: 20 * time.Second})
		exported := exports.NewService(app, cfg)
		exported.BindTaskNotifications()
		if err := dispatcher.Start(); err != nil {
			return err
		}
		connections.SetDispatchGuard(dispatcher)
		// The export worker is process-owned like the dispatcher: it consumes
		// durable result markers and never depends on a browser session.
		exportsCtx, cancelExports := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			exported.Run(exportsCtx)
		}()
		exportWorker = func() {
			cancelExports()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				app.Logger().Warn("export worker did not stop within the shutdown allowance")
			}
		}
		event.Router.Any("/{path...}", apis.WrapStdHandler(web.NewHandlerWithServices(app, cfg, connections, store, dispatcher, exported)))
		return event.Next()
	})
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id:       "drain-mail-dispatcher",
		Priority: -10000,
		Func: func(event *core.TerminateEvent) error {
			mu.Lock()
			terminating = true
			active := dispatcher
			stopExports := exportWorker
			mu.Unlock()
			if stopExports != nil {
				stopExports()
			}
			if active != nil {
				// The container stop allowance is 45 seconds (compose.yaml and
				// the documented Coolify setting). Graph requests are bounded at
				// 20 seconds, and final persistence needs headroom, so the drain
				// budget is 35 seconds.
				ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
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
