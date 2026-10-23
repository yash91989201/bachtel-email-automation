package intake_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// TestRecipientCellExpansion covers comma splitting of the selected recipient
// mapping: optional spacing, copied sibling values, single addresses, empty
// tokens, wholly empty cells, ignored blank rows and unsupported separators.
func TestRecipientCellExpansion(t *testing.T) {
	env := newEnv(t)
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name", "Location"},
		{"a@example.com,b@example.com", "Shared Name", "Remote"},
		{" c@example.com ,  d@example.com ", "Spaced", "Paris"},
		{"single@example.com", "Single", "Lisbon"},
		{"e@example.com,, f@example.com,", "Empty Tokens", "Berlin"},
		{"", "No Address", "Rome"},
		{"", "", ""},
		{"g@example.com; h@example.com", "Semicolon", "Oslo"},
		{"i@example.com j@example.com", "Space Separated", "Kyiv"},
	}}))

	want := intake.Counts{Total: 12, SourceRows: 7, Eligible: 7, Invalid: 5}
	if task.Counts != want {
		t.Fatalf("counts = %+v, want %+v", task.Counts, want)
	}

	rows := env.rows(task.ID)
	if len(rows) != 7 {
		t.Fatalf("membership rows = %d, want the 7 eligible entries", len(rows))
	}
	byKey := map[string]intake.RowSnapshot{}
	for _, row := range rows {
		byKey[row.Normalized["candidate_email"]] = row
	}
	for _, address := range []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com", "single@example.com", "e@example.com", "f@example.com"} {
		row, ok := byKey[address]
		if !ok {
			t.Fatalf("%s was not admitted: %+v", address, rows)
		}
		if row.State != intake.RowEligible {
			t.Fatalf("%s state = %q, want eligible", address, row.State)
		}
	}
	// Siblings copy the other mapped values and keep their own position and the
	// full original cell.
	a, b := byKey["a@example.com"], byKey["b@example.com"]
	if a.SourceRow != 2 || b.SourceRow != 2 {
		t.Fatalf("sibling source rows = %d and %d, want 2", a.SourceRow, b.SourceRow)
	}
	if a.RecipientIndex != 1 || b.RecipientIndex != 2 {
		t.Fatalf("sibling positions = %d and %d, want 1 and 2", a.RecipientIndex, b.RecipientIndex)
	}
	if a.OriginalRecipient != "a@example.com,b@example.com" || b.OriginalRecipient != a.OriginalRecipient {
		t.Fatalf("original cell = %q / %q", a.OriginalRecipient, b.OriginalRecipient)
	}
	if a.Normalized["candidate_name"] != "Shared Name" || b.Normalized["candidate_name"] != "Shared Name" {
		t.Fatalf("sibling copied values = %q / %q", a.Normalized["candidate_name"], b.Normalized["candidate_name"])
	}
	if a.Raw["candidate_email"] != "a@example.com" || b.Raw["candidate_email"] != "b@example.com" {
		t.Fatalf("sibling raw recipients = %q / %q", a.Raw["candidate_email"], b.Raw["candidate_email"])
	}
	// Optional comma spacing produces the same trimmed addresses.
	if c := byKey["c@example.com"]; c.RecipientIndex != 1 || c.Raw["candidate_email"] != " c@example.com " {
		t.Fatalf("spaced entry = %+v", c)
	}

	// Each sibling renders its own address and its own recipient placeholder,
	// while the original full cell stays available for comparison.
	previewA, err := env.service.Preview(context.Background(), env.owner, task.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	previewB, err := env.service.Preview(context.Background(), env.owner, task.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if previewA.Rendered.Recipient != "a@example.com" || previewB.Rendered.Recipient != "b@example.com" {
		t.Fatalf("preview recipients = %q / %q", previewA.Rendered.Recipient, previewB.Rendered.Recipient)
	}
	if previewA.OriginalRecipient != "a@example.com,b@example.com" || previewA.RecipientIndex != 1 || previewB.RecipientIndex != 2 {
		t.Fatalf("preview provenance = %+v / %+v", previewA, previewB)
	}
	if previewA.Rendered.Subject != "Application for Shared Name" || previewB.Rendered.Subject != previewA.Rendered.Subject {
		t.Fatalf("sibling subjects = %q / %q", previewA.Rendered.Subject, previewB.Rendered.Subject)
	}
}

// TestExpandedEntryLimit enforces the 1,000-entry bound after splitting and
// before duplicate or invalid exclusions, keeps a sheet whose extra rows hold
// only unmapped values usable, and publishes nothing when refused.
func TestExpandedEntryLimit(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	// Exactly 1,000 expanded entries from 500 source rows.
	rows := make([][]string, 0, 501)
	rows = append(rows, []string{"Email", "Applicant Full Name"})
	for i := range 500 {
		rows = append(rows, []string{
			fmt.Sprintf("left%d@example.com, right%d@example.com", i, i),
			fmt.Sprintf("Person %d", i),
		})
	}
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: rows}))
	if task.Counts.Total != intake.MaxRecipientEntries || task.Counts.SourceRows != 500 || task.Counts.Eligible != intake.MaxRecipientEntries {
		t.Fatalf("counts = %+v, want 1000 entries from 500 sources", task.Counts)
	}

	// Rows holding only unmapped values never consume the entry bound.
	unmapped := make([][]string, 0, 1006)
	unmapped = append(unmapped, []string{"Email", "Applicant Full Name", "Notes"})
	for i := range 1000 {
		unmapped = append(unmapped, []string{fmt.Sprintf("person%d@example.com", i), fmt.Sprintf("Person %d", i), ""})
	}
	for range 5 {
		unmapped = append(unmapped, []string{"", "", "note outside the mapped table"})
	}
	task = env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: unmapped}))
	if task.Counts.Total != 1000 || task.Counts.SourceRows != 1000 {
		t.Fatalf("unmapped rows consumed the limit: %+v", task.Counts)
	}

	// One cell with an excessive number of entries is refused as a whole,
	// whatever their validation outcome.
	excessive := make([]string, 0, intake.MaxRecipientEntries+1)
	for range 501 {
		excessive = append(excessive, "duplicate@example.com")
	}
	for i := range 500 {
		excessive = append(excessive, fmt.Sprintf("extra%d@example.com", i))
	}
	cases := map[string][]byte{
		"one cell over the bound": buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
			{"Email", "Applicant Full Name"},
			{strings.Join(excessive, ", "), "One Cell"},
		}}),
		"duplicate and invalid heavy": buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
			{"Email", "Applicant Full Name"},
			{strings.Join(excessive, ","), "Mixed"},
			{"", "No Address"},
		}}),
	}
	for name, workbook := range cases {
		t.Run(name, func(t *testing.T) {
			staged := env.stage(workbook, name+".xlsx")
			err := env.service.Validate(ctx, env.owner, staged.ID, "Candidates", 1)
			var failure *intake.ValidationError
			if !errors.As(err, &failure) || !strings.Contains(failure.Message, "more than 1000 recipient entries") {
				t.Fatalf("validate error = %v, want the expanded-recipient limit", err)
			}
			if rows := env.rows(staged.ID); len(rows) != 0 {
				t.Fatalf("refused import published %d memberships", len(rows))
			}
			if _, err := env.service.Manifest(ctx, env.owner, staged.ID); err == nil {
				t.Fatal("refused import published a manifest")
			}
			if loaded := env.mustLoad(staged.ID); loaded.Status != intake.TaskStaging {
				t.Fatalf("status = %q, want staging", loaded.Status)
			}
		})
	}
}

