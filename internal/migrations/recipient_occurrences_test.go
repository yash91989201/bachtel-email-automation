package migrations

import (
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// The occurrence migration replaces one-row-per-source-row membership with
// one-membership-per-candidate and adds per-entry provenance. This test builds
// a fresh temporary database and proves both invariants plus the backfill of an
// existing membership, without losing its id, candidate claim or attempt
// eligibility fields.
func TestRecipientOccurrenceSchemaEnforcesMembershipAndBackfills(t *testing.T) {
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

	members, err := app.FindCollectionByNameOrId(intake.TaskCandidatesCollection)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"recipient_index", "original_recipient"} {
		if members.Fields.GetByName(name) == nil {
			t.Fatalf("%s field is missing", name)
		}
	}
	indexes := strings.Join(members.Indexes, "\n")
	if !strings.Contains(indexes, "idx_task_candidates_membership") {
		t.Fatalf("task/candidate membership index is missing:\n%s", indexes)
	}
	if strings.Contains(indexes, "idx_task_candidates_row") {
		t.Fatalf("the old unique source-row index survived:\n%s", indexes)
	}

	owner := newOperator(t, app, "occurrence-owner@example.test")
	campaigns, err := app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	owned := core.NewRecord(campaigns)
	owned.Set("owner", owner)
	owned.Set("name", "Occurrence fixture")
	if err := app.Save(owned); err != nil {
		t.Fatal(err)
	}

	tasks, err := app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	task := core.NewRecord(tasks)
	task.Set("campaign", owned.Id)
	task.Set("status", intake.TaskReady)
	source, err := filesystem.NewFileFromBytes([]byte("workbook"), "task.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	task.Set("source", source)
	if err := app.Save(task); err != nil {
		t.Fatal(err)
	}

	candidates, err := app.FindCollectionByNameOrId(intake.CandidatesCollection)
	if err != nil {
		t.Fatal(err)
	}
	newCandidate := func(emailKey string) *core.Record {
		record := core.NewRecord(candidates)
		record.Set("campaign", owned.Id)
		record.Set("email_key", emailKey)
		record.Set("submission_state", intake.SubmissionNone)
		return record
	}
	first := newCandidate("first@example.test")
	if err := app.Save(first); err != nil {
		t.Fatal(err)
	}
	second := newCandidate("second@example.test")
	if err := app.Save(second); err != nil {
		t.Fatal(err)
	}
	// The first candidate already owns this task's reservation.
	first.Set("claim_task", task.Id)
	if err := app.Save(first); err != nil {
		t.Fatal(err)
	}

	newMember := func(candidateID string, position int) *core.Record {
		record := core.NewRecord(members)
		record.Set("task", task.Id)
		record.Set("candidate", candidateID)
		record.Set("source_row", 2)
		record.Set("recipient_index", position)
		record.Set("original_recipient", "first@example.test, second@example.test")
		record.Set("state", intake.RowEligible)
		record.Set("raw", map[string]string{"candidate_email": "first@example.test, second@example.test"})
		record.Set("normalized", map[string]string{"candidate_email": "first@example.test, second@example.test"})
		return record
	}
	firstMember := newMember(first.Id, 1)
	if err := app.Save(firstMember); err != nil {
		t.Fatalf("two recipients sharing a source row were rejected: %v", err)
	}
	if err := app.Save(newMember(second.Id, 2)); err != nil {
		t.Fatalf("second sibling membership was rejected: %v", err)
	}
	if err := app.Save(newMember(first.Id, 3)); err == nil {
		t.Fatal("a repeated candidate membership in one task was accepted")
	}

	// A legacy membership has no recorded position or original cell. The
	// backfill must restore both while keeping its id, candidate claim and
	// attempts reference.
	firstMemberID := firstMember.Id
	if _, err := app.DB().NewQuery("UPDATE task_candidates SET recipient_index = 0, original_recipient = '' WHERE id = {:id}").
		Bind(dbx.Params{"id": firstMemberID}).
		Execute(); err != nil {
		t.Fatal(err)
	}
	if err := backfillOccurrences(app); err != nil {
		t.Fatal(err)
	}
	restored, err := app.FindRecordById(intake.TaskCandidatesCollection, firstMemberID)
	if err != nil {
		t.Fatalf("the membership id did not survive the backfill: %v", err)
	}
	if restored.GetInt("recipient_index") != 1 {
		t.Fatalf("recipient position = %d, want 1", restored.GetInt("recipient_index"))
	}
	if restored.GetString("original_recipient") != "first@example.test" {
		t.Fatalf("original recipient = %q, want the saved single-recipient value", restored.GetString("original_recipient"))
	}
	claimed, err := app.FindRecordById(intake.CandidatesCollection, first.Id)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.GetString("claim_task") != task.Id {
		t.Fatalf("candidate claim = %q, want the surviving task reservation", claimed.GetString("claim_task"))
	}
}
