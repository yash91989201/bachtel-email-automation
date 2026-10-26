package web

// Task Settings route contracts: a directly loadable, read-only GET and a
// bounded owner-only POST that renames through POST/Redirect/GET.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

func TestTaskSettingsRenameFlow(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	settings := views.TaskSectionPath(id, taskID, views.TaskSectionSettings)

	page := fetchTaskPage(t, handler, cookie, settings)
	for _, want := range []string{`id="task-settings"`, `value="book.xlsx"`, "Leave blank to use the workbook filename", "Save changes"} {
		if !strings.Contains(page, want) {
			t.Errorf("Settings is missing %q", want)
		}
	}
	if strings.Contains(page, `aria-label="Task lifecycle"`) {
		t.Error("Settings must stay outside the lifecycle tracker")
	}
	links, current := sectionNav(t, page, "Task navigation")
	wantLinks := []string{
		views.TaskSectionPath(id, taskID, views.TaskSectionOverview),
		views.TaskSectionPath(id, taskID, views.TaskSectionRecipients),
		views.TaskSectionPath(id, taskID, views.TaskSectionTest),
		views.TaskSectionPath(id, taskID, views.TaskSectionSending),
		views.TaskSectionPath(id, taskID, views.TaskSectionResults),
		settings,
	}
	if strings.Join(links, "|") != strings.Join(wantLinks, "|") {
		t.Errorf("task navigation = %v, want %v", links, wantLinks)
	}
	if current != settings {
		t.Errorf("current section = %q, want Settings", current)
	}

	// A plain GET changes nothing.
	before, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	fetchTaskPage(t, handler, cookie, settings)
	after, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if before.GetString("name") != after.GetString("name") || before.GetString("updated") != after.GetString("updated") {
		t.Fatal("GET Settings wrote task state")
	}

	rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/name",
		url.Values{"name": {"  November follow-up  "}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != settings+"?saved=1" {
		t.Fatalf("rename: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	saved := fetchTaskPage(t, handler, cookie, settings+"?saved=1")
	for _, want := range []string{"Task name saved", "November follow-up", `value="November follow-up"`} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved Settings is missing %q", want)
		}
	}
	list := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks")
	if !strings.Contains(list, "November follow-up") || !strings.Contains(list, "Workbook book.xlsx") {
		t.Error("the campaign list must show the new name and the original workbook filename")
	}
	// Refreshing Settings shows feedback again but never replays the save.
	first, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	fetchTaskPage(t, handler, cookie, settings+"?saved=1")
	second, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetString("updated") != second.GetString("updated") || second.GetString("name") != "November follow-up" {
		t.Fatal("refreshing the success page replayed the rename")
	}

	// Clearing the name restores the workbook filename.
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/name",
		url.Values{"name": {"   "}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("blank rename: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	reset := fetchTaskPage(t, handler, cookie, settings)
	if !strings.Contains(reset, `value="book.xlsx"`) {
		t.Error("clearing the name must prefill the workbook filename")
	}
}

func TestTaskSettingsRefusesInvalidAndUnauthorizedSaves(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	settings := views.TaskSectionPath(id, taskID, views.TaskSectionSettings)
	namePath := "/campaigns/" + id + "/tasks/" + taskID + "/name"
	storedName := func() string {
		t.Helper()
		record, err := app.FindRecordById(intake.TasksCollection, taskID)
		if err != nil {
			t.Fatal(err)
		}
		return record.GetString("name")
	}

	tooLong := strings.Repeat("é", 256)
	rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {tooLong}}, withCookie(cookie))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name: status=%d, want 400", rec.Code)
	}
	for _, want := range []string{"at most 255 characters", tooLong, `id="task-settings"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("refused save is missing %q", want)
		}
	}
	if storedName() != "book.xlsx" {
		t.Error("a refused save changed the stored name")
	}

	// A failing store keeps Settings selected, the draft and the prior name.
	if _, err := app.DB().NewQuery(`
		CREATE TRIGGER fail_task_name_update BEFORE UPDATE OF name ON send_tasks
		BEGIN SELECT RAISE(ABORT, 'storage failure'); END
	`).Execute(); err != nil {
		t.Skipf("this SQLite build cannot install a failure trigger: %v", err)
	}
	rec = do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Draft name"}}, withCookie(cookie))
	if _, err := app.DB().NewQuery("DROP TRIGGER fail_task_name_update").Execute(); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("storage failure: status=%d, want 500", rec.Code)
	}
	for _, want := range []string{`value="Draft name"`, "could not be saved", `id="task-settings"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("storage failure page is missing %q", want)
		}
	}
	if storedName() != "book.xlsx" {
		t.Error("a failed storage write changed the stored name")
	}

	// Anonymous, foreign and wrong-campaign requests are refused unchanged.
	if rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Anonymous"}}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous save = %d, want 401", rec.Code)
	}
	provisionOperatorAs(t, app, "second@example.com", "second-password")
	foreign := loginAs(t, handler, "second@example.com", "second-password")
	if rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Foreign"}}, withCookie(foreign)); rec.Code != http.StatusNotFound {
		t.Errorf("foreign save = %d, want 404", rec.Code)
	}
	other := createCampaign(t, handler, cookie, "Marketing")
	wrongCampaign := "/campaigns/" + other + "/tasks/" + taskID + "/name"
	if rec := do(t, handler, http.MethodPost, wrongCampaign, url.Values{"name": {"Wrong campaign"}}, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Errorf("wrong-campaign save = %d, want 404", rec.Code)
	}
	if storedName() != "book.xlsx" {
		t.Error("a refused request changed the stored name")
	}
	if rec := do(t, handler, http.MethodGet, "/campaigns/"+other+"/tasks/"+taskID+"/settings", nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Errorf("wrong-campaign settings GET = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, settings, nil, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("anonymous settings GET = %d, want a login redirect", rec.Code)
	}
}

// Settings works before validation and after terminal closure, and it never
// exposes processing controls.
func TestTaskSettingsWorksInStagingAndClosedStates(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	settings := views.TaskSectionPath(id, taskID, views.TaskSectionSettings)

	staging := fetchTaskPage(t, handler, cookie, settings)
	if !strings.Contains(staging, `id="task-settings"`) || !strings.Contains(staging, "Save changes") {
		t.Fatal("Settings must be usable by a staging task")
	}
	for _, control := range []string{"Start sending", "Schedule sending", "Stop sending", "Retry all"} {
		if strings.Contains(staging, control) {
			t.Errorf("Settings exposes the processing control %q", control)
		}
	}

	record, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("status", "completed")
	record.Set("closed_at", "2026-10-20 10:00:00.000Z")
	record.Set("closure_requested_at", "2026-10-19 10:00:00.000Z")
	record.Set("closure_actor", operatorID(t, app))
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	closed := fetchTaskPage(t, handler, cookie, settings)
	if !strings.Contains(closed, `id="task-settings"`) {
		t.Fatal("Settings must stay available on a closed task")
	}
	rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/name",
		url.Values{"name": {"Closed task name"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rename closed task: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	closed = fetchTaskPage(t, handler, cookie, settings)
	if !strings.Contains(closed, `value="Closed task name"`) {
		t.Error("a closed task must keep its new name")
	}
	if !strings.Contains(closed, "This task is terminal") {
		t.Error("renaming must not reopen or hide the closed state")
	}
}
