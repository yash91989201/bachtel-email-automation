package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
	"golang.org/x/net/html"
)

func TestTaskUploadPersistsSendingTypeWithoutStarting(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignID, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d", rec.Code)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("sending_type", views.TaskSendingTypeScheduled); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("workbook", "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(mixedWorkbook(t)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/campaigns/"+campaignID+"/tasks", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	wantLocation := "/campaigns/" + campaignID + "/tasks/"
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), wantLocation) || !strings.HasSuffix(rec.Header().Get("Location"), "#recipients") {
		t.Fatalf("upload redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	taskID := strings.TrimSuffix(strings.TrimPrefix(rec.Header().Get("Location"), wantLocation), "#recipients")
	if taskID == "" || strings.Contains(taskID, "/") {
		t.Fatalf("invalid task id in redirect %q", rec.Header().Get("Location"))
	}
	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := task.GetString("sending_type"); got != views.TaskSendingTypeScheduled {
		t.Fatalf("stored sending type = %q, want %q", got, views.TaskSendingTypeScheduled)
	}
	if got := task.GetString("status"); got != intake.TaskStaging {
		t.Fatalf("upload status = %q, want staged", got)
	}
	if got := task.GetString("schedule_state"); got != intake.ScheduleNone {
		t.Fatalf("upload schedule state = %q, want none", got)
	}
}

func TestSendingModeURLStateCannotChangeSavedType(t *testing.T) {
	f := newScheduleFixture(t)
	baseTask := "/campaigns/" + f.campaignID + "/tasks/" + f.taskID
	save := do(t, f.handler, http.MethodPost, baseTask+"/sending-type", url.Values{"sending_type": {views.TaskSendingTypeScheduled}}, withCookie(f.cookie))
	if save.Code != http.StatusSeeOther {
		t.Fatalf("save scheduled mode: status=%d body=%.300s", save.Code, save.Body.String())
	}
	beforeAttempts := sendingModeRecordCount(t, f, sending.AttemptsCollection)
	beforeActions := sendingModeRecordCount(t, f, sending.ActionsCollection)
	for _, mode := range []string{views.TaskSendingTypeDirect, views.TaskSendingTypeScheduled, views.TaskSendingTypeChoose, "invalid"} {
		page := do(t, f.handler, http.MethodGet, baseTask+"?sending_type="+url.QueryEscape(mode), nil, withCookie(f.cookie))
		if page.Code != http.StatusOK {
			t.Fatalf("GET sending_type=%q: %d", mode, page.Code)
		}
		doc := sendingModeDocument(t, page.Body.String())
		if mode == views.TaskSendingTypeChoose {
			sendingModeAssertChoices(t, doc)
		} else {
			sendingModeAssertChoicesAbsent(t, doc)
			if !strings.Contains(page.Body.String(), "Choose sending time") {
				t.Errorf("GET sending_type=%q did not retain saved scheduled mode", mode)
			}
		}
	}
	poll := do(t, f.handler, http.MethodGet, f.base+"/progress?sending_type=choose", nil, withCookie(f.cookie))
	if poll.Code != http.StatusOK {
		t.Fatalf("GET progress chooser: %d", poll.Code)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := task.GetString("sending_type"); got != views.TaskSendingTypeScheduled {
		t.Errorf("GET navigation changed saved sending type to %q", got)
	}
	page := do(t, f.handler, http.MethodGet, baseTask, nil, withCookie(f.cookie))
	pageDoc := sendingModeDocument(t, page.Body.String())
	live := sendingModeNode(pageDoc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && sendingModeAttr(n, "id") == "sending-live"
	})
	if live == nil || sendingModeAttr(live, "hx-get") != f.base+"/progress" {
		t.Errorf("saved-mode progress URL = %q", sendingModeAttr(live, "hx-get"))
	}
	if got := sendingModeRecordCount(t, f, sending.AttemptsCollection); got != beforeAttempts {
		t.Errorf("GET mode navigation created attempts: got %d, before %d", got, beforeAttempts)
	}
	if got := sendingModeRecordCount(t, f, sending.ActionsCollection); got != beforeActions {
		t.Errorf("GET mode navigation created actions: got %d, before %d", got, beforeActions)
	}
	// A later change must replace the shared task value, not just this URL.
	changed := do(t, f.handler, http.MethodPost, baseTask+"/sending-type", url.Values{"sending_type": {views.TaskSendingTypeDirect}}, withCookie(f.cookie))
	if changed.Code != http.StatusSeeOther || changed.Header().Get("Location") != baseTask+"#sending" {
		t.Fatalf("change to direct: status=%d location=%q", changed.Code, changed.Header().Get("Location"))
	}
	if got := taskSendingTypeStored(t, f); got != views.TaskSendingTypeDirect {
		t.Fatalf("changed saved type = %q", got)
	}
	reloaded := do(t, f.handler, http.MethodGet, baseTask, nil, withCookie(f.cookie))
	sendingModeAssertAction(t, sendingModeDocument(t, reloaded.Body.String()), f.base+"/start#sending", true)
	if got := sendingModeRecordCount(t, f, sending.ActionsCollection); got != beforeActions {
		t.Errorf("saving a type created sending actions: got %d, before %d", got, beforeActions)
	}
	if got := f.dispatcher.synchronizationCalls; got != 0 {
		t.Errorf("GET mode navigation crossed dispatch barrier %d times", got)
	}
}

func TestSendingModeSavedScheduleSurvivesChooserAndReload(t *testing.T) {
	f := newScheduleFixture(t)
	at := time.Now().In(sending.ScheduleZone).Add(2 * time.Hour).Truncate(time.Minute)
	created := do(t, f.handler, http.MethodPost, f.base+"/schedule", scheduleForm("mode-schedule-123456", at), withCookie(f.cookie))
	if created.Code != http.StatusSeeOther {
		t.Fatalf("enable schedule: %d %.400s", created.Code, created.Body.String())
	}
	storedAt := f.storedScheduleAt(t)
	beforeActions := sendingModeRecordCount(t, f, sending.ActionsCollection)
	beforeAttempts := sendingModeRecordCount(t, f, sending.AttemptsCollection)

	chooser := do(t, f.handler, http.MethodGet, f.base+"?sending_type=choose", nil, withCookie(f.cookie))
	if chooser.Code != http.StatusOK {
		t.Fatalf("open chooser: %d", chooser.Code)
	}
	chooserDoc := sendingModeDocument(t, chooser.Body.String())
	sendingModeAssertChoices(t, chooserDoc)
	if !strings.Contains(chooser.Body.String(), presentation.Timestamp(storedAt)) {
		t.Error("chooser omitted the saved schedule")
	}

	selected := do(t, f.handler, http.MethodPost, "/campaigns/"+f.campaignID+"/tasks/"+f.taskID+"/sending-type", url.Values{"sending_type": {views.TaskSendingTypeScheduled}}, withCookie(f.cookie))
	if selected.Code != http.StatusSeeOther {
		t.Fatalf("save scheduled mode: %d", selected.Code)
	}
	scheduled := do(t, f.handler, http.MethodGet, "/campaigns/"+f.campaignID+"/tasks/"+f.taskID+"?sending_type=direct", nil, withCookie(f.cookie))
	if scheduled.Code != http.StatusOK {
		t.Fatalf("open scheduled mode: %d", scheduled.Code)
	}
	scheduledDoc := sendingModeDocument(t, scheduled.Body.String())
	sendingModeAssertAction(t, scheduledDoc, f.base+"/start#sending", false)
	sendingModeAssertScheduleEditor(t, scheduledDoc, true)
	scheduleFormNode := sendingModeForm(scheduledDoc, f.base+"/schedule#sending")
	if scheduleFormNode == nil {
		t.Fatal("scheduled mode has no schedule editor")
	}
	local := storedAt.In(sending.ScheduleZone)
	for name, want := range map[string]string{
		"schedule_date": local.Format("2006-01-02"),
		"schedule_time": local.Format("15:04"),
	} {
		control := sendingModeNode(scheduleFormNode, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data != "option" && sendingModeAttr(n, "name") == name
		})
		got := sendingModeAttr(control, "value")
		if control != nil && control.Data == "select" {
			selected := sendingModeNode(control, func(n *html.Node) bool {
				return n.Type == html.ElementNode && n.Data == "option" && sendingModeHasAttr(n, "selected")
			})
			got = sendingModeAttr(selected, "value")
		}
		if control == nil || got != want {
			t.Errorf("saved schedule %s value = %q, want %q", name, got, want)
		}
	}
	if got := f.storedScheduleAt(t); !got.Equal(storedAt) {
		t.Fatalf("navigation changed schedule from %s to %s", storedAt, got)
	}
	if got := sendingModeRecordCount(t, f, sending.ActionsCollection); got != beforeActions {
		t.Errorf("GET navigation created actions: got %d, before %d", got, beforeActions)
	}
	if got := sendingModeRecordCount(t, f, sending.AttemptsCollection); got != beforeAttempts {
		t.Errorf("GET navigation created attempts: got %d, before %d", got, beforeAttempts)
	}
}

