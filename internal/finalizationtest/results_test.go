package finalizationtest

import (
	"bytes"
	"context"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// Post-execution results keep the final snapshot identity and frozen values
// beside the original source evidence.
func TestResultsWorkbookCarriesFinalProvenance(t *testing.T) {
	f := newFixture(t)
	f.addPreparedRun(t, []preparation.RowResult{{
		SourceRow: 2,
		Values:    map[string]string{"candidate_email": "ada@example.com", "candidate_name": "  Ada   Lovelace  ", "role": "Engineer"},
	}})
	snapshot, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "results-final-123456", f.owner)
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("status", "completed")
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	service := exports.NewService(f.app, config.Config{})
	if err := service.GenerateTask(context.Background(), f.owner, f.task, false); err != nil {
		t.Fatal(err)
	}
	data, _, err := service.DownloadTask(context.Background(), f.owner, f.task)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book.GetRows("Recipient results")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("results rows = %d, want header plus the accepted entry: %v", len(rows), rows)
	}
	header := rows[0]
	find := func(label string) int {
		for index, value := range header {
			if value == label {
				return index
			}
		}
		t.Fatalf("header missing %q: %v", label, header)
		return -1
	}
	if rows[1][find("Final snapshot")] != snapshot.ID {
		t.Fatalf("final snapshot provenance = %q", rows[1][find("Final snapshot")])
	}
	if rows[1][find("Final worksheet row")] != "2" || rows[1][find("Source worksheet row")] != "2" {
		t.Fatalf("row provenance = %v", rows[1])
	}
	if rows[1][find("Email (2)")] != "ada@example.com" || rows[1][find("Name (2)")] != "  Ada   Lovelace  " {
		t.Fatalf("frozen final values = %v", rows[1])
	}
	// The original worksheet stays exactly as uploaded.
	original, err := book.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if original[0][0] != "Email" || original[1][1] != "Ada" {
		t.Fatalf("original worksheet changed: %v", original)
	}
}
