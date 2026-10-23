package sending

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workerFixture struct {
	mu                                        sync.Mutex
	now                                       time.Time
	queue                                     []*Work
	retryReadyAt                              time.Time
	due                                       time.Time
	dueQueue                                  []*Work
	manualAfter                               bool
	advanceOnDeadlineQuery                    bool
	settings                                  Settings
	intents, results                          int
	intentError, resultError                  bool
	recovered                                 bool
	nextWorkCalls, deadlineCalls, settleCalls int
	fireCalls                                 int
	// copyCalls, copySawResults and warnings record the Sent-copy handoff:
	// how often the hook ran, how many outcomes were already persisted when it
	// did, and how many copy warnings the dispatcher stored.
	copyCalls, copySawResults int
	warnings                  int
	warningError              bool
	lastWarning               string
}

func (f *workerFixture) clock() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *workerFixture) after(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	if f.manualAfter {
		f.mu.Unlock()
		return make(chan time.Time)
	}
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return time.After(time.Millisecond)
}
func (f *workerFixture) DispatcherState(context.Context, string, string) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, nil
}
func (f *workerFixture) NextWork(_ context.Context, exhausted ...string) (*Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextWorkCalls++
	if len(f.queue) == 0 || (!f.retryReadyAt.IsZero() && f.now.Before(f.retryReadyAt)) {
		return nil, nil
	}
	for _, w := range f.queue {
		if !slices.Contains(exhausted, w.ConnectionID) {
			return w, nil
		}
	}
	return nil, nil
}
func (f *workerFixture) NextWorkDeadline(context.Context) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deadlineCalls++
	if len(f.queue) == 0 && f.due.IsZero() {
		return time.Time{}, nil
	}
	if f.advanceOnDeadlineQuery && f.retryReadyAt.After(f.now) {
		f.now = f.retryReadyAt
	}
	earliest := f.retryReadyAt
	if !f.due.IsZero() && (earliest.IsZero() || f.due.Before(earliest)) {
		earliest = f.due
	}
	return earliest, nil
}
func (f *workerFixture) FireScheduled(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fireCalls++
	if f.due.IsZero() || f.now.Before(f.due) {
		return false, nil
	}
	f.due = time.Time{}
	f.queue = append(f.queue, f.dueQueue...)
	f.dueQueue = nil
	return true, nil
}
func (f *workerFixture) BeginAttempt(_ context.Context, w *Work, start, next time.Time) (*Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.intentError {
		return nil, errors.New("disk failure")
	}
	// A durable Stop clears the queue; a launch that raced past it must see the
	// same refusal the real store reports for work that is no longer eligible.
	selected := slices.Index(f.queue, w)
	if selected < 0 {
		return nil, ErrNoWork
	}
	f.queue = slices.Delete(f.queue, selected, selected+1)
	f.intents++
	return &Attempt{ID: fmt.Sprint(f.intents), Work: *w}, nil
}
func (f *workerFixture) RecordActualStart(_ context.Context, _ *Work, _ string, start, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings.Starts = append(f.settings.Starts, start)
	f.settings.NextEligible = next
	return nil
}
func (f *workerFixture) CompleteAttempt(context.Context, string, Outcome) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resultError {
		return errors.New("disk failure")
	}
	f.results++
	return nil
}
func (f *workerFixture) RecordCopyWarning(_ context.Context, _, _, _, warning string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.warningError {
		return errors.New("disk failure")
	}
	f.warnings++
	f.lastWarning = warning
	return nil
}
func (f *workerFixture) SetCooldown(_ context.Context, _, _ string, until time.Time, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings.CooldownUntil = until
	return nil
}
func (f *workerFixture) Recover(context.Context) error { f.recovered = true; return nil }
func (f *workerFixture) PauseMailbox(_ context.Context, ownerID, _ string) error {
	// The real store pauses that operator's tasks, so no further queued work for
	// the mailbox exists. Mirror that contract instead of a process-wide stop.
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.queue[:0]
	for _, w := range f.queue {
		if w.OwnerID != ownerID {
			kept = append(kept, w)
		}
	}
	f.queue = kept
	return nil
}
func (f *workerFixture) PauseAll(context.Context, string) error { return nil }
func (f *workerFixture) PauseTask(context.Context, string, string, string) error {
	return nil
}
func (f *workerFixture) SettleTasks(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settleCalls++
	return nil
}

