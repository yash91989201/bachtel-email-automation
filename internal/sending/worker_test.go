package sending

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workerFixture struct {
	mu                       sync.Mutex
	now                      time.Time
	queue                    []*Work
	settings                 Settings
	intents, results         int
	intentError, resultError bool
	recovered                bool
}

func (f *workerFixture) clock() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *workerFixture) after(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return time.After(time.Millisecond)
}
func (f *workerFixture) DispatcherState(context.Context) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settings, nil
}
func (f *workerFixture) NextWork(context.Context) (*Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return nil, nil
	}
	return f.queue[0], nil
}
func (f *workerFixture) BeginAttempt(_ context.Context, w *Work, start, next time.Time) (*Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.intentError {
		return nil, errors.New("disk failure")
	}
	f.queue = f.queue[1:]
	f.intents++
	return &Attempt{ID: fmt.Sprint(f.intents), Work: *w}, nil
}
func (f *workerFixture) RecordActualStart(_ context.Context, _ string, start, next time.Time) error {
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
func (f *workerFixture) SetCooldown(_ context.Context, until time.Time, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings.CooldownUntil = until
	return nil
}
func (f *workerFixture) Recover(context.Context) error                   { f.recovered = true; return nil }
func (f *workerFixture) PauseMailbox(context.Context, string) error      { return nil }
func (f *workerFixture) PauseTask(context.Context, string, string) error { return nil }
func (f *workerFixture) SettleTasks(context.Context) error               { return nil }

type workerSender func(context.Context, Message) SendResult

func (s workerSender) Send(ctx context.Context, m Message) SendResult { return s(ctx, m) }

func newWorkerFixture(n int) *workerFixture {
	f := &workerFixture{now: time.Now().UTC(), settings: Settings{Rate: 30}}
	for i := 0; i < n; i++ {
		f.queue = append(f.queue, &Work{Kind: KindTest, Recipient: fmt.Sprintf("%d@example.com", i), Subject: "frozen", Body: "<p>body</p>"})
	}
	return f
}
func startFixture(t *testing.T, f *workerFixture, s Sender) *Dispatcher {
	t.Helper()
	d := NewDispatcher(f, s, WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: f.after})
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
	for i := 0; i < 4; i++ {
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
	d := NewDispatcher(f, workerSender(func(ctx context.Context, _ Message) SendResult {
		entered <- struct{}{}
		<-ctx.Done()
		return SendResult{Kind: OutcomeUncertain, PauseMailbox: true}
	}), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: f.after, RequestTimeout: 50 * time.Millisecond})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
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
	d := NewDispatcher(f, NewGraph(blockingWorkerTokens{entered: entered, release: release}, GraphOptions{HTTPClient: client}), WorkerOptions{DataDir: t.TempDir(), Now: f.clock, After: after})
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
	for i := 0; i < 3; i++ {
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
