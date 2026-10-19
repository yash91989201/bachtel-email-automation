package sending

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// DispatchStore is the durable application state machine, not an in-memory queue.
type DispatchStore interface {
	DispatcherState(context.Context) (Settings, error)
	NextWork(context.Context) (*Work, error)
	BeginAttempt(context.Context, *Work, time.Time, time.Time) (*Attempt, error)
	RecordActualStart(context.Context, string, time.Time, time.Time) error
	CompleteAttempt(context.Context, string, Outcome) error
	SetCooldown(context.Context, time.Time, string) error
	Recover(context.Context) error
	PauseMailbox(context.Context, string) error
	PauseTask(context.Context, string, string) error
	SettleTasks(context.Context) error
}

type WorkerOptions struct {
	DataDir        string
	RequestTimeout time.Duration
	Now            func() time.Time
	After          func(time.Duration) <-chan time.Time
}

type Dispatcher struct {
	store                     DispatchStore
	sender                    Sender
	options                   WorkerOptions
	mu                        sync.Mutex // serializes launch, stop, and response safety barriers
	wake                      chan struct{}
	done                      chan struct{}
	ctx                       context.Context
	cancel                    context.CancelFunc
	lock                      *ownershipLock
	started, stopping, paused bool
	fatalStorage              bool
	inflight                  int
	lastStart                 time.Time
	limiter                   *rate.Limiter
}

func NewDispatcher(store DispatchStore, sender Sender, options WorkerOptions) *Dispatcher {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.After == nil {
		options.After = time.After
	}
	if options.RequestTimeout <= 0 || options.RequestTimeout > 30*time.Second {
		options.RequestTimeout = 20 * time.Second
	}
	return &Dispatcher{store: store, sender: sender, options: options, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

// Start obtains exclusive process ownership before doing any recovery or sending.
func (d *Dispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return fmt.Errorf("dispatcher already started")
	}
	lock, err := acquireOwnership(d.options.DataDir)
	if err != nil {
		return err
	}
	if err = d.store.Recover(context.Background()); err != nil {
		lock.Close()
		return err
	}
	d.lock = lock
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.limiter = rate.NewLimiter(rate.Every(time.Minute/DefaultRate), 1)
	d.started = true
	go d.run()
	return nil
}

func (d *Dispatcher) notify() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) InFlight(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inflight, nil
}

// Wake is called after an explicit operator action; durable paused tasks remain paused.
func (d *Dispatcher) Wake() {
	d.mu.Lock()
	if !d.fatalStorage {
		d.paused = false
	}
	d.mu.Unlock()
	d.notify()
}

// Synchronize runs an operator mutation behind the same barrier as new starts.
// Stop actions must use this to guarantee that a launch cannot cross their return.
func (d *Dispatcher) Synchronize(action func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := action()
	if err == nil {
		if !d.fatalStorage {
			d.paused = false
		}
	}
	d.notify()
	return err
}

// Shutdown cancels only the coordinator. Requests keep independent deadlines and
// all outcomes are recorded before process ownership is released.
func (d *Dispatcher) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return nil
	}
	d.stopping = true
	d.cancel()
	d.mu.Unlock()
	d.notify()
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		if d.stopping && d.inflight == 0 {
			_ = d.store.SettleTasks(context.Background())
			_ = d.lock.Close()
			d.mu.Unlock()
			return
		}
		delay := 250 * time.Millisecond
		if !d.stopping && !d.paused && d.inflight < 4 {
			var err error
			delay, err = d.launch()
			if err != nil {
				d.pause("Sending stopped because its durable state could not be saved. Restart after checking storage.")
			}
		}
		if err := d.store.SettleTasks(context.Background()); err != nil {
			d.pause("Sending stopped because task progress could not be saved. Restart after checking storage.")
		}
		d.mu.Unlock()
		if delay <= 0 {
			continue
		}
		select {
		case <-d.wake:
		case <-d.options.After(delay):
		}
	}
}

func (d *Dispatcher) pause(reason string) {
	d.paused = true
	d.fatalStorage = true
	_ = d.store.PauseMailbox(context.Background(), reason)
}

func (d *Dispatcher) launch() (time.Duration, error) {
	return d.launchPrepared(d.sender, false)
}

