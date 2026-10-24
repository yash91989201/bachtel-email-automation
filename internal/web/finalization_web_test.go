package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// The task workspace exposes finalization before testing, then shows the
// selected snapshot, its protected download and snapshot-bound approval state.
func TestTaskFinalizationUploadDownloadAndTestState(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignID, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d %.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, campaignID, mixedWorkbook(t))
	path := "/campaigns/" + campaignID + "/tasks/" + taskID
	recipientsPath := views.TaskSectionPath(campaignID, taskID, views.TaskSectionRecipients)
	testPath := views.TaskSectionPath(campaignID, taskID, views.TaskSectionTest)
	page := fetchTaskPage(t, handler, cookie, recipientsPath)
	for _, want := range []string{
		`id="task-finalization"`,
		`id="final-workbook"`,
		"Approve as-is becomes available after a completed preparation run is selected",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("pre-finalization Recipients section missing %q", want)
		}
	}
	testPage := fetchTaskPage(t, handler, cookie, testPath)
	if !strings.Contains(testPage, "Finalize the sending data before sending a test for approval") {
		t.Fatal("pre-finalization Test & approve section is missing the prerequisite")
	}
	if strings.Contains(testPage, "Approve received formatting") {
		t.Fatal("formatting approval is offered before dataset finalization")
	}

	finalizeTask(t, app, handler, cookie, campaignID, taskID)
	page = fetchTaskPage(t, handler, cookie, recipientsPath)
	for _, want := range []string{
		"Selected final snapshot",
		"1 accepted · 4 excluded",
		"Download accepted final sheet",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("finalized Recipients section missing %q", want)
		}
	}
	testPage = fetchTaskPage(t, handler, cookie, testPath)
	for _, want := range []string{
		"Approve received formatting",
		"Formatting is not approved for this exact final snapshot and current mailbox.",
	} {
		if !strings.Contains(testPage, want) {
			t.Fatalf("finalized Test & approve section missing %q", want)
		}
	}
	link := regexp.MustCompile(`href="([^"]*finalization/sending\.xlsx[^"]*)"`).FindStringSubmatch(page)
	if len(link) != 2 {
		t.Fatalf("final sheet download link missing: %.1200s", page)
	}
	rec := do(t, handler, http.MethodGet, link[1], nil, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "PK") {
		t.Fatalf("final sheet download: status=%d prefix=%q", rec.Code, rec.Body.String()[:min(2, rec.Body.Len())])
	}
	anonymous := do(t, handler, http.MethodGet, link[1], nil, nil)
	if anonymous.Code != http.StatusSeeOther || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous final sheet download = %d %q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	// Approve as-is without a selected prepared run is an inline remedy, not a
	// server error or a snapshot.
	rec = do(t, handler, http.MethodPost, path+"/finalization/approve", url.Values{
		"action_id": {"approve-without-run-123456"}, "confirm_approve": {"yes"},
	}, withCookie(cookie))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "select a completed preparation run") {
		t.Fatalf("approve without prepared data: %d %.400s", rec.Code, rec.Body.String())
	}
}
