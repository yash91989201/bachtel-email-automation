package exports_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

func startExports(t *testing.T, service *exports.Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return stop
}

func runExports(t *testing.T, service *exports.Service) { startExports(t, service) }

func waitForArtifactState(t *testing.T, f *exportFixture, taskID, state string) *core.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := f.app.FindRecordsByFilter(exports.ArtifactsCollection, "kind='task_results' && task={:task}", "", 1, 0, map[string]any{"task": taskID})
		if err == nil && len(rows) == 1 && rows[0].GetString("state") == state {
			return rows[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task export did not reach %q", state)
	return nil
}

func setTask(t *testing.T, f *exportFixture, taskID, status string, marker bool) {
	t.Helper()
	task, err := f.app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("status", status)
	task.Set("result_generation_needed", marker)
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
}

func TestTaskNotificationWaitsForTerminalMarkerAndIgnoresRollback(t *testing.T) {
	f := newExportFixture(t)
	taskID := f.stage(t, [][]string{{"ada@example.invalid", "Ada"}})
	f.service.BindTaskNotifications()
	startExports(t, f.service)

	// A marker on a running task must not trigger generation. The terminal
	// stopped update commits the final state and then activates the worker.
	setTask(t, f, taskID, "stopping", true)
	time.Sleep(30 * time.Millisecond)
	if rows := f.records(t, exports.ArtifactsCollection, "task='"+taskID+"'"); len(rows) != 0 {
		t.Fatalf("nonterminal marker generated an artifact: %v", rows)
	}
	setTask(t, f, taskID, "stopped", true)
	waitForArtifactState(t, f, taskID, exports.StateReady)

	rolledBackID := f.stage(t, [][]string{{"grace@example.invalid", "Grace"}})
	err := f.app.RunInTransaction(func(tx core.App) error {
		record, err := tx.FindRecordById(intake.TasksCollection, rolledBackID)
		if err != nil {
			return err
		}
		record.Set("status", "stopped")
		record.Set("result_generation_needed", true)
		if err := tx.Save(record); err != nil {
			return err
		}
		return errors.New("rollback test")
	})
	if err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	rolledBack, err := f.app.FindRecordById(intake.TasksCollection, rolledBackID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.GetString("status") == "stopped" || rolledBack.GetBool("result_generation_needed") {
		t.Fatal("rolled-back terminal marker was persisted")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "task='"+rolledBackID+"'"); len(rows) != 0 {
		t.Fatalf("rolled-back update activated export generation: %v", rows)
	}
}

func TestNotificationCommittedDuringGenerationIsNotLost(t *testing.T) {
	f := newExportFixture(t)
	first := f.stage(t, [][]string{{"first@example.invalid", "First"}})
	second := f.stage(t, [][]string{{"second@example.invalid", "Second"}})
	setTask(t, f, first, "stopped", false)
	setTask(t, f, second, "running", false)
	f.service.BindTaskNotifications()
	var once sync.Once
	created := make(chan struct{})
	f.app.OnRecordAfterCreateSuccess(exports.ArtifactsCollection).BindFunc(func(e *core.RecordEvent) error {
		if e.Record.GetString("task") == first {
			once.Do(func() {
				record, err := e.App.FindRecordById(intake.TasksCollection, second)
				if err != nil {
					e.App.Logger().Error("load second task in artifact barrier", "error", err)
					close(created)
					return
				}
				record.Set("status", "stopped")
				record.Set("result_generation_needed", true)
				if err := e.App.Save(record); err != nil {
					e.App.Logger().Error("commit second task marker in artifact barrier", "error", err)
				}
				close(created)
			})
		}
		return e.Next()
	})
	startExports(t, f.service)
	setTask(t, f, first, "stopped", true)
	select {
	case <-created:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not create the first task artifact")
	}
	waitForArtifactState(t, f, first, exports.StateReady)
	waitForArtifactState(t, f, second, exports.StateReady)
}

func TestOrdinaryRecoveredGenerationFailureStaysFailedAcrossRestarts(t *testing.T) {
	f := newExportFixture(t)
	taskID := f.stage(t, [][]string{{"bad@example.invalid", "Bad"}})
	setTask(t, f, taskID, "stopped", false)
	if err := f.service.GenerateTask(context.Background(), taskID, false); err != nil {
		t.Fatal(err)
	}
	artifact := waitForArtifactState(t, f, taskID, exports.StateReady)
	artifact.Set("state", exports.StateGenerating)
	if err := f.app.Save(artifact); err != nil {
		t.Fatal(err)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	f.removeStoredFile(t, task, task.GetString("source"))
	stop := startExports(t, f.service)
	failed := waitForArtifactState(t, f, taskID, exports.StateFailed)
	if failed.GetString("error") == "" {
		t.Fatal("failed export did not record an operator-facing error")
	}
	failed.Set("error", "preserve failed generation")
	if err := f.app.Save(failed); err != nil {
		t.Fatal(err)
	}
	stop()

	startExports(t, f.service)
	time.Sleep(100 * time.Millisecond)
	failed = waitForArtifactState(t, f, taskID, exports.StateFailed)
	if failed.GetString("error") != "preserve failed generation" {
		t.Fatal("restart retried an ordinary failed export without an explicit retry")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "task='"+taskID+"'"); len(rows) != 1 {
		t.Fatalf("restart created another failed artifact: %d", len(rows))
	}
}

func TestCancellationDuringGenerationLeavesClaimForNextStartup(t *testing.T) {
	f := newExportFixture(t)
	taskID := f.stage(t, [][]string{{"cancel@example.invalid", "Cancel"}})
	setTask(t, f, taskID, "stopped", true)
	started := make(chan struct{}, 1)
	released := make(chan struct{})
	type savedLogger struct {
		db    *dbx.DB
		query dbx.QueryLogFunc
		exec  dbx.ExecLogFunc
	}
	var saved []savedLogger
	var once sync.Once
	for _, builder := range []dbx.Builder{f.app.ConcurrentDB(), f.app.NonconcurrentDB()} {
		db, ok := builder.(*dbx.DB)
		if !ok {
			t.Fatalf("database builder has type %T", builder)
		}
		query, exec := db.QueryLogFunc, db.ExecLogFunc
		saved = append(saved, savedLogger{db: db, query: query, exec: exec})
		db.QueryLogFunc = func(ctx context.Context, elapsed time.Duration, querySQL string, rows *sql.Rows, err error) {
			if strings.Contains(querySQL, "task_candidates") {
				once.Do(func() {
					started <- struct{}{}
					<-released
				})
			}
			if query != nil {
				query(ctx, elapsed, querySQL, rows, err)
			}
		}
		db.ExecLogFunc = func(ctx context.Context, elapsed time.Duration, querySQL string, result sql.Result, err error) {
			if exec != nil {
				exec(ctx, elapsed, querySQL, result, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.service.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		close(released)
		<-done
		t.Fatal("generation did not reach task snapshot query")
	}
	cancel()
	close(released)
	<-done
	for _, logger := range saved {
		logger.db.QueryLogFunc, logger.db.ExecLogFunc = logger.query, logger.exec
	}
	artifact := waitForArtifactState(t, f, taskID, exports.StateGenerating)
	if artifact.GetString("file") != "" {
		t.Fatal("canceled generation unexpectedly published a file")
	}
	startExports(t, f.service)
	ready := waitForArtifactState(t, f, taskID, exports.StateReady)
	if ready.GetString("file") == "" {
		t.Fatal("next startup did not rebuild canceled generation")
	}
}

func TestIdleExportWorkerExecutesNoRecurringSQL(t *testing.T) {
	f := newExportFixture(t)
	var calls atomic.Int64
	type savedLogger struct {
		db    *dbx.DB
		query dbx.QueryLogFunc
		exec  dbx.ExecLogFunc
	}
	var saved []savedLogger
	for _, builder := range []dbx.Builder{f.app.ConcurrentDB(), f.app.NonconcurrentDB()} {
		db, ok := builder.(*dbx.DB)
		if !ok {
			t.Fatalf("database builder has type %T", builder)
		}
		query, exec := db.QueryLogFunc, db.ExecLogFunc
		saved = append(saved, savedLogger{db: db, query: query, exec: exec})
		db.QueryLogFunc = func(ctx context.Context, elapsed time.Duration, querySQL string, rows *sql.Rows, err error) {
			calls.Add(1)
			if query != nil {
				query(ctx, elapsed, querySQL, rows, err)
			}
		}
		db.ExecLogFunc = func(ctx context.Context, elapsed time.Duration, querySQL string, result sql.Result, err error) {
			calls.Add(1)
			if exec != nil {
				exec(ctx, elapsed, querySQL, result, err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.service.Run(ctx) }()
	time.Sleep(150 * time.Millisecond)
	before := calls.Load()
	if before == 0 {
		cancel()
		<-done
		for _, logger := range saved {
			logger.db.QueryLogFunc, logger.db.ExecLogFunc = logger.query, logger.exec
		}
		t.Fatal("query instrumentation did not observe initial discovery")
	}
	time.Sleep(5200 * time.Millisecond)
	stopCalls := calls.Load()
	cancel()
	<-done
	for _, logger := range saved {
		logger.db.QueryLogFunc, logger.db.ExecLogFunc = logger.query, logger.exec
	}
	if stopCalls != before {
		t.Fatalf("idle worker executed %d additional SQL calls in 5.2 seconds", stopCalls-before)
	}
}

func TestStartupRebuildsInterruptedTaskExport(t *testing.T) {
	f := newExportFixture(t)
	taskID := f.mixedTask(t)
	readyTaskID := f.stage(t, [][]string{{"ready@example.invalid", "Ready"}})
	deletedTaskID := f.stage(t, [][]string{{"deleted@example.invalid", "Deleted"}})
	setTask(t, f, taskID, "stopped", false)
	setTask(t, f, readyTaskID, "stopped", false)
	setTask(t, f, deletedTaskID, "stopped", false)
	if err := f.service.GenerateTask(context.Background(), taskID, false); err != nil {
		t.Fatal(err)
	}
	artifact := waitForArtifactState(t, f, taskID, exports.StateReady)
	attemptsBefore := f.attemptsFingerprint(t)
	memberStates := func() string {
		var state strings.Builder
		for _, member := range f.records(t, intake.TaskCandidatesCollection, "task='"+taskID+"'") {
			state.WriteString(member.Id + ":" + member.GetString("state") + ":" + member.GetString("reason") + "\n")
		}
		return state.String()
	}
	membersBefore := memberStates()
	candidatesBefore := len(f.records(t, intake.CandidatesCollection, "campaign='"+f.campaignID+"'"))
	if err := f.service.GenerateTask(context.Background(), readyTaskID, false); err != nil {
		t.Fatal(err)
	}
	readyBefore := waitForArtifactState(t, f, readyTaskID, exports.StateReady)
	readyDigest := readyBefore.GetString("digest")
	if err := f.service.GenerateTask(context.Background(), deletedTaskID, false); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteTask(context.Background(), deletedTaskID); err != nil {
		t.Fatal(err)
	}
	artifact.Set("state", exports.StateGenerating)
	if err := f.app.Save(artifact); err != nil {
		t.Fatal(err)
	}
	setTask(t, f, taskID, "stopped", false)
	startExports(t, f.service)
	ready := waitForArtifactState(t, f, taskID, exports.StateReady)
	if ready.GetString("file") == "" {
		t.Fatal("recovered task export has no published file")
	}
	readyAfter := waitForArtifactState(t, f, readyTaskID, exports.StateReady)
	if readyAfter.GetString("digest") != readyDigest {
		t.Fatal("startup changed a ready task export")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "task='"+deletedTaskID+"'"); len(rows) != 0 {
		t.Fatal("startup recreated a deleted task export without a marker")
	}
	if after := f.attemptsFingerprint(t); after != attemptsBefore {
		t.Fatal("export recovery changed recorded attempt history")
	}
	if after := memberStates(); after != membersBefore {
		t.Fatal("export recovery changed task candidate states")
	}
	if after := len(f.records(t, intake.CandidatesCollection, "campaign='"+f.campaignID+"'")); after != candidatesBefore {
		t.Fatal("export recovery changed campaign candidate count")
	}
}

func TestStartupRebuildsOnlyPendingCampaignArtifactAndPreservesSiblingState(t *testing.T) {
	f := newExportFixture(t)
	if err := f.service.GenerateCampaign(context.Background(), f.campaignID, false); err != nil {
		t.Fatal(err)
	}
	readyPackageRows := f.records(t, exports.ArtifactsCollection, "kind='campaign_package' && campaign='"+f.campaignID+"'")
	if len(readyPackageRows) != 1 || readyPackageRows[0].GetString("state") != exports.StateReady {
		t.Fatalf("campaign pair was not generated: %v", readyPackageRows)
	}
	readyPackage := readyPackageRows[0]
	packageAsOf := readyPackage.GetDateTime("as_of").Time()
	readyReportRows := f.records(t, exports.ArtifactsCollection, "kind='campaign_report' && campaign='"+f.campaignID+"'")
	if len(readyReportRows) != 1 || !readyReportRows[0].GetDateTime("as_of").Time().Equal(packageAsOf) {
		t.Fatal("new campaign report and package do not share a snapshot time")
	}
	collection, err := f.app.FindCollectionByNameOrId(exports.ArtifactsCollection)
	if err != nil {
		t.Fatal(err)
	}
	add := func(kind, state string) *core.Record {
		t.Helper()
		record := core.NewRecord(collection)
		record.Set("kind", kind)
		record.Set("campaign", f.campaignID)
		record.Set("state", state)
		if err := f.app.Save(record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	reportRows := f.records(t, exports.ArtifactsCollection, "kind='campaign_report' && campaign='"+f.campaignID+"'")
	if len(reportRows) != 1 {
		t.Fatalf("got %d campaign reports, want one", len(reportRows))
	}
	pendingReport := reportRows[0]
	pendingReport.Set("state", exports.StateGenerating)
	if err := f.app.Save(pendingReport); err != nil {
		t.Fatal(err)
	}
	readyPackage.Set("state", exports.StateGenerating)
	if err := f.app.Save(readyPackage); err != nil {
		t.Fatal(err)
	}
	stopPair := startExports(t, f.service)

	deadline := time.Now().Add(5 * time.Second)
	var report *core.Record
	for time.Now().Before(deadline) {
		report, err = f.app.FindRecordById(exports.ArtifactsCollection, pendingReport.Id)
		if err == nil && report.GetString("state") == exports.StateReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if report == nil || report.GetString("state") != exports.StateReady || report.GetString("file") == "" {
		t.Fatalf("interrupted report was not rebuilt: %#v", report)
	}
	deadline = time.Now().Add(5 * time.Second)
	var packageReady *core.Record
	for time.Now().Before(deadline) {
		packageReady, err = f.app.FindRecordById(exports.ArtifactsCollection, readyPackage.Id)
		if err == nil && packageReady.GetString("state") == exports.StateReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if packageReady == nil || packageReady.GetString("state") != exports.StateReady || !packageReady.GetDateTime("as_of").Time().Equal(report.GetDateTime("as_of").Time()) {
		t.Fatalf("interrupted pair: report state=%s as_of=%s; package=%v error=%v", report.GetString("state"), report.GetString("as_of"), packageReady, err)
	}
	packageDigest := packageReady.GetString("digest")
	packageAsOf = packageReady.GetDateTime("as_of").Time()
	stopPair()

	// Interrupt only the report next. Startup must preserve the ready package
	// and must not rewrite its bytes or snapshot metadata.
	report, err = f.app.FindRecordById(exports.ArtifactsCollection, pendingReport.Id)
	if err != nil {
		t.Fatal(err)
	}
	report.Set("state", exports.StateGenerating)
	if err := f.app.Save(report); err != nil {
		t.Fatal(err)
	}
	stop := startExports(t, f.service)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		report, err = f.app.FindRecordById(exports.ArtifactsCollection, pendingReport.Id)
		if err == nil && report.GetString("state") == exports.StateReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ready, err := f.app.FindRecordById(exports.ArtifactsCollection, readyPackage.Id)
	if err != nil {
		t.Fatal(err)
	}
	if ready.GetString("state") != exports.StateReady || ready.GetString("digest") != packageDigest || !ready.GetDateTime("as_of").Time().Equal(packageAsOf) {
		t.Fatal("ready campaign package changed while rebuilding its report sibling")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "kind='campaign_package' && campaign='"+f.campaignID+"'"); len(rows) != 1 {
		t.Fatalf("campaign package sibling count changed: got %d", len(rows))
	}
	stop()

	if err := f.service.DeleteCampaign(context.Background(), f.campaignID); err != nil {
		t.Fatal(err)
	}
	pendingReport = add(exports.KindCampaignReport, exports.StateGenerating)
	startExports(t, f.service)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		report, err = f.app.FindRecordById(exports.ArtifactsCollection, pendingReport.Id)
		if err == nil && report.GetString("state") == exports.StateReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if report == nil || report.GetString("state") != exports.StateReady {
		t.Fatal("requested deleted report was not rebuilt")
	}
	if rows := f.records(t, exports.ArtifactsCollection, "kind='campaign_package' && campaign='"+f.campaignID+"'"); len(rows) != 0 {
		t.Fatalf("startup recreated an unrequested deleted package: %d artifacts", len(rows))
	}
}
