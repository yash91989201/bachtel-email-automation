package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// A refused structural final upload renders the page with the edit path open,
// the error visible and the previously accepted snapshot still selected.
func TestFinalUploadRejectionOpensEditPathAndKeepsSnapshot(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignID := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignID, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("revision: %d %.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, campaignID, mixedWorkbook(t))
	finalizeTask(t, app, handler, cookie, campaignID, taskID)
	recipientsPath := views.TaskSectionPath(campaignID, taskID, views.TaskSectionRecipients)
	page := fetchTaskPage(t, handler, cookie, recipientsPath)
	selected := regexp.MustCompile(`Selected final snapshot</strong> <code>([a-z0-9]+)</code>`).FindStringSubmatch(page)
	if len(selected) != 2 {
		t.Fatalf("selected final snapshot missing before the refused upload: %.600s", page)
	}

	// The source workbook has none of the mapped final-sheet columns, so the
	// upload fails structurally before any entry is classified.
	sheet := taskWorkbook(t, "Sheet1", [][]string{{"Name"}, {"Ada"}})
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("action_id", "rejected-final-upload-123456"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("workbook", "broken.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(sheet); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/finalization/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("structural final upload: status=%d %.600s", rec.Code, rec.Body.String())
	}
	rejected := rec.Body.String()
	for _, want := range []string{
		"missing required mapped column(s)",
		"Selected final snapshot",
		"Download accepted final sheet",
		`id="finalization-edit-trigger"`,
	} {
		if !strings.Contains(rejected, want) {
			t.Fatalf("refused upload page is missing %q: %.1200s", want, rejected)
		}
	}
	if !regexp.MustCompile(`id="finalization-edit-trigger"[\s\S]{0,800}?aria-expanded="true"`).MatchString(rejected) {
		t.Fatal("refused upload did not open the edit path")
	}

	// The prior snapshot survives the refusal and nothing was partially
	// accepted or replaced.
	page = fetchTaskPage(t, handler, cookie, recipientsPath)
	if !strings.Contains(page, "<code>"+selected[1]+"</code>") {
		t.Fatalf("refused upload replaced the selected snapshot %q", selected[1])
	}
	if !strings.Contains(page, "Download accepted final sheet") {
		t.Fatal("refused upload removed the prior snapshot download")
	}
}

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
		"Approve as-is becomes available after a completed preparation run and its backend review",
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
