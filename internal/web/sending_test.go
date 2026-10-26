package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Every sending surface shares the operator and Origin boundaries, including
// the polling fragment and diagnostic sends when production is disabled.
func TestSendingRoutesRequireOperator(t *testing.T) {
	_, handler := newTaskServer(t)
	base := "/campaigns/test/tasks/test/sending"
	for _, path := range []string{base, base + "/progress"} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d", path, rr.Code)
		}
	}
	for _, action := range []string{"test", "approve", "start", "resume", "stop", "retry", "retry_all", "resend", "resolve", "rate", "artifacts"} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, base+"/"+action, strings.NewReader("")))
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", action, rr.Code)
		}
		req := httptest.NewRequest(http.MethodPost, base+"/"+action, strings.NewReader(""))
		req.Header.Set("Origin", "https://foreign.example")
		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("foreign %s: %d", action, rr.Code)
		}
	}
}

func TestDiagnosticRejectsDisconnectedBeforeQueue(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/sending/test", url.Values{"action_id": {"test-disconnected-123456"}, "test_address": {"operator@example.com"}}, withCookie(cookie))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reconnect the mailbox before sending a test") {
		t.Fatalf("test: %d %.300s", rec.Code, rec.Body.String())
	}
	attempts, err := app.FindAllRecords(sending.AttemptsCollection)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("disconnected test created %d attempts", len(attempts))
	}
}

