// Eligibility tests exercise the store directly against a real PocketBase
// app: campaign-scoped identity, the reservation compare-and-swap, and the
// history that survives a release. Nothing here sends email.
package intake_test

import (
	"context"
	"sync"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
)

// newCampaignRecord stores a minimal campaign: name and recipient header are
// its only required fields.
func newCampaignRecord(t *testing.T, app core.App, name string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("name", name)
	record.Set("source_headers", []string{"Email"})
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// newTaskRecord stores a staging task. Its source file field is required, so
// every task carries one small upload.
func newTaskRecord(t *testing.T, app core.App, campaignID string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	source, err := filesystem.NewFileFromBytes([]byte("book"), "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("campaign", campaignID)
	record.Set("status", intake.TaskStaging)
	record.Set("source", source)
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

func TestEnsureCandidateCreatesOncePerCampaignKey(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	campaignA := newCampaignRecord(t, app, "Eligibility A")
	campaignB := newCampaignRecord(t, app, "Eligibility B")

	if _, err := store.EnsureCandidate(app, "", "person@example.com"); err == nil {
		t.Fatal("EnsureCandidate without a campaign should fail")
	}
	if _, err := store.EnsureCandidate(app, campaignA, "  "); err == nil {
		t.Fatal("EnsureCandidate without an address should fail")
	}

	first, err := store.EnsureCandidate(app, campaignA, "person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.EnsureCandidate(app, campaignA, "person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("second EnsureCandidate created a second identity: %q != %q", again.ID, first.ID)
	}
	if again.CampaignID != campaignA || again.EmailKey != "person@example.com" {
		t.Fatalf("identity = %+v, want campaign %q and key person@example.com", again, campaignA)
	}

	other, err := store.EnsureCandidate(app, campaignB, "person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatalf("the same address in another campaign reused identity %q", other.ID)
	}

	// The unique index is the guard: two lookups for one key leave exactly
	// one stored identity per campaign.
	count, err := app.CountRecords(intake.CandidatesCollection, dbx.HashExp{"campaign": campaignA})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("campaign A identity count = %d, want 1", count)
	}

	if _, err := store.LoadCandidate(app, "missing-candidate"); err == nil {
		t.Fatal("LoadCandidate for an unknown id should fail")
	}
}

func TestEnsureCandidateKeepsSubmissionHistory(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility History")

	candidate, err := store.EnsureCandidate(app, campaignID, "kept@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSubmitted(ctx, candidate.ID); err != nil {
		t.Fatal(err)
	}

	again, err := store.EnsureCandidate(app, campaignID, "kept@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != candidate.ID {
		t.Fatalf("identity changed after a submission: %q != %q", again.ID, candidate.ID)
	}
	if again.SubmissionState != intake.SubmissionSubmitted || again.State() != intake.CandidateSubmitted {
		t.Fatalf("submission state = %q (%s), want submitted", again.SubmissionState, again.State())
	}
}

func TestClaimReservesAndReportsStates(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility Claim")
	taskA := newTaskRecord(t, app, campaignID)
	taskB := newTaskRecord(t, app, campaignID)

	free, err := store.EnsureCandidate(app, campaignID, "free@example.com")
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := store.EnsureCandidate(app, campaignID, "done@example.com")
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := store.EnsureCandidate(app, campaignID, "review@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSubmitted(ctx, submitted.ID); err != nil {
		t.Fatal(err)
	}
	// Only dispatch work ever writes uncertainty; the reservation service
	// must report it as-is, so the test writes it directly.
	uncertainRecord, err := app.FindRecordById(intake.CandidatesCollection, uncertain.ID)
	if err != nil {
		t.Fatal(err)
	}
	uncertainRecord.Set("submission_state", intake.SubmissionUncertain)
	if err := app.Save(uncertainRecord); err != nil {
		t.Fatal(err)
	}

	first, err := store.Claim(ctx, taskA, []string{free.ID})
	if err != nil {
		t.Fatal(err)
	}
	if first[free.ID] != (intake.ClaimOutcome{Claimed: true, State: intake.CandidateFree}) {
		t.Fatalf("first claim = %+v, want a free reservation", first[free.ID])
	}
	claimedRecord, err := app.FindRecordById(intake.CandidatesCollection, free.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimedRecord.GetDateTime("claimed_at").Time().IsZero() {
		t.Fatal("a successful claim must store its claim timestamp")
	}

	// A repeat claim by the owner is reported as not-claimed, not as a
	// second winner.
	repeat, err := store.Claim(ctx, taskA, []string{free.ID})
	if err != nil {
		t.Fatal(err)
	}
	if repeat[free.ID] != (intake.ClaimOutcome{Claimed: false, State: intake.CandidateClaimed}) {
		t.Fatalf("owner repeat claim = %+v, want not-claimed and state claimed", repeat[free.ID])
	}

	rival, err := store.Claim(ctx, taskB, []string{free.ID})
	if err != nil {
		t.Fatal(err)
	}
	if rival[free.ID] != (intake.ClaimOutcome{Claimed: false, State: intake.CandidateClaimed}) {
		t.Fatalf("rival claim = %+v, want not-claimed and state claimed", rival[free.ID])
	}

	history, err := store.Claim(ctx, taskB, []string{submitted.ID, uncertain.ID})
	if err != nil {
		t.Fatal(err)
	}
	if history[submitted.ID] != (intake.ClaimOutcome{Claimed: false, State: intake.CandidateSubmitted}) {
		t.Fatalf("submitted claim = %+v, want not-claimed and state submitted", history[submitted.ID])
	}
	if history[uncertain.ID] != (intake.ClaimOutcome{Claimed: false, State: intake.CandidateUncertain}) {
		t.Fatalf("uncertain claim = %+v, want not-claimed and state uncertain", history[uncertain.ID])
	}

	empty, err := store.Claim(ctx, taskA, nil)
	if err != nil {
		t.Fatalf("Claim without candidates: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Claim without candidates returned %d outcomes, want 0", len(empty))
	}
	if _, err := store.Claim(ctx, "", []string{free.ID}); err == nil {
		t.Fatal("Claim without a task should fail")
	}
}

func TestClaimCollapsesDuplicateIDs(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility Duplicate")
	taskID := newTaskRecord(t, app, campaignID)

	candidate, err := store.EnsureCandidate(app, campaignID, "once@example.com")
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := store.Claim(ctx, taskID, []string{candidate.ID, candidate.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("duplicate input returned %d outcomes, want 1", len(outcomes))
	}
	if outcomes[candidate.ID] != (intake.ClaimOutcome{Claimed: true, State: intake.CandidateFree}) {
		t.Fatalf("outcome = %+v, want a free reservation", outcomes[candidate.ID])
	}
}

func TestStoreClaimAdmitsExactlyOneOwner(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility Race")
	candidate, err := store.EnsureCandidate(app, campaignID, "raced@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Eight simultaneous actions race for one candidate: exactly one may
	// win, whichever task it is.
	const racers = 8
	tasks := make([]string, racers)
	for i := range racers {
		tasks[i] = newTaskRecord(t, app, campaignID)
	}
	var wait sync.WaitGroup
	results := make([]bool, racers)
	errs := make([]error, racers)
	for i := range racers {
		wait.Go(func() {
			outcomes, err := store.Claim(ctx, tasks[i], []string{candidate.ID})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = outcomes[candidate.ID].Claimed
		})
	}
	wait.Wait()

	winners := 0
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("concurrent claim %d: %v", i, errs[i])
		}
		if results[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claims = %d winners, want exactly 1", winners)
	}
}

func TestReleaseClearsOnlyOwnClaims(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility Release")
	taskA := newTaskRecord(t, app, campaignID)
	taskB := newTaskRecord(t, app, campaignID)

	plain, err := store.EnsureCandidate(app, campaignID, "plain@example.com")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := store.EnsureCandidate(app, campaignID, "kept@example.com")
	if err != nil {
		t.Fatal(err)
	}
	uncertain, err := store.EnsureCandidate(app, campaignID, "uncertain@example.com")
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := store.EnsureCandidate(app, campaignID, "submitted@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Claim(ctx, taskA, []string{plain.ID, uncertain.ID, submitted.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(ctx, taskB, []string{kept.ID}); err != nil {
		t.Fatal(err)
	}
	// Submission history written while a claim is held: Release must clear
	// the reservation without touching it.
	for id, state := range map[string]string{
		uncertain.ID: intake.SubmissionUncertain,
		submitted.ID: intake.SubmissionSubmitted,
	} {
		record, err := app.FindRecordById(intake.CandidatesCollection, id)
		if err != nil {
			t.Fatal(err)
		}
		record.Set("submission_state", state)
		if err := app.Save(record); err != nil {
			t.Fatal(err)
		}
	}

	released, err := store.Release(ctx, taskA)
	if err != nil {
		t.Fatal(err)
	}
	if released != 3 {
		t.Fatalf("released = %d, want 3 reservations of the one task", released)
	}

	plainAfter, err := store.LoadCandidate(app, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if plainAfter.ClaimTaskID != "" || plainAfter.State() != intake.CandidateFree {
		t.Fatalf("released candidate = %+v, want free with no owner", plainAfter)
	}
	releasedRecord, err := app.FindRecordById(intake.CandidatesCollection, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !releasedRecord.GetDateTime("claimed_at").Time().IsZero() {
		t.Fatal("release must clear the claim timestamp")
	}

	keptAfter, err := store.LoadCandidate(app, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if keptAfter.ClaimTaskID != taskB || keptAfter.State() != intake.CandidateClaimed {
		t.Fatalf("another task's claim = %+v, want it still reserved by %s", keptAfter, taskB)
	}

	for id, want := range map[string]string{
		uncertain.ID: intake.CandidateUncertain,
		submitted.ID: intake.CandidateSubmitted,
	} {
		after, err := store.LoadCandidate(app, id)
		if err != nil {
			t.Fatal(err)
		}
		if after.ClaimTaskID != "" || after.State() != want {
			t.Fatalf("released candidate %s = %+v, want no owner and state %s", id, after, want)
		}
	}

	if n, err := store.Release(ctx, "task-without-reservations"); err != nil || n != 0 {
		t.Fatalf("Release without reservations = %d (err=%v), want 0 and no error", n, err)
	}
}

func TestMarkSubmittedRecordsHistory(t *testing.T) {
	app := newTestApp(t)
	store := intake.NewStore(app)
	ctx := context.Background()
	campaignID := newCampaignRecord(t, app, "Eligibility Submit")
	taskID := newTaskRecord(t, app, campaignID)

	candidate, err := store.EnsureCandidate(app, campaignID, "sendable@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Claim(ctx, taskID, []string{candidate.ID}); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkSubmitted(ctx, candidate.ID); err != nil {
		t.Fatal(err)
	}
	after, err := store.LoadCandidate(app, candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SubmissionState != intake.SubmissionSubmitted {
		t.Fatalf("submission state = %q, want submitted", after.SubmissionState)
	}
	if after.ClaimTaskID != "" {
		t.Fatalf("claim owner = %q, want the reservation cleared", after.ClaimTaskID)
	}
	if after.State() != intake.CandidateSubmitted {
		t.Fatalf("state = %q, want submitted", after.State())
	}

	// A candidate claim cannot be re-reserved once its history exists.
	outcomes, err := store.Claim(ctx, taskID, []string{candidate.ID})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[candidate.ID] != (intake.ClaimOutcome{Claimed: false, State: intake.CandidateSubmitted}) {
		t.Fatalf("claim after submit = %+v, want not-claimed and state submitted", outcomes[candidate.ID])
	}

	err = store.MarkSubmitted(ctx, "missing-candidate")
	if err == nil {
		t.Fatal("MarkSubmitted for an unknown id should fail")
	}
	if err.Error() != "candidate not found" {
		t.Fatalf("error = %q, want the fixed text %q", err.Error(), "candidate not found")
	}
}
