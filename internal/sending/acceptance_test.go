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

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

// testKey is a fixed deployment key that seals fixture mailbox credentials.
var testKey = bytes.Repeat([]byte{7}, 32)

// resolverFunc adapts a fake sender to the mailbox Resolver so the fixture
// dispatcher resolves every connection to the same simulated mailbox.
type resolverFunc func(context.Context, string) (sending.Sender, error)

func (r resolverFunc) Sender(ctx context.Context, connectionID string) (sending.Sender, error) {
	return r(ctx, connectionID)
}

func fixedResolver(s sending.Sender) sending.Resolver {
	return resolverFunc(func(context.Context, string) (sending.Sender, error) { return s, nil })
}

// provisionOperator creates an operator account and returns its record id.
func provisionOperator(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// installConnection claims one current, connected Titan mailbox for owner.
func installConnection(t *testing.T, app core.App, ownerID, sender string) string {
	t.Helper()
	store := mailbox.NewStore(app, func() ([]byte, error) { return testKey, nil })
	connection, err := store.Install(context.Background(), mailbox.Candidate{
		OwnerID:    ownerID,
		Provider:   mailbox.ProviderTitan,
		Sender:     sender,
		Identity:   sender + "@titan",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "mailbox-password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection.ID
}

type acceptanceFixture struct {
	app           core.App
	store         *sending.Store
	intake        *intake.Service
	campaignID    string
	clock         *acceptanceClock
	ownerID       string
	connectionID  string
	senderAddress string
	pandocPath    string
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

func newAcceptanceFixture(t *testing.T) *acceptanceFixture {
	t.Helper()
	app := testutil.NewApp(t)
	stub := filepath.Join(t.TempDir(), "pandoc")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = \"--version\" ]; then echo 'pandoc acceptance'; exit 0; fi\ndone\nprintf '%s' '<p>Hello {{candidate_name}}</p>'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ownerID := provisionOperator(t, app, "operator@example.invalid")
	connectionID := installConnection(t, app, ownerID, "sender@example.invalid")
	f := &acceptanceFixture{
		app:           app,
		clock:         &acceptanceClock{now: time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)},
		ownerID:       ownerID,
		connectionID:  connectionID,
		senderAddress: "sender@example.invalid",
		pandocPath:    stub,
	}
	f.store = sending.NewStore(app, sending.Options{Now: f.clock.Now})
	f.intake = intake.NewService(app, campaign.NewServiceWithConverter(app, convert.Converter{Path: stub}), intake.Limits{})
	f.campaignID = f.createCampaign(t, ownerID, "Simulated acceptance")
	return f
}

// createCampaign creates an owned campaign with one configured revision.
func (f *acceptanceFixture) createCampaign(t *testing.T, ownerID, name string) string {
	t.Helper()
	return f.createCampaignWithSubject(t, ownerID, name, "Hello {{candidate_name}}")
}

// createCampaignWithSubject builds the same revision shape with an explicit
// subject, so a test can exercise the recipient placeholder.
func (f *acceptanceFixture) createCampaignWithSubject(t *testing.T, ownerID, name, subject string) string {
	t.Helper()
	campaigns := campaign.NewServiceWithConverter(f.app, convert.Converter{Path: f.pandocPath})
	ctx := context.Background()
	created, err := campaigns.CreateCampaign(ctx, ownerID, campaign.CreateCampaignInput{Name: name, SourceHeaders: []string{"Email", "Name"}})
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
	_, _, err = campaigns.CreateRevision(ctx, ownerID, created.ID, campaign.RevisionInput{Subject: subject, Document: doc.Bytes(), DocumentName: "body.docx", Mappings: []campaign.Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail}, {Variable: "candidate_name", Header: "Name", Required: true, Format: campaign.FormatName}}})
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}

// addOperator provisions a second operator with its own connected mailbox.
func (f *acceptanceFixture) addOperator(t *testing.T, email, sender string) (ownerID, connectionID string) {
	t.Helper()
	ownerID = provisionOperator(t, f.app, email)
	connectionID = installConnection(t, f.app, ownerID, sender)
	return ownerID, connectionID
}

func (f *acceptanceFixture) task(t *testing.T, count int) string {
	t.Helper()
	return f.taskFor(t, f.ownerID, f.campaignID, count)
}

func (f *acceptanceFixture) taskFor(t *testing.T, ownerID, campaignID string, count int) string {
	t.Helper()
	book := excelize.NewFile()
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name"}); err != nil {
		t.Fatal(err)
	}
	for i := range count {
		row := []string{fmt.Sprintf("candidate%04d@example.invalid", i), fmt.Sprintf("Candidate %04d", i)}
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", i+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), ownerID, campaignID, "acceptance.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), ownerID, task.ID, "Candidates", 1); err != nil {
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
	return f.dispatcherFor(t, fixedResolver(mailbox))
}

