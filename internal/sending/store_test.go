package sending_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

var storeCtx = context.Background()

func storeRecord(t *testing.T, f *acceptanceFixture, collection, id string) *core.Record {
	t.Helper()
	r, e := f.app.FindRecordById(collection, id)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func storeSave(t *testing.T, f *acceptanceFixture, r *core.Record) {
	t.Helper()
	if e := f.app.Save(r); e != nil {
		t.Fatal(e)
	}
}
func storePreview(t *testing.T, f *acceptanceFixture, task string) *sending.Progress {
	t.Helper()
	p, e := f.store.Preview(storeCtx, f.ownerID, task)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func storeBegin(t *testing.T, f *acceptanceFixture) *sending.Attempt {
	t.Helper()
	w, e := f.store.NextWork(storeCtx)
	if e != nil || w == nil {
		t.Fatalf("next work: %v, %v", w, e)
	}
	a, e := f.store.BeginAttempt(storeCtx, w, f.clock.Now(), f.clock.Now().Add(3*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func storeApprove(t *testing.T, f *acceptanceFixture, task string) *sending.Attempt {
	t.Helper()
	_, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	if e = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if e = f.store.Approve(storeCtx, f.ownerID, task, a.ID, "operator", "approve-"+task); e != nil {
		t.Fatal(e)
	}
	return a
}
func storeAct(t *testing.T, f *acceptanceFixture, task, kind, id string, count int) *sending.ActionResult {
	t.Helper()
	r, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{TaskID: task, Kind: kind, ActionID: id, Confirmed: true, Actor: "operator", ExpectedCount: count})
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestStoreApprovalExactHashAndExplicitAction(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	a, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_name": "Sample", "candidate_email": "sample@example.invalid"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.store.Approve(storeCtx, f.ownerID, task, a.ID, "operator", "premature"); e == nil {
		t.Fatal("queued test approved")
	}
	a = storeBegin(t, f)
	if e = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Approved || p.Submitted != 0 || p.Eligible != 1 {
		t.Fatalf("test changed approval or candidates: %+v", p)
	}
	req := sending.ActionRequest{ActionID: "start", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 1}
	for _, kind := range []string{"start", "resume", "retry", "retry_all", "resend"} {
		blocked := req
		blocked.ActionID = "unapproved-" + kind
		blocked.Kind = kind
		if _, err := f.store.Act(storeCtx, f.ownerID, blocked); err == nil || !strings.Contains(err.Error(), "approve") {
			t.Fatalf("submitted test without approval permitted %s: %v", kind, err)
		}
	}
	if p := storePreview(t, f, task); p.Reserved != 0 || p.Submitted != 0 || p.Eligible != 1 {
		t.Fatalf("unapproved actions changed candidate state: %+v", p)
	}
	tr := storeRecord(t, f, intake.TasksCollection, task)
	hash := tr.GetString("content_hash")
	tr.Set("content_hash", strings.Repeat("a", 64))
	storeSave(t, f, tr)
	if e = f.store.Approve(storeCtx, f.ownerID, task, a.ID, "operator", "wrong-hash"); e == nil {
		t.Fatal("wrong hash approved")
	}
	tr.Set("content_hash", hash)
	storeSave(t, f, tr)
	if e = f.store.Approve(storeCtx, f.ownerID, task, a.ID, "operator", "approved"); e != nil {
		t.Fatal(e)
	}
	if e = f.store.Approve(storeCtx, f.ownerID, task, a.ID, "operator", "approved"); e != nil {
		t.Fatal(e)
	}
	if !storePreview(t, f, task).Approved {
		t.Fatal("approval missing")
	}
	if _, e = f.store.Act(storeCtx, f.ownerID, req); e != nil {
		t.Fatalf("default store refused approved start: %v", e)
	}
	candidate := storeBegin(t, f)
	if candidate.Kind != sending.KindCandidate || candidate.ContentHash != hash {
		t.Fatalf("approved candidate attempt: %+v", candidate)
	}
}

func TestCorruptPersistedPacingHistoryStopsDispatch(t *testing.T) {
	f := newAcceptanceFixture(t)
	// The per-connection pacing row is created on first use, then corrupted.
	if _, err := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.DB().NewQuery("UPDATE dispatcher_state SET starts='not-json' WHERE connection={:conn}").Bind(dbx.Params{"conn": f.connectionID}).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID); err == nil {
		t.Fatal("corrupt pacing history was accepted")
	}
}

func TestCorruptConfigurationAndCandidateSnapshotsStopWorkSelection(t *testing.T) {
	for _, field := range []string{"mappings", "raw"} {
		t.Run(field, func(t *testing.T) {
			f := newAcceptanceFixture(t)
			task := f.task(t, 1)
			storeApprove(t, f, task)
			storeAct(t, f, task, "start", "start-"+field, 1)
			tr := storeRecord(t, f, intake.TasksCollection, task)
			if field == "mappings" {
				_, err := f.app.DB().NewQuery("UPDATE campaign_configurations SET mappings='{' WHERE id={:id}").Bind(dbx.Params{"id": tr.GetString("configuration")}).Execute()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := f.app.DB().NewQuery("UPDATE task_candidates SET raw='{' WHERE task={:task}").Bind(dbx.Params{"task": task}).Execute()
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.store.NextWork(storeCtx); err == nil {
				t.Fatalf("corrupt %s snapshot was silently accepted", field)
			}
		})
	}
}

func TestStoreActionsRollbackReplayPauseStopAndStaleStart(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 3)
	storeApprove(t, f, task)
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "wrong-count", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 2}); e == nil {
		t.Fatal("stale count accepted")
	}
	if p := storePreview(t, f, task); p.Reserved != 0 || p.Eligible != 3 {
		t.Fatalf("claim leaked after rollback: %+v", p)
	}
	first := storeAct(t, f, task, "start", "start", 3)
	replay := storeAct(t, f, task, "start", "start", 3)
	if !replay.Repeated || replay.Claimed != first.Claimed {
		t.Fatal("replay mutated result")
	}
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "start", TaskID: task, Kind: "stop"}); e == nil {
		t.Fatal("action id reused")
	}
	stale, e := f.store.NextWork(storeCtx)
	if e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	if e = f.store.PauseTask(storeCtx, f.ownerID, task, "operator pause"); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Reserved != 0 || p.InFlight != 1 || p.Eligible != 2 {
		t.Fatalf("pause did not release only safe claims: %+v", p)
	}
	if _, e = f.store.BeginAttempt(storeCtx, stale, f.clock.Now(), f.clock.Now()); !errors.Is(e, sending.ErrNoWork) {
		t.Fatalf("stale start: %v", e)
	}
	storeAct(t, f, task, "stop", "stop", 0)
	if e = f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Status != "stopping" {
		t.Fatalf("in-flight stop did not drain: %+v", p)
	}
	if e = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if e = f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Status != "stopped" || !p.ArtifactNeeded {
		t.Fatalf("stop not settled: %+v", p)
	}
}

