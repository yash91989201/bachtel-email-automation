package intake_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Task names are trimmed, default to the stored workbook filename, accept
// duplicates and ordinary Unicode, and are refused above 255 code points
// without leaving a task behind.
func TestStageResolvesAndPersistsTaskNames(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"person@example.com", "Person Name"},
	}})

	stage := func(filename, name, sendingType string) (*intake.Task, error) {
		t.Helper()
		return env.service.StageWithSendingType(ctx, env.owner, env.campaign, filename, data, sendingType, name)
	}

	named, err := stage("recipients.xlsx", "  October outreach  ", intake.TaskSendingTypeDirect)
	if err != nil {
		t.Fatalf("stage named task: %v", err)
	}
	if named.Name != "October outreach" {
		t.Errorf("name = %q, want trimmed explicit name", named.Name)
	}
	if named.SourceName != "recipients.xlsx" || named.SendingType != intake.TaskSendingTypeDirect {
		t.Errorf("source name or preference changed: %#v", named)
	}
	loaded, err := env.service.LoadTask(ctx, env.owner, named.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "October outreach" || loaded.SourceName != "recipients.xlsx" {
		t.Errorf("loaded task = %#v, want the saved name and source filename", loaded)
	}
	listed, err := env.service.ListTasks(ctx, env.owner, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, task := range listed {
		if task.ID == named.ID {
			found = true
			if task.Name != "October outreach" {
				t.Errorf("listed name = %q", task.Name)
			}
		}
	}
	if !found {
		t.Fatal("the new task is missing from the campaign task list")
	}

	for _, blank := range []string{"", "   "} {
		task, err := stage("book.xlsx", blank, "")
		if err != nil {
			t.Fatalf("stage blank name %q: %v", blank, err)
		}
		if task.Name != "book.xlsx" {
			t.Errorf("blank name %q resolved to %q, want the workbook filename", blank, task.Name)
		}
	}

	// A browser path must not leak into the name or the stored source name.
	pathed, err := stage(`C:\Users\ada\recipients.xlsx`, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if pathed.Name != "recipients.xlsx" || pathed.SourceName != "recipients.xlsx" {
		t.Errorf("path upload name = %q, source = %q", pathed.Name, pathed.SourceName)
	}

	// Duplicate names stay distinct tasks.
	first, err := stage("october.xlsx", "October outreach", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := stage("october.xlsx", "October outreach", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.Name != second.Name {
		t.Fatalf("duplicate names must create two tasks: %#v %#v", first, second)
	}

	// Exactly 255 Unicode code points are accepted; 256 are refused.
	limit := strings.Repeat("é", intake.MaxTaskNameLength)
	if task, err := stage("unicode.xlsx", limit, ""); err != nil || task.Name != limit {
		t.Fatalf("255-code-point name: task=%#v err=%v", task, err)
	}
	before, err := env.service.ListTasks(ctx, env.owner, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stage("unicode.xlsx", limit+"é", "")
	var failure *intake.ValidationError
	if !errors.As(err, &failure) || !strings.Contains(failure.Message, "255") {
		t.Fatalf("256-code-point name error = %v, want a name-length validation error", err)
	}
	after, err := env.service.ListTasks(ctx, env.owner, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("refused name created a task: before=%d after=%d", len(before), len(after))
	}
}
