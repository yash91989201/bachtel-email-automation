package web

// Terminal Close task surface tests: explicit confirmation, the closing/closed
// state and refusal of later processing actions.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

func TestCloseTaskAfterConfirmationIsTerminal(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Closeable")
	upload := uploadWorkbook(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email"},
		{"close-me@example.test"},
	}))
	taskID := taskIDFromRedirect(t, upload, campaignID)

	// Without the explicit confirmation nothing changes.
	unconfirmed := do(t, handler, http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/close", url.Values{
		"action_id": {"close-task-unconfirmed"},
	}, withCookie(cookie))
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed close status = %d", unconfirmed.Code)
	}
	record, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.GetString("closed_at") != "" {
		t.Fatal("unconfirmed request closed the task")
	}

	closed := do(t, handler, http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/close", url.Values{
		"action_id":     {"close-task-confirmed"},
		"confirm_close": {"yes"},
	}, withCookie(cookie))
	if closed.Code != http.StatusSeeOther {
		t.Fatalf("close status = %d body=%.400s", closed.Code, closed.Body.String())
	}
	if closed.Header().Get("Location") != "/campaigns/"+campaignID+"/tasks/"+taskID+"#overview" {
		t.Fatalf("close location = %q", closed.Header().Get("Location"))
	}
	record, err = app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.GetString("closed_at") == "" || record.GetString("closure_actor") == "" {
		t.Fatalf("closure not durable: %+v", record)
	}

	page := fetchTaskPage(t, handler, cookie, "/campaigns/"+campaignID+"/tasks/"+taskID)
	if !strings.Contains(page, "Closed") || strings.Contains(page, "Close task") {
		t.Fatalf("closed task page wrong: %.600s", page)
	}
	// The results route stays mounted and is never forbidden by closure; this
	// staging task has no artifact yet, so a missing file is the normal answer.
	results := do(t, handler, http.MethodGet, "/campaigns/"+campaignID+"/tasks/"+taskID+"/results.xlsx", nil, withCookie(cookie))
	if results.Code == http.StatusForbidden || results.Code == http.StatusUnauthorized {
		t.Fatalf("closed task lost download access: %d", results.Code)
	}

	// A second valid request stays idempotent.
	repeated := do(t, handler, http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/close", url.Values{
		"action_id":     {"close-task-repeated"},
		"confirm_close": {"yes"},
	}, withCookie(cookie))
	if repeated.Code != http.StatusSeeOther {
		t.Fatalf("repeated close status = %d body=%.400s", repeated.Code, repeated.Body.String())
	}

	// A foreign operator cannot close or see the task.
	provisionOperatorAs(t, app, "other@example.test", "other-horse-battery")
	otherCookie := loginAs(t, handler, "other@example.test", "other-horse-battery")
	foreign := do(t, handler, http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/close", url.Values{
		"action_id":     {"close-task-foreign"},
		"confirm_close": {"yes"},
	}, withCookie(otherCookie))
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign close status = %d", foreign.Code)
	}
}

func TestAccountShowsNamedBlockingTasks(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Blocking campaign")
	upload := uploadWorkbook(t, handler, cookie, campaignID, taskWorkbook(t, "Sheet1", [][]string{
		{"Email"},
		{"blocked@example.test"},
	}))
	taskID := taskIDFromRedirect(t, upload, campaignID)

	page := do(t, handler, http.MethodGet, "/account/mailbox", nil, withCookie(cookie))
	body := page.Body.String()
	if !strings.Contains(body, "Connection changes are locked") || !strings.Contains(body, "book.xlsx") {
		t.Fatalf("account page lacks named lock warning: %.600s", body)
	}
	if !strings.Contains(body, "/campaigns/"+campaignID+"/tasks/"+taskID) {
		t.Fatalf("lock warning lacks task link: %.600s", body)
	}
	assertCurrentAccountTab(t, body, "/account/mailbox", "/account/ai")

	// A locked account refuses to switch or disconnect the mailbox and keeps
	// Mailbox selected. Editing the registration is same-mailbox repair, which
	// the lock deliberately allows.
	lockedDisconnect := do(t, handler, http.MethodPost, "/connection/disconnect", url.Values{}, withCookie(cookie))
	if lockedDisconnect.Code != http.StatusConflict || !strings.Contains(lockedDisconnect.Body.String(), "Connection changes are locked") {
		t.Fatalf("locked disconnect: status=%d body=%.400s", lockedDisconnect.Code, lockedDisconnect.Body.String())
	}
	assertCurrentAccountTab(t, lockedDisconnect.Body.String(), "/account/mailbox", "/account/ai")
	_ = app
}