func TestStoreFailuresUncertainResolutionAndRetryHistory(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 2)
	failed := storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, failed.ID, sending.Outcome{State: sending.AttemptFailed, Reason: "bad recipient"}); e != nil {
		t.Fatal(e)
	}
	uncertain := storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, uncertain.ID, sending.Outcome{State: sending.AttemptReview, Reason: "lost response", PauseMailbox: true}); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Failed != 1 || p.Held != 1 {
		t.Fatalf("wrong outcomes: %+v", p)
	}
	storeAct(t, f, task, "retry_all", "retry", 1)
	retry := storeBegin(t, f)
	if retry.ID == failed.ID || retry.Subject != failed.Subject || retry.Body != failed.Body {
		t.Fatal("retry overwrote immutable history")
	}
	if e := f.store.CompleteAttempt(storeCtx, retry.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if e := f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	req := sending.ActionRequest{ActionID: "resend-no-risk", TaskID: task, MemberID: uncertain.MemberID, Kind: "resend", Confirmed: true, ExpectedCount: 1}
	if _, e := f.store.Act(storeCtx, f.ownerID, req); e == nil {
		t.Fatal("resend without duplicate risk accepted")
	}
	req.Kind = "resolve"
	req.ActionID = "resolve"
	req.Actor = "operator"
	req.Basis = "verified in provider sent items"
	if _, e := f.store.Act(storeCtx, f.ownerID, req); e != nil {
		t.Fatal(e)
	}
	p := storePreview(t, f, task)
	if p.Submitted != 2 || p.Held != 0 {
		t.Fatalf("manual resolution not reflected: %+v", p)
	}
	original := storeRecord(t, f, sending.AttemptsCollection, uncertain.ID)
	if original.GetString("state") != sending.AttemptReview || original.GetString("reason") != "lost response" {
		t.Fatal("resolution rewrote provider history")
	}
	audits, e := f.app.FindRecordsByFilter(sending.ResolutionsCollection, "member={:m}", "", 0, 0, dbx.Params{"m": uncertain.MemberID})
	if e != nil || len(audits) != 1 || audits[0].GetString("actor") != "operator" || audits[0].GetString("basis") != req.Basis {
		t.Fatalf("missing audit: %v %v", audits, e)
	}
}