// TestDuplicateGroupingAcrossExpandedEntries runs the existing
// trimmed-lowercase grouping over every expanded entry before validity checks.
func TestDuplicateGroupingAcrossExpandedEntries(t *testing.T) {
	env := newEnv(t)
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"a@example.com, A@example.com, b@example.com", "Mixed Cell"},
		{"a@example.com", "Cross Row"},
		{"dup@example.com, dup@example.com", ""},
		{"ok@example.com, bad-address, ok2@example.com", "Valid Siblings"},
	}}))

	// a@example.com occurs three times, so every occurrence is held; both
	// dup@example.com entries are held even though their required value is
	// missing; the valid siblings of the invalid entry are independent.
	want := intake.Counts{Total: 9, SourceRows: 4, Eligible: 3, Invalid: 1, Duplicate: 5}
	if task.Counts != want {
		t.Fatalf("counts = %+v, want %+v", task.Counts, want)
	}
	rows := env.rows(task.ID)
	admitted := map[string]bool{}
	for _, row := range rows {
		admitted[row.Normalized["candidate_email"]] = true
	}
	if !admitted["b@example.com"] || !admitted["ok@example.com"] || !admitted["ok2@example.com"] {
		t.Fatalf("valid siblings were not admitted: %+v", rows)
	}
	if len(rows) != 3 || admitted["a@example.com"] || admitted["dup@example.com"] {
		t.Fatalf("duplicate entries reached candidate storage: %+v", rows)
	}

	manifest, err := env.service.Manifest(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var aOccurrences []intake.Occurrence
	for _, group := range manifest.Groups {
		if group.EmailKey == "a@example.com" {
			aOccurrences = group.Occurrences
		}
	}
	if fmt.Sprint(aOccurrences) != "[{2 1} {2 2} {3 1}]" {
		t.Fatalf("group occurrences = %v, want every occurrence with its position", aOccurrences)
	}
	// The grouping must not let the missing-value duplicate bypass detection.
	for _, entry := range manifest.Rows {
		if entry.Row == 4 && entry.State != "" {
			t.Fatalf("missing-value duplicate carries state %q", entry.State)
		}
	}

	// A later upload's duplicate group never modifies the campaign candidate a
	// previous task created.
	first := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"existing@example.com", "Existing"},
	}}))
	existingRows := env.rows(first.ID)
	candidate, err := env.app.FindRecordById(intake.CandidatesCollection, existingRows[0].CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.service.MarkSubmitted(context.Background(), env.owner, candidate.Id); err != nil {
		t.Fatal(err)
	}
	before, err := env.app.FindRecordById(intake.CandidatesCollection, candidate.Id)
	if err != nil {
		t.Fatal(err)
	}
	env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"existing@example.com, other@example.com", "Existing Duplicate"},
		{"existing@example.com", "Again"},
	}}))
	after, err := env.app.FindRecordById(intake.CandidatesCollection, candidate.Id)
	if err != nil {
		t.Fatal(err)
	}
	if before.GetString("submission_state") != after.GetString("submission_state") ||
		before.GetString("claim_task") != after.GetString("claim_task") ||
		before.GetString("updated") != after.GetString("updated") {
		t.Fatalf("duplicate upload modified the existing candidate: %v -> %v", before, after)
	}
}

