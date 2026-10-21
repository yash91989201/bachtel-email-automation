package sending

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// DispatchStore is the durable application state machine, not an in-memory
// queue. Every method is mailbox-scoped: pacing, cooldowns and pauses belong to
// one connection, while a shared storage failure stops every launch.
type DispatchStore interface {
	DispatcherState(context.Context, string, string) (Settings, error)
	NextWork(context.Context) (*Work, error)
	NextWorkDeadline(context.Context) (time.Time, error)
	FireScheduled(context.Context) (bool, error)
	BeginAttempt(context.Context, *Work, time.Time, time.Time) (*Attempt, error)
	RecordActualStart(context.Context, *Work, string, time.Time, time.Time) error
	CompleteAttempt(context.Context, string, Outcome) error
	SetCooldown(context.Context, string, string, time.Time, string) error
	Recover(context.Context) error
	PauseMailbox(context.Context, string, string) error
	PauseAll(context.Context, string) error
	PauseTask(context.Context, string, string, string) error
	SettleTasks(context.Context) error
}

type WorkerOptions struct {
	DataDir        string
	RequestTimeout time.Duration
	Now            func() time.Time
	After          func(time.Duration) <-chan time.Time
}

// Dispatcher is the single owner of the datastore's sending state. It resolves
// each work item's mailbox connection to a concrete sender, paces that mailbox
// independently, and keeps at most four simultaneous requests overall.
type Dispatcher struct {
	store                     DispatchStore
	resolver                  Resolver
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
	inflightByConnection      map[string]int
	inflightByOwner           map[string]int
	lastStart                 map[string]time.Time
	limiters                  map[string]*rate.Limiter
	lifecycleErr              error
}

func NewDispatcher(store DispatchStore, resolver Resolver, options WorkerOptions) *Dispatcher {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.After == nil {
		options.After = time.After
	}
	if options.RequestTimeout <= 0 || options.RequestTimeout > 30*time.Second {
		options.RequestTimeout = 20 * time.Second
	}
	return &Dispatcher{
		store:                store,
		resolver:             resolver,
		options:              options,
		wake:                 make(chan struct{}, 1),
		done:                 make(chan struct{}),
		inflightByConnection: make(map[string]int),
		inflightByOwner:      make(map[string]int),
		lastStart:            make(map[string]time.Time),
		limiters:             make(map[string]*rate.Limiter),
	}
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
		return errors.Join(err, lock.Close())
	}
	d.lock = lock
	d.ctx, d.cancel = context.WithCancel(context.Background())
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

// InFlight reports how many requests are currently outstanding.
func (d *Dispatcher) InFlight(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inflight, nil
}

// InFlightFor reports how many requests the operator's mailbox currently has in
// flight, so a connection change waits only for its own work.
func (d *Dispatcher) InFlightFor(ctx context.Context, ownerID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inflightByOwner[ownerID], nil
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
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.lifecycleErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		if d.stopping && d.inflight == 0 {
			d.lifecycleErr = errors.Join(d.lifecycleErr, d.store.SettleTasks(context.Background()), d.lock.Close())
			d.mu.Unlock()
			return
		}
		delay := time.Duration(0)
		if !d.stopping && !d.paused && d.inflight < 4 {
			var err error
			delay, err = d.launch()
			if err != nil {
				d.pause("Sending stopped because its durable state could not be saved. Restart after checking storage.")
			}
		}
		// A negative delay means the just-launched attempt should be followed by
		// another opportunity check so the coordinator can fill available slots.
		if delay < 0 {
			d.mu.Unlock()
			continue
		}
		// Settle only when an event or a real timer brought the coordinator here.
		// A full, paused or idle dispatcher then sleeps until an event instead of
		// repeatedly querying and updating SQLite.
		if !d.fatalStorage {
			if err := d.store.SettleTasks(context.Background()); err != nil {
				d.pause("Sending stopped because task progress could not be saved. Restart after checking storage.")
			}
		}
		d.mu.Unlock()
		select {
		case <-d.wake:
		case <-timer(d.options.After, delay):
		}
	}
}

func timer(after func(time.Duration) <-chan time.Time, delay time.Duration) <-chan time.Time {
	if delay <= 0 {
		return nil
	}
	return after(delay)
}

func (d *Dispatcher) pause(reason string) {
	d.paused = true
	d.fatalStorage = true
	d.lifecycleErr = errors.Join(d.lifecycleErr, d.store.PauseAll(context.Background(), reason))
}