type workerSender func(context.Context, Message) SendResult

func (s workerSender) Send(ctx context.Context, m Message) SendResult { return s(ctx, m) }

// resolverFunc adapts a fake sender to the mailbox Resolver interface so the
// fixture exercises the dispatcher's per-connection resolution path.
type resolverFunc func(context.Context, string) (Sender, error)

func (r resolverFunc) Sender(ctx context.Context, connectionID string) (Sender, error) {
	return r(ctx, connectionID)
}

func fixedResolver(s Sender) Resolver {
	return resolverFunc(func(context.Context, string) (Sender, error) { return s, nil })
}

const (
	fixtureOwner      = "operator-one"
	fixtureConnection = "connection-one"
)

func newWorkerFixture(n int) *workerFixture {
	f := &workerFixture{now: time.Now().UTC(), settings: Settings{Rate: 30}}
	for i := range n {
		f.queue = append(f.queue, &Work{Kind: KindTest, OwnerID: fixtureOwner, ConnectionID: fixtureConnection, Recipient: fmt.Sprintf("%d@example.com", i), Subject: "frozen", Body: "<p>body</p>"})
	}
	return f
}
func TestWorkerIdleWaitsForWakeWithoutPollingStore(t *testing.T) {
	f := newWorkerFixture(0)
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.nextWorkCalls == 1 && f.deadlineCalls == 1
	})
	time.Sleep(30 * time.Millisecond)
	f.mu.Lock()
	before := f.nextWorkCalls
	f.queue = append(f.queue, &Work{Kind: KindTest, OwnerID: fixtureOwner, ConnectionID: fixtureConnection, Recipient: "wake@example.invalid"})
	f.mu.Unlock()
	if before != 1 {
		t.Fatalf("idle dispatcher polled work %d times", before)
	}
	d.notify()
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.intents == 1 })
}

type settleFailureStore struct {
	DispatchStore
	err error
}

func (s settleFailureStore) SettleTasks(context.Context) error { return s.err }

func TestWorkerShutdownReportsSettlementFailureAndReleasesOwnership(t *testing.T) {
	f := newWorkerFixture(0)
	dir := t.TempDir()
	want := errors.New("settlement persistence failed")
	d := NewDispatcher(settleFailureStore{DispatchStore: f, err: want}, fixedResolver(workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	})), WorkerOptions{DataDir: dir})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); !errors.Is(err, want) {
		t.Fatalf("Shutdown error = %v, want %v", err, want)
	}
	second := NewDispatcher(newWorkerFixture(0), fixedResolver(workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	})), WorkerOptions{DataDir: dir})
	if err := second.Start(); err != nil {
		t.Fatalf("ownership remained held after failed settlement: %v", err)
	}
	if err := second.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerFutureRetryTimerStartsWithoutWake(t *testing.T) {
	f := newWorkerFixture(1)
	f.mu.Lock()
	f.retryReadyAt = f.now.Add(45 * time.Millisecond)
	f.mu.Unlock()
	_ = startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.intents == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deadlineCalls == 0 {
		t.Fatal("worker did not consult the persisted retry deadline")
	}
}

