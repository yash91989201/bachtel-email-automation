package intake_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Renaming is a metadata-only operation available in every lifecycle state. It
// must keep the source filename, schedule, workflow evidence and closure record
// intact while applying the creation rules to the new name.
func TestRenameTaskAcrossLifecycleStates(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"person@example.com", "Person Name"},
	}})

	states := []struct {
		name     string
		status   string
		schedule string
		closing  bool
		closed   bool
	}{
		{name: "staging", status: intake.TaskStaging, schedule: intake.ScheduleNone},
		{name: "active", status: "running", schedule: intake.ScheduleNone},
		{name: "scheduled", status: intake.TaskReady, schedule: intake.ScheduleScheduled},
		{name: "completed", status: "completed", schedule: intake.ScheduleFired},
		{name: "closing", status: "running", schedule: intake.ScheduleFired, closing: true},
		{name: "closed", status: "completed", schedule: intake.ScheduleCancelled, closed: true},
	}
	scheduledAt := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			task, err := env.service.Stage(ctx, env.owner, env.campaign, "recipients.xlsx", data, "")
			if err != nil {
				t.Fatal(err)
			}
			record, err := env.app.FindRecordById(intake.TasksCollection, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			record.Set("status", state.status)
			record.Set("schedule_state", state.schedule)
			record.Set("content_hash", "pinned-hash")
			record.Set("validation_error", "kept validation error")
			if state.schedule == intake.ScheduleScheduled || state.schedule == intake.ScheduleFired {
				record.Set("schedule_at", scheduledAt)
			}
			if state.closing || state.closed {
				record.Set("closure_requested_at", scheduledAt)
				record.Set("closure_actor", env.owner)
			}
			if state.closed {
				record.Set("closed_at", scheduledAt.Add(time.Hour))
			}
			if err := env.app.Save(record); err != nil {
				t.Fatal(err)
			}

			if err := env.service.RenameTask(ctx, env.owner, task.ID, "  November follow-up  "); err != nil {
				t.Fatalf("rename in %s state: %v", state.name, err)
			}
			loaded, err := env.service.LoadTask(ctx, env.owner, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Name != "November follow-up" {
				t.Errorf("name = %q, want the trimmed new name", loaded.Name)
			}
			if loaded.SourceName != "recipients.xlsx" {
				t.Errorf("source filename = %q, want it unchanged", loaded.SourceName)
			}
			if loaded.Status != state.status || loaded.ScheduleState != state.schedule {
				t.Errorf("state changed: status=%q schedule=%q", loaded.Status, loaded.ScheduleState)
			}
			if loaded.ValidationError != "kept validation error" || loaded.ContentHash != "pinned-hash" {
				t.Error("workflow evidence changed with the name")
			}
			if state.schedule == intake.ScheduleScheduled && !loaded.ScheduleAt.Equal(scheduledAt) {
				t.Errorf("schedule = %v, want %v", loaded.ScheduleAt, scheduledAt)
			}
			if state.closing && !loaded.Closing() {
				t.Error("a pending closure was cleared")
			}
			if state.closed && !loaded.Closed() {
				t.Error("a terminal closure was cleared")
			}
		})
	}
}

// Custom names are shared across tasks, a blank name restores the workbook
// filename, and an over-long name is refused without touching the old name.
func TestRenameTaskAppliesNameRules(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"person@example.com", "Person Name"},
	}})
	stage := func() *intake.Task {
		t.Helper()
		task, err := env.service.Stage(ctx, env.owner, env.campaign, "recipients.xlsx", data, "")
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	first, second := stage(), stage()

	if err := env.service.RenameTask(ctx, env.owner, first.ID, "October outreach"); err != nil {
		t.Fatal(err)
	}
	if err := env.service.RenameTask(ctx, env.owner, second.ID, "October outreach"); err != nil {
		t.Fatalf("duplicate name must be allowed: %v", err)
	}
	for _, task := range []*intake.Task{first, second} {
		loaded, err := env.service.LoadTask(ctx, env.owner, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Name != "October outreach" {
			t.Errorf("task %s name = %q", task.ID, loaded.Name)
		}
	}

	if err := env.service.RenameTask(ctx, env.owner, first.ID, "   "); err != nil {
		t.Fatal(err)
	}
	loaded, err := env.service.LoadTask(ctx, env.owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "recipients.xlsx" {
		t.Errorf("blank rename = %q, want the workbook filename", loaded.Name)
	}
	if loaded.SourceName != "recipients.xlsx" {
		t.Errorf("blank rename changed the source filename to %q", loaded.SourceName)
	}

	tooLong := strings.Repeat("é", intake.MaxTaskNameLength+1)
	var failure *intake.ValidationError
	if err := env.service.RenameTask(ctx, env.owner, first.ID, tooLong); !errors.As(err, &failure) || !strings.Contains(failure.Message, "255") {
		t.Fatalf("over-long rename error = %v, want a name-length validation error", err)
	}
	loaded, err = env.service.LoadTask(ctx, env.owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "recipients.xlsx" {
		t.Errorf("refused rename replaced the prior name with %q", loaded.Name)
	}
}

// Ownership is rechecked in the intake transaction: another operator and an
// unknown id are the same not-found refusal and change nothing.
func TestRenameTaskRequiresOwnership(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"person@example.com", "Person Name"},
	}})
	task, err := env.service.Stage(ctx, env.owner, env.campaign, "recipients.xlsx", data, "")
	if err != nil {
		t.Fatal(err)
	}
	foreign := newOperator(t, env.app, "foreign@example.test")
	if err := env.service.RenameTask(ctx, foreign, task.ID, "Foreign name"); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign rename error = %v, want ErrNotFound", err)
	}
	if err := env.service.RenameTask(ctx, env.owner, "missing-task", "Missing"); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("unknown-task rename error = %v, want ErrNotFound", err)
	}
	loaded, err := env.service.LoadTask(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "recipients.xlsx" {
		t.Errorf("unauthorized rename changed the name to %q", loaded.Name)
	}
}

// A storage failure aborts the metadata write and leaves the prior name
// authoritative.
func TestRenameTaskStorageFailureKeepsPriorName(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"person@example.com", "Person Name"},
	}})
	task, err := env.service.Stage(ctx, env.owner, env.campaign, "recipients.xlsx", data, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.service.RenameTask(ctx, env.owner, task.ID, "Kept name"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.app.DB().NewQuery(`
		CREATE TRIGGER fail_task_name_update BEFORE UPDATE OF name ON send_tasks
		BEGIN SELECT RAISE(ABORT, 'storage failure'); END
	`).Execute(); err != nil {
		t.Skipf("this SQLite build cannot install a failure trigger: %v", err)
	}
	if err := env.service.RenameTask(ctx, env.owner, task.ID, "Lost name"); err == nil {
		t.Fatal("rename under a failing store reported success")
	}
	if _, err := env.app.DB().NewQuery("DROP TRIGGER fail_task_name_update").Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err := env.service.LoadTask(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Kept name" {
		t.Errorf("name after failed storage = %q, want the prior name", loaded.Name)
	}
}