func TestSendingTypePOSTValidationAndStateGuards(t *testing.T) {
	f := newScheduleFixture(t)
	path := "/campaigns/" + f.campaignID + "/tasks/" + f.taskID + "/sending-type"
	// The upload fixture records its required Direct/Scheduled preference.
	uploaded := taskSendingTypeStored(t, f)
	if uploaded != views.TaskSendingTypeDirect {
		t.Fatalf("upload stored sending type = %q, want %q", uploaded, views.TaskSendingTypeDirect)
	}
	for _, value := range []string{"", "choose", "invalid"} {
		rec := do(t, f.handler, http.MethodPost, path, url.Values{"sending_type": {value}}, withCookie(f.cookie))
		if rec.Code < http.StatusBadRequest || rec.Code >= http.StatusInternalServerError {
			t.Errorf("invalid sending_type %q status=%d, want client error", value, rec.Code)
		}
	}
	if got := taskSendingTypeStored(t, f); got != uploaded {
		t.Fatalf("invalid selection changed stored type to %q", got)
	}

	for _, state := range []string{intake.ScheduleScheduled, intake.ScheduleMissed} {
		if _, err := f.app.DB().NewQuery("UPDATE " + intake.TasksCollection + " SET schedule_state={:state} WHERE id={:id}").Bind(dbx.Params{"state": state, "id": f.taskID}).Execute(); err != nil {
			t.Fatal(err)
		}
		rec := do(t, f.handler, http.MethodPost, path, url.Values{"sending_type": {views.TaskSendingTypeDirect}}, withCookie(f.cookie))
		if rec.Code < http.StatusBadRequest || rec.Code >= http.StatusInternalServerError {
			t.Errorf("switch to direct with schedule state %q status=%d, want client error", state, rec.Code)
		}
		if got := taskSendingTypeStored(t, f); got != uploaded {
			t.Errorf("schedule state %q changed stored type to %q", state, got)
		}
	}

	if _, err := f.app.DB().NewQuery("UPDATE " + intake.TasksCollection + " SET schedule_state='none' WHERE id={:id}").Bind(dbx.Params{"id": f.taskID}).Execute(); err != nil {
		t.Fatal(err)
	}
	started := do(t, f.handler, http.MethodPost, f.base+"/start", url.Values{"action_id": {"type-guard-start-123456"}, "expected_count": {"1"}, "confirmed": {"yes"}}, withCookie(f.cookie))
	if started.Code != http.StatusSeeOther {
		t.Fatalf("start fixture task: %d %.300s", started.Code, started.Body.String())
	}
	rec := do(t, f.handler, http.MethodPost, path, url.Values{"sending_type": {views.TaskSendingTypeScheduled}}, withCookie(f.cookie))
	if rec.Code < http.StatusBadRequest || rec.Code >= http.StatusInternalServerError {
		t.Errorf("change type after start status=%d, want client error", rec.Code)
	}
}

