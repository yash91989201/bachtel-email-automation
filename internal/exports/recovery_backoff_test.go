package exports_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Fail the recovery transaction or discovery read once without changing the
// underlying temporary store. This exercises the real worker's timed retry.
type transientExportApp struct {
	core.App
	recoveryFailure  atomic.Bool
	discoveryFailure atomic.Bool
	recoveryCalls    atomic.Int64
	discoveryCalls   atomic.Int64
	failed           chan struct{}
}

func (a *transientExportApp) RunInTransaction(fn func(core.App) error) error {
	a.recoveryCalls.Add(1)
	if a.recoveryFailure.CompareAndSwap(true, false) {
		a.failed <- struct{}{}
		return errors.New("temporary recovery storage failure")
	}
	return a.App.RunInTransaction(fn)
}

func (a *transientExportApp) FindRecordsByFilter(collection any, filter, sort string, limit, offset int, params ...dbx.Params) ([]*core.Record, error) {
	if collection == intake.TasksCollection {
		a.discoveryCalls.Add(1)
		if a.discoveryFailure.CompareAndSwap(true, false) {
			a.failed <- struct{}{}
			return nil, errors.New("temporary discovery failure")
		}
	}
	return a.App.FindRecordsByFilter(collection, filter, sort, limit, offset, params...)
}

func TestExportWorkerRetriesOnlyTemporaryRecoveryAndDiscoveryErrors(t *testing.T) {
	for _, recovery := range []bool{true, false} {
		name := "discovery"
		if recovery {
			name = "recovery"
		}
		t.Run(name, func(t *testing.T) {
			f := newExportFixture(t)
			task := f.stage(t, [][]string{{"retry@example.invalid", "Retry"}})
			setTask(t, f, task, "completed", true)
			app := &transientExportApp{App: f.app, failed: make(chan struct{}, 1)}
			app.recoveryFailure.Store(recovery)
			app.discoveryFailure.Store(!recovery)
			service := exports.NewService(app, f.cfg)
			runExports(t, service)
			select {
			case <-app.failed:
			case <-time.After(3 * time.Second):
				t.Fatal("failure injection was not reached")
			}
			time.Sleep(100 * time.Millisecond)
			if recovery && app.recoveryCalls.Load() != 1 {
				t.Fatal("recovery retried without backoff")
			}
			if !recovery && app.discoveryCalls.Load() != 1 {
				t.Fatal("discovery retried without backoff")
			}
			waitForArtifactState(t, f, task, exports.StateReady)
		})
	}
}

func TestExportWorkerShutdownCancelsErrorBackoff(t *testing.T) {
	f := newExportFixture(t)
	app := &transientExportApp{App: f.app, failed: make(chan struct{}, 1)}
	app.recoveryFailure.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); exports.NewService(app, f.cfg).Run(ctx) }()
	select {
	case <-app.failed:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not reach failed recovery")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for recovery backoff")
	}
}

// The export worker may finish shutdown before the dispatcher persists its last
// bounded request outcome. The marker must survive that ordering.
func TestLateStopSettlementMarkerIsRecoveredOnNextStartup(t *testing.T) {
	f := newExportFixture(t)
	task := f.stage(t, [][]string{{"late@example.invalid", "Late"}})
	f.approve(t, task)
	f.act(t, f.ownerID, task, "start", "late-start", 1)
	attempt := f.begin(t, "")
	f.service.BindTaskNotifications()
	stop := startExports(t, f.service)
	stop()
	f.act(t, f.ownerID, task, "stop", "late-stop", 0)
	if err := f.store.CompleteAttempt(context.Background(), attempt.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SettleTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	if record.GetString("status") != "stopped" || !record.GetBool("result_generation_needed") {
		t.Fatal("late settlement did not leave a durable terminal marker")
	}
	before := f.attemptsFingerprint(t)
	next := exports.NewService(f.app, f.cfg)
	startExports(t, next)
	waitForArtifactState(t, f, task, exports.StateReady)
	if got := f.attemptsFingerprint(t); got != before {
		t.Fatal("export restart changed attempt history")
	}
	work, err := f.store.NextWork(context.Background())
	if err != nil || work != nil {
		t.Fatalf("export restart created dispatch work: %v %v", work, err)
	}
}

func TestCampaignPairClaimsRollbackWhenSiblingIsOwned(t *testing.T) {
	f := newExportFixture(t)
	if err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, false); err != nil {
		t.Fatal(err)
	}
	artifacts := f.records(t, exports.ArtifactsCollection, "task=''")
	for _, artifact := range artifacts {
		if artifact.GetString("kind") == exports.KindCampaignReport {
			artifact.Set("state", exports.StatePending)
		} else {
			artifact.Set("state", exports.StateGenerating)
			artifact.Set("generation_started", time.Now().UTC())
		}
		if err := f.app.Save(artifact); err != nil {
			t.Fatal(err)
		}
	}
	err := f.service.GenerateCampaign(context.Background(), f.ownerID, f.campaignID, false)
	if !errors.Is(err, exports.ErrInProgress) {
		t.Fatalf("competing campaign claim: %v", err)
	}
	report, err := f.app.FindFirstRecordByFilter(exports.ArtifactsCollection, "kind='campaign_report' && campaign={:campaign}", dbx.Params{"campaign": f.campaignID})
	if err != nil {
		t.Fatal(err)
	}
	if report.GetString("state") != exports.StatePending {
		t.Fatalf("partial report claim was not rolled back: %s", report.GetString("state"))
	}
}

func TestDeletingFailedTaskExportCancelsStartupRegeneration(t *testing.T) {
	f := newExportFixture(t)
	task := f.stage(t, [][]string{{"remove@example.invalid", "Remove"}})
	setTask(t, f, task, "stopped", true)
	collection, err := f.app.FindCollectionByNameOrId(exports.ArtifactsCollection)
	if err != nil {
		t.Fatal(err)
	}
	failed := core.NewRecord(collection)
	failed.Set("owner", f.ownerID)
	failed.Set("kind", exports.KindTaskResults)
	failed.Set("campaign", f.campaignID)
	failed.Set("task", task)
	failed.Set("state", exports.StateFailed)
	if err := f.app.Save(failed); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteTask(context.Background(), f.ownerID, task); err != nil {
		t.Fatal(err)
	}
	record, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	if record.GetBool("result_generation_needed") {
		t.Fatal("intentional deletion retained an automatic generation marker")
	}
	startExports(t, exports.NewService(f.app, f.cfg))
	time.Sleep(100 * time.Millisecond)
	if rows := f.records(t, exports.ArtifactsCollection, "task='"+task+"'"); len(rows) != 0 {
		t.Fatal("startup recreated an intentionally deleted failed export")
	}
}
