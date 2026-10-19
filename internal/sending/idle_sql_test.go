package sending_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

type observedIdleStore struct {
	*sending.Store
	checked chan struct{}
}

func (s *observedIdleStore) NextWorkDeadline(ctx context.Context) (time.Time, error) {
	at, err := s.Store.NextWorkDeadline(ctx)
	if err == nil && at.IsZero() {
		select {
		case s.checked <- struct{}{}:
		default:
		}
	}
	return at, err
}

// Count real SQL on both PocketBase connection pools, including transactions.
// An idle interval longer than the former 250ms poll must execute no queries.
func TestIdleDispatcherExecutesNoRecurringSQLAndWakesForTest(t *testing.T) {
	f := newAcceptanceFixture(t, false)
	task := f.task(t, 1)
	store := &observedIdleStore{Store: sending.NewStore(f.app, sending.Options{}), checked: make(chan struct{}, 1)}
	var calls atomic.Int64
	type savedLogger struct {
		db    *dbx.DB
		query dbx.QueryLogFunc
		exec  dbx.ExecLogFunc
	}
	var saved []savedLogger
	for _, builder := range []dbx.Builder{f.app.ConcurrentDB(), f.app.NonconcurrentDB()} {
		db := builder.(*dbx.DB)
		query, exec := db.QueryLogFunc, db.ExecLogFunc
		saved = append(saved, savedLogger{db: db, query: query, exec: exec})
		db.QueryLogFunc = func(ctx context.Context, elapsed time.Duration, sql string, rows *sql.Rows, err error) {
			calls.Add(1)
			if query != nil {
				query(ctx, elapsed, sql, rows, err)
			}
		}
		db.ExecLogFunc = func(ctx context.Context, elapsed time.Duration, sql string, result sql.Result, err error) {
			calls.Add(1)
			if exec != nil {
				exec(ctx, elapsed, sql, result, err)
			}
		}
	}
	mailbox := &acceptanceMailbox{}
	d := sending.NewDispatcher(store, mailbox, sending.WorkerOptions{DataDir: f.app.DataDir()})
	if err := d.Start(); err != nil {
		for _, logger := range saved {
			logger.db.QueryLogFunc, logger.db.ExecLogFunc = logger.query, logger.exec
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
			return
		}
		for _, logger := range saved {
			logger.db.QueryLogFunc, logger.db.ExecLogFunc = logger.query, logger.exec
		}
	})
	select {
	case <-store.checked:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher did not finish its initial idle probe")
	}
	// InFlight takes the coordinator mutex, so even a final settlement query
	// following the deadline check has completed before this snapshot.
	if _, err := d.InFlight(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if before == 0 {
		t.Fatal("SQL instrumentation did not observe the startup probe")
	}
	time.Sleep(750 * time.Millisecond)
	if got := calls.Load(); got != before {
		t.Fatalf("idle dispatcher executed %d additional SQL calls", got-before)
	}
	attempt, err := store.QueueTest(context.Background(), sending.TestRequest{
		ActionID: "idle-sql-test", TaskID: task, Recipient: "operator@example.invalid",
		Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.Wake()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		active, err := d.InFlight(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if mailbox.count() == 1 && active == 0 {
			r, err := f.app.FindRecordById(sending.AttemptsCollection, attempt.ID)
			if err != nil || r.GetString("state") != sending.AttemptSubmitted {
				t.Fatalf("woken test did not persist its acceptance: %v", err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("explicit wake did not dispatch the queued test")
}