func TestWorkerRescansWhenRetryBecomesDueDuringDeadlineQuery(t *testing.T) {
	f := newWorkerFixture(1)
	f.mu.Lock()
	f.retryReadyAt = f.now.Add(45 * time.Millisecond)
	f.advanceOnDeadlineQuery = true
	f.mu.Unlock()
	_ = startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.intents == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deadlineCalls < 1 || f.now.Before(f.retryReadyAt) {
		t.Fatalf("due retry was not rescanned after deadline query: calls=%d now=%s due=%s", f.deadlineCalls, f.now, f.retryReadyAt)
	}
}

func TestWorkerFiresDueScheduleBeforeCandidateLaunch(t *testing.T) {
	f := newWorkerFixture(0)
	f.mu.Lock()
	f.due = f.now.Add(45 * time.Millisecond)
	f.dueQueue = []*Work{{Kind: KindTest, OwnerID: fixtureOwner, ConnectionID: fixtureConnection, Recipient: "scheduled@example.invalid"}}
	f.mu.Unlock()
	_ = startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.intents == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fireCalls == 0 {
		t.Fatal("dispatcher never consulted a pending schedule")
	}
	if len(f.queue) != 0 || len(f.dueQueue) != 0 {
		t.Fatalf("due schedule was not consumed: queue=%d dueQueue=%d", len(f.queue), len(f.dueQueue))
	}
}

func TestWorkerObservesNewScheduleWhileIdleWithoutWaitingForOldDeadline(t *testing.T) {
	f := newWorkerFixture(0)
	f.mu.Lock()
	f.manualAfter = true
	f.due = f.now.Add(time.Hour)
	f.mu.Unlock()
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.deadlineCalls > 0 })
	// The first deadline timer never fires in this fixture, so only the
	// operator wake can observe the newly stored schedule.
	f.mu.Lock()
	f.now = f.now.Add(10 * time.Minute)
	f.due = f.now
	f.dueQueue = []*Work{{Kind: KindTest, OwnerID: fixtureOwner, ConnectionID: fixtureConnection, Recipient: "rescheduled@example.invalid"}}
	f.mu.Unlock()
	d.Wake()
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.intents == 1 })
}

func startFixture(t *testing.T, f *workerFixture, s Sender) *Dispatcher {
	t.Helper()
	d := NewDispatcher(f, fixedResolver(s), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: f.after})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return d
}
func eventually(t *testing.T, p func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition never reached")
}

func TestWorkerFourSlotsAndShutdownDrainsIndependentRequests(t *testing.T) {
	f := newWorkerFixture(10)
	entered := make(chan context.Context, 10)
	release := make(chan struct{})
	d := startFixture(t, f, workerSender(func(ctx context.Context, _ Message) SendResult {
		entered <- ctx
		<-release
		return SendResult{Kind: OutcomeSubmitted}
	}))
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("four requests did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("fifth request exceeded capacity")
	case <-time.After(10 * time.Millisecond):
	}
	f.mu.Lock()
	workCalls, deadlineCalls, settles := f.nextWorkCalls, f.deadlineCalls, f.settleCalls
	f.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	f.mu.Lock()
	if workCalls != f.nextWorkCalls || deadlineCalls != f.deadlineCalls || settles != f.settleCalls {
		f.mu.Unlock()
		t.Fatal("full dispatcher continued querying SQLite")
	}
	f.mu.Unlock()
	stopped := make(chan error, 1)
	go func() { stopped <- d.Shutdown(context.Background()) }()
	eventually(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return d.stopping })
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain hung")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != 4 || f.intents != 4 {
		t.Fatalf("intents=%d outcomes=%d", f.intents, f.results)
	}
}

const (
	fixtureOwnerTwo      = "operator-two"
	fixtureConnectionTwo = "connection-two"
)