func TestStoreRecoveryPausedIntentAndDurablePacing(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 3)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 3)
	a := storeBegin(t, f)
	until := f.clock.Now().Add(7 * time.Minute)
	if e := f.store.SetCooldown(storeCtx, f.connectionID, f.ownerID, until, "throttle"); e != nil {
		t.Fatal(e)
	}
	if e := f.store.PauseTask(storeCtx, f.ownerID, task, "paused with active send"); e != nil {
		t.Fatal(e)
	}
	before, e := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID)
	if e != nil {
		t.Fatal(e)
	}
	restarted := sending.NewStore(f.app, sending.Options{Now: f.clock.Now})
	if e = restarted.Recover(storeCtx); e != nil {
		t.Fatal(e)
	}
	after, e := restarted.DispatcherState(storeCtx, f.connectionID, f.ownerID)
	if e != nil {
		t.Fatal(e)
	}
	if !after.CooldownUntil.Equal(until) || !after.NextEligible.Equal(before.NextEligible) || len(after.Starts) != len(before.Starts) {
		t.Fatalf("recovery lost pacing: %+v", after)
	}
	p := storePreview(t, f, task)
	if p.Status != "paused" || !p.ArtifactNeeded || p.Held != 1 || p.Eligible != 2 {
		t.Fatalf("paused recovery: %+v", p)
	}
	if r := storeRecord(t, f, sending.AttemptsCollection, a.ID); r.GetString("state") != sending.AttemptReview {
		t.Fatal("unfinished intent not held")
	}
	if w, e := restarted.NextWork(storeCtx); e != nil || w != nil {
		t.Fatalf("restart auto sent: %v %v", w, e)
	}
	storeAct(t, f, task, "stop", "stop", 0)
	if e = restarted.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	storeAct(t, f, task, "artifacts", "artifacts", 0)
	if w, e := restarted.NextWork(storeCtx); e != nil || w != nil {
		t.Fatalf("artifact marker queued sends: %v %v", w, e)
	}
}

func TestStoreNextWorkDeadlineTracksOnlyActionableFutureRetries(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 1)
	futureTest := f.clock.Now().Add(5 * time.Minute)
	queued, err := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "deadline-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_name": "Sample", "candidate_email": "sample@example.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	queuedRecord := storeRecord(t, f, sending.AttemptsCollection, queued.ID)
	queuedRecord.Set("retry_at", futureTest)
	storeSave(t, f, queuedRecord)
	member, err := f.app.FindFirstRecordByFilter(intake.TaskCandidatesCollection, "task={:task}", dbx.Params{"task": task})
	if err != nil {
		t.Fatal(err)
	}
	futureCandidate := f.clock.Now().Add(7 * time.Minute)
	member.Set("retry_at", futureCandidate)
	storeSave(t, f, member)
	deadline, err := f.store.NextWorkDeadline(storeCtx)
	if err != nil || !deadline.Equal(futureTest) {
		t.Fatalf("deadline should favor queued test: %v, %v", deadline, err)
	}
	queuedRecord.Set("retry_at", f.clock.Now().Add(-time.Second))
	storeSave(t, f, queuedRecord)
	deadline, err = f.store.NextWorkDeadline(storeCtx)
	if err != nil || deadline.After(f.clock.Now()) {
		t.Fatalf("due retry must trigger immediate rescan: %v, %v", deadline, err)
	}
	queuedRecord.Set("retry_at", futureTest)
	queuedRecord.Set("state", sending.AttemptRetryable)
	storeSave(t, f, queuedRecord)
	deadline, err = f.store.NextWorkDeadline(storeCtx)
	if err != nil || !deadline.Equal(futureCandidate) {
		t.Fatalf("historical retry attempt scheduled work: %v, %v", deadline, err)
	}
	storeAct(t, f, task, "stop", "stop-for-deadline", 0)
	deadline, err = f.store.NextWorkDeadline(storeCtx)
	if err != nil || !deadline.IsZero() {
		t.Fatalf("stopped task scheduled candidate work: %v, %v", deadline, err)
	}
}