// dispatcherFor starts a dispatcher with a resolver that can map each mailbox
// connection to its own simulated sender.
func (f *acceptanceFixture) dispatcherFor(t *testing.T, resolver sending.Resolver) *sending.Dispatcher {
	t.Helper()
	return f.dispatcherWith(t, resolver, 3*time.Second)
}

// dispatcherWith starts a dispatcher whose request deadline is long enough for
// a test to hold one mailbox's slots open deliberately.
func (f *acceptanceFixture) dispatcherWith(t *testing.T, resolver sending.Resolver, requestTimeout time.Duration) *sending.Dispatcher {
	t.Helper()
	d := sending.NewDispatcher(f.store, resolver, sending.WorkerOptions{DataDir: f.app.DataDir(), Now: f.clock.Now, After: f.clock.After, RequestTimeout: requestTimeout})
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
	attempt, err := f.store.QueueTest(context.Background(), f.ownerID, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Actor: "acceptance", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(sending.AttemptsCollection, attempt.ID)
		return e == nil && r.GetString("state") == sending.AttemptSubmitted
	})
	if err = f.store.Approve(context.Background(), f.ownerID, task, attempt.ID, "acceptance", "approve-"+task); err != nil {
		t.Fatal(err)
	}
}
func (f *acceptanceFixture) act(t *testing.T, task, kind, id string, count int) *sending.ActionResult {
	t.Helper()
	result, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: id, TaskID: task, Kind: kind, Actor: "acceptance", Confirmed: true, ExpectedCount: count})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAcceptanceCandidatePipelineAndReplay(t *testing.T) {
	f := newAcceptanceFixture(t)
	task := f.task(t, 500)
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)
	// Approval is an explicit action following a separately submitted test.
	if _, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "unapproved", TaskID: task, Kind: "start", Confirmed: true, ExpectedCount: 500}); err == nil {
		t.Fatal("unapproved candidate start succeeded")
	}
	f.approve(t, task, d)
	p, err := f.store.Preview(context.Background(), f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if p.Submitted != 0 || p.Eligible != 500 {
		t.Fatalf("test changed candidate eligibility: %+v", p)
	}
	first := f.act(t, task, "start", "start-500", 500)
	if first.Claimed != 500 {
		t.Fatalf("claimed %d, want 500", first.Claimed)
	}
	repeated := f.act(t, task, "start", "start-500", 500)
	if !repeated.Repeated || repeated.Claimed != 500 {
		t.Fatalf("replay = %+v", repeated)
	}
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(intake.TasksCollection, task)
		return e == nil && r.GetString("status") == "completed"
	})
	p, err = f.store.Preview(context.Background(), f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if p.Submitted != 500 || p.Remaining != 0 || p.Held != 0 || p.InFlight != 0 || !p.ArtifactNeeded {
		t.Fatalf("final progress = %+v", p)
	}
	attempts, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "", "started_at", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 501 || mailbox.count() != 501 {
		t.Fatalf("attempts=%d requests=%d, want 501 including formatting test", len(attempts), mailbox.count())
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
	if len(seen) != 500 {
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
	f := newAcceptanceFixture(t)
	first := f.task(t, 12)
	overlap := f.task(t, 12)
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)
	f.approve(t, first, d)
	gate := make(chan struct{})
	mailbox.mu.Lock()
	mailbox.hold = gate
	mailbox.mu.Unlock()
	result := f.act(t, first, "start", "shared-start", 12)
	if result.Claimed != 12 {
		t.Fatal(result)
	}
	// A second upload with identical campaign recipients cannot claim those rows.
	if _, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "overlapping-start", TaskID: overlap, Kind: "start", Confirmed: true, ExpectedCount: 12}); err == nil {
		t.Fatal("overlapping task claimed reserved recipients")
	}
	d.Wake()
	acceptanceEventually(t, func() bool { return mailbox.count() == 5 }) // one prior test plus four held sends
	if err := d.Synchronize(func() error {
		_, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "stop-shared", TaskID: first, Kind: "stop"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// A test while stopping shares capacity. It must wait for an active slot.
	test, err := f.store.QueueTest(context.Background(), f.ownerID, sending.TestRequest{ActionID: "during-stop", TaskID: first, Recipient: "diagnostic@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Diagnostic"}})
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
		p, e := f.store.Preview(context.Background(), f.ownerID, first)
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
	f.act(t, first, "resume", "resume-shared", 8)
	d.Wake()
	acceptanceEventually(t, func() bool {
		p, e := f.store.Preview(context.Background(), f.ownerID, first)
		return e == nil && p.Submitted == 12 && p.Status == "completed"
	})
	if mailbox.count() != 14 {
		t.Fatalf("requests=%d, want 12 candidates + 2 tests", mailbox.count())
	}
}