// TestWorkerInFlightForCountsOnlyOwnerRequests proves the dispatcher tracks
// in-flight requests per owner: two mailboxes filling their own slots are
// counted separately, and one owner's requests never inflate another's total.
func TestWorkerInFlightForCountsOnlyOwnerRequests(t *testing.T) {
	f := newWorkerFixture(0)
	f.mu.Lock()
	for i := range 4 {
		owner, connection := fixtureOwner, fixtureConnection
		if i%2 == 1 {
			owner, connection = fixtureOwnerTwo, fixtureConnectionTwo
		}
		f.queue = append(f.queue, &Work{Kind: KindTest, OwnerID: owner, ConnectionID: connection, Recipient: fmt.Sprintf("%d@example.com", i), Subject: "frozen", Body: "<p>body</p>"})
	}
	f.mu.Unlock()
	entered := make(chan context.Context, 4)
	release := make(chan struct{})
	d := startFixture(t, f, workerSender(func(ctx context.Context, _ Message) SendResult {
		entered <- ctx
		<-release
		return SendResult{Kind: OutcomeSubmitted}
	}))
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("four requests did not start")
		}
	}
	total, err := d.InFlight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.InFlightFor(context.Background(), fixtureOwner)
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.InFlightFor(context.Background(), fixtureOwnerTwo)
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || first != 2 || second != 2 {
		t.Fatalf("in-flight total=%d first=%d second=%d, want 4/2/2", total, first, second)
	}
	close(release)
}

func TestWorkerIntentFailureSendsNothingAndCannotWakePastStorageBarrier(t *testing.T) {
	f := newWorkerFixture(2)
	f.intentError = true
	called := make(chan struct{}, 1)
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		called <- struct{}{}
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return d.fatalStorage })
	f.mu.Lock()
	f.intentError = false
	f.mu.Unlock()
	d.Wake()
	select {
	case <-called:
		t.Fatal("storage failure permitted a later send")
	case <-time.After(20 * time.Millisecond):
	}
}

// A provider failure pauses only the affected mailbox: its durable pause ends
// that mailbox's queued work, but the dispatcher-wide stop stays clear so other
// mailboxes keep launching.
func TestWorkerMailboxPauseStopsOnlyItsOwnWork(t *testing.T) {
	f := newWorkerFixture(10)
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeAuthFailure, PauseMailbox: true}
	}))
	eventually(t, func() bool {
		d.mu.Lock()
		inflight, paused := d.inflight, d.paused
		d.mu.Unlock()
		f.mu.Lock()
		defer f.mu.Unlock()
		return inflight == 0 && !paused && f.results >= 1 && len(f.queue) == 0
	})
	f.mu.Lock()
	results, nextWorkCalls := f.results, f.nextWorkCalls
	f.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if results != f.results {
		t.Fatal("paused mailbox sent work after its tasks were paused")
	}
	if nextWorkCalls > f.nextWorkCalls+1 {
		t.Fatal("paused mailbox kept querying SQLite")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.paused || d.fatalStorage {
		t.Fatal("a mailbox-scoped pause stopped the whole dispatcher")
	}
}

func TestWorkerOutcomeSaveFailureHaltsFutureStarts(t *testing.T) {
	f := newWorkerFixture(5)
	f.resultError = true
	called := make(chan struct{}, 5)
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		called <- struct{}{}
		return SendResult{Kind: OutcomeSubmitted}
	}))
	eventually(t, func() bool { d.mu.Lock(); defer d.mu.Unlock(); return d.fatalStorage })
	d.Wake()
	time.Sleep(20 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != 0 {
		t.Fatal("failed outcome unexpectedly saved")
	}
	if f.intents >= 5 {
		t.Fatal("continued launching after storage failure")
	}
}

