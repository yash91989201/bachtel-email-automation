package finalizationtest

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

func TestHeaderPreflightDefersEligibilityUntilCleanedReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tasks := intake.NewService(f.app, f.campaigns, intake.DefaultLimits())
	configuration, err := f.campaigns.LoadConfiguration(ctx, f.owner, f.config)
	if err != nil {
		t.Fatal(err)
	}
	raw := []sheetRow{
		{Values: map[string]string{"candidate_email": "a@example.com / B@example.com", "candidate_name": "  Ada  "}},
		{Values: map[string]string{"candidate_email": "dup@example.com", "candidate_name": "Dup"}},
		{Values: map[string]string{"candidate_email": "DUP@example.com", "candidate_name": ""}},
		{Values: map[string]string{"candidate_email": "NOT AN EMAIL", "candidate_name": "Invalid"}},
		{Values: map[string]string{"candidate_email": "missing@example.com", "candidate_name": ""}},
	}
	data, err := sendingSheet(configuration, raw)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Stage(ctx, f.owner, f.campaign, "raw.xlsx", data)
	if err != nil {
		t.Fatal(err)
	}
	f.task = task.ID
	if err := tasks.Validate(ctx, f.owner, f.task, "Sending", 1); err != nil {
		t.Fatal(err)
	}
	pinned, err := tasks.LoadTask(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Status != intake.TaskReady || pinned.ConfigurationID != f.config || pinned.Counts.SourceRows != 5 || pinned.Counts.Total != 0 || pinned.Counts.Invalid != 0 || pinned.Counts.Duplicate != 0 {
		t.Fatalf("preflight = %+v", pinned)
	}
	assertNoCandidates := func() {
		t.Helper()
		for _, collection := range []string{intake.CandidatesCollection, intake.TaskCandidatesCollection} {
			count, err := f.app.CountRecords(collection)
			if err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("%s records before finalization = %d", collection, count)
			}
		}
	}
	assertNoCandidates()
	mapped, err := tasks.MappedRows(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if len(mapped) != 5 || mapped[0].Values["candidate_email"] != "a@example.com / B@example.com" {
		t.Fatalf("mapped source = %+v", mapped)
	}
	input, err := preparation.BuildInput(configuration, pinned.SourceDigest, mapped)
	if err != nil {
		t.Fatal(err)
	}
	// The real response validator permits separator cleanup while preserving
	// each supplied occurrence. Missing fields and malformed addresses survive
	// into the backend review rather than being filtered at intake.
	cleaned := `{"rows":[{"id":"2","values":{"candidate_email":"a@example.com, b@example.com","candidate_name":"Ada","role":""}},{"id":"3","values":{"candidate_email":"dup@example.com","candidate_name":"Dup","role":""}},{"id":"4","values":{"candidate_email":"dup@example.com","candidate_name":"","role":""}},{"id":"5","values":{"candidate_email":"NOT AN EMAIL","candidate_name":"Invalid","role":""}},{"id":"6","values":{"candidate_email":"missing@example.com","candidate_name":"","role":""}}]}`
	rows, err := preparation.ParseResponse(cleaned, input)
	if err != nil {
		t.Fatal(err)
	}
	runID := f.addPreparedRun(t, rows)
	review, err := f.service.ReviewPrepared(ctx, f.owner, f.task, runID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Counts.Entries != 6 || review.Counts.Accepted != 2 || review.Counts.Duplicate != 2 || review.Counts.Invalid != 1 || review.Counts.MissingValue != 2 {
		t.Fatalf("cleaned review = %+v", review.Counts)
	}
	if review.Entries[1].Recipient != "b@example.com" || review.Entries[1].Position != 2 {
		t.Fatalf("cleaned sibling = %+v", review.Entries[1])
	}
	assertNoCandidates()
	sheet, _, err := f.service.PreparedSheet(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(sheet))
	if err != nil {
		t.Fatal(err)
	}
	grid, err := book.GetRows("Sending")
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	if len(grid) != 3 || grid[1][0] != "a@example.com" || grid[2][0] != "b@example.com" {
		t.Fatalf("prepared sheet = %v", grid)
	}
	report, _, err := f.service.PreparedExclusionSheet(ctx, f.owner, f.task, runID)
	if err != nil || len(report) == 0 {
		t.Fatalf("report bytes=%d err=%v", len(report), err)
	}
	snapshot, err := f.service.ApproveAsIs(ctx, f.owner, f.task, "cleaned-review-12345", f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts != review.Counts || len(snapshot.Exclusions) != 4 {
		t.Fatalf("final differs from review: %+v", snapshot.Counts)
	}
	if err := tasks.Revalidate(ctx, f.owner, f.task); err != nil {
		t.Fatal(err)
	}
	pinned, err = tasks.LoadTask(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.SelectedFinalSnapshotID != "" || pinned.SelectedPreparationRunID != "" || pinned.FinalSnapshotDigest != "" || pinned.Counts.Total != 0 {
		t.Fatalf("repreflight retains sending data: %+v", pinned)
	}
	members, err := tasks.Rows(ctx, f.owner, f.task)
	if err != nil || len(members) != 0 {
		t.Fatalf("repreflight memberships=%v err=%v", members, err)
	}
}

func TestPreparedReviewBoundsAndOwnerIsolation(t *testing.T) {
	f := newFixture(t)
	rows := []preparation.RowResult{{SourceRow: 2, Values: map[string]string{"candidate_email": strings.Repeat("a@example.com,", 1000) + "last@example.com", "candidate_name": "Ada"}}}
	runID := f.addPreparedRun(t, rows)
	if _, err := f.service.ReviewPrepared(context.Background(), f.owner, f.task, runID); !errors.Is(err, finalization.ErrTooManyEntries) {
		t.Fatalf("oversized review = %v", err)
	}
	other := f.createOperator(t, "foreign-review@example.test")
	if _, err := f.service.ReviewPrepared(context.Background(), other, f.task, runID); !errors.Is(err, finalization.ErrNotFound) {
		t.Fatalf("foreign review = %v", err)
	}
}

func TestHeaderPreflightRejectsMissingAndAmbiguousMappings(t *testing.T) {
	for _, headers := range [][]string{{"Email", "Wrong Name", "Role"}, {"Email", " Email ", "Name", "Role"}} {
		t.Run(strings.Join(headers, "-"), func(t *testing.T) {
			f := newFixture(t)
			tasks := intake.NewService(f.app, f.campaigns, intake.DefaultLimits())
			book := excelize.NewFile()
			if err := book.SetSheetRow("Sheet1", "A1", &headers); err != nil {
				t.Fatal(err)
			}
			data, err := book.WriteToBuffer()
			if err != nil {
				t.Fatal(err)
			}
			if err := book.Close(); err != nil {
				t.Fatal(err)
			}
			task, err := tasks.Stage(context.Background(), f.owner, f.campaign, "headers.xlsx", data.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if err := tasks.Validate(context.Background(), f.owner, task.ID, "Sheet1", 1); err == nil {
				t.Fatal("ambiguous or missing mapped header accepted")
			}
			loaded, err := tasks.LoadTask(context.Background(), f.owner, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Status != intake.TaskStaging || loaded.ConfigurationID != "" {
				t.Fatalf("failed preflight published selection: %+v", loaded)
			}
		})
	}
}

// A preflighted task holds no memberships and no eligibility evidence, so the
// start preflight must report it as pinned-and-waiting rather than unvalidated,
// with an empty claimable set. The mailbox-binding test covers the refusal of an
// explicit start or schedule before a final snapshot exists.
func TestPreflightReportsPinnedTaskWithoutMemberships(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tasks := intake.NewService(f.app, f.campaigns, intake.DefaultLimits())
	book := excelize.NewFile()
	if err := book.SetSheetRow("Sheet1", "A1", &[]string{"Email", "Name", "Role"}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Sheet1", "A2", &[]string{"ada@example.com", "Ada", "Engineer"}); err != nil {
		t.Fatal(err)
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Stage(ctx, f.owner, f.campaign, "waiting.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.Validate(ctx, f.owner, task.ID, "Sheet1", 1); err != nil {
		t.Fatal(err)
	}
	result, err := tasks.Preflight(ctx, f.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Ready {
		t.Fatalf("preflighted task reported unready: %+v", result)
	}
	if len(result.Claimable) != 0 {
		t.Fatalf("claimable before finalization = %d", len(result.Claimable))
	}
	for _, reason := range result.Reasons {
		if strings.Contains(reason, "not been validated") {
			t.Fatalf("preflighted task reported as unvalidated: %q", reason)
		}
	}
}

// The prepared review combines campaign history and AI findings with the
// backend checks, and the prepared exclusion report carries each held entry with
// its source location, original value and reason.
func TestPreparedReviewCountsHistoryAndFindings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	otherTask := f.createTask(t)
	candidates, err := f.app.FindCollectionByNameOrId(intake.CandidatesCollection)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct {
		email   string
		state   string
		claimed string
	}{
		{email: "sent@example.com", state: intake.SubmissionSubmitted},
		{email: "claimed@example.com", state: intake.SubmissionNone, claimed: otherTask},
	} {
		record := core.NewRecord(candidates)
		record.Set("campaign", f.campaign)
		record.Set("email_key", seed.email)
		record.Set("submission_state", seed.state)
		record.Set("claim_task", seed.claimed)
		if err := f.app.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	runID := f.addPreparedRun(t, []preparation.RowResult{
		{SourceRow: 2, Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}, Findings: []preparation.Finding{{Row: 2, Field: "candidate_name", Header: "Name", Scope: preparation.ScopeShared, Note: "the name looks like a role"}}},
		{SourceRow: 3, Values: map[string]string{"candidate_email": "sent@example.com", "candidate_name": "Sent"}},
		{SourceRow: 4, Values: map[string]string{"candidate_email": "claimed@example.com", "candidate_name": "Claimed"}},
		{SourceRow: 5, Values: map[string]string{"candidate_email": "clean@example.com", "candidate_name": "Clean"}},
	})
	review, err := f.service.ReviewPrepared(ctx, f.owner, f.task, runID)
	if err != nil {
		t.Fatal(err)
	}
	if review.Counts.Accepted != 1 || review.Counts.Excluded != 3 || review.Counts.AIHeld != 1 || review.Counts.History != 2 {
		t.Fatalf("review counts = %+v", review.Counts)
	}
	if review.Entries[0].Recipient != "clean@example.com" {
		t.Fatalf("accepted entries = %+v", review.Entries)
	}
	reasons := map[int]string{}
	for _, exclusion := range review.Exclusions {
		texts := make([]string, 0, len(exclusion.Reasons))
		for _, reason := range exclusion.Reasons {
			texts = append(texts, reason.Text)
		}
		reasons[exclusion.SourceRow] = strings.Join(texts, "; ")
	}
	if !strings.Contains(reasons[2], "the name looks like a role") {
		t.Fatalf("AI finding reason = %q", reasons[2])
	}
	if !strings.Contains(reasons[3], "already submitted") || !strings.Contains(reasons[4], "another task of this campaign") {
		t.Fatalf("history reasons = %q / %q", reasons[3], reasons[4])
	}
	report, _, err := f.service.PreparedExclusionSheet(ctx, f.owner, f.task, runID)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	sheet := book.GetSheetList()[0]
	grid, err := book.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 4 {
		t.Fatalf("report rows = %d, want header plus the three held entries: %v", len(grid), grid)
	}
	header := grid[0]
	column := func(label string) int {
		for index, value := range header {
			if value == label {
				return index
			}
		}
		t.Fatalf("report header %q missing from %v", label, header)
		return -1
	}
	recipient := column("Recipient")
	row := column("Original source row")
	reason := column("Reason")
	reported := map[string]string{}
	for _, cells := range grid[1:] {
		reported[cells[recipient]] = cells[row] + "|" + cells[reason]
	}
	if got := reported["sent@example.com"]; !strings.HasPrefix(got, "3|") || !strings.Contains(got, "already submitted") {
		t.Fatalf("report history row = %q", got)
	}
	if got := reported["ada@example.com"]; !strings.HasPrefix(got, "2|") || !strings.Contains(got, "the name looks like a role") {
		t.Fatalf("report finding row = %q", got)
	}
	if _, ok := reported["clean@example.com"]; ok {
		t.Fatalf("report includes an accepted entry: %v", reported)
	}
}

// A stored workbook that no longer matches the digest pinned on the task cannot
// be reviewed or approved through the run's evidence.
func TestPreparedReviewAndApprovalRefuseChangedSource(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runID := f.addPreparedRun(t, []preparation.RowResult{{SourceRow: 2, Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}}})
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("source_digest", "replaced-workbook-digest")
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReviewPrepared(ctx, f.owner, f.task, runID); !errors.Is(err, finalization.ErrStale) {
		t.Fatalf("review of a changed source = %v", err)
	}
	if _, err := f.service.ApproveAsIs(ctx, f.owner, f.task, "changed-source-123456", f.owner); !errors.Is(err, finalization.ErrStale) {
		t.Fatalf("approval of a changed source = %v", err)
	}
	snapshots, err := f.app.FindRecordsByFilter(finalization.SnapshotsCollection, "task = {:task}", "", 0, 0, map[string]any{"task": f.task})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("refused approval created %d snapshots", len(snapshots))
	}
}

// Only a completed run with output is review evidence.
func TestPreparedReviewRefusesIncompleteRun(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	runs, err := f.app.FindCollectionByNameOrId(preparation.RunsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(runs)
	record.Set("owner", f.owner)
	record.Set("task", f.task)
	record.Set("kind", preparation.KindInitial)
	record.Set("state", preparation.RunRunning)
	record.Set("provider", "custom")
	record.Set("api_root", "https://example.test/v1")
	record.Set("api_format", "chat_completions")
	record.Set("model", "test-model")
	record.Set("configuration_generation", 1)
	record.Set("credential_generation", 1)
	record.Set("configuration", f.config)
	record.Set("content_hash", "content-hash-1")
	record.Set("base_prompt_version", preparation.BasePromptVersion)
	record.Set("base_prompt", "base prompt")
	record.Set("input_digest", "input-digest")
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReviewPrepared(ctx, f.owner, f.task, record.Id); !errors.Is(err, finalization.ErrNoPrepared) {
		t.Fatalf("incomplete review = %v", err)
	}
}

// The mapped-row source boundary refuses a staging task with an actionable
// message and refuses a stored workbook that no longer matches its digest, so
// preparation can never read unselected or replaced source bytes.
func TestMappedRowsRefusesStagingAndChangedWorkbook(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tasks := intake.NewService(f.app, f.campaigns, intake.DefaultLimits())
	book := excelize.NewFile()
	if err := book.SetSheetRow("Sheet1", "A1", &[]string{"Email", "Name", "Role"}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Sheet1", "A2", &[]string{"ada@example.com", "Ada", "Engineer"}); err != nil {
		t.Fatal(err)
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	staged, err := tasks.Stage(ctx, f.owner, f.campaign, "staging.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.MappedRows(ctx, f.owner, staged.ID); err == nil || !strings.Contains(err.Error(), "worksheet and mapped headers") {
		t.Fatalf("staging mapped rows = %v", err)
	}
	if err := tasks.Validate(ctx, f.owner, staged.ID, "Sheet1", 1); err != nil {
		t.Fatal(err)
	}
	record, err := f.app.FindRecordById(intake.TasksCollection, staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("source_digest", "replaced-workbook-digest")
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.MappedRows(ctx, f.owner, staged.ID); err == nil || !strings.Contains(err.Error(), "no longer matches the digest") {
		t.Fatalf("changed workbook mapped rows = %v", err)
	}
}