// connectionResolver returns the simulated mailbox of one connection, so each
// operator's sends are observed separately.
func connectionResolver(t *testing.T, mailboxes map[string]*acceptanceMailbox) sending.Resolver {
	t.Helper()
	return resolverFunc(func(_ context.Context, connectionID string) (sending.Sender, error) {
		m, ok := mailboxes[connectionID]
		if !ok {
			return nil, fmt.Errorf("unexpected connection %q", connectionID)
		}
		return m, nil
	})
}

// TestAcceptancePerMailboxCapacityIsIndependent proves one operator holding
// every slot of its own mailbox never blocks another operator's sending.
func TestAcceptancePerMailboxCapacityIsIndependent(t *testing.T) {
	f := newAcceptanceFixture(t)
	ownerB, connectionB := f.addOperator(t, "second@example.invalid", "second@example.invalid")
	campaignB := f.createCampaign(t, ownerB, "Second workspace")
	taskA := f.task(t, 12)
	taskB := f.taskFor(t, ownerB, campaignB, 12)

	mailboxA, mailboxB := &acceptanceMailbox{}, &acceptanceMailbox{}
	d := f.dispatcherWith(t, connectionResolver(t, map[string]*acceptanceMailbox{f.connectionID: mailboxA, connectionB: mailboxB}), 30*time.Second)
	f.approve(t, taskA, d)
	approveFor(t, f, ownerB, taskB)
	releaseA := make(chan struct{})
	defer close(releaseA)
	mailboxA.mu.Lock()
	mailboxA.hold = releaseA
	mailboxA.mu.Unlock()

	actFor(t, f, f.ownerID, taskA, "start", "capacity-a", 12)
	d.Wake()
	acceptanceEventually(t, func() bool { return mailboxA.count() == 5 }) // one approved test plus four held sends

	actFor(t, f, ownerB, taskB, "start", "capacity-b", 12)
	d.Wake()
	// Operator A still holds all four slot-consuming requests, so B's send can
	// only start if capacity belongs to each mailbox connection.
	acceptanceEventually(t, func() bool { return mailboxB.count() >= 1 })
	mailboxA.mu.Lock()
	activeA, maxA := mailboxA.active, mailboxA.maxActive
	mailboxA.mu.Unlock()
	if activeA != 4 || maxA != 4 {
		t.Fatalf("first mailbox active=%d max=%d when the second mailbox sent, want 4/4", activeA, maxA)
	}
	// B fills its own four slots while A keeps holding its four.
	acceptanceEventually(t, func() bool { return mailboxB.count() >= 4 })
}