func taskSendingTypeStored(t *testing.T, f *scheduleFixture) string {
	t.Helper()
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.GetString("sending_type")
}

func TestSendingModeClearDirectRedirectsWithoutStarting(t *testing.T) {
	f := newScheduleFixture(t)
	at := time.Now().In(sending.ScheduleZone).Add(2 * time.Hour)
	created := do(t, f.handler, http.MethodPost, f.base+"/schedule", scheduleForm("clear-direct-schedule-123456", at), withCookie(f.cookie))
	if created.Code != http.StatusSeeOther {
		t.Fatalf("enable schedule: %d %.400s", created.Code, created.Body.String())
	}
	beforeAttempts := sendingModeRecordCount(t, f, sending.AttemptsCollection)
	beforeStarts := sendingModeCountActions(t, f, "start")
	fields := url.Values{
		"action_id":       {"clear-direct-action-123456"},
		"schedule_action": {"clear"},
		"sending_type":    {views.TaskSendingTypeDirect},
	}
	cleared := do(t, f.handler, http.MethodPost, f.base+"/schedule", fields, withCookie(f.cookie))
	wantLocation := "/campaigns/" + f.campaignID + "/tasks/" + f.taskID + "#sending"
	if cleared.Code != http.StatusSeeOther || cleared.Header().Get("Location") != wantLocation {
		t.Fatalf("clear redirect: status=%d location=%q, want %q", cleared.Code, cleared.Header().Get("Location"), wantLocation)
	}
	if state := f.storedScheduleState(t); state != intake.ScheduleNone {
		t.Fatalf("schedule state after clear = %q, want none", state)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := task.GetString("status"); got != intake.TaskReady {
		t.Fatalf("clear started task: status = %q", got)
	}
	if got := sendingModeCountActions(t, f, "start"); got != beforeStarts {
		t.Errorf("clear created a start action: got %d, before %d", got, beforeStarts)
	}
	if got := sendingModeRecordCount(t, f, sending.AttemptsCollection); got != beforeAttempts {
		t.Errorf("clear created sending attempts: got %d, before %d", got, beforeAttempts)
	}
}

func TestSendingModeStartedTaskIgnoresForgedChoice(t *testing.T) {
	f := newScheduleFixture(t)
	fields := url.Values{
		"action_id":      {"started-mode-action-123456"},
		"expected_count": {"1"},
		"confirmed":      {"yes"},
		"sending_type":   {views.TaskSendingTypeDirect},
	}
	started := do(t, f.handler, http.MethodPost, f.base+"/start", fields, withCookie(f.cookie))
	if started.Code != http.StatusSeeOther {
		t.Fatalf("start task: %d %.400s", started.Code, started.Body.String())
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := task.GetString("status"); got != "running" {
		t.Fatalf("task status after start = %q, want running", got)
	}
	for _, endpoint := range []string{f.base, f.base + "/progress"} {
		page := do(t, f.handler, http.MethodGet, endpoint+"?sending_type=scheduled", nil, withCookie(f.cookie))
		if page.Code != http.StatusOK {
			t.Fatalf("GET forged mode at %s: %d", endpoint, page.Code)
		}
		doc := sendingModeDocument(t, page.Body.String())
		sendingModeAssertChoicesAbsent(t, doc)
		sendingModeAssertActionAbsent(t, doc, f.base+"/start#sending")
		sendingModeAssertScheduleEditor(t, doc, false)
		if sendingModeForm(doc, f.base+"/stop#sending") == nil {
			t.Error("forged mode hid the existing stop control")
		}
	}
	if got := sendingModeCountActions(t, f, "start"); got != 1 {
		t.Errorf("start action count = %d, want 1", got)
	}
}

func sendingModeRecordCount(t *testing.T, f *scheduleFixture, collection string) int {
	t.Helper()
	count, err := f.app.CountRecords(collection)
	if err != nil {
		t.Fatal(err)
	}
	return int(count)
}

func sendingModeCountActions(t *testing.T, f *scheduleFixture, kind string) int {
	t.Helper()
	records, err := f.app.FindRecordsByFilter(sending.ActionsCollection, "task={:task} && kind={:kind}", "", 0, 0, dbx.Params{"task": f.taskID, "kind": kind})
	if err != nil {
		t.Fatal(err)
	}
	return len(records)
}

func sendingModeDocument(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func sendingModeAttr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func sendingModeHasAttr(node *html.Node, name string) bool {
	if node == nil {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == name {
			return true
		}
	}
	return false
}

func sendingModeNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := sendingModeNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func sendingModeForm(root *html.Node, action string) *html.Node {
	return sendingModeNode(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "form" && sendingModeAttr(n, "action") == action
	})
}

func sendingModeAssertAction(t *testing.T, doc *html.Node, action string, want bool) {
	t.Helper()
	got := sendingModeForm(doc, action) != nil
	if got != want {
		t.Errorf("form action %q present=%t, want %t", action, got, want)
	}
}

func sendingModeAssertActionAbsent(t *testing.T, doc *html.Node, actions ...string) {
	t.Helper()
	for _, action := range actions {
		sendingModeAssertAction(t, doc, action, false)
	}
}

func sendingModeAssertScheduleEditor(t *testing.T, doc *html.Node, want bool) {
	t.Helper()
	got := sendingModeNode(doc, func(n *html.Node) bool {
		id := sendingModeAttr(n, "id")
		return n.Type == html.ElementNode && n.Data == "form" && strings.HasPrefix(id, "sending-schedule-") && !strings.HasPrefix(id, "sending-schedule-clear-")
	}) != nil
	if got != want {
		t.Errorf("schedule editor present=%t, want %t", got, want)
	}
}

func sendingModeAssertChoices(t *testing.T, doc *html.Node) {
	t.Helper()
	form := sendingModeNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "form" && sendingModeAttr(n, "method") == "post" && strings.HasSuffix(sendingModeAttr(n, "action"), "/sending-type")
	})
	if form == nil {
		t.Fatal("mode chooser has no native POST form")
	}
	for _, value := range []string{views.TaskSendingTypeDirect, views.TaskSendingTypeScheduled} {
		button := sendingModeNode(form, func(n *html.Node) bool {
			return n.Type == html.ElementNode && n.Data == "button" && sendingModeAttr(n, "type") == "submit" && sendingModeAttr(n, "name") == "sending_type" && sendingModeAttr(n, "value") == value
		})
		if button == nil {
			t.Errorf("GET chooser is missing %s choice", value)
		}
	}
}

func sendingModeAssertChoicesAbsent(t *testing.T, doc *html.Node) {
	t.Helper()
	button := sendingModeNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "button" && sendingModeAttr(n, "name") == "sending_type"
	})
	if button != nil {
		t.Error("started task exposes first-start mode choices")
	}
}