func TestSendingPollRefreshesConfirmationIdentityAndRecovery(t *testing.T) {
	data := views.TaskPageData{
		CampaignID: "campaign-1", TaskID: "task-1", StoredStatus: "paused", Approved: true,
		Remaining: 2, Rate: 17, Hash: "hash", LatestTest: "new-successful-test", ActionID: "actions-",
		Controls: views.TaskControls{Resume: true, RetryRow: true, ResolveRow: true},
		Rows: []views.TaskAdmittedRow{{
			ID: "member", Row: 2, State: "auth_failure", Reason: "Reconnect Microsoft",
			AttemptID: "old-attempt-outside-recents", AttemptRecipient: "candidate@example.com",
			AttemptState: "auth_failure", AttemptTime: "18 Oct 2026, 12:00 PM +05:30",
		}},
	}
	render := func(data views.TaskPageData) string {
		var out bytes.Buffer
		if err := views.TaskSendingLive(data).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	before := render(data)
	for _, want := range []string{"old-attempt-outside-recents", "candidate@example.com", `id="sending-current-rate">17`, "Reconnect Microsoft", "sending-retry-member"} {
		if !strings.Contains(before, want) {
			t.Errorf("poll missing %q", want)
		}
	}
	ids := regexp.MustCompile(`id="(sending-action-resume-[^"]+)"`)
	oldID := ids.FindStringSubmatch(before)
	data.Remaining = 1
	freshID := ids.FindStringSubmatch(render(data))
	if len(oldID) != 2 || len(freshID) != 2 || oldID[1] == freshID[1] {
		t.Fatal("fresh count retained stale confirmation form identity")
	}
	// A same-count mailbox or approval change still invalidates the confirmation.
	data.Remaining = 2
	data.SenderStatus = "disconnected"
	if ids.FindStringSubmatch(render(data))[1] == oldID[1] {
		t.Fatal("same-count mailbox change retained a stale confirmation")
	}
}

type recordingSendingDispatcher struct{ synchronizationCalls int }

func (*recordingSendingDispatcher) Wake() {}
func (d *recordingSendingDispatcher) Synchronize(action func() error) error {
	d.synchronizationCalls++
	return action()
}
func (*recordingSendingDispatcher) InFlightFor(context.Context, string) (int, error) { return 0, nil }

func TestSendingPacingAndTaskOwnership(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	connection := installMailbox(t, app, operatorID(t, app), "sender@example.test")
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.Intake = intake.DefaultLimits()
	dispatcher := &recordingSendingDispatcher{}
	handler = wrapWithPocketBase(t, app, NewHandler(app, cfg, testServices(t, app, cfg, dispatcher)))
	base := "/campaigns/" + id + "/tasks/" + taskID + "/sending"
	// A stale rate-only form is refused and cannot reset concurrency.
	stale := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-stale-123456"}, "rate": {"20"}}, withCookie(cookie))
	if stale.Code != http.StatusBadRequest || !strings.Contains(stale.Body.String(), "Enter both Concurrent emails and Pacing") {
		t.Fatalf("stale rate-only form: %d %.300s", stale.Code, stale.Body.String())
	}
	if dispatcher.synchronizationCalls != 0 {
		t.Fatalf("stale form acquired dispatch barrier %d times", dispatcher.synchronizationCalls)
	}
	for _, invalid := range []struct{ rate, concurrency string }{{"0", "2"}, {"31", "2"}, {"1.5", "2"}, {"15", "0"}, {"15", "11"}, {"15", "2.5"}, {"", "2"}, {"15", ""}} {
		rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-invalid-123456"}, "rate": {invalid.rate}, "concurrency": {invalid.concurrency}}, withCookie(cookie))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("settings %v: %d", invalid, rec.Code)
		}
	}
	if dispatcher.synchronizationCalls != 0 {
		t.Fatalf("invalid settings acquired dispatch barrier %d times", dispatcher.synchronizationCalls)
	}
	// A refused save reopens Sending settings and keeps both readable drafts
	// while the summary still shows the saved pair.
	refused := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-draft-123456"}, "rate": {"18"}, "concurrency": {"12"}}, withCookie(cookie))
	body := refused.Body.String()
	for _, want := range []string{`value="18"`, `value="12"`, `id="sending-settings"`, `aria-expanded="true"`, `id="sending-current-rate">15<`, `id="sending-current-concurrency">2<`} {
		if refused.Code != http.StatusBadRequest || !strings.Contains(body, want) {
			t.Fatalf("refused save missing %q: %d", want, refused.Code)
		}
	}
	// Each valid pair must serialize with launches, including a sharp reduction.
	for index, pair := range []struct{ rate, concurrency string }{{"30", "10"}, {"1", "1"}} {
		rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-barrier-123456"}, "rate": {pair.rate}, "concurrency": {pair.concurrency}}, withCookie(cookie))
		if rec.Code != http.StatusSeeOther || dispatcher.synchronizationCalls != index+1 {
			t.Fatalf("settings %v: status=%d barrier calls=%d", pair, rec.Code, dispatcher.synchronizationCalls)
		}
	}
	rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-valid-123456"}, "rate": {"17"}, "concurrency": {"3"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid settings: %d", rec.Code)
	}
	if dispatcher.synchronizationCalls != 3 {
		t.Fatalf("valid settings skipped dispatch barrier: calls=%d", dispatcher.synchronizationCalls)
	}
	settings, err := sending.NewStore(app, sending.StoreOptions{}).DispatcherState(context.Background(), connection.ID, operatorID(t, app))
	if err != nil || settings.Rate != 17 || settings.Concurrency != 3 {
		t.Fatalf("persisted settings: %+v %v", settings, err)
	}
	page := do(t, handler, http.MethodGet, base+"/progress", nil, withCookie(cookie))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `id="sending-current-rate">17`) || !strings.Contains(page.Body.String(), `id="sending-current-concurrency">3`) {
		t.Fatalf("settings poll: %d", page.Code)
	}
	// The task overview shows the operator's own mailbox and its state.
	full := do(t, handler, http.MethodGet, "/campaigns/"+id+"/tasks/"+taskID+"/overview", nil, withCookie(cookie))
	if full.Code != http.StatusOK || !strings.Contains(full.Body.String(), "<dt>Sender</dt><dd><strong data-sender-status=\"connected\">sender@example.test</strong> (connected)") {
		t.Fatalf("sending mailbox summary: %d %.400s", full.Code, full.Body.String())
	}
	wrongCampaign := createCampaign(t, handler, cookie, "Another")
	page = do(t, handler, http.MethodGet, "/campaigns/"+wrongCampaign+"/tasks/"+taskID+"/sending/progress", nil, withCookie(cookie))
	if page.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign task access: %d", page.Code)
	}
	// Another operator cannot use this task's sending surfaces and changes nothing.
	otherID := provisionOperatorAs(t, app, "second@example.com", "second-password")
	otherCookie := loginAs(t, handler, "second@example.com", "second-password")
	installMailbox(t, app, otherID, "second@example.test")
	other := do(t, handler, http.MethodGet, base+"/progress", nil, withCookie(otherCookie))
	if other.Code != http.StatusNotFound {
		t.Fatalf("cross-operator task access: %d", other.Code)
	}
	mutation := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-cross-operator-1"}, "rate": {"5"}, "concurrency": {"1"}}, withCookie(otherCookie))
	if mutation.Code != http.StatusNotFound {
		t.Fatalf("cross-operator settings mutation: %d", mutation.Code)
	}
	settings, err = sending.NewStore(app, sending.StoreOptions{}).DispatcherState(context.Background(), connection.ID, operatorID(t, app))
	if err != nil || settings.Rate != 17 || settings.Concurrency != 3 {
		t.Fatalf("cross-operator settings changed state: %+v %v", settings, err)
	}

	// Another task sharing the mailbox shows the same saved pair.
	sibling := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	siblingBase := "/campaigns/" + id + "/tasks/" + sibling + "/sending"
	siblingPage := do(t, handler, http.MethodGet, siblingBase, nil, withCookie(cookie))
	siblingBody := siblingPage.Body.String()
	for _, want := range []string{`id="sending-current-rate">17`, `id="sending-current-concurrency">3`, `name="concurrency"`, `name="rate"`} {
		if siblingPage.Code != http.StatusOK || !strings.Contains(siblingBody, want) {
			t.Fatalf("sibling task settings missing %q: %d", want, siblingPage.Code)
		}
	}
}