func TestStoreSafeRetriesAreBoundedAndAppendHistory(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 2)
	var first string
	for i := range 4 {
		a := storeBegin(t, f)
		if first == "" {
			first = a.MemberID
		}
		if a.MemberID != first {
			t.Fatal("retry skipped first candidate")
		}
		if a.RetryCount != i {
			t.Fatalf("retry count %d want %d", a.RetryCount, i)
		}
		if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptRetryable, Reason: "safe rejection"}); e != nil {
			t.Fatal(e)
		}
	}
	p := storePreview(t, f, task)
	if p.Status != "paused" || p.Failed != 1 || p.Eligible != 1 || p.Reserved != 0 {
		t.Fatalf("retry limit did not pause and release: %+v", p)
	}
	attempts, e := f.app.FindRecordsByFilter(sending.AttemptsCollection, "member={:m}", "created", 0, 0, dbx.Params{"m": first})
	if e != nil || len(attempts) != 4 {
		t.Fatalf("retry history: %d %v", len(attempts), e)
	}
}

func TestStoreConcurrentTasksClaimCandidateOnce(t *testing.T) {
	f := newAcceptanceFixture(t)
	a := f.task(t, 1)
	b := f.task(t, 1)
	storeApprove(t, f, a)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, task := range []string{a, b} {
		wg.Add(1)
		go func(task string, i int) {
			defer wg.Done()
			_, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: fmt.Sprint("claim", i), TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 1})
			results <- e
		}(task, i)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful claims %d, want exactly one", success)
	}
}

func TestStoreLongBodyAndImmutableIntakeExclusion(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	tr := storeRecord(t, f, intake.TasksCollection, task)
	cfg := storeRecord(t, f, campaign.ConfigurationsCollection, tr.GetString("configuration"))
	body := "<p>" + strings.Repeat("long body text ", 1000) + "</p>"
	cfg.Set("body_html", body)
	storeSave(t, f, cfg)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 1)
	a := storeBegin(t, f)
	if a.Body != body {
		t.Fatalf("body truncated: %d", len(a.Body))
	}
	cfg.Set("body_html", "new body")
	storeSave(t, f, cfg)
	if got := storeRecord(t, f, sending.AttemptsCollection, a.ID).GetString("body"); got != body {
		t.Fatal("attempt changed with configuration")
	}
	if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptReview}); e != nil {
		t.Fatal(e)
	}
	excluded := f.task(t, 1)
	if p := storePreview(t, f, excluded); p.Eligible != 0 || p.Held != 1 {
		t.Fatalf("uncertain intake not excluded: %+v", p)
	}
	m := storeRecord(t, f, intake.TaskCandidatesCollection, a.MemberID)
	c := storeRecord(t, f, intake.CandidatesCollection, m.GetString("candidate"))
	c.Set("submission_state", intake.SubmissionNone)
	storeSave(t, f, c)
	if p := storePreview(t, f, excluded); p.Eligible != 0 {
		t.Fatal("immutable intake exclusion auto admitted")
	}
}

func TestStoreBulkRecoveryAppliesOnlyTheSelectedMembers(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 2)
	first := storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, first.ID, sending.Outcome{State: sending.AttemptFailed, Reason: "temporary configuration failure"}); e != nil {
		t.Fatal(e)
	}
	second := storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, second.ID, sending.Outcome{State: sending.AttemptFailed, Reason: "temporary configuration failure"}); e != nil {
		t.Fatal(e)
	}
	storeAct(t, f, task, "stop", "stop", 0)
	if e := f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.RetryableFailed != 2 {
		t.Fatalf("expected two retryable failures: %+v", p)
	}
	// A selection narrowed to one member retries exactly that member.
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "bulk-retry-one", TaskID: task, Kind: "retry", MemberIDs: []string{first.MemberID}, Confirmed: true, ExpectedCount: 1}); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Reserved != 1 || p.RetryableFailed != 1 {
		t.Fatalf("bulk retry claimed the wrong members: %+v", p)
	}
	// A stale selected count is refused without claiming anything.
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "bulk-retry-stale", TaskID: task, Kind: "retry", MemberIDs: []string{first.MemberID, second.MemberID}, Confirmed: true, ExpectedCount: 1}); e == nil {
		t.Fatal("bulk retry accepted a stale selected count")
	}
	if p := storePreview(t, f, task); p.Reserved != 1 || p.RetryableFailed != 1 {
		t.Fatalf("refused bulk retry changed claims: %+v", p)
	}
}