// When every mailbox is full, due queued work must not make the coordinator
// rescan SQLite in a loop; it waits for a request to complete.
func TestWorkerSaturatedMailboxesWaitWithoutRescanning(t *testing.T) {
	f := newWorkerFixture(0)
	f.mu.Lock()
	f.retryReadyAt = f.now.Add(-time.Minute)
	for i := range 12 {
		owner, connection := fixtureOwner, fixtureConnection
		if i%2 == 1 {
			owner, connection = fixtureOwnerTwo, fixtureConnectionTwo
		}
		f.queue = append(f.queue, &Work{Kind: KindTest, OwnerID: owner, ConnectionID: connection, Recipient: fmt.Sprintf("%d@example.com", i), Subject: "frozen", Body: "<p>body</p>"})
	}
	f.mu.Unlock()
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		entered <- struct{}{}
		<-release
		return SendResult{Kind: OutcomeSubmitted}
	}))
	for range 8 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("both mailboxes did not fill their four slots")
		}
	}
	time.Sleep(30 * time.Millisecond)
	f.mu.Lock()
	workCalls, deadlineCalls, settles := f.nextWorkCalls, f.deadlineCalls, f.settleCalls
	f.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	f.mu.Lock()
	rescanned := workCalls != f.nextWorkCalls || deadlineCalls != f.deadlineCalls || settles != f.settleCalls
	f.mu.Unlock()
	close(release)
	if rescanned {
		t.Fatal("fully saturated dispatcher kept querying SQLite")
	}
}

// Token refresh must happen before a permit is committed. Otherwise several
// goroutines can spend spaced permits and then POST together after one refresh.
func TestWorkerTokenPreparationDoesNotSpendStartsOrBlockStop(t *testing.T) {
	f := newWorkerFixture(5)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	tokens := blockingWorkerTokens{entered: entered, release: release}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	d := startFixture(t, f, NewGraph(tokens, GraphOptions{BaseURL: server.URL}))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("authorization never started")
	}
	f.mu.Lock()
	intents, starts := f.intents, len(f.settings.Starts)
	f.mu.Unlock()
	if intents != 0 || starts != 0 {
		t.Fatalf("authorization wait consumed intent/start: %d/%d", intents, starts)
	}
	stopped := make(chan error, 1)
	go func() {
		stopped <- d.Synchronize(func() error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.queue = nil // durable Stop has removed safe future work
			return nil
		})
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop blocked behind authorization refresh")
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	if posts.Load() != 0 {
		t.Fatal("request launched after Stop")
	}
}

type blockingWorkerTokens struct {
	entered chan struct{}
	release chan struct{}
}

func (s blockingWorkerTokens) AccessToken(ctx context.Context) (string, error) {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
		return "prepared-token", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestWorkerShutdownDrainsRequestsUnderBoundedDeadline(t *testing.T) {
	f := newWorkerFixture(8)
	entered := make(chan struct{}, 4)
	d := NewDispatcher(f, fixedResolver(workerSender(func(ctx context.Context, _ Message) SendResult {
		entered <- struct{}{}
		<-ctx.Done()
		return SendResult{Kind: OutcomeUncertain, PauseMailbox: true}
	})), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: f.after, RequestTimeout: 50 * time.Millisecond})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("requests did not fill capacity")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.intents != 4 || f.results != 4 {
		t.Fatalf("intents/outcomes = %d/%d", f.intents, f.results)
	}
}

func TestWorkerGraphPOSTStartsStaySpacedAfterDelayedAuthorization(t *testing.T) {
	f := newWorkerFixture(3)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	posts := make(chan time.Time, 3)
	observed := make(chan struct{}, 3)
	client := &http.Client{Transport: graphRoundTripFunc(func(*http.Request) (*http.Response, error) {
		posts <- f.clock()
		observed <- struct{}{}
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: http.NoBody}, nil
	})}
	// Advance the controlled clock only after the previous request reached its
	// actual HTTP transport. This exposes actual POST starts in clock order.
	after := func(delay time.Duration) <-chan time.Time {
		select {
		case <-observed:
		case <-time.After(20 * time.Millisecond):
		}
		f.mu.Lock()
		f.now = f.now.Add(delay)
		f.mu.Unlock()
		return time.After(time.Millisecond)
	}
	d := NewDispatcher(f, fixedResolver(NewGraph(blockingWorkerTokens{entered: entered, release: release}, GraphOptions{HTTPClient: client})), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: after})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	f.mu.Lock()
	f.now = f.now.Add(30 * time.Second)
	releasedAt := f.now
	if f.intents != 0 {
		t.Error("intent recorded before authorization completed")
	}
	f.mu.Unlock()
	close(release)
	var previous time.Time
	for range 3 {
		select {
		case at := <-posts:
			if at.Before(releasedAt) {
				t.Fatal("POST used a permit preceding token refresh")
			}
			if !previous.IsZero() && at.Sub(previous) < 2*time.Second {
				t.Fatalf("POST starts bunched: %s", at.Sub(previous))
			}
			previous = at
		case <-time.After(time.Second):
			t.Fatal("POST did not start")
		}
	}
}

