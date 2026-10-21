package sending_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/reset"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// (c *acceptanceClock).Advance moves the simulated wall clock forward.
func (c *acceptanceClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestTwoOperatorWorkflow exercises both operators end to end against
// simulated transports: 1,000-row intake, duplicate exclusion, independent
// approvals and pacing, concurrent dispatch, an unattended scheduled start, a
// restart, and a mailbox replacement that must not touch the other workspace.
func TestTwoOperatorWorkflow(t *testing.T) {
	f := newAcceptanceFixture(t)
	ctx := context.Background()
	ownerA := f.ownerID

	taskA := f.task(t, 1000)
	ownerB, connectionB := f.addOperator(t, "second@example.invalid", "second@example.invalid")
	campaignB := f.createCampaign(t, ownerB, "Second workspace")
	taskB := f.taskFor(t, ownerB, campaignB, 5)
	duplicates := f.taskWithDuplicates(t, ownerA, f.campaignID, 6, 2)

	duplicateCounts := f.taskCounts(t, duplicates)
	if duplicateCounts.Duplicate != 2 {
		t.Fatalf("duplicate rows = %d, want the repeated pair excluded", duplicateCounts.Duplicate)
	}
	if duplicateCounts.Eligible != 6 {
		t.Fatalf("eligible rows = %d, want the 6 unique rows after excluding the duplicate pair", duplicateCounts.Eligible)
	}

	simulated := &acceptanceMailbox{}
	d := f.dispatcher(t, simulated)
	f.approve(t, taskA, d)
	if _, err := f.store.Act(ctx, ownerB, sending.ActionRequest{ActionID: "b-unapproved", TaskID: taskB, Kind: "start", Confirmed: true, ExpectedCount: 5}); err == nil {
		t.Fatal("operator B started without its own formatting approval")
	}
	approveFor(t, f, ownerB, taskB)

	// Per-mailbox pacing: each operator chooses its own valid rate and reads
	// only its own rolling state.
	if err := f.store.SetRate(ctx, ownerA, 30); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetRate(ctx, ownerB, 25); err != nil {
		t.Fatal(err)
	}
	stateA, err := f.store.DispatcherState(ctx, f.connectionID, ownerA)
	if err != nil {
		t.Fatal(err)
	}
	stateB, err := f.store.DispatcherState(ctx, connectionB, ownerB)
	if err != nil {
		t.Fatal(err)
	}
	if stateA.Rate != 30 || stateB.Rate != 25 {
		t.Fatalf("pacing rates = %d/%d, want independent 30/25", stateA.Rate, stateB.Rate)
	}

	startFor(t, f, ownerA, taskA, "workflow-start-a")
	startFor(t, f, ownerB, taskB, "workflow-start-b")

	// An unattended scheduled start for a second task of operator A fires once
	// when its stored time is reached.
	scheduledCampaign := f.createCampaign(t, ownerA, "Scheduled workspace")
	scheduledTask := f.taskFor(t, ownerA, scheduledCampaign, 3)
	f.approve(t, scheduledTask, d)
	at := f.clock.Now().Add(sending.ScheduleLead + 2*time.Minute)
	if _, err = f.store.Schedule(ctx, ownerA, sending.ScheduleRequest{ActionID: "workflow-schedule", TaskID: scheduledTask, Actor: "acceptance", At: at, Confirmed: true, ExpectedCount: 3}); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(sending.ScheduleLead + 3*time.Minute)
	d.Wake()

	acceptanceEventually(t, func() bool {
		doneA := f.taskSubmitted(t, taskA) == 1000
		doneB := f.taskSubmitted(t, taskB) == 5
		doneScheduled := f.taskSubmitted(t, scheduledTask) == 3
		return doneA && doneB && doneScheduled
	})

	// Each operator sees only their own progress and recipients.
	progressA, err := f.store.Preview(ctx, ownerA, taskA)
	if err != nil {
		t.Fatal(err)
	}
	progressB, err := f.store.Preview(ctx, ownerB, taskB)
	if err != nil {
		t.Fatal(err)
	}
	if progressA.Submitted != 1000 || progressB.Submitted != 5 {
		t.Fatalf("isolated progress = %d/%d", progressA.Submitted, progressB.Submitted)
	}
	if _, err = f.store.Preview(ctx, ownerB, taskA); err != sending.ErrNotFound {
		t.Fatalf("operator B read operator A's task: %v", err)
	}

	// A restart interrupts nothing already completed but must recover
	// conservatively; the scheduled task never fires twice.
	shutdownCtx, cancelShutdown := context.WithTimeout(ctx, 5*time.Second)
	if err := d.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	cancelShutdown()
	restarted := f.dispatcher(t, simulated)
	restarted.Wake()
	acceptanceEventually(t, func() bool { return f.taskSubmitted(t, scheduledTask) == 3 })
	if status := f.taskStatus(t, scheduledTask); status != "completed" {
		t.Fatalf("scheduled task status after restart = %q", status)
	}

	// Replacing operator B's mailbox deletes only B's sending data.
	mailboxes := mailbox.NewStore(f.app, func() ([]byte, error) { return testKey, nil })
	operation, err := mailboxes.StageReplacement(ctx, mailbox.Candidate{
		OwnerID:    ownerB,
		Provider:   mailbox.ProviderTitan,
		Sender:     "replacement@example.invalid",
		Credential: mailbox.Credential{Provider: mailbox.ProviderTitan, Password: "replacement-password"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = reset.NewService(f.app, mailboxes).Confirm(ctx, ownerB, operation.ID); err != nil {
		t.Fatal(err)
	}
	if status := f.taskStatus(t, taskB); status != "" {
		t.Fatalf("operator B task survived replacement with status %q", status)
	}
	if status := f.taskStatus(t, taskA); status != "completed" {
		t.Fatalf("operator A task after B's replacement = %q, want unchanged completed", status)
	}
	currentB, err := mailboxes.Current(ctx, ownerB)
	if err != nil || currentB.Sender != "replacement@example.invalid" {
		t.Fatalf("replacement connection = %+v, %v", currentB, err)
	}
}

// taskWithDuplicates stages a workbook whose last two rows repeat one address.
func (f *acceptanceFixture) taskWithDuplicates(t *testing.T, ownerID, campaignID string, unique, repeated int) string {
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
	row := 2
	for i := range unique {
		values := []string{fmt.Sprintf("unique%04d@example.invalid", i), fmt.Sprintf("Unique %04d", i)}
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", row), &values); err != nil {
			t.Fatal(err)
		}
		row++
	}
	for i := range repeated {
		values := []string{"duplicate@example.invalid", fmt.Sprintf("Duplicate %d", i)}
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", row), &values); err != nil {
			t.Fatal(err)
		}
		row++
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), ownerID, campaignID, "duplicates.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), ownerID, task.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func (f *acceptanceFixture) taskCounts(t *testing.T, taskID string) intake.Counts {
	t.Helper()
	task, err := f.intake.LoadTask(context.Background(), f.ownerID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.Counts
}

func (f *acceptanceFixture) taskSubmitted(t *testing.T, taskID string) int {
	t.Helper()
	records, err := f.app.FindRecordsByFilter(sending.AttemptsCollection, "task = {:t} && kind = 'candidate' && state = 'submitted'", "", 0, 0, map[string]any{"t": taskID})
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func (f *acceptanceFixture) taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	record, err := f.app.FindRecordById("send_tasks", taskID)
	if err != nil {
		return ""
	}
	return record.GetString("status")
}
