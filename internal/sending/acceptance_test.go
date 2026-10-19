// These acceptance tests use real XLSX intake, migrations and SQLite history,
// and a simulated mailbox. No test contacts Microsoft or sends a real email.
package sending_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

type acceptanceFixture struct {
	app        core.App
	store      *sending.Store
	intake     *intake.Service
	campaignID string
	clock      *acceptanceClock
}

// The clock advances only when the worker waits; database work and goroutine
// scheduling therefore cannot turn this 40-minute campaign into a slow test.
type acceptanceClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *acceptanceClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *acceptanceClock) After(d time.Duration) <-chan time.Time {
	time.Sleep(time.Millisecond)
	c.mu.Lock()
	if d > 0 {
		c.now = c.now.Add(d)
	}
	now := c.now
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return ch
}

func newAcceptanceFixture(t *testing.T, allowed bool) *acceptanceFixture {
	t.Helper()
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
	stub := filepath.Join(t.TempDir(), "pandoc")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = \"--version\" ]; then echo 'pandoc acceptance'; exit 0; fi\ndone\nprintf '%s' '<p>Hello {{candidate_name}}</p>'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewServiceWithConverter(app, convert.Converter{Path: stub})
	created, err := campaigns.CreateCampaign(context.Background(), campaign.CreateCampaignInput{Name: "Simulated acceptance", SourceHeaders: []string{"Email", "Name"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc bytes.Buffer
	z := zip.NewWriter(&doc)
	part, err := z.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p/></w:body></w:document>`)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = campaigns.CreateRevision(context.Background(), created.ID, campaign.RevisionInput{Subject: "Hello {{candidate_name}}", Document: doc.Bytes(), DocumentName: "body.docx", Mappings: []campaign.Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail}, {Variable: "candidate_name", Header: "Name", Required: true, Format: campaign.FormatName}}})
	if err != nil {
		t.Fatal(err)
	}
	clock := &acceptanceClock{now: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)}
	return &acceptanceFixture{app: app, store: sending.NewStore(app, sending.Options{AllowCandidateSending: allowed, Now: clock.Now}), intake: intake.NewService(app, campaigns, intake.Limits{}), campaignID: created.ID, clock: clock}
}

func (f *acceptanceFixture) task(t *testing.T, count int) string {
	t.Helper()
	book := excelize.NewFile()
	defer book.Close()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		row := []string{fmt.Sprintf("candidate%04d@example.invalid", i), fmt.Sprintf("Candidate %04d", i)}
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", i+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), f.campaignID, "acceptance.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), task.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

type acceptanceMailbox struct {
	mu                sync.Mutex
	messages          []sending.Message
	active, maxActive int
	hold              <-chan struct{}
	uncertain         string
}

func (m *acceptanceMailbox) Send(ctx context.Context, message sending.Message) sending.SendResult {
	m.mu.Lock()
	m.messages = append(m.messages, message)
	m.active++
	if m.active > m.maxActive {
		m.maxActive = m.active
	}
	hold := m.hold
	uncertain := m.uncertain
	m.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	m.mu.Lock()
	m.active--
	m.mu.Unlock()
	if message.Recipient == uncertain {
		return sending.SendResult{Kind: sending.OutcomeUncertain, Error: "simulated disconnect", PauseMailbox: true}
	}
	return sending.SendResult{Kind: sending.OutcomeSubmitted}
}
func (m *acceptanceMailbox) count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.messages) }
func acceptanceEventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("simulated dispatch did not reach its expected durable state")
}
func (f *acceptanceFixture) dispatcher(t *testing.T, mailbox sending.Sender) *sending.Dispatcher {
	t.Helper()
	d := sending.NewDispatcher(f.store, mailbox, sending.WorkerOptions{DataDir: f.app.DataDir(), Now: f.clock.Now, After: f.clock.After, RequestTimeout: 3 * time.Second})
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return d
}
func (f *acceptanceFixture) approve(t *testing.T, task string, d *sending.Dispatcher) {
	t.Helper()
	attempt, err := f.store.QueueTest(context.Background(), sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Actor: "acceptance", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(sending.AttemptsCollection, attempt.ID)
		return e == nil && r.GetString("state") == sending.AttemptSubmitted
	})
	if err = f.store.Approve(context.Background(), task, attempt.ID, "acceptance", "approve-"+task); err != nil {
		t.Fatal(err)
	}
}
func acceptanceAct(t *testing.T, store *sending.Store, task, kind, id string, count int) *sending.ActionResult {
	t.Helper()
	result, err := store.Act(context.Background(), sending.ActionRequest{ActionID: id, TaskID: task, Kind: kind, Actor: "acceptance", Confirmed: true, ExpectedCount: count})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAcceptanceThousandCandidatePipelineAndReplay(t *testing.T) {
	f := newAcceptanceFixture(t, true)
	task := f.task(t, 1000)
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)
	// Approval is an explicit action following a separately submitted test.
	if _, err := f.store.Act(context.Background(), sending.ActionRequest{ActionID: "unapproved", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 1000}); err == nil {
		t.Fatal("unapproved candidate start succeeded")
	}
	f.approve(t, task, d)
	p, err := f.store.Preview(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if p.Submitted != 0 || p.Eligible != 1000 {
		t.Fatalf("test changed candidate eligibility: %+v", p)
	}
	first := acceptanceAct(t, f.store, task, "start", "start-1000", 1000)
	if first.Claimed != 1000 {
		t.Fatalf("claimed %d, want 1000", first.Claimed)
	}
	repeated := acceptanceAct(t, f.store, task, "start", "start-1000", 1000)
	if !repeated.Repeated || repeated.Claimed != 1000 {
		t.Fatalf("replay = %+v", repeated)
	}
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(intake.TasksCollection, task)
		return e == nil && r.GetString("status") == "completed"
	})
	p, err = f.store.Preview(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if p.Submitted != 1000 || p.Remaining != 0 || p.Held != 0 || p.InFlight != 0 || !p.ArtifactNeeded {
		t.Fatalf("final progress = %+v", p)
	}
	attempts, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "", "started_at", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1001 || mailbox.count() != 1001 {
		t.Fatalf("attempts=%d requests=%d, want 1001 including formatting test", len(attempts), mailbox.count())
	}
	seen := map[string]bool{}
	for i, a := range attempts {
		if a.GetString("state") != sending.AttemptSubmitted {
			t.Fatalf("attempt %s not submitted", a.Id)
		}
		if a.GetString("kind") == sending.KindCandidate {
			address := a.GetString("recipient")
			if seen[address] {
				t.Fatalf("recipient submitted twice: %s", address)
			}
			seen[address] = true
			if a.GetString("subject") == "" || a.GetString("body") == "" || a.GetString("content_hash") == "" {
				t.Fatal("attempt lost immutable content")
			}
		}
		started := a.GetDateTime("started_at").Time()
		if i > 0 && started.Sub(attempts[i-1].GetDateTime("started_at").Time()) < time.Minute/25 {
			t.Fatalf("starts %d and %d violated default spacing", i-1, i)
		}
		if i >= 25 && started.Sub(attempts[i-25].GetDateTime("started_at").Time()) < time.Minute {
			t.Fatalf("rolling allowance exceeded at %d", i)
		}
	}
	if len(seen) != 1000 {
		t.Fatalf("unique candidate requests=%d", len(seen))
	}
	mailbox.mu.Lock()
	max := mailbox.maxActive
	mailbox.mu.Unlock()
	if max > 4 {
		t.Fatalf("max in flight=%d", max)
	}
}

func TestAcceptanceSharedCapacityCrossTaskClaimsAndStop(t *testing.T) {
	f := newAcceptanceFixture(t, true)
	first := f.task(t, 12)
	overlap := f.task(t, 12)
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)
	f.approve(t, first, d)
	gate := make(chan struct{})
	mailbox.mu.Lock()
	mailbox.hold = gate
	mailbox.mu.Unlock()
	result := acceptanceAct(t, f.store, first, "start", "shared-start", 12)
	if result.Claimed != 12 {
		t.Fatal(result)
	}
	// A second upload with identical campaign recipients cannot claim those rows.
	if _, err := f.store.Act(context.Background(), sending.ActionRequest{ActionID: "overlapping-start", TaskID: overlap, Kind: "start", Confirmed: true, ExpectedCount: 12}); err == nil {
		t.Fatal("overlapping task claimed reserved recipients")
	}
	d.Wake()
	acceptanceEventually(t, func() bool { return mailbox.count() == 5 }) // one prior test plus four held sends
	if err := d.Synchronize(func() error {
		_, err := f.store.Act(context.Background(), sending.ActionRequest{ActionID: "stop-shared", TaskID: first, Kind: "stop"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A test while stopping shares capacity. It must wait for an active slot.
	test, err := f.store.QueueTest(context.Background(), sending.TestRequest{ActionID: "during-stop", TaskID: first, Recipient: "diagnostic@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Diagnostic"}})
	if err != nil {
		t.Fatal(err)
	}
	d.Wake()
	time.Sleep(20 * time.Millisecond)
	if mailbox.count() != 5 {
		t.Fatalf("fifth active request bypassed shared capacity: %d requests", mailbox.count())
	}
	close(gate)
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(sending.AttemptsCollection, test.ID)
		return e == nil && r.GetString("state") == sending.AttemptSubmitted
	})
	acceptanceEventually(t, func() bool {
		p, e := f.store.Preview(context.Background(), first)
		return e == nil && p.Status == "stopped" && p.Submitted == 4 && p.Remaining == 8
	})
	if mailbox.count() != 6 {
		t.Fatalf("stop launched additional candidates: %d requests", mailbox.count())
	}
	mailbox.mu.Lock()
	max := mailbox.maxActive
	mailbox.mu.Unlock()
	if max != 4 {
		t.Fatalf("max in flight=%d, want 4", max)
	}
	acceptanceAct(t, f.store, first, "resume", "resume-shared", 8)
	d.Wake()
	acceptanceEventually(t, func() bool {
		p, e := f.store.Preview(context.Background(), first)
		return e == nil && p.Submitted == 12 && p.Status == "completed"
	})
	if mailbox.count() != 14 {
		t.Fatalf("requests=%d, want 12 candidates + 2 tests", mailbox.count())
	}
}

func TestAcceptanceRestartConservativeIntentsAndCooldown(t *testing.T) {
	for _, crash := range []string{"before_transmission", "after_transmission", "accepted_before_outcome_commit"} {
		t.Run(crash, func(t *testing.T) {
			f := newAcceptanceFixture(t, true)
			task := f.task(t, 5)
			ctx := context.Background()
			sample, err := f.store.QueueTest(ctx, sending.TestRequest{ActionID: "restart-test", TaskID: task, Recipient: "operator@example.invalid", Actor: "acceptance", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
			if err != nil {
				t.Fatal(err)
			}
			w, err := f.store.NextWork(ctx)
			if err != nil || w == nil {
				t.Fatalf("test work=%v err=%v", w, err)
			}
			if _, err = f.store.BeginAttempt(ctx, w, f.clock.Now(), f.clock.Now()); err != nil {
				t.Fatal(err)
			}
			if err = f.store.CompleteAttempt(ctx, sample.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
				t.Fatal(err)
			}
			if err = f.store.Approve(ctx, task, sample.ID, "acceptance", "restart-approve"); err != nil {
				t.Fatal(err)
			}
			acceptanceAct(t, f.store, task, "start", "restart-start", 5)
			w, err = f.store.NextWork(ctx)
			if err != nil || w == nil {
				t.Fatalf("candidate work=%v err=%v", w, err)
			}
			lost, err := f.store.BeginAttempt(ctx, w, f.clock.Now(), f.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			// All three crash points have the same durable evidence. Even the intent
			// that crashed before transmission must be held, never inferred unsent.
			w, err = f.store.NextWork(ctx)
			if err != nil || w == nil {
				t.Fatalf("second work=%v err=%v", w, err)
			}
			accepted, err := f.store.BeginAttempt(ctx, w, f.clock.Now(), f.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err = f.store.CompleteAttempt(ctx, accepted.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
				t.Fatal(err)
			}
			cooldown := f.clock.Now().Add(2 * time.Minute)
			if err = f.store.SetCooldown(ctx, cooldown, "simulated persisted Retry-After"); err != nil {
				t.Fatal(err)
			}
			f.store = sending.NewStore(f.app, sending.Options{AllowCandidateSending: true, Now: f.clock.Now})
			if err = f.store.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			p, err := f.store.Preview(ctx, task)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != "interrupted" || p.Held != 1 || p.Submitted != 1 || p.Remaining != 3 || !p.ArtifactNeeded {
				t.Fatalf("recovered=%+v", p)
			}
			state, err := f.store.DispatcherState(ctx)
			if err != nil || !state.CooldownUntil.Equal(cooldown) {
				t.Fatalf("cooldown=%+v err=%v", state, err)
			}
			lostRecord, err := f.app.FindRecordById(sending.AttemptsCollection, lost.ID)
			if err != nil {
				t.Fatal(err)
			}
			if lostRecord.GetString("state") != sending.AttemptReview || lostRecord.GetString("body") != lost.Body || lostRecord.GetString("recipient") != lost.Recipient {
				t.Fatal("unfinished intent lost immutable content or uncertainty")
			}
			mailbox := &acceptanceMailbox{}
			d := f.dispatcher(t, mailbox)
			time.Sleep(20 * time.Millisecond)
			if mailbox.count() != 0 {
				t.Fatal("restart automatically resumed candidates")
			}
			acceptanceAct(t, f.store, task, "resume", "restart-resume", 3)
			d.Wake()
			acceptanceEventually(t, func() bool {
				p, e := f.store.Preview(ctx, task)
				return e == nil && p.Status == "completed" && p.Submitted == 4 && p.Held == 1
			})
			if mailbox.count() != 3 {
				t.Fatalf("restart requests=%d, want safe remainder 3", mailbox.count())
			}
			rows, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "kind='candidate'", "started_at", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 5 {
				t.Fatalf("candidate attempt history=%d, want 5", len(rows))
			}
			for _, r := range rows {
				if r.Id == lost.ID || r.Id == accepted.ID {
					continue
				}
				if r.GetString("recipient") == lost.Recipient || r.GetString("recipient") == accepted.Recipient {
					t.Fatal("restart resent uncertain or submitted candidate")
				}
				if r.GetDateTime("started_at").Time().Before(cooldown) {
					t.Fatal("restart bypassed persisted cooldown")
				}
			}
		})
	}
}
