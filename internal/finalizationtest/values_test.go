package finalizationtest

import (
	"bytes"
	"context"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// The accepted final values are the same values used by download, preview,
// snapshot-specific test and candidate work. Populated text is never normalized
// a second time, and finalization itself creates no AI evidence.
func TestAcceptedFinalValuesDriveEveryPath(t *testing.T) {
	f := newFixture(t)
	f.connectMailbox(t)
	configuration, err := f.campaigns.LoadConfiguration(context.Background(), f.owner, f.config)
	if err != nil {
		t.Fatal(err)
	}
	// The trailing/extra whitespace would be collapsed by a formatting rule.
	values := map[string]string{
		"candidate_email": "  ADA@EXAMPLE.COM ",
		"candidate_name":  "  Ada   Lovelace  ",
		"role":            "Engineer",
	}
	entry, err := f.upload(t, "values-final-123456", []sheetRow{{Values: values}})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Counts.Accepted != 1 {
		t.Fatalf("snapshot = %+v", entry.Counts)
	}
	// Finalization does not create AI evidence.
	runs, err := f.app.FindRecordsByFilter("preparation_runs", "task = {:task}", "", 0, 0, map[string]any{"task": f.task})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("final upload created %d AI runs", len(runs))
	}
	// The downloaded accepted sheet contains the operator values apart from the
	// documented recipient trim/lowercase.
	sheet, _, err := f.service.FinalSheet(context.Background(), f.owner, f.task, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(sheet))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := book.GetRows("Sending")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("sending sheet rows = %d", len(rows))
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	labels := campaign.MappingLabels(configuration.Mappings)
	column := func(label string) int {
		for index, value := range rows[0] {
			if value == label {
				return index
			}
		}
		t.Fatalf("label %q missing from %v", label, rows[0])
		return -1
	}
	if rows[1][column(labels[0])] != "ada@example.com" || rows[1][column(labels[1])] != "  Ada   Lovelace  " || rows[1][column(labels[2])] != "Engineer" {
		t.Fatalf("downloaded values = %v", rows[1])
	}

	// A snapshot-specific test renders the accepted member values with only the
	// destination overridden.
	store := sending.NewStore(f.app, sending.StoreOptions{})
	member := f.eligibleMember(t)
	attempt := f.submitTest(t, store, "values-test-123456", member)
	attemptRecord, err := f.app.FindRecordById(sending.AttemptsCollection, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if attemptRecord.GetString("subject") != "Hello   Ada   Lovelace  " {
		t.Fatalf("test subject = %q", attemptRecord.GetString("subject"))
	}
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "values-approve-123456"); err != nil {
		t.Fatal(err)
	}
	// Candidate work renders the same accepted values.
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "values-start-123456", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	work, err := store.NextWork(context.Background())
	if err != nil || work == nil {
		t.Fatalf("next work = %v %v", work, err)
	}
	if work.Subject != attemptRecord.GetString("subject") || work.Body != attemptRecord.GetString("body") {
		t.Fatalf("send content differs from the approved test: %q/%q vs %q/%q", work.Subject, work.Body, attemptRecord.GetString("subject"), attemptRecord.GetString("body"))
	}
	if work.SnapshotID != entry.ID || work.SnapshotDigest != entry.Digest {
		t.Fatalf("work snapshot identity = %q %q", work.SnapshotID, work.SnapshotDigest)
	}
}

// The Approve-as-is path runs from prepared evidence through exclusions,
// exact-snapshot test approval and explicit candidate start with simulated
// providers. No AI call happens after finalization.
func TestApproveAsIsEndToEndPath(t *testing.T) {
	f := newFixture(t)
	f.connectMailbox(t)
	f.addPreparedRun(t, []preparation.RowResult{
		{SourceRow: 2, Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "  Ada   Lovelace  ", "role": "Engineer"}, Corrections: []preparation.Correction{{Row: 2, Field: "candidate_name", Header: "Name", Note: "Cleaned spacing"}}},
		{SourceRow: 3, Values: map[string]string{"candidate_email": "missing@example.com", "candidate_name": "", "role": ""}},
	})
	snapshot, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "e2e-approve-123456", f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.Accepted != 1 || snapshot.Counts.Excluded != 1 {
		t.Fatalf("prepared finalization = %+v", snapshot.Counts)
	}
	// Prepared download contains only accepted entries, while the final
	// download and exclusion report preserve the accepted and rejected sets.
	preparedSheet, _, err := f.service.PreparedSheet(context.Background(), f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if rows := readSheet(t, preparedSheet); len(rows) != 3 {
		t.Fatalf("prepared sheet rows = %d, want header plus both prepared entries: %v", len(rows), rows)
	}
	finalSheet, _, err := f.service.FinalSheet(context.Background(), f.owner, f.task, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalRows := readSheet(t, finalSheet)
	if rows := len(finalRows); rows != 2 {
		t.Fatalf("final sheet rows = %d, want header plus one accepted entry: %v", rows, finalRows)
	}
	exclusions, _, err := f.service.ExclusionSheet(context.Background(), f.owner, f.task, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rows := readSheet(t, exclusions); len(rows) != 2 {
		t.Fatalf("exclusion rows = %d, want header plus one rejected entry: %v", len(rows), rows)
	}
	// One explicit test for this exact snapshot, then one explicit start.
	store := sending.NewStore(f.app, sending.StoreOptions{})
	member := f.eligibleMember(t)
	attempt := f.submitTest(t, store, "e2e-test-123456", member)
	if err := store.Approve(context.Background(), f.owner, f.task, attempt, f.owner, "e2e-format-approval-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Act(context.Background(), f.owner, sending.ActionRequest{ActionID: "e2e-start-123456", TaskID: f.task, Kind: "start", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	work, err := store.NextWork(context.Background())
	if err != nil || work == nil {
		t.Fatalf("candidate work = %v %v", work, err)
	}
	if work.SnapshotID != snapshot.ID || work.SnapshotDigest != snapshot.Digest || work.Subject != "Hello   Ada   Lovelace  " {
		t.Fatalf("candidate work = %+v", work)
	}
	// The only preparation run is the one supplied as evidence; finalization,
	// test and start added none.
	runs, err := f.app.FindRecordsByFilter("preparation_runs", "task = {:task}", "", 0, 0, map[string]any{"task": f.task})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("preparation runs = %d, want the single captured run", len(runs))
	}
}

// readSheet opens one workbook and returns its first worksheet rows.
func readSheet(t *testing.T, data []byte) [][]string {
	t.Helper()
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book.GetRows(book.GetSheetList()[0])
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