// resolveSender binds the work item's mailbox connection to its provider
// sender. An unusable connection is reported with a fixed reason and a short
// cooldown so the coordinator does not spin.
func (d *Dispatcher) resolveSender(connectionID string) (Sender, bool) {
	ctx, cancel := context.WithTimeout(d.ctx, d.options.RequestTimeout)
	sender, err := d.resolver.Sender(ctx, connectionID)
	cancel()
	if err != nil || sender == nil {
		return nil, false
	}
	return sender, true
}

func (d *Dispatcher) launch() (time.Duration, error) {
	return d.launchPrepared(nil, nil, false)
}

// launchPrepared launches one opportunity. A prepared sender skips the
// authorization step that must run outside the start barrier.
func (d *Dispatcher) launchPrepared(sender Sender, work *Work, prepared bool) (time.Duration, error) {
	ctx := context.Background()
	// Due schedules are evaluated before normal launches so a due task starts
	// even while another task is running, sharing the same pacing and slots.
	fired, err := d.store.FireScheduled(ctx)
	if err != nil {
		return 0, err
	}
	if fired && work == nil {
		return -1, nil
	}
	if work == nil {
		work, err = d.store.NextWork(ctx)
		if err != nil {
			return 0, err
		}
	}
	if work == nil {
		deadline, err := d.store.NextWorkDeadline(ctx)
		if err != nil {
			return 0, err
		}
		now := d.options.Now()
		if deadline.After(now) {
			return deadline.Sub(now), nil
		}
		if !deadline.IsZero() {
			return -1, nil
		}
		return 0, nil
	}

	settings, err := d.store.DispatcherState(ctx, work.ConnectionID, work.OwnerID)
	if err != nil {
		return 0, err
	}
	if settings.Rate == 0 {
		settings.Rate = DefaultRate
	}
	if err = ValidateRate(settings.Rate); err != nil {
		return 0, err
	}
	now := d.options.Now()
	interval := time.Minute / time.Duration(settings.Rate)
	if last := d.lastStart[work.ConnectionID]; !last.IsZero() && last.Add(interval).After(settings.NextEligible) {
		settings.NextEligible = last.Add(interval)
	}
	if next := nextStart(now, settings); next.After(now) {
		return next.Sub(now), nil
	}
	if d.inflightByConnection[work.ConnectionID] >= 4 {
		// The global cap already bounds this; keep the per-mailbox bound
		// explicit for the documented contract.
		return 0, nil
	}

	if sender == nil {
		resolved, ok := d.resolveSender(work.ConnectionID)
		if !ok {
			// The connection cannot produce a sender right now (unknown
			// provider, unreadable credential, disconnected). Delay this
			// mailbox without touching another mailbox's work.
			if err := d.store.SetCooldown(ctx, work.ConnectionID, work.OwnerID, now.Add(time.Minute), "The mailbox connection is not usable; reconnect it."); err != nil {
				return 0, err
			}
			return time.Minute, nil
		}
		sender = resolved
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
		// Re-read work and pacing: operator actions, in-flight responses,
		// cooldowns and configuration may have changed while authorization waited.
		return d.launchPrepared(ready, work, true)
	}

	limiter := d.limiters[work.ConnectionID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(interval), 1)
		d.limiters[work.ConnectionID] = limiter
	}
	limiter.SetLimitAt(now, rate.Every(interval))
	if !limiter.AllowN(now, 1) {
		return interval, nil
	}

	attempt, err := d.store.BeginAttempt(ctx, work, now, now.Add(interval))
	if errors.Is(err, ErrNoWork) {
		return -1, nil
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
		err := d.store.RecordActualStart(ctx, &attempt.Work, attempt.ID, actual, actual.Add(interval))
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
	d.lastStart[work.ConnectionID] = start.at
	d.inflight++
	d.inflightByConnection[work.ConnectionID]++
	d.inflightByOwner[work.OwnerID]++
	return -1, nil
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
		if err := d.store.SetCooldown(context.Background(), work.ConnectionID, work.OwnerID, outcome.RetryAt, "Provider throttling or safe retry wait"); err != nil {
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
		if err := d.store.PauseMailbox(context.Background(), work.OwnerID, outcome.Reason); err != nil {
			d.pause("Sending stopped because the mailbox pause could not be saved. Restart after checking storage.")
		}
	}
	d.inflight--
	d.inflightByConnection[work.ConnectionID]--
	if d.inflightByConnection[work.ConnectionID] <= 0 {
		delete(d.inflightByConnection, work.ConnectionID)
	}
	d.inflightByOwner[work.OwnerID]--
	if d.inflightByOwner[work.OwnerID] <= 0 {
		delete(d.inflightByOwner, work.OwnerID)
	}
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
