package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/database"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
	"golang.org/x/sys/unix"
)

func TestServerOwnsDispatcherUntilDrainBeforeDatabaseClose(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir(), DBConnect: database.Open})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	testutil.FastPasswords(t, app)
	cfg := config.Config{Origin: "http://127.0.0.1:8090"}
	mailboxes := mailbox.NewStore(app, cfg.EncryptionKey)
	connections := microsoft.NewService(app, cfg, mailboxes)
	bindSending(app, cfg)
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	event := &core.ServeEvent{Router: router}
	event.App = app
	if err := app.OnServe().Trigger(event); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestDispatcherOwnershipChild$")
	child.Env = append(os.Environ(), "BACHTEL_LOCK_PROBE="+filepath.Join(app.DataDir(), ".sending.lock"))
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("ownership probe in a second process: %v: %s", err, output)
	}
	resolver := &connectionResolver{cfg: cfg, connections: mailboxes, microsoft: connections}
	other := sending.NewDispatcher(sending.NewStore(app, sending.Options{}), resolver, sending.WorkerOptions{DataDir: app.DataDir()})
	if err := other.Start(); err == nil {
		t.Fatal("second dispatcher acquired the running server's store")
	}
	checked := false
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Priority: -9999,
		Func: func(event *core.TerminateEvent) error {
			// This runs at PocketBase's HTTP shutdown priority, before its
			// database finalizer. The dispatcher must have released ownership.
			if _, err := app.DB().NewQuery("SELECT 1").Execute(); err != nil {
				t.Fatal("database closed before dispatcher drain", err)
			}
			if err := other.Start(); err != nil {
				t.Fatal("dispatcher ownership was not released during drain", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := other.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			checked = true
			return event.Next()
		},
	})
	termination := &core.TerminateEvent{}
	termination.App = app
	if err := app.OnTerminate().Trigger(termination); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("shutdown verification did not run")
	}
}

func TestDispatcherOwnershipChild(t *testing.T) {
	path := os.Getenv("BACHTEL_LOCK_PROBE")
	if path == "" {
		t.Skip("subprocess ownership probe")
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
			t.Fatal(err)
		}
		t.Fatal("a second process acquired the active dispatcher's lock")
	}
}

// Sending recovery emits notifications while returning through after-commit
// hooks. Export snapshots must wait until all of those hooks have finished.
func TestExportsWaitForSendingRecoveryBeforeTakingSnapshots(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	testutil.FastPasswords(t, app)
	if err := operator.Provision(app, "recovery@example.test", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	owner, err := app.FindAuthRecordByEmail(operator.CollectionName, "recovery@example.test")
	if err != nil {
		t.Fatal(err)
	}
	campaigns, err := app.FindCollectionByNameOrId("campaigns")
	if err != nil {
		t.Fatal(err)
	}
	target := core.NewRecord(campaigns)
	target.Set("name", "Recovery ordering")
	target.Set("owner", owner.Id)
	if err := app.Save(target); err != nil {
		t.Fatal(err)
	}
	tasks, err := app.FindCollectionByNameOrId("send_tasks")
	if err != nil {
		t.Fatal(err)
	}
	task := core.NewRecord(tasks)
	task.Set("campaign", target.Id)
	task.Set("status", "running")
	source, err := filesystem.NewFileFromBytes([]byte("isolated ordering fixture"), "ordering.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	task.Set("source", source)
	if err := app.Save(task); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	unblock := func() { released.Do(func() { close(release) }) }
	defer unblock()
	app.OnRecordAfterUpdateSuccess("send_tasks").BindFunc(func(e *core.RecordEvent) error {
		if e.Record.Id == task.Id && e.Record.GetString("status") == "interrupted" && e.Record.GetBool("result_generation_needed") {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
		}
		return e.Next()
	})
	cfg := config.Config{Origin: "http://127.0.0.1:8090"}
	bindSending(app, cfg)
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	event := &core.ServeEvent{Router: router}
	event.App = app
	started := make(chan error, 1)
	go func() { started <- app.OnServe().Trigger(event) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("sending recovery was not reached")
	}
	time.Sleep(50 * time.Millisecond)
	artifacts, err := app.FindRecordsByFilter("export_artifacts", "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatal("export began before sending recovery completed")
	}
	unblock()
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		termination := &core.TerminateEvent{}
		termination.App = app
		if err := app.OnTerminate().Trigger(termination); err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		artifacts, err := app.FindRecordsByFilter("export_artifacts", "task={:task}", "", 0, 0, map[string]any{"task": task.Id})
		if err != nil {
			t.Fatal(err)
		}
		// This minimal task has no validation manifest: a generation failure
		// proves the recovered marker reached the worker without sending anything.
		if len(artifacts) == 1 && artifacts[0].GetString("state") == "failed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recovered task marker did not activate export generation")
}
