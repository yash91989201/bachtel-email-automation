package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web"
)

// bindSending attaches the sole dispatcher, the mailbox connection services and
// the export worker to the server's lifetime. Neither request cancellation nor
// logout owns their contexts. Candidate actions require approved test formatting
// for the exact configuration and explicit operator authorization.
func bindSending(app core.App, cfg config.Config) {
	var mu sync.Mutex
	var dispatcher *sending.Dispatcher
	var exportWorker func()
	var preparationWorker func()
	var terminating bool
	app.OnServe().BindFunc(func(event *core.ServeEvent) error {
		mu.Lock()
		defer mu.Unlock()
		if terminating {
			return errors.New("application is shutting down")
		}
		mailboxes := mailbox.NewStore(app, cfg.EncryptionKey)
		connections := microsoft.NewService(app, cfg, mailboxes)
		store := sending.NewStore(app, sending.Options{RestoreReview: cfg.RestoreReview})
		resolver := &connectionResolver{cfg: cfg, connections: mailboxes, microsoft: connections}
		dispatcher = sending.NewDispatcher(store, resolver, sending.WorkerOptions{
			DataDir:        app.DataDir(),
			RequestTimeout: 20 * time.Second,
		})
		// Application-settings rotation waits for the owner's in-flight
		// submissions to settle before the credentials are swapped.
		connections.SetDrainer(dispatcher)
		aiStore := ai.NewStore(app, cfg.EncryptionKey)
		campaigns := campaign.NewService(app, cfg.PandocPath)
		intakeService := intake.NewService(app, campaigns, cfg.Intake)
		prepared := preparation.NewService(app, intakeService, campaigns, aiStore)
		// Preparation settlement releases a requested closure only after the
		// AI work reached a terminal state; a closed task never publishes.
		prepared.SetSettled(func(taskID string) {
			if err := store.SettleClosure(context.Background(), taskID); err != nil {
				app.Logger().Error("settle closure after preparation", "task", taskID, "error", err)
			}
		})
		finalized := finalization.NewService(app, intakeService, campaigns, prepared)
		exported := exports.NewService(app, cfg)
		exported.BindTaskNotifications()
		resets := reset.NewService(app, mailboxes)
		resets.SetDrainer(dispatcher)
		if err := dispatcher.Start(); err != nil {
			return err
		}
		// Resume an interrupted destructive replacement before serving: a
		// workspace blocked by an unfinished reset must stay blocked until its
		// files are cleaned up and the target mailbox is activated.
		if err := resets.Recover(context.Background()); err != nil {
			app.Logger().Error("resume mailbox replacement", "error", err)
		}
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
		// The preparation worker is process-owned like the dispatcher and the
		// export worker: an interrupted run is marked incomplete at the next
		// startup and only an explicit retry starts it again.
		prepCtx, cancelPreparation := context.WithCancel(context.Background())
		prepDone := make(chan struct{})
		go func() {
			defer close(prepDone)
			prepared.Run(prepCtx)
		}()
		preparationWorker = func() {
			cancelPreparation()
			select {
			case <-prepDone:
			case <-time.After(5 * time.Second):
				app.Logger().Warn("preparation worker did not stop within the shutdown allowance")
			}
		}
		event.Router.Any("/{path...}", apis.WrapStdHandler(web.NewHandler(app, cfg, web.Services{
			Connections:  connections,
			Mailboxes:    mailboxes,
			Sending:      store,
			Dispatcher:   dispatcher,
			Exports:      exported,
			Resets:       resets,
			AI:           aiStore,
			Preparation:  prepared,
			Finalization: finalized,
		})))
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
			stopPreparation := preparationWorker
			mu.Unlock()
			if stopPreparation != nil {
				stopPreparation()
			}
			if stopExports != nil {
				stopExports()
			}
			if active != nil {
				// The container stop allowance is 45 seconds (compose.yaml and
				// the documented Coolify setting). Provider requests are bounded
				// at 20 seconds, and final persistence needs headroom, so the
				// drain budget is 35 seconds.
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