// TestAcceptanceMailboxPauseDoesNotStallAnotherOperator proves an uncertain
// outcome for one mailbox leaves another operator's running task submitting,
// with no Wake or Synchronize after the pause.
func TestAcceptanceMailboxPauseDoesNotStallAnotherOperator(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	ownerB, connectionB := f.addOperator(t, "second@example.invalid", "second@example.invalid")
	campaignB := f.createCampaign(t, ownerB, "Second workspace")
	taskA := f.task(t, 4)
	taskB := f.taskFor(t, ownerB, campaignB, 12)

	releaseB := make(chan struct{})
	mailboxA := &acceptanceMailbox{uncertain: "candidate0000@example.invalid"}
	mailboxB := &acceptanceMailbox{}
	d := f.dispatcherWith(t, connectionResolver(t, map[string]*acceptanceMailbox{f.connectionID: mailboxA, connectionB: mailboxB}), 30*time.Second)
	f.approve(t, taskA, d)
	approveFor(t, f, ownerB, taskB)
	mailboxB.mu.Lock()
	mailboxB.hold = releaseB
	mailboxB.mu.Unlock()

	actFor(t, f, f.ownerID, taskA, "start", "pause-a", 4)
	actFor(t, f, ownerB, taskB, "start", "pause-b", 12)
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, err := f.app.FindRecordById("send_tasks", taskA)
		return err == nil && r.GetString("status") == "paused"
	})
	// Operator B's first four requests waited only on this test's gate. Releasing
	// it must let the coordinator launch the rest of B's task without any
	// operator action.
	close(releaseB)
	acceptanceEventually(t, func() bool {
		p, err := f.store.Preview(ctx, ownerB, taskB)
		return err == nil && p.Submitted == 12
	})
}