func TestStoreBulkResolveRecordsEverySelectedMember(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 2)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 2)
	first := storeBegin(t, f)
	second := storeBegin(t, f)
	for _, a := range []*sending.Attempt{first, second} {
		if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptReview, Reason: "lost response"}); e != nil {
			t.Fatal(e)
		}
	}
	if p := storePreview(t, f, task); p.Held != 2 {
		t.Fatalf("expected two uncertain rows: %+v", p)
	}
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "bulk-resolve", TaskID: task, Kind: "resolve", MemberIDs: []string{first.MemberID, second.MemberID}, Actor: "operator", Basis: "verified in provider sent items", Confirmed: true, ExpectedCount: 2}); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Submitted != 2 || p.Held != 0 {
		t.Fatalf("bulk resolution not reflected: %+v", p)
	}
	audits, e := f.app.FindRecordsByFilter(sending.ResolutionsCollection, "action_id='bulk-resolve'", "", 0, 0)
	if e != nil || len(audits) != 2 {
		t.Fatalf("bulk resolution audits = %d, %v", len(audits), e)
	}
	for _, audit := range audits {
		if audit.GetString("basis") != "verified in provider sent items" {
			t.Fatalf("bulk resolution lost its basis: %q", audit.GetString("basis"))
		}
	}
}

func TestStoreResumeSafeRemainderAfterRetryBatchCompleted(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 3)
	storeApprove(t, f, task)
	storeAct(t, f, task, "start", "start", 3)
	a := storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptFailed, Reason: "temporary configuration failure"}); e != nil {
		t.Fatal(e)
	}
	storeAct(t, f, task, "stop", "stop", 0)
	if e := f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	storeAct(t, f, task, "retry_all", "retry", 1)
	a = storeBegin(t, f)
	if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if e := f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	if p := storePreview(t, f, task); p.Status != "completed" || p.Eligible != 2 {
		t.Fatalf("unexpected retry batch state: %+v", p)
	}
	storeAct(t, f, task, "resume", "resume", 2)
	if p := storePreview(t, f, task); p.Status != "running" || p.Reserved != 2 {
		t.Fatalf("safe remainder stranded: %+v", p)
	}
}

func TestStoreCorrectionRequiresNewIntakeAndTestRetriesKeepHistory(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	_, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "retry-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_name": "Sample", "candidate_email": "sample@example.invalid"}})
	if e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	if e = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptRetryable, Reason: "safe test rejection"}); e != nil {
		t.Fatal(e)
	}
	next := storeBegin(t, f)
	if next.ID == a.ID || next.RetryCount != 1 || next.Body != a.Body {
		t.Fatal("test retry did not append immutable record")
	}
	if r := storeRecord(t, f, sending.AttemptsCollection, a.ID); r.GetString("state") != sending.AttemptRetryable || r.GetString("reason") != "safe test rejection" {
		t.Fatal("test history was overwritten")
	}
	if e = f.store.CompleteAttempt(storeCtx, next.ID, sending.Outcome{State: sending.AttemptSubmitted}); e != nil {
		t.Fatal(e)
	}
	if e = f.store.Approve(storeCtx, f.ownerID, task, next.ID, "operator", "approve"); e != nil {
		t.Fatal(e)
	}
	storeAct(t, f, task, "start", "start", 1)
	a = storeBegin(t, f)
	if e = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptFailed, RequiresCorrection: true, Reason: "correct recipient data in a new upload"}); e != nil {
		t.Fatal(e)
	}
	if e = f.store.SettleTasks(storeCtx); e != nil {
		t.Fatal(e)
	}
	p := storePreview(t, f, task)
	if p.Failed != 1 || p.RetryableFailed != 0 || p.Members[0].State != "correction_required" {
		t.Fatalf("correction lost: %+v", p)
	}
	if _, e = f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "retry-correction", TaskID: task, Kind: "retry_all", Confirmed: true, ExpectedCount: 1}); e == nil {
		t.Fatal("uncorrected data retried")
	}
}

func TestStoreStaleConfiguration(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	storeApprove(t, f, task)
	req := sending.ActionRequest{ActionID: "start", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 1}
	camp := storeRecord(t, f, campaign.CampaignsCollection, f.campaignID)
	camp.Set("current_configuration", "")
	storeSave(t, f, camp)
	if _, e := f.store.Act(storeCtx, f.ownerID, req); e == nil || !strings.Contains(e.Error(), "configuration changed") {
		t.Fatalf("stale configuration accepted: %v", e)
	}
	if p := storePreview(t, f, task); p.Eligible != 1 || p.Reserved != 0 {
		t.Fatal("stale start changed claims")
	}
}

