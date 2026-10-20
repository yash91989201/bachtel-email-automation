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

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/microsoft"
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
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Diagnostic test was not queued") {
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
	data := views.SendingData{Base: "/sending", Hash: "hash", Status: "paused", Approved: true, Remaining: 2, Rate: 17, LatestTest: "new-successful-test", Rows: []views.SendingRow{{ID: "member", Number: 2, State: "auth_failure", Reason: "Reconnect Microsoft", Attempt: views.SendingOutcome{ID: "old-attempt-outside-recents", Recipient: "candidate@example.com", State: "auth_failure", Time: "2026-10-18T12:00:00+05:30"}}}}
	render := func(data views.SendingData) string {
		var out bytes.Buffer
		if err := views.SendingProgress(data).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	before := render(data)
	for _, want := range []string{"new-successful-test", "sending-retry-member", "old-attempt-outside-recents", "candidate@example.com", "Current pacing: 17", "Reconnect Microsoft"} {
		if !strings.Contains(before, want) {
			t.Errorf("poll missing %q", want)
		}
	}
	ids := regexp.MustCompile(`id="(sending-action-start-[^"]+)"`)
	oldID := ids.FindStringSubmatch(before)
	data.Remaining = 1
	freshID := ids.FindStringSubmatch(render(data))
	if len(oldID) != 2 || len(freshID) != 2 || oldID[1] == freshID[1] {
		t.Fatal("fresh count retained stale confirmation form identity")
	}
}

type recordingSendingDispatcher struct{ synchronizationCalls int }

func (*recordingSendingDispatcher) Wake() {}
func (d *recordingSendingDispatcher) Synchronize(action func() error) error {
	d.synchronizationCalls++
	return action()
}

func TestSendingPacingAndTaskOwnership(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.Intake = intake.DefaultLimits()
	dispatcher := &recordingSendingDispatcher{}
	handler = wrapWithPocketBase(t, app, NewHandlerWithDispatcher(app, cfg, microsoft.NewService(app, cfg), sending.NewStore(app, sending.StoreOptions{}), dispatcher))
	base := "/campaigns/" + id + "/tasks/" + taskID + "/sending"
	for _, invalid := range []string{"0", "31", "1.5"} {
		rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-invalid-123456"}, "rate": {invalid}}, withCookie(cookie))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("rate %s: %d", invalid, rec.Code)
		}
	}
	if dispatcher.synchronizationCalls != 0 {
		t.Fatalf("invalid rates acquired dispatch barrier %d times", dispatcher.synchronizationCalls)
	}
	// Each valid update must serialize with launches, including a sharp reduction.
	for index, rate := range []string{"30", "1"} {
		rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-barrier-123456"}, "rate": {rate}}, withCookie(cookie))
		if rec.Code != http.StatusSeeOther || dispatcher.synchronizationCalls != index+1 {
			t.Fatalf("rate %s: status=%d barrier calls=%d", rate, rec.Code, dispatcher.synchronizationCalls)
		}
	}
	rec := do(t, handler, http.MethodPost, base+"/rate", url.Values{"action_id": {"pacing-valid-123456"}, "rate": {"17"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid rate: %d", rec.Code)
	}
	if dispatcher.synchronizationCalls != 3 {
		t.Fatalf("valid rate skipped dispatch barrier: calls=%d", dispatcher.synchronizationCalls)
	}
	settings, err := sending.NewStore(app, sending.StoreOptions{}).DispatcherState(context.Background())
	if err != nil || settings.Rate != 17 {
		t.Fatalf("persisted pacing: %+v %v", settings, err)
	}
	page := do(t, handler, http.MethodGet, base+"/progress", nil, withCookie(cookie))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Current pacing: 17") {
		t.Fatalf("pacing poll: %d", page.Code)
	}
	wrongCampaign := createCampaign(t, handler, cookie, "Another")
	page = do(t, handler, http.MethodGet, "/campaigns/"+wrongCampaign+"/tasks/"+taskID+"/sending/progress", nil, withCookie(cookie))
	if page.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign task access: %d", page.Code)
	}
}