// TestWorkerCopyHookRunsOnlyAfterDurableSubmission proves the ordering the
// recovery rules depend on: the accepted outcome and its timestamp are stored
// before any separate copy work starts.
func TestWorkerCopyHookRunsAfterDurableSubmission(t *testing.T) {
	f := newWorkerFixture(1)
	startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(ctx context.Context, acceptedAt time.Time) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("copy work must carry a bounded deadline")
			}
			if acceptedAt.IsZero() {
				t.Error("copy work must receive the recorded acceptance time")
			}
			f.mu.Lock()
			f.copyCalls++
			f.copySawResults = f.results
			f.mu.Unlock()
			return nil
		}}
	}))
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.copyCalls == 1 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != 1 || f.copySawResults != 1 {
		t.Fatalf("outcomes=%d, outcomes at copy time=%d; the submission must be durable first", f.results, f.copySawResults)
	}
	if f.warnings != 0 {
		t.Fatalf("copy warnings = %d, want none for a confirmed copy", f.warnings)
	}
}

// TestWorkerHoldsInFlightUntilCopySettles proves a launched copy keeps its
// mailbox slot and its owner's drain obligation.
func TestWorkerHoldsInFlightUntilCopySettles(t *testing.T) {
	f := newWorkerFixture(1)
	entered := make(chan struct{})
	release := make(chan struct{})
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(context.Context, time.Time) error {
			close(entered)
			<-release
			return nil
		}}
	}))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("copy work did not start")
	}
	f.mu.Lock()
	results := f.results
	f.mu.Unlock()
	if results != 1 {
		t.Fatalf("outcomes = %d while the copy was open, want the durable submission first", results)
	}
	for _, name := range []string{"global", "owner"} {
		var count int
		var err error
		if name == "global" {
			count, err = d.InFlight(context.Background())
		} else {
			count, err = d.InFlightFor(context.Background(), fixtureOwner)
		}
		if err != nil || count != 1 {
			t.Fatalf("%s in-flight = %d (%v) while copy was open, want one", name, count, err)
		}
	}
	close(release)
	eventually(t, func() bool {
		global, err := d.InFlight(context.Background())
		return err == nil && global == 0
	})
}

// TestWorkerCopyFailureWarnsWithoutStoppingSending proves a failed copy is a
// warning on the accepted attempt, not a sending failure: no pause and the
// mailbox keeps launching.
func TestWorkerCopyFailureWarnsWithoutStoppingSending(t *testing.T) {
	f := newWorkerFixture(2)
	startFixture(t, f, workerSender(func(_ context.Context, message Message) SendResult {
		if message.Recipient != "0@example.com" {
			return SendResult{Kind: OutcomeSubmitted}
		}
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(context.Context, time.Time) error {
			return ErrTitanSentCopy
		}}
	}))
	eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.results == 2 && f.warnings == 1
	})
	f.mu.Lock()
	warning := f.lastWarning
	f.mu.Unlock()
	if warning != SentCopyWarning {
		t.Fatalf("warning = %q, want the fixed %q", warning, SentCopyWarning)
	}
}