func TestStoreInvalidStoredRateAndActionQueryFailure(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	if e := f.store.SetRate(storeCtx, f.ownerID, 0); e == nil {
		t.Fatal("zero rate accepted")
	}
	if e := f.store.SetRate(storeCtx, f.ownerID, 31); e == nil {
		t.Fatal("excess rate accepted")
	}
	// Create the per-connection pacing row, then store a fractional rate.
	if _, e := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.app.DB().NewQuery("UPDATE dispatcher_state SET rate=1.5 WHERE connection={:conn}").Bind(dbx.Params{"conn": f.connectionID}).Execute(); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID); e == nil {
		t.Fatal("fractional stored rate accepted")
	}
	if _, e := f.app.DB().NewQuery("DROP TABLE sending_actions").Execute(); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: "stop", TaskID: task, Kind: "stop"}); e == nil {
		t.Fatal("query failure treated as missing action")
	}
	if p := storeRecord(t, f, intake.TasksCollection, task); p.GetBool("result_generation_needed") {
		t.Fatal("mutation escaped failed action lookup")
	}
}

type storeGraphTokens struct{}

func (storeGraphTokens) AccessToken(context.Context) (string, error) { return "simulated-token", nil }

func TestStorePreparedGraphDispatchRoundRobinUsesCommittedStarts(t *testing.T) {
	f := newAcceptanceFixture(t)
	first := f.task(t, 4)
	second := f.task(t, 4)
	storeApprove(t, f, first)
	// Keep task snapshots in one mailbox while giving the second task distinct
	// campaign identities. Both tasks are eligible before either one is started.
	members, e := f.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t}", "source_row", 0, 0, dbx.Params{"t": second})
	if e != nil {
		t.Fatal(e)
	}
	collection, e := f.app.FindCollectionByNameOrId(intake.CandidatesCollection)
	if e != nil {
		t.Fatal(e)
	}
	for i, m := range members {
		c := core.NewRecord(collection)
		c.Set("campaign", f.campaignID)
		c.Set("email_key", fmt.Sprintf("second%04d@example.invalid", i))
		c.Set("submission_state", intake.SubmissionNone)
		storeSave(t, f, c)
		m.Set("candidate", c.Id)
		storeSave(t, f, m)
	}
	var mu sync.Mutex
	var submitted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Message struct {
				To []struct {
					Email struct {
						Address string `json:"address"`
					} `json:"emailAddress"`
				} `json:"toRecipients"`
			} `json:"message"`
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		submitted = append(submitted, payload.Message.To[0].Email.Address)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	graph := sending.NewGraph(storeGraphTokens{}, sending.GraphOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	d := f.dispatcher(t, graph)
	if e = d.Synchronize(func() error {
		for i, task := range []string{first, second} {
			if _, e := f.store.Act(storeCtx, f.ownerID, sending.ActionRequest{ActionID: fmt.Sprint("fair-start", i), TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 4}); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	acceptanceEventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(submitted) == 8 })
	mu.Lock()
	defer mu.Unlock()
	firstCount, secondCount := 0, 0
	for _, recipient := range submitted[:4] {
		if strings.HasPrefix(recipient, "second") {
			secondCount++
		} else {
			firstCount++
		}
	}
	if firstCount != 2 || secondCount != 2 {
		t.Fatalf("prepared authorization starved a task; first four submissions=%v", submitted[:4])
	}
}

// approveFor queues, submits and approves a formatting test for one operator.
func approveFor(t *testing.T, f *acceptanceFixture, ownerID, task string) {
	t.Helper()
	_, err := f.store.QueueTest(storeCtx, ownerID, sending.TestRequest{ActionID: "test-" + ownerID + "-" + task, TaskID: task, Recipient: "operator@example.invalid", Actor: "acceptance", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.store.NextWork(storeCtx)
	if err != nil || w == nil {
		t.Fatalf("test work = %v %v", w, err)
	}
	a, err := f.store.BeginAttempt(storeCtx, w, f.clock.Now(), f.clock.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Approve(storeCtx, ownerID, task, a.ID, "acceptance", "approve-"+ownerID+"-"+task); err != nil {
		t.Fatal(err)
	}
}

// actFor runs one operator action against that operator's own task.
func actFor(t *testing.T, f *acceptanceFixture, ownerID, task, kind, id string, count int) *sending.ActionResult {
	t.Helper()
	r, err := f.store.Act(storeCtx, ownerID, sending.ActionRequest{ActionID: id, TaskID: task, Kind: kind, Confirmed: true, Actor: "operator", ExpectedCount: count})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestMultiOperatorTaskAccessIsIsolated proves that another operator's task is
// indistinguishable from a missing one for every send-like action, and that a
// refused cross-operator action mutates nothing.
func TestMultiOperatorTaskAccessIsIsolated(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	otherOwner, _ := f.addOperator(t, "other@example.invalid", "other-sender@example.invalid")

	if _, err := f.store.Preview(storeCtx, otherOwner, task); !errors.Is(err, sending.ErrNotFound) {
		t.Fatalf("cross-operator preview = %v, want ErrNotFound", err)
	}
	if _, err := f.store.QueueTest(storeCtx, otherOwner, sending.TestRequest{ActionID: "cross-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}}); !errors.Is(err, sending.ErrNotFound) {
		t.Fatalf("cross-operator queue test = %v, want ErrNotFound", err)
	}
	if _, err := f.store.Act(storeCtx, otherOwner, sending.ActionRequest{ActionID: "cross-start", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 1}); !errors.Is(err, sending.ErrNotFound) {
		t.Fatalf("cross-operator act = %v, want ErrNotFound", err)
	}

	// Nothing was approved, reserved, queued or submitted.
	if p := storePreview(t, f, task); p.Approved || p.Reserved != 0 || p.Submitted != 0 || p.Eligible != 1 {
		t.Fatalf("cross-operator actions mutated state: %+v", p)
	}
	attempts, err := f.app.FindAllRecords(sending.AttemptsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("cross-operator actions created %d attempts", len(attempts))
	}
}

// TestMultiOperatorPacingAndInFlightAreIndependent proves pacing state is
// per-connection: a cooldown on one mailbox never leaks into another's
// dispatcher state or blocks the selection of its work.
func TestMultiOperatorPacingAndInFlightAreIndependent(t *testing.T) {
	f := newAcceptanceFixture(t)
	otherOwner, otherConnection := f.addOperator(t, "second@example.invalid", "second-sender@example.invalid")
	otherCampaign := f.createCampaign(t, otherOwner, "Second operator")
	theirs := f.taskFor(t, otherOwner, otherCampaign, 1)

	// A cooldown recorded for one mailbox must not touch the other's pacing.
	until := f.clock.Now().Add(30 * time.Minute)
	if err := f.store.SetCooldown(storeCtx, f.connectionID, f.ownerID, until, "throttle"); err != nil {
		t.Fatal(err)
	}
	mineState, err := f.store.DispatcherState(storeCtx, f.connectionID, f.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if !mineState.CooldownUntil.Equal(until) {
		t.Fatalf("cooldown not recorded on its own connection: %+v", mineState)
	}
	theirState, err := f.store.DispatcherState(storeCtx, otherConnection, otherOwner)
	if err != nil {
		t.Fatal(err)
	}
	if !theirState.CooldownUntil.IsZero() {
		t.Fatalf("cooldown leaked across mailboxes: %+v", theirState)
	}

	// This mailbox's cooldown does not delay work selection for the other one.
	approveFor(t, f, otherOwner, theirs)
	actFor(t, f, otherOwner, theirs, "start", "start-theirs", 1)
	w, err := f.store.NextWork(storeCtx)
	if err != nil || w == nil {
		t.Fatalf("next work = %v, %v", w, err)
	}
	if w.OwnerID != otherOwner {
		t.Fatalf("cooldown blocked the other mailbox's work: %+v", w)
	}
}

// TestStoreCopyWarningKeepsSubmittedOutcome proves a copy warning is only added
// to already submitted attempt details: state, acceptance timestamp and
// eligibility stay exactly as recorded, and the warning is scoped to its owner
// and connection.
func TestStoreCopyWarningKeepsSubmittedOutcome(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	if _, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "copy-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}}); e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	accepted := time.Date(2026, 10, 23, 5, 21, 18, 0, time.UTC)
	if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted, AcceptedAt: accepted}); e != nil {
		t.Fatal(e)
	}
	r := storeRecord(t, f, sending.AttemptsCollection, a.ID)
	if got := r.GetDateTime("finished_at").Time().UTC(); !got.Equal(accepted) {
		t.Fatalf("finished_at = %s, want the acceptance time %s", got, accepted)
	}
	connection := r.GetString("connection")
	for range 2 { // a repeated warning is a no-op, not a second sentence
		if e := f.store.RecordCopyWarning(storeCtx, a.ID, f.ownerID, connection, sending.SentCopyWarning); e != nil {
			t.Fatal(e)
		}
	}
	r = storeRecord(t, f, sending.AttemptsCollection, a.ID)
	if got := r.GetString("state"); got != sending.AttemptSubmitted {
		t.Fatalf("state = %q, want the accepted submission retained", got)
	}
	if got := r.GetDateTime("finished_at").Time().UTC(); !got.Equal(accepted) {
		t.Fatalf("finished_at = %s after the warning, want %s", got, accepted)
	}
	if got := strings.Count(r.GetString("reason"), sending.SentCopyWarning); got != 1 {
		t.Fatalf("reason = %q, want one copy warning", r.GetString("reason"))
	}
	progress := storePreview(t, f, task)
	if progress.Approved || progress.LatestSuccessfulTestID != a.ID || progress.Submitted != 0 {
		t.Fatalf("copy warning changed approval or test history: %+v", progress)
	}
	if len(progress.Recent) == 0 || progress.Recent[0].State != sending.AttemptSubmitted || !strings.Contains(progress.Recent[0].Reason, sending.SentCopyWarning) {
		t.Fatalf("attempt details hide the warning: %+v", progress.Recent)
	}
	for _, scope := range []struct{ owner, connection string }{
		{"another-operator", connection},
		{f.ownerID, "another-connection"},
	} {
		if e := f.store.RecordCopyWarning(storeCtx, a.ID, scope.owner, scope.connection, sending.SentCopyWarning); !errors.Is(e, sending.ErrNoWork) {
			t.Fatalf("cross-scope warning error = %v, want refusal", e)
		}
	}
}

// TestStoreCopyWarningRefusesUnfinishedAttempts proves copy warnings cannot
// become a second outcome for an attempt that is still in flight.
func TestStoreCopyWarningRefusesUnfinishedAttempts(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	if _, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "in-flight-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}}); e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	r := storeRecord(t, f, sending.AttemptsCollection, a.ID)
	if e := f.store.RecordCopyWarning(storeCtx, a.ID, f.ownerID, r.GetString("connection"), sending.SentCopyWarning); !errors.Is(e, sending.ErrNoWork) {
		t.Fatalf("in-flight warning error = %v, want refusal", e)
	}
	r = storeRecord(t, f, sending.AttemptsCollection, a.ID)
	if r.GetString("state") != sending.AttemptInFlight || strings.Contains(r.GetString("reason"), sending.SentCopyWarning) {
		t.Fatalf("in-flight attempt was modified: state=%q reason=%q", r.GetString("state"), r.GetString("reason"))
	}
}

// TestStoreRecoveryKeepsDurableSubmissionWithoutCopyReplay proves startup never
// downgrades or replays a submission recorded by an earlier run, including one
// whose copy work was interrupted.
func TestStoreRecoveryKeepsDurableSubmissionWithoutCopyReplay(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 1)
	if _, e := f.store.QueueTest(storeCtx, f.ownerID, sending.TestRequest{ActionID: "restart-test", TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}}); e != nil {
		t.Fatal(e)
	}
	a := storeBegin(t, f)
	accepted := time.Date(2026, 10, 22, 4, 0, 0, 0, time.UTC)
	if e := f.store.CompleteAttempt(storeCtx, a.ID, sending.Outcome{State: sending.AttemptSubmitted, AcceptedAt: accepted}); e != nil {
		t.Fatal(e)
	}
	before := storeRecord(t, f, sending.AttemptsCollection, a.ID)
	reason, state := before.GetString("reason"), before.GetString("state")
	attemptsBefore, e := f.app.CountRecords(sending.AttemptsCollection)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.store.Recover(storeCtx); e != nil {
		t.Fatal(e)
	}
	after := storeRecord(t, f, sending.AttemptsCollection, a.ID)
	if after.GetString("state") != state || after.GetString("reason") != reason {
		t.Fatalf("recovery changed the stored submission: state=%q reason=%q", after.GetString("state"), after.GetString("reason"))
	}
	if got := after.GetDateTime("finished_at").Time().UTC(); !got.Equal(accepted) {
		t.Fatalf("recovery changed the acceptance time: %s", got)
	}
	attemptsAfter, e := f.app.CountRecords(sending.AttemptsCollection)
	if e != nil {
		t.Fatal(e)
	}
	if attemptsAfter != attemptsBefore {
		t.Fatalf("attempts = %d after recovery, want %d: no resend or copy replay", attemptsAfter, attemptsBefore)
	}
}