func TestAcceptanceRestartConservativeIntentsAndCooldown(t *testing.T) {
	for _, crash := range []string{"before_transmission", "after_transmission", "accepted_before_outcome_commit"} {
		t.Run(crash, func(t *testing.T) {
			f := newAcceptanceFixture(t)
			task := f.task(t, 5)
			ctx := context.Background()
			sample, err := f.store.QueueTest(ctx, f.ownerID, sending.TestRequest{ActionID: "restart-test", TaskID: task, Recipient: "operator@example.invalid", Actor: "acceptance", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
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
			if err = f.store.Approve(ctx, f.ownerID, task, sample.ID, "acceptance", "restart-approve"); err != nil {
				t.Fatal(err)
			}
			f.act(t, task, "start", "restart-start", 5)
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
			if err = f.store.SetCooldown(ctx, f.connectionID, f.ownerID, cooldown, "simulated persisted Retry-After"); err != nil {
				t.Fatal(err)
			}
			f.store = sending.NewStore(f.app, sending.Options{Now: f.clock.Now})
			if err = f.store.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			p, err := f.store.Preview(ctx, f.ownerID, task)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != "interrupted" || p.Held != 1 || p.Submitted != 1 || p.Remaining != 3 || !p.ArtifactNeeded {
				t.Fatalf("recovered=%+v", p)
			}
			state, err := f.store.DispatcherState(ctx, f.connectionID, f.ownerID)
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
			f.act(t, task, "resume", "restart-resume", 3)
			d.Wake()
			acceptanceEventually(t, func() bool {
				p, e := f.store.Preview(ctx, f.ownerID, task)
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

// taskWithRows stages and validates one workbook with explicit data rows.
func (f *acceptanceFixture) taskWithRows(t *testing.T, campaignID string, rows [][]string) string {
	t.Helper()
	book := excelize.NewFile()
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name"}); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", i+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), f.ownerID, campaignID, "expanded.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), f.ownerID, task.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

// TestAcceptanceExpandedSiblingsSendIndependently sends two recipients that
// shared one workbook cell to separate addresses with their own content, and
// confirms submission history still applies per email.
func TestAcceptanceExpandedSiblingsSendIndependently(t *testing.T) {
	f := newAcceptanceFixture(t)
	campaignID := f.createCampaignWithSubject(t, f.ownerID, "Expanded siblings", "To {{candidate_email}} for {{candidate_name}}")
	task := f.taskWithRows(t, campaignID, [][]string{
		{"alice@example.invalid, bob@example.invalid", "Shared Person"},
		{"carol@example.invalid", "Carol"},
	})
	mailbox := &acceptanceMailbox{}
	d := f.dispatcher(t, mailbox)

	// Saving, listing and previewing the snapshot never dispatches anything.
	rows, err := f.intake.Rows(context.Background(), f.ownerID, task)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("membership rows = %d, want the three expanded recipients", len(rows))
	}
	var alice, bob intake.RowSnapshot
	for _, row := range rows {
		switch row.Normalized["candidate_email"] {
		case "alice@example.invalid":
			alice = row
		case "bob@example.invalid":
			bob = row
		}
	}
	if alice.ID == "" || bob.ID == "" || alice.SourceRow != bob.SourceRow || alice.RecipientIndex != 1 || bob.RecipientIndex != 2 {
		t.Fatalf("sibling memberships = %+v / %+v", alice, bob)
	}
	for _, row := range []intake.RowSnapshot{alice, bob} {
		if _, err := f.intake.Preview(context.Background(), f.ownerID, task, row.ID); err != nil {
			t.Fatal(err)
		}
	}
	mailbox.mu.Lock()
	sentBeforeStart := len(mailbox.messages)
	mailbox.mu.Unlock()
	if sentBeforeStart != 0 {
		t.Fatalf("import or preview dispatched %d messages", sentBeforeStart)
	}
	attempts, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "task={:t} && kind='candidate'", "", 0, 0, dbx.Params{"t": task})
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("import or preview created %d attempts", len(attempts))
	}

	f.approve(t, task, d)
	if act := f.act(t, task, "start", "expanded-start", 3); act.Claimed != 3 {
		t.Fatalf("claimed %d, want 3", act.Claimed)
	}
	d.Wake()
	acceptanceEventually(t, func() bool {
		r, e := f.app.FindRecordById(intake.TasksCollection, task)
		return e == nil && r.GetString("status") == "completed"
	})

	attempts, err = f.app.FindRecordsByFilter(sending.AttemptsCollection, "task={:t} && kind='candidate'", "recipient", 0, 0, dbx.Params{"t": task})
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 {
		t.Fatalf("candidate attempts = %d, want one per expanded recipient", len(attempts))
	}
	want := map[string]string{
		"alice@example.invalid": "To alice@example.invalid for Shared Person",
		"bob@example.invalid":   "To bob@example.invalid for Shared Person",
		"carol@example.invalid": "To carol@example.invalid for Carol",
	}
	ids := map[string]bool{}
	for _, attempt := range attempts {
		recipient := attempt.GetString("recipient")
		if ids[attempt.Id] {
			t.Fatalf("attempt %s recorded twice", attempt.Id)
		}
		ids[attempt.Id] = true
		if attempt.GetString("subject") != want[recipient] {
			t.Fatalf("%s subject = %q, want %q", recipient, attempt.GetString("subject"), want[recipient])
		}
		if attempt.GetString("state") != sending.AttemptSubmitted {
			t.Fatalf("%s attempt state = %q", recipient, attempt.GetString("state"))
		}
	}

	// Campaign history still applies per email: a later upload of one sibling
	// is held as already submitted and cannot be started.
	second := f.taskWithRows(t, campaignID, [][]string{{"alice@example.invalid", "Alice"}})
	secondRows, err := f.intake.Rows(context.Background(), f.ownerID, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondRows) != 1 || secondRows[0].State != intake.RowAlreadySubmitted {
		t.Fatalf("re-uploaded sibling = %+v, want already submitted", secondRows)
	}
	if _, err := f.store.Act(context.Background(), f.ownerID, sending.ActionRequest{ActionID: "expanded-again", TaskID: second, Kind: "start", Confirmed: true, ExpectedCount: 1}); err == nil {
		t.Fatal("a submitted sibling was scheduled again")
	}
}
