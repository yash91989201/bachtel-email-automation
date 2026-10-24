package finalizationtest

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// setSourceRows replaces the stored source workbook so a prepared run can
// reference several source rows with their original values.
func (f *fixture) setSourceRows(t *testing.T, rows [][]string) {
	t.Helper()
	book := excelize.NewFile()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name", "Role"}); err != nil {
		t.Fatal(err)
	}
	for index, row := range rows {
		if err := book.SetSheetRow("Candidates", "A"+strconv.Itoa(index+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := filesystem.NewFileFromBytes(data.Bytes(), "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	record, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("source", source)
	record.Set("source_digest", intake.WorkbookDigest(data.Bytes()))
	if err := f.app.Save(record); err != nil {
		t.Fatalf("replace source workbook: %v", err)
	}
}

// The sanitization report lists every prepared occurrence once with its backend
// outcome, its applicable findings, the source row's corrections and the
// original and prepared value of every mapping. It never changes the other
// prepared downloads.
func TestSanitizationReportAuditsEveryPreparedOccurrence(t *testing.T) {
	f := newFixture(t)
	f.setSourceRows(t, [][]string{
		{"ada@example.com, grace@example.com", "  Ada  ", "=SUM(A1:A2)"},
		{"linus@example.com, ken@example.com", "Linus", "Kernel"},
	})
	runID := f.addPreparedRun(t, []preparation.RowResult{
		{
			SourceRow:   2,
			Values:      map[string]string{"candidate_email": "ada@example.com, grace@example.com", "candidate_name": "Ada", "role": "=SUM(A1:A2)"},
			Corrections: []preparation.Correction{{Row: 2, Field: "candidate_name", Header: "Name", Note: "removed surrounding spaces"}},
			Findings:    []preparation.Finding{{Row: 2, Field: "role", Header: "Role", Scope: preparation.ScopeShared, Note: "confirm the role"}},
		},
		{
			SourceRow:   3,
			Values:      map[string]string{"candidate_email": "linus@example.com, ken@example.com", "candidate_name": "Linus", "role": "Kernel"},
			Corrections: []preparation.Correction{{Row: 3, Field: "role", Header: "Role", Note: "normalized role text"}},
			Findings:    []preparation.Finding{{Row: 3, Field: "candidate_email", Header: "Email", Scope: preparation.ScopeRecipient, Position: 2, Note: "uncertain address"}},
		},
	})
	ctx := context.Background()
	sheetBefore, _, err := f.service.PreparedSheet(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	exclusionsBefore, _, err := f.service.PreparedExclusionSheet(ctx, f.owner, f.task, runID)
	if err != nil {
		t.Fatal(err)
	}
	report, filename, err := f.service.SanitizationReport(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if filename == "" || len(report) == 0 {
		t.Fatalf("report filename=%q bytes=%d", filename, len(report))
	}
	sheetAfter, _, err := f.service.PreparedSheet(ctx, f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	exclusionsAfter, _, err := f.service.PreparedExclusionSheet(ctx, f.owner, f.task, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sheetBefore, sheetAfter) {
		t.Fatal("the sanitization report changed the prepared sending sheet")
	}
	if !bytes.Equal(exclusionsBefore, exclusionsAfter) {
		t.Fatal("the sanitization report changed the prepared exclusion report")
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
	rows, err := book.GetRows(finalization.SanitizationSheetName)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("report rows = %d, want header plus four occurrences: %v", len(rows), rows)
	}
	header := rows[0]
	seen := make(map[string]struct{}, len(header))
	for _, label := range header {
		if _, repeated := seen[label]; repeated {
			t.Fatalf("duplicated column label %q: %v", label, header)
		}
		seen[label] = struct{}{}
	}
	index := func(label string) int {
		for i, value := range header {
			if value == label {
				return i
			}
		}
		t.Fatalf("report missing %q: %v", label, header)
		return -1
	}

	// Both occurrences of the shared-finding row are held and repeat it.
	for _, row := range rows[1:3] {
		if row[index("Source row")] != "2" || row[index("Outcome")] != preparation.OutcomeHeld {
			t.Fatalf("shared-finding occurrence = %v", row)
		}
		if !strings.Contains(row[index("Exclusion reasons")], "ai: Role: confirm the role") ||
			!strings.Contains(row[index("AI findings")], "Role: confirm the role") {
			t.Fatalf("shared finding missing from %v", row)
		}
		if !strings.Contains(row[index("AI corrections")], "Name: removed surrounding spaces") {
			t.Fatalf("correction missing from %v", row)
		}
	}
	if rows[1][index("Prepared recipient")] != "ada@example.com" || rows[2][index("Prepared recipient")] != "grace@example.com" {
		t.Fatalf("prepared recipients = %q / %q", rows[1][index("Prepared recipient")], rows[2][index("Prepared recipient")])
	}

	// The recipient-scoped finding holds and reports only its address; its
	// sibling stays accepted with the correction kept apart from the findings.
	if rows[3][index("Outcome")] != preparation.OutcomeAccepted ||
		rows[3][index("Exclusion reasons")] != "" || rows[3][index("AI findings")] != "" {
		t.Fatalf("accepted sibling = %v", rows[3])
	}
	if !strings.Contains(rows[3][index("AI corrections")], "Role: normalized role text") {
		t.Fatalf("accepted sibling lost its correction: %v", rows[3])
	}
	if rows[4][index("Outcome")] != preparation.OutcomeHeld ||
		!strings.Contains(rows[4][index("Exclusion reasons")], "Email (address 2): uncertain address") ||
		!strings.Contains(rows[4][index("AI findings")], "Email (address 2): uncertain address") {
		t.Fatalf("recipient-scoped held occurrence = %v", rows[4])
	}
	if rows[3][index("Position")] != "1" || rows[4][index("Position")] != "2" {
		t.Fatalf("occurrence positions = %q / %q", rows[3][index("Position")], rows[4][index("Position")])
	}

	// The uploaded value and the prepared value are separate columns.
	if rows[1][index("Name (original)")] != "  Ada  " || rows[1][index("Name (prepared)")] != "Ada" {
		t.Fatalf("mapping pair = %q / %q", rows[1][index("Name (original)")], rows[1][index("Name (prepared)")])
	}

	// Formula-like text is stored as text, never promoted to a formula.
	originalCell, err := excelize.CoordinatesToCellName(index("Role (original)")+1, 2)
	if err != nil {
		t.Fatal(err)
	}
	preparedCell, err := excelize.CoordinatesToCellName(index("Role (prepared)")+1, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range []string{originalCell, preparedCell} {
		cellType, err := book.GetCellType(finalization.SanitizationSheetName, cell)
		if err != nil {
			t.Fatal(err)
		}
		if cellType == excelize.CellTypeFormula {
			t.Fatalf("formula-like text in %s was promoted to a formula", cell)
		}
	}

	other := f.createOperator(t, "sanitization-foreign@example.test")
	if _, _, err := f.service.SanitizationReport(ctx, other, f.task); !errors.Is(err, finalization.ErrNotFound) {
		t.Fatalf("foreign download = %v", err)
	}
	taskRecord, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	taskRecord.Set("selected_preparation_run", "")
	if err := f.app.Save(taskRecord); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.SanitizationReport(ctx, f.owner, f.task); !errors.Is(err, finalization.ErrNoPrepared) {
		t.Fatalf("download without a selected run = %v", err)
	}
}
