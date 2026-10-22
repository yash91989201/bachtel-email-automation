package web

// Schedule submission tests: the native date/time start form posts behind the dispatch
// barrier, renders its stored state on the sending and task pages, refuses a
// too-soon instant with the exact reason, and clears with a fresh action ID.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// scheduleFixture stages one mixed task, approves a submitted formatting test
// against it, and serves the sending surface from an enabled store behind a
// recording dispatcher.
type scheduleFixture struct {
	app        core.App
	handler    http.Handler
	cookie     *http.Cookie
	campaignID string
	taskID     string
	base       string
	dispatcher *recordingSendingDispatcher
}

func newScheduleFixture(t *testing.T) *scheduleFixture {
	t.Helper()
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	owner := operatorID(t, app)
	installMailbox(t, app, owner, "sender@example.test")
	store := sending.NewStore(app, sending.StoreOptions{})
	ctx := context.Background()
	attempt, err := store.QueueTest(ctx, owner, sending.TestRequest{
		ActionID:  "schedule-fixture-test-123456",
		TaskID:    taskID,
		Actor:     "operator",
		Recipient: "sample@example.invalid",
		Values:    map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"},
	})
	if err != nil {
		t.Fatalf("queue test: %v", err)
	}
	if _, err := app.DB().NewQuery("UPDATE submission_attempts SET state='submitted' WHERE id={:id}").Bind(dbx.Params{"id": attempt.ID}).Execute(); err != nil {
		t.Fatalf("mark test submitted: %v", err)
	}
	if err := store.Approve(ctx, owner, taskID, attempt.ID, "operator", "approve-"+taskID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.Intake = intake.DefaultLimits()
	dispatcher := &recordingSendingDispatcher{}
	handler = wrapWithPocketBase(t, app, NewHandler(app, cfg, testServices(t, app, cfg, dispatcher)))
	return &scheduleFixture{
		app:        app,
		handler:    handler,
		cookie:     cookie,
		campaignID: id,
		taskID:     taskID,
		base:       "/campaigns/" + id + "/tasks/" + taskID + "/sending",
		dispatcher: dispatcher,
	}
}

// scheduleForm maps one absolute instant to the native date/time form fields. The mixed
// workbook leaves exactly one eligible row, so the expected count is 1.
func scheduleForm(actionID string, at time.Time) url.Values {
	local := at.In(sending.ScheduleZone)
	return url.Values{
		"action_id":       {actionID},
		"schedule_action": {"enable"},
		"schedule_date":   {local.Format("2006-01-02")},
		"schedule_time":   {local.Format("15:04")},
		"confirmed":       {"yes"},
		"expected_count":  {"1"},
	}
}

// storedScheduleAt returns the task's stored schedule instant.
func (f *scheduleFixture) storedScheduleAt(t *testing.T) time.Time {
	t.Helper()
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.GetDateTime("schedule_at").Time().UTC()
}

// storedScheduleState returns the task's recorded schedule state.
func (f *scheduleFixture) storedScheduleState(t *testing.T) string {
	t.Helper()
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.GetString("schedule_state")
}

func TestScheduleEnableTooSoonTaskPageAndClear(t *testing.T) {
	f := newScheduleFixture(t)

	// A valid schedule two hours ahead crosses the dispatch barrier once.
	at := time.Now().In(sending.ScheduleZone).Add(2 * time.Hour)
	rec := do(t, f.handler, http.MethodPost, f.base+"/schedule", scheduleForm("schedule-enable-123456", at), withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("enable: %d %.600s", rec.Code, rec.Body.String())
	}
	if f.dispatcher.synchronizationCalls != 1 {
		t.Fatalf("schedule crossed %d dispatch barriers, want 1", f.dispatcher.synchronizationCalls)
	}
	stored := f.storedScheduleAt(t)
	page := do(t, f.handler, http.MethodGet, f.base, nil, withCookie(f.cookie))
	for _, want := range []string{presentation.Timestamp(stored), "scheduled"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("sending page missing %q after schedule: %.600s", want, page.Body.String())
		}
	}

	// Repeating the same action ID must not create a second schedule action.
	rec = do(t, f.handler, http.MethodPost, f.base+"/schedule", scheduleForm("schedule-enable-123456", at), withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("repeated enable: %d %.400s", rec.Code, rec.Body.String())
	}
	actions, err := f.app.FindRecordsByFilter(sending.ActionsCollection, "task={:t} && kind='schedule'", "", 0, 0, dbx.Params{"t": f.taskID})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("schedule actions = %d, want 1", len(actions))
	}

	// Ten minutes ahead sits inside the 30-minute lead and reports the reason.
	tooSoon := time.Now().In(sending.ScheduleZone).Add(10 * time.Minute)
	rec = do(t, f.handler, http.MethodPost, f.base+"/schedule", scheduleForm("schedule-soon-123456", tooSoon), withCookie(f.cookie))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "30 minutes") {
		t.Fatalf("too soon: %d %.400s", rec.Code, rec.Body.String())
	}

	// The task page shows the stored instant and state.
	taskPage := do(t, f.handler, http.MethodGet, "/campaigns/"+f.campaignID+"/tasks/"+f.taskID, nil, withCookie(f.cookie))
	for _, want := range []string{presentation.Timestamp(stored), "scheduled"} {
		if !strings.Contains(taskPage.Body.String(), want) {
			t.Fatalf("task page missing %q after schedule: %.600s", want, taskPage.Body.String())
		}
	}

	// Clearing needs a fresh action ID and resets the stored state.
	rec = do(t, f.handler, http.MethodPost, f.base+"/schedule", url.Values{
		"action_id":       {"schedule-clear-123456"},
		"schedule_action": {"clear"},
	}, withCookie(f.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear: %d %.400s", rec.Code, rec.Body.String())
	}
	cleared, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.GetString("schedule_state") != intake.ScheduleNone {
		t.Fatalf("schedule state after clear = %q, want %q", cleared.GetString("schedule_state"), intake.ScheduleNone)
	}
}

// A refused schedule keeps the operator's entered date and time, names the
// problem, and records no schedule.
func TestScheduleRefusalRetainsEnteredTime(t *testing.T) {
	f := newScheduleFixture(t)
	selectMode := do(t, f.handler, http.MethodPost, "/campaigns/"+f.campaignID+"/tasks/"+f.taskID+"/sending-type", url.Values{"sending_type": {"scheduled"}}, withCookie(f.cookie))
	if selectMode.Code != http.StatusSeeOther {
		t.Fatalf("select scheduled mode: %d %.300s", selectMode.Code, selectMode.Body.String())
	}
	fields := url.Values{
		"action_id":       {"schedule-retain-123456"},
		"schedule_action": {"enable"},
		"expected_count":  {"2"},
		"confirmed":       {"yes"},
		"schedule_date":   {"2020-01-02"},
		"schedule_time":   {"21:05"},
	}
	rec := do(t, f.handler, http.MethodPost, f.base+"/schedule", fields, withCookie(f.cookie))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("too-soon schedule: %d %.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"choose a start time at least 30 minutes from now",
		`value="2020-01-02"`,
		`value="21:05"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("refused schedule missing %q: %.600s", want, body)
		}
	}
	if state := f.storedScheduleState(t); state != intake.ScheduleNone {
		t.Fatalf("refused schedule stored state %q, want none", state)
	}
}