func (d *Dispatcher) launchPrepared(sender Sender, prepared bool) (time.Duration, error) {
	ctx := context.Background()
	s, err := d.store.DispatcherState(ctx)
	if err != nil {
		return 0, err
	}
	if s.Rate == 0 {
		s.Rate = DefaultRate
	}
	if err = ValidateRate(s.Rate); err != nil {
		return 0, err
	}
	now := d.options.Now()
	if !d.lastStart.IsZero() && d.lastStart.Add(time.Minute/time.Duration(s.Rate)).After(s.NextEligible) {
		s.NextEligible = d.lastStart.Add(time.Minute / time.Duration(s.Rate))
	}
	if next := nextStart(now, s); next.After(now) {
		return next.Sub(now), nil
	}
	work, err := d.store.NextWork(ctx)
	if err != nil || work == nil {
		return 250 * time.Millisecond, err
	}
	if preparation, ok := sender.(PreparingSender); ok && !prepared {
		// Authorization may refresh over the network. Stop/operator actions must
		// remain available while it waits, and no paced start is spent yet.
		d.mu.Unlock()
		prepareCtx, cancel := context.WithTimeout(d.ctx, d.options.RequestTimeout)
		ready := preparation.Prepare(prepareCtx)
		cancel()
		d.mu.Lock()
		if d.stopping || d.paused || d.fatalStorage {
			return 0, nil
		}
		// Re-read both work and pacing: operator actions, in-flight responses,
		// cooldowns and configuration may have changed while authorization waited.
		return d.launchPrepared(ready, true)
	}
	d.limiter.SetLimitAt(now, rate.Every(time.Minute/time.Duration(s.Rate)))
	if !d.limiter.AllowN(now, 1) {
		return time.Minute / time.Duration(s.Rate), nil
	}
	attempt, err := d.store.BeginAttempt(ctx, work, now, now.Add(time.Minute/time.Duration(s.Rate)))
	if errors.Is(err, ErrNoWork) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	// The request goroutine must reach its launch barrier before the
	// coordinator may consider another opportunity. A delayed goroutine therefore
	// cannot spend an old permit beside a later request.
	type startResult struct {
		at  time.Time
		err error
	}
	started := make(chan startResult, 1)
	go func() {
		actual := d.options.Now()
		err := d.store.RecordActualStart(ctx, attempt.ID, actual, actual.Add(time.Minute/time.Duration(s.Rate)))
		if err != nil {
			started <- startResult{err: err}
			return
		}
		started <- startResult{at: d.options.Now()}
		d.send(sender, attempt, &attempt.Work)
	}()
	start := <-started
	if start.err != nil {
		return 0, start.err
	}
	d.lastStart = start.at
	d.inflight++
	return 0, nil
}

func (d *Dispatcher) send(sender Sender, attempt *Attempt, work *Work) {
	ctx, cancel := context.WithTimeout(context.Background(), d.options.RequestTimeout)
	defer cancel()
	result := sender.Send(ctx, Message{Recipient: work.Recipient, Subject: work.Subject, HTML: work.Body})
	d.mu.Lock()
	defer d.mu.Unlock()
	outcome := Outcome{Reason: result.Error, RetryAt: result.RetryAfter, PauseMailbox: result.PauseMailbox, RequiresCorrection: result.RequiresCorrection}
	switch result.Kind {
	case OutcomeSubmitted:
		outcome.State = AttemptSubmitted
	case OutcomeDefiniteFailure:
		outcome.State = AttemptFailed
	case OutcomeAuthFailure:
		outcome.State = AttemptAuth
		outcome.PauseMailbox = true
	case OutcomeSafeRetry:
		outcome.State = AttemptRetryable
		if outcome.RetryAt.IsZero() {
			outcome.RetryAt = d.options.Now().Add(retryDelay(work.RetryCount))
		}
		if err := d.store.SetCooldown(context.Background(), outcome.RetryAt, "Microsoft throttling or safe retry wait"); err != nil {
			d.pause("Sending stopped because the cooldown could not be saved. Restart after checking storage.")
		}
	default:
		outcome.State = AttemptReview
		outcome.PauseMailbox = true
	}
	// Pause the in-process coordinator before any persistence call can fail.
	if outcome.PauseMailbox {
		d.paused = true
	}
	if err := d.store.CompleteAttempt(context.Background(), attempt.ID, outcome); err != nil {
		d.pause("Sending stopped because a submission outcome could not be saved. Review unfinished attempts after restarting.")
	}
	if outcome.PauseMailbox {
		if err := d.store.PauseMailbox(context.Background(), outcome.Reason); err != nil {
			d.pause("Sending stopped because the mailbox pause could not be saved. Restart after checking storage.")
		}
	}
	d.inflight--
	d.notify()
}

func retryDelay(retries int) time.Duration {
	if retries < 0 {
		retries = 0
	}
	if retries > 5 {
		retries = 5
	}
	// Bounded jitter prevents synchronized retrying clients while preserving a
	// minimum safe backoff; the shared coordinator still paces every retry.
	base := time.Second * time.Duration(1<<retries)
	return base + time.Duration(time.Now().UnixNano()%int64(base/2+1))
}