// TestWorkerOutcomeFailureSkipsCopy proves copy work never begins when the
// accepted outcome could not be stored.
func TestWorkerOutcomeFailureSkipsCopy(t *testing.T) {
	f := newWorkerFixture(1)
	f.mu.Lock()
	f.resultError = true
	f.mu.Unlock()
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(context.Context, time.Time) error {
			f.mu.Lock()
			f.copyCalls++
			f.mu.Unlock()
			return nil
		}}
	}))
	eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.fatalStorage
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.copyCalls != 0 {
		t.Fatal("copy work started although the submission result could not be stored")
	}
}

// TestWorkerCopyWarningStorageFailureKeepsSubmitted applies the existing
// storage barrier when the warning itself cannot be saved.
func TestWorkerCopyWarningStorageFailureKeepsSubmitted(t *testing.T) {
	f := newWorkerFixture(1)
	f.mu.Lock()
	f.warningError = true
	f.mu.Unlock()
	d := startFixture(t, f, workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(context.Context, time.Time) error {
			return ErrTitanSentCopy
		}}
	}))
	eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.fatalStorage
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != 1 {
		t.Fatalf("outcomes = %d, want the committed submission to survive", f.results)
	}
}

// TestWorkerShutdownDrainsCopyWithinRequestDeadline proves stop waits for
// launched copy work and that the copy is bounded by the request deadline
// rather than an unlimited wait.
func TestWorkerShutdownDrainsCopyWithinRequestDeadline(t *testing.T) {
	f := newWorkerFixture(1)
	entered := make(chan struct{})
	d := NewDispatcher(f, fixedResolver(workerSender(func(context.Context, Message) SendResult {
		return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(ctx context.Context, _ time.Time) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}}
	})), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: f.after, RequestTimeout: 20 * time.Millisecond})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("copy work did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown did not settle the copy: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results != 1 {
		t.Fatalf("outcomes = %d, want the submission retained", f.results)
	}
}

// TestWorkerCopyDoesNotBlockAnotherMailbox proves a slow Sent copy occupies only
// its own mailbox slot: another operator's submission completes while the copy
// is still open, and the copied mailbox still reports its drain obligation.
func TestWorkerCopyDoesNotBlockAnotherMailbox(t *testing.T) {
	f := newWorkerFixture(0)
	f.mu.Lock()
	f.queue = append(f.queue,
		&Work{Kind: KindTest, OwnerID: fixtureOwner, ConnectionID: fixtureConnection, Recipient: "slow@example.com", Subject: "s", Body: "<p>b</p>"},
		&Work{Kind: KindTest, OwnerID: fixtureOwnerTwo, ConnectionID: fixtureConnectionTwo, Recipient: "fast@example.com", Subject: "s", Body: "<p>b</p>"},
	)
	f.mu.Unlock()
	release := make(chan struct{})
	entered := make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	d := startFixture(t, f, workerSender(func(_ context.Context, message Message) SendResult {
		if message.Recipient == "slow@example.com" {
			return SendResult{Kind: OutcomeSubmitted, SaveCopy: func(context.Context, time.Time) error {
				close(entered)
				<-release
				return nil
			}}
		}
		return SendResult{Kind: OutcomeSubmitted}
	}))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("copy work did not start")
	}
	// Both submissions are recorded while the first mailbox's copy is still open.
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.results == 2 })
	if count, err := d.InFlightFor(context.Background(), fixtureOwner); err != nil || count != 1 {
		t.Fatalf("copying mailbox in-flight = %d (%v), want its slot held until the copy settles", count, err)
	}
	if count, err := d.InFlightFor(context.Background(), fixtureOwnerTwo); err != nil || count != 0 {
		t.Fatalf("other mailbox in-flight = %d (%v), want it free", count, err)
	}
	unblock()
	eventually(t, func() bool {
		count, err := d.InFlightFor(context.Background(), fixtureOwner)
		return err == nil && count == 0
	})
}