// TestOccurrenceAwarePublicationAndReport verifies every excluded occurrence
// keeps its own source coordinates, individual recipient and copied source
// columns, with collision-safe metadata labels.
func TestOccurrenceAwarePublicationAndReport(t *testing.T) {
	env := newEnv(t)
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name", "Reason"},
		{"a@example.com, invalid, a@example.com", "Ada", "imported"},
		{"b@example.com", "", "missing name"},
	}}))
	if task.Counts.Total != 4 || task.Counts.SourceRows != 2 || task.Counts.Duplicate != 2 || task.Counts.Invalid != 1 || task.Counts.Ineligible != 1 {
		t.Fatalf("counts = %+v", task.Counts)
	}

	manifest, err := env.service.Manifest(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != intake.ManifestVersion || manifest.SourceRows != 2 {
		t.Fatalf("manifest = version %d, source rows %d", manifest.Version, manifest.SourceRows)
	}
	type occurrenceRecord struct {
		row      int
		position int
		state    string
	}
	var got []occurrenceRecord
	for _, entry := range manifest.Rows {
		got = append(got, occurrenceRecord{row: entry.Row, position: entry.Position, state: entry.State})
	}
	want := []occurrenceRecord{
		{row: 2, position: 1, state: ""},
		{row: 2, position: 2, state: ""},
		{row: 2, position: 3, state: ""},
		{row: 3, position: 1, state: intake.RowIneligible},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("manifest entries = %v, want %v", got, want)
	}
	if entries := manifest.Groups[0].Occurrences; len(entries) != 2 || entries[0].Position != 1 || entries[1].Position != 3 {
		t.Fatalf("duplicate occurrences = %+v, want positions 1 and 3", entries)
	}

	data, _, err := env.service.ExclusionReport(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	grid, err := file.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(grid[0], "|"), "Email|Applicant Full Name|Reason|Source worksheet row|Recipient position|Recipient|Reason (2)|Affected fields|Reason source|Original mapped values|Proposed mapped values"; got != want {
		t.Fatalf("report header = %q, want %q", got, want)
	}
	if len(grid) != 5 {
		t.Fatalf("report rows = %d, want header plus four excluded entries: %v", len(grid), grid)
	}
	type reportEntry struct {
		row       string
		position  string
		recipient string
		reason    string
		name      string
		note      string
	}
	entries := make([]reportEntry, 0, len(grid)-1)
	for _, row := range grid[1:] {
		entries = append(entries, reportEntry{row: row[3], position: row[4], recipient: row[5], reason: row[6], name: row[1], note: row[2]})
	}
	wantReport := []reportEntry{
		{row: "2", position: "1", recipient: "a@example.com", reason: "duplicate recipient address in this upload; every row of the group is held", name: "Ada", note: "imported"},
		{row: "2", position: "2", recipient: "invalid", reason: "the recipient cell is empty or is not a single usable email address", name: "Ada", note: "imported"},
		{row: "2", position: "3", recipient: "a@example.com", reason: "duplicate recipient address in this upload; every row of the group is held", name: "Ada", note: "imported"},
		{row: "3", position: "1", recipient: "b@example.com", reason: "missing required value(s): candidate_name", name: "", note: "missing name"},
	}
	if fmt.Sprint(entries) != fmt.Sprint(wantReport) {
		t.Fatalf("report entries = %+v, want %+v", entries, wantReport)
	}
}

// TestManifestVersionBoundary reads a version 1 manifest as singleton
// occurrences without resplitting saved cells, and round-trips the current
// layout.
func TestManifestVersionBoundary(t *testing.T) {
	legacy := []byte(`{
		"version": 1,
		"sheet": "Candidates",
		"header_row": 1,
		"headers": ["Email", "Applicant Full Name"],
		"rows": [
			{"row": 2, "email_key": "a@example.com, b@example.com", "reasons": ["invalid_address"]},
			{"row": 3, "email_key": "carol@example.com", "state": "eligible"},
			{"row": 3, "email_key": "dana@example.com", "state": "eligible"}
		],
		"duplicate_groups": [{"group": 1, "email_key": "carol@example.com", "rows": [3, 4]}]
	}`)
	var manifest intake.Manifest
	if err := json.Unmarshal(legacy, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 {
		t.Fatalf("read version = %d, want the saved 1", manifest.Version)
	}
	if manifest.Rows[0].Position != 1 || manifest.Rows[0].Recipient != "a@example.com, b@example.com" {
		t.Fatalf("legacy singleton = %+v, want the saved cell at position 1 without resplitting", manifest.Rows[0])
	}
	if manifest.SourceRows != 2 {
		t.Fatalf("source rows = %d, want the two distinct saved rows", manifest.SourceRows)
	}
	if entries := manifest.Groups[0].Occurrences; len(entries) != 2 || entries[0] != (intake.Occurrence{Row: 3, Position: 1}) || entries[1] != (intake.Occurrence{Row: 4, Position: 1}) {
		t.Fatalf("legacy group occurrences = %+v", entries)
	}

	encoded, err := json.Marshal(intake.Manifest{
		Version:    intake.ManifestVersion,
		Sheet:      "Candidates",
		HeaderRow:  1,
		Headers:    []string{"Email"},
		SourceRows: 1,
		Rows:       []intake.ManifestRow{{Row: 2, Position: 2, Recipient: "b@example.com", EmailKey: "b@example.com", State: intake.RowEligible}},
		Groups:     []intake.DuplicateGroup{{Group: 1, EmailKey: "b@example.com", Occurrences: []intake.Occurrence{{Row: 2, Position: 2}, {Row: 5, Position: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip intake.Manifest
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Version != intake.ManifestVersion || len(roundTrip.Rows) != 1 || roundTrip.Rows[0].Position != 2 {
		t.Fatalf("round trip = %+v", roundTrip)
	}
	if fmt.Sprint(roundTrip.Groups) != fmt.Sprint([]intake.DuplicateGroup{{Group: 1, EmailKey: "b@example.com", Occurrences: []intake.Occurrence{{Row: 2, Position: 2}, {Row: 5, Position: 1}}}}) {
		t.Fatalf("round-trip groups = %+v", roundTrip.Groups)
	}
}