// A bulk recovery post carries one member_id per selected row; the handler
// must pass the whole selection to the atomic action, not just the first.
func TestBulkRecoveryPostsEverySelectedMember(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignID, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name", "Location"},
		{"alice@example.com", "Ada Lovelace", "Remote"},
		{"carol@example.com", "Carol Danvers", "Lisbon"},
	}))
	finalizeTask(t, app, handler, cookie, campaignID, taskID)
	owner := operatorID(t, app)
	installMailbox(t, app, owner, "sender@example.test")
	store := sending.NewStore(app, sending.StoreOptions{})
	ctx := context.Background()
	attempt, err := store.QueueTest(ctx, owner, sending.TestRequest{
		ActionID: "bulk-recovery-test-123456", TaskID: taskID, Actor: "operator",
		Recipient: "sample@example.invalid",
		MemberID:  firstEligibleMember(t, app, taskID),
	})
	if err != nil {
		t.Fatalf("queue test: %v", err)
	}
	if _, err = app.DB().NewQuery("UPDATE submission_attempts SET state='submitted' WHERE id={:id}").Bind(dbx.Params{"id": attempt.ID}).Execute(); err != nil {
		t.Fatalf("mark test submitted: %v", err)
	}
	if err = store.Approve(ctx, owner, taskID, attempt.ID, "operator", "approve-"+taskID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	base := "/campaigns/" + campaignID + "/tasks/" + taskID + "/sending"
	if rec := do(t, handler, http.MethodPost, base+"/start", url.Values{"action_id": {"bulk-recovery-start-123456"}, "expected_count": {"2"}, "confirmed": {"yes"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("start: %d %.400s", rec.Code, rec.Body.String())
	}
	if rec := do(t, handler, http.MethodPost, base+"/stop", url.Values{"action_id": {"bulk-recovery-stop-123456"}, "expected_count": {"0"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("stop: %d %.400s", rec.Code, rec.Body.String())
	}
	if err = store.SettleTasks(ctx); err != nil {
		t.Fatalf("settle: %v", err)
	}
	members, err := app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t}", "source_row", 0, 0, dbx.Params{"t": taskID})
	if err != nil || len(members) != 2 {
		t.Fatalf("members = %d, %v", len(members), err)
	}
	for _, member := range members {
		member.Set("dispatch_state", sending.AttemptFailed)
		if err = app.Save(member); err != nil {
			t.Fatal(err)
		}
	}
	bulk := url.Values{
		"action_id":      {"bulk-recovery-retry-123456"},
		"expected_count": {"2"},
		"confirmed":      {"yes"},
		"member_id":      {members[0].Id, members[1].Id},
	}
	if rec := do(t, handler, http.MethodPost, base+"/retry", bulk, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk retry: %d %.400s", rec.Code, rec.Body.String())
	}
	progress, err := store.Preview(ctx, owner, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Reserved != 2 || progress.RetryableFailed != 0 {
		t.Fatalf("bulk retry claimed %d reserved, %d retryable; want 2 and 0", progress.Reserved, progress.RetryableFailed)
	}
}

// A provider rejection pauses the task; the paused task must still render the
// per-row Retry dialog, not an empty action column.
func TestRecoveryActionsRenderAfterProviderPause(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignID, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name", "Location"},
		{"alice@example.com", "Ada Lovelace", "Remote"},
		{"carol@example.com", "Carol Danvers", "Lisbon"},
	}))
	finalizeTask(t, app, handler, cookie, campaignID, taskID)
	owner := operatorID(t, app)
	installMailbox(t, app, owner, "sender@example.test")
	store := sending.NewStore(app, sending.StoreOptions{})
	ctx := context.Background()
	attempt, err := store.QueueTest(ctx, owner, sending.TestRequest{
		ActionID: "pause-recovery-test-123456", TaskID: taskID, Actor: "operator",
		Recipient: "sample@example.invalid",
		MemberID:  firstEligibleMember(t, app, taskID),
	})
	if err != nil {
		t.Fatalf("queue test: %v", err)
	}
	if _, err = app.DB().NewQuery("UPDATE submission_attempts SET state='submitted' WHERE id={:id}").Bind(dbx.Params{"id": attempt.ID}).Execute(); err != nil {
		t.Fatalf("mark test submitted: %v", err)
	}
	if err = store.Approve(ctx, owner, taskID, attempt.ID, "operator", "approve-"+taskID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	base := "/campaigns/" + campaignID + "/tasks/" + taskID + "/sending"
	if rec := do(t, handler, http.MethodPost, base+"/start", url.Values{"action_id": {"pause-recovery-start-123456"}, "expected_count": {"2"}, "confirmed": {"yes"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("start: %d %.300s", rec.Code, rec.Body.String())
	}
	work, err := store.NextWork(ctx)
	if err != nil || work == nil {
		t.Fatalf("next work: %v", err)
	}
	first, err := store.BeginAttempt(ctx, work, time.Now(), time.Now().Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteAttempt(ctx, first.ID, sending.Outcome{State: sending.AttemptFailed, Reason: "Titan rejected the message; operator action is required.", PauseMailbox: true}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.GetString("status") != "paused" {
		t.Fatalf("task status = %q, want paused", task.GetString("status"))
	}
	page := do(t, handler, http.MethodGet, base, nil, withCookie(cookie))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-slot="dialog-trigger"`) {
		t.Fatalf("paused recovery row has no action: %d %.400s", page.Code, page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "Retry worksheet row") {
		t.Fatalf("paused recovery row is missing its retry dialog: %.400s", page.Body.String())
	}
}
