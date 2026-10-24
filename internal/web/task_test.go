package web

// Task intake operator surface tests. Every test drives the real HTTP handler
// against a real PocketBase app with a stub converter: no Pandoc, no sleeps.

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// taskSubject is the content revision the task fixtures validate against.
const taskSubject = "Application for {{candidate_name}}"

// newTaskServer boots the UI behind a stub converter with the documented
// intake limits resolved, so the upload page renders real byte counts.
func newTaskServer(t *testing.T) (*pocketbase.PocketBase, http.Handler) {
	t.Helper()
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.PandocPath = stubPandoc(t)
	cfg.Intake = intake.DefaultLimits()
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	return app, handler
}

// taskWorkbook writes one worksheet of rows as an XLSX byte stream. Row
// indices are original worksheet rows: rows[0] is row 1.
func taskWorkbook(t *testing.T, sheet string, rows [][]string) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	if sheet != "Sheet1" {
		if err := file.SetSheetName("Sheet1", sheet); err != nil {
			t.Fatal(err)
		}
	}
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		values := row
		if err := file.SetSheetRow(sheet, cell, &values); err != nil {
			t.Fatal(err)
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// mixedWorkbook is the fixture for the full flow: a duplicate pair, an
// unusable address, one missing required value, and one clean row. The clean
// row's name is clean; new-revision rendering preserves supplied values.
func mixedWorkbook(t *testing.T) []byte {
	t.Helper()
	return taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name", "Location"},
		{"alice@example.com", "Ada Lovelace", "Remote"},
		{" ALICE@example.com ", "Grace Hopper", "Paris"},
		{"not-an-address", "Zoe Quinn", "London"},
		{"bob@example.com", "", "Berlin"},
		{"carol@example.com", "Carol Danvers", "Lisbon"},
	})
}

// uploadWorkbook posts one workbook to the campaign's task upload route.
func uploadWorkbook(t *testing.T, handler http.Handler, cookie *http.Cookie, campaignID string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("sending_type", "direct"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("workbook", "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
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
	return rec
}

// taskIDFromRedirect extracts the staged task id from an upload redirect.
func taskIDFromRedirect(t *testing.T, rec *httptest.ResponseRecorder, campaignID string) string {
	t.Helper()
	prefix := "/campaigns/" + campaignID + "/tasks/"
	id := strings.TrimPrefix(rec.Header().Get("Location"), prefix)
	id, _, _ = strings.Cut(id, "/")
	if rec.Code != http.StatusSeeOther || id == "" || strings.Contains(id, "/") {
		t.Fatalf("upload redirect: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	return id
}

// fetchTaskPage GETs a page and asserts it leaks no internal detail.
func fetchTaskPage(t *testing.T, handler http.Handler, cookie *http.Cookie, path string) string {
	t.Helper()
	rec := do(t, handler, http.MethodGet, path, nil, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status=%d body=%.400s", path, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"pb_data", "/tmp/", "panic"} {
		if strings.Contains(body, leak) {
			t.Fatalf("GET %s leaks %q: %.400s", path, leak, body)
		}
	}
	return body
}

// previewOptions returns (rowID, worksheet row, state) triples from the ready
// page's preview select, in worksheet-row order.
func previewOptions(t *testing.T, body string) [][3]string {
	t.Helper()
	re := regexp.MustCompile(`value="([^"]+)"[^>]*>Worksheet row ([0-9]+)(?: · [^<(]*)?(?: \(([a-z_]+)\))?</option>`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatalf("no preview options in page: %.600s", body)
	}
	options := make([][3]string, 0, len(matches))
	for _, m := range matches {
		options = append(options, [3]string{m[1], m[2], m[3]})
	}
	return options
}

// previewRowID returns the id of the admitted row with the given state and
// worksheet row number.
func previewRowID(t *testing.T, body string, sourceRow int) string {
	t.Helper()
	for _, option := range previewOptions(t, body) {
		if option[1] == strconv.Itoa(sourceRow) {
			return option[0]
		}
	}
	t.Fatalf("no preview option for worksheet row %d in %.600s", sourceRow, body)
	return ""
}

// assertCategory checks one mutually exclusive validation category row.
func assertCategory(t *testing.T, body, label string, want int) {
	t.Helper()
	wantCell := "<td>" + label + "</td><td>" + strconv.Itoa(want) + "</td>"
	if !strings.Contains(body, wantCell) {
		t.Errorf("validation category %q = %d missing from page: %.600s", label, want, body)
	}
}

// stageTask uploads and validates a workbook, returning the task id. It
// assumes the campaign has one revision and the workbook's sheet is "Sheet1".
func stageTask(t *testing.T, handler http.Handler, cookie *http.Cookie, campaignID string, data []byte) string {
	t.Helper()
	rec := uploadWorkbook(t, handler, cookie, campaignID, data)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := taskIDFromRedirect(t, rec, campaignID)
	rec = do(t, handler, http.MethodPost, "/campaigns/"+campaignID+"/tasks/"+taskID+"/select",
		url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	return taskID
}

// TestTaskRoutesBehindSession covers behaviour 1: every task route sits behind
// the session, with GETs bounced to login and unsafe methods answered 401.
func TestTaskRoutesBehindSession(t *testing.T) {
	_, handler := newTaskServer(t)
	for _, path := range []string{
		"/campaigns/x/tasks",
		"/campaigns/x/tasks/new",
		"/campaigns/x/tasks/y",
		"/campaigns/x/tasks/y/excluded.xlsx",
	} {
		rec := do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("anonymous GET %s: status=%d location=%q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, path := range []string{
		"/campaigns/x/tasks",
		"/campaigns/x/tasks/y/select",
		"/campaigns/x/tasks/y/revalidate",
		"/campaigns/x/tasks/y/preview",
	} {
		rec := do(t, handler, http.MethodPost, path, url.Values{}, nil)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "authentication required") {
			t.Fatalf("anonymous POST %s: status=%d body=%q", path, rec.Code, rec.Body.String())
		}
	}
}

// TestTaskUploadReviewFlow covers behaviour 2: the full journey from upload to
// review to preview, with counts, the exclusion link, and the pinned render.
func assertTaskOverviewSelected(t *testing.T, body, campaignID, taskID string) {
	t.Helper()
	href := views.TaskSectionPath(campaignID, taskID, views.TaskSectionOverview)
	tag := regexp.MustCompile(`aria-current="page"[^<]*`).FindString(body)
	if !strings.Contains(body, href) || tag == "" {
		t.Fatal("an unsectioned task request must select Overview")
	}
}

func TestTaskUploadReviewFlow(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	// Upload page: documented constraints, literal numbers, workbook input.
	upload := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/new")
	for _, want := range []string{
		"10.0 MiB",
		"64.0 MiB",
		"16.0 MiB",
		"1,000 recipient entries",
		"case sensitive",
		"cached value",
		"Password-protected",
		".xls",
		`name="workbook"`,
		`accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"`,
		`href="/campaigns/` + id + `/tasks"`,
	} {
		if !strings.Contains(upload, want) {
			t.Errorf("upload page missing %q: %.600s", want, upload)
		}
	}

	// Upload stages the workbook and redirects to the staging task page.
	rec := uploadWorkbook(t, handler, cookie, id, mixedWorkbook(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := taskIDFromRedirect(t, rec, id)

	staging := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	assertTaskOverviewSelected(t, staging, id, taskID)
	stagingRecipients := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	for _, want := range []string{
		"Sheet1",
		`name="sheet"`,
		`name="header_row"`,
		"Current selection:",
	} {
		if !strings.Contains(stagingRecipients, want) {
			t.Errorf("staging Recipients section missing %q: %.600s", want, stagingRecipients)
		}
	}

	// Confirm the selection: the task becomes ready.
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/select",
		url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	ready := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	for _, want := range []string{
		`href="/campaigns/` + id + `/tasks/` + taskID + `/excluded.xlsx"`,
		"Content hash:",
		"duplicate recipient address in this upload",
		"not a single usable email address",
		"missing required value(s): candidate_name",
		`hx-post="/campaigns/` + id + `/tasks/` + taskID + `/preview"`,
		`hx-target="#task-preview-result"`,
	} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready page missing %q: %.600s", want, ready)
		}
	}
	if !strings.Contains(ready, "5 source rows · 5 recipient entries · 1 eligible at validation · 4 excluded at validation") {
		t.Errorf("validation summary missing from ready page: %.600s", ready)
	}
	assertCategory(t, ready, "Eligible", 1)
	assertCategory(t, ready, "Ineligible or missing data", 1)
	assertCategory(t, ready, "Duplicate", 2)
	assertCategory(t, ready, "Invalid address", 1)

	results := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionResults))
	for _, want := range []string{"Result columns:", "Email Status, Submitted At, Email Error"} {
		if !strings.Contains(results, want) {
			t.Errorf("Results section missing %q: %.600s", want, results)
		}
	}
	testSection := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionTest))
	if !strings.Contains(testSection, "Finalize the sending data before sending a test for approval.") {
		t.Errorf("Test & approve section missing the pre-finalization prerequisite: %.600s", testSection)
	}

	// The list page reports both tasks' status, selection and counts.
	list := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks")
	for _, want := range []string{
		"Needs formatting approval",
		"book.xlsx",
		`href="/account/mailbox"`,
		`href="/campaigns/` + id + `/tasks/new"`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("task list missing %q: %.600s", want, list)
		}
	}

	// Preview the clean row: the pinned subject, body and normalized value.
	rowID := previewRowID(t, ready, 6)
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/preview",
		url.Values{"row": {rowID}}, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	preview := rec.Body.String()
	for _, want := range []string{
		"Application for Carol Danvers",
		">To</p>",
		"carol@example.com",
		"Dear Carol Danvers",
		"Carol Danvers",
	} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview missing %q: %.600s", want, preview)
		}
	}

	// A row from no task is reported as unavailable, never as a server error.
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/preview",
		url.Values{"row": {"does-not-exist"}}, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "not part of this task") {
		t.Fatalf("foreign row preview: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	// An ineligible row renders its fixed missing-value message inline at 200.
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/preview",
		url.Values{"row": {previewRowID(t, ready, 5)}}, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "missing required value(s): candidate_name") {
		t.Fatalf("ineligible preview: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
}

// TestTaskUploadRejection covers behaviour 3: a corrupt file is refused with
// the fixed message and stores no task, and the 1,000-row support bound is
// reported at the selection step.
func TestTaskUploadRejection(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	t.Run("missing file", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if err := writer.WriteField("sending_type", "direct"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/campaigns/"+id+"/tasks", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "choose an XLSX workbook to upload") {
			t.Fatalf("missing file: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing sending preference", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
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
		req := httptest.NewRequest(http.MethodPost, "/campaigns/"+id+"/tasks", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "choose Direct or Scheduled sending") {
			t.Fatalf("missing sending preference: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if stored, err := app.FindRecordsByFilter(intake.TasksCollection, "", "", 0, 0); err != nil || len(stored) != 0 {
			t.Fatalf("refused upload stored %d tasks, err=%v", len(stored), err)
		}
	})

	t.Run("corrupt workbook", func(t *testing.T) {
		rec := uploadWorkbook(t, handler, cookie, id, []byte("this is not a workbook"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("corrupt upload: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "the workbook could not be read; the file looks corrupt") {
			t.Fatalf("corrupt message missing: %.600s", body)
		}
		stored, err := app.FindRecordsByFilter(intake.TasksCollection, "", "", 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(stored) != 0 {
			t.Fatalf("corrupt upload stored %d tasks, want 0", len(stored))
		}
	})

	t.Run("row support bound", func(t *testing.T) {
		rows := [][]string{{"Email", "Applicant Full Name"}}
		for i := 0; i <= intake.MaxRecipientEntries; i++ {
			rows = append(rows, []string{"person" + strconv.Itoa(i) + "@example.com", "Person " + strconv.Itoa(i)})
		}
		rec := uploadWorkbook(t, handler, cookie, id, taskWorkbook(t, "Sheet1", rows))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("oversized upload: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		taskID := taskIDFromRedirect(t, rec, id)
		rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/select",
			url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("row bound: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "more than 1000 recipient entries") {
			t.Fatalf("row bound message missing: %.600s", rec.Body.String())
		}
	})
}

// TestTaskSelectionErrors covers behaviour 4: a missing mapped header names
// the header inline, an unknown worksheet and a non-integer header row are
// refused with their fixed messages, all at 400 without changing the task.
func TestTaskSelectionErrors(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	newStagingTask := func(t *testing.T, data []byte) string {
		t.Helper()
		rec := uploadWorkbook(t, handler, cookie, id, data)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("upload: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		return taskIDFromRedirect(t, rec, id)
	}
	selectTask := func(taskID, sheet, headerRow string) *httptest.ResponseRecorder {
		return do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/select",
			url.Values{"sheet": {sheet}, "header_row": {headerRow}}, withCookie(cookie))
	}

	t.Run("missing mapped header", func(t *testing.T) {
		taskID := newStagingTask(t, taskWorkbook(t, "Sheet1", [][]string{
			{"Email", "Location"},
			{"alice@example.com", "Remote"},
		}))
		rec := selectTask(taskID, "Sheet1", "1")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Applicant Full Name") {
			t.Fatalf("header name missing from message: %.600s", rec.Body.String())
		}
	})

	t.Run("unknown worksheet", func(t *testing.T) {
		taskID := newStagingTask(t, mixedWorkbook(t))
		rec := selectTask(taskID, "Nope", "1")
		if rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), "that worksheet is not part of the uploaded workbook") {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-integer header row", func(t *testing.T) {
		taskID := newStagingTask(t, mixedWorkbook(t))
		rec := selectTask(taskID, "Sheet1", "first")
		if rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), "choose the row number of the header row") {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
	})
}

// TestExclusionReportDownload covers behaviour 5: the download is session
// gated and serves one Excluded worksheet holding every held row.
func TestExclusionReportDownload(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	path := "/campaigns/" + id + "/tasks/" + taskID + "/excluded.xlsx"

	rec := do(t, handler, http.MethodGet, path, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous download: status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = do(t, handler, http.MethodGet, path, nil, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("download: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("content type = %q", got)
	}
	if disposition := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment; filename=") {
		t.Fatalf("content disposition = %q", disposition)
	}
	if cache := rec.Header().Get("Cache-Control"); cache != "no-store" {
		t.Fatalf("cache control = %q", cache)
	}

	// Sending changes the task state, but its validated exclusion artifact stays private and available.
	for _, status := range []string{"running", "stopping", "paused", "completed"} {
		taskRecord, err := app.FindRecordById(intake.TasksCollection, taskID)
		if err != nil {
			t.Fatal(err)
		}
		taskRecord.Set("status", status)
		if err := app.Save(taskRecord); err != nil {
			t.Fatal(err)
		}
		download := do(t, handler, http.MethodGet, path, nil, withCookie(cookie))
		if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), rec.Body.Bytes()) {
			t.Fatalf("%s exclusion report: status=%d", status, download.Code)
		}
	}

	file, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	sheets := file.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "Excluded" {
		t.Fatalf("report worksheets = %v, want one Excluded sheet", sheets)
	}
	grid, err := file.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 5 {
		t.Fatalf("report rows = %d, want header + 4 held rows", len(grid))
	}
	// Source columns come first; the appended metadata holds the original
	// worksheet row and the individual recipient.
	want := map[string]bool{"2": true, "3": true, "4": true, "5": true}
	for _, row := range grid[1:] {
		if len(row) < 6 {
			t.Fatalf("report entry lacks metadata columns: %v", row)
		}
		if !want[row[3]] {
			t.Errorf("report holds unexpected worksheet row %q", row[3])
		}
		delete(want, row[3])
	}
	if len(want) != 0 {
		t.Errorf("report missing worksheet rows %v", want)
	}
}

// TestAllHeldTaskOffersDownloadOnly covers behaviour 6: a task whose rows are
// all held still offers the download, states that nothing sends email, and
// offers no start or send control.
func TestAllHeldTaskOffersDownloadOnly(t *testing.T) {
	for name, rows := range map[string][][]string{
		"duplicates and invalid addresses": {
			{"Email", "Applicant Full Name"},
			{"held@example.com", "Held One"},
			{" HELD@example.com ", "Held Two"},
			{"not-an-address", "Held Three"},
		},
		"admitted rows missing required values": {
			{"Email", "Applicant Full Name"},
			{"valid@example.com", ""},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, handler := newTaskServer(t)
			cookie := login(t, handler)
			id := createCampaign(t, handler, cookie, "Engineers")
			if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
				t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
			}
			taskID := stageTask(t, handler, cookie, id, taskWorkbook(t, "Sheet1", rows))

			body := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
			for _, want := range []string{
				`href="/campaigns/` + id + `/tasks/` + taskID + `/excluded.xlsx"`,
				"No eligible recipients",
				"Download exclusion report",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("all-held page missing %q: %.600s", want, body)
				}
			}
			if strings.Contains(body, `name="schedule_date"`) || strings.Contains(body, `name="confirmed"`) {
				t.Errorf("all-held page offers a start/send control: %.600s", body)
			}
			assertCategory(t, body, "Eligible", 0)
		})
	}
}

// TestTaskRevalidation covers behaviour 7: a second revision marks the task
// stale, revalidate returns 303, and previews then render the new subject.
func TestTaskRevalidation(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	path := "/campaigns/" + id + "/tasks/" + taskID

	// A second revision changes the content hash: the task is stale.
	if rec := postRevision(t, handler, cookie, id, "Interview with {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("second revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	stale := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	if !strings.Contains(stale, "The campaign content changed after this task was validated") {
		t.Fatalf("stale warning missing: %.600s", stale)
	}

	rec := do(t, handler, http.MethodPost, path+"/revalidate", url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revalidate: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	fresh := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	if strings.Contains(fresh, "The campaign content changed after this task was validated") {
		t.Fatalf("stale warning persists after revalidate: %.600s", fresh)
	}
	rec = do(t, handler, http.MethodPost, path+"/preview",
		url.Values{"row": {previewRowID(t, fresh, 6)}}, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview after revalidate: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Interview with Carol Danvers") {
		t.Fatalf("new subject missing after revalidate: %.600s", rec.Body.String())
	}
}

// TestTaskCrossCampaign404 covers behaviour 8: a task id under another
// campaign's path is a plain 404 on every task route.
func TestTaskCrossCampaign404(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	campaignA := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, campaignA, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, campaignA, mixedWorkbook(t))
	campaignB := createCampaign(t, handler, cookie, "Contractors")

	if rec := do(t, handler, http.MethodGet, "/campaigns/"+campaignB+"/tasks/"+taskID, nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign GET: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+campaignB+"/tasks/"+taskID+"/select",
		url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign select: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if rec := do(t, handler, http.MethodGet, "/campaigns/"+campaignB+"/tasks/"+taskID+"/excluded.xlsx",
		nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("cross-campaign download: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	// An unknown task id under the owning campaign is also a 404.
	if rec := do(t, handler, http.MethodGet, "/campaigns/"+campaignA+"/tasks/no-such-task",
		nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown task GET: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
}

// TestTaskPagesAvoidInternalDetail covers behaviour 9: no page body contains
// the data directory, a temp path, or a panic marker.
func TestTaskPagesAvoidInternalDetail(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))

	pages := []string{
		"/campaigns/" + id + "/tasks",
		"/campaigns/" + id + "/tasks/new",
		"/campaigns/" + id + "/tasks/" + taskID,
	}
	for _, path := range pages {
		rec := do(t, handler, http.MethodGet, path, nil, withCookie(cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d", path, rec.Code)
		}
		body := rec.Body.String()
		for _, leak := range []string{"pb_data", "/tmp/", "panic", app.DataDir()} {
			if strings.Contains(body, leak) {
				t.Fatalf("GET %s leaks %q: %.400s", path, leak, body)
			}
		}
	}
}

// A staged workbook survives the campaign having no template yet: after a
// revision is created the same upload validates without another upload.
func TestStagedWorkbookValidatesAfterTemplateCreation(t *testing.T) {
	_, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "No template yet")

	rec := uploadWorkbook(t, handler, cookie, id, mixedWorkbook(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := taskIDFromRedirect(t, rec, id)
	staged := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	for _, want := range []string{"Add a template before validating this workbook", "Validation is unavailable", "Add template"} {
		if !strings.Contains(staged, want) {
			t.Errorf("staged page missing %q: %.600s", want, staged)
		}
	}

	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/select",
		url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select after template: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	ready := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	if !strings.Contains(ready, "5 source rows · 5 recipient entries · 1 eligible at validation · 4 excluded at validation") {
		t.Fatalf("staged workbook did not validate after template creation: %.600s", ready)
	}
}

// Rendering a recipient preview is read-only: it creates no attempt and
// changes no task state.
func TestTaskPreviewCreatesNoAttempt(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))
	ready := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	rowID := previewRowID(t, ready, 6)

	rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/preview",
		url.Values{"row": {rowID}}, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Carol Danvers") {
		t.Fatalf("preview: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	attempts, err := app.FindRecordsByFilter(sending.AttemptsCollection, "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("preview created %d attempts, want 0", len(attempts))
	}
	taskRecord, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if taskRecord.GetString("status") != intake.TaskReady {
		t.Fatalf("preview changed task status to %q", taskRecord.GetString("status"))
	}
}

// TestExpandedRecipientReview covers one source row with two eligible entries
// and one invalid entry, same-row duplicates, independent preview selection and
// the protected report download.
func TestExpandedRecipientReview(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Expanded review")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	rec := uploadWorkbook(t, handler, cookie, id, taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name", "Location"},
		{"a@example.com, b@example.com, invalid", "Shared Person", "Remote"},
		{"c@example.com, c@example.com", "Duplicate Pair", "Paris"},
	}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := taskIDFromRedirect(t, rec, id)
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/select",
		url.Values{"sheet": {"Sheet1"}, "header_row": {"1"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	page := fetchTaskPage(t, handler, cookie, views.TaskSectionPath(id, taskID, views.TaskSectionRecipients))
	if !strings.Contains(page, "2 source rows · 5 recipient entries · 2 eligible at validation · 3 excluded at validation") {
		t.Fatalf("summary does not distinguish source rows from entries: %.1200s", page)
	}
	// Each eligible sibling is independently selectable with its own address
	// and entry position.
	for _, want := range []string{"recipient 1 · a@example.com", "recipient 2 · b@example.com"} {
		if !strings.Contains(page, want) {
			t.Fatalf("preview selector missing %q", want)
		}
	}
	// The invalid entry stays visible with its position and does not disable
	// the eligible siblings; both same-row duplicates are reported separately.
	for _, want := range []string{"invalid", "<td>3</td>", "c@example.com", "Duplicate"} {
		if !strings.Contains(page, want) {
			t.Fatalf("task page missing %q: %.1200s", want, page)
		}
	}

	members, err := app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t}", "source_row,recipient_index", 0, 0, dbx.Params{"t": taskID})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("membership rows = %d, want the two eligible siblings", len(members))
	}
	for _, want := range []string{"a@example.com", "b@example.com"} {
		var memberID string
		for _, member := range members {
			candidate, err := app.FindRecordById(intake.CandidatesCollection, member.GetString("candidate"))
			if err != nil {
				t.Fatal(err)
			}
			if candidate.GetString("email_key") == want {
				memberID = member.Id
			}
		}
		if memberID == "" {
			t.Fatalf("no membership for %s", want)
		}
		rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/tasks/"+taskID+"/preview",
			url.Values{"row": {memberID}}, withCookie(cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("preview %s: status=%d", want, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<p class=\"font-semibold\">"+want+"</p>") {
			t.Fatalf("preview %s did not show its own To address: %.800s", want, body)
		}
		if !strings.Contains(body, "a@example.com, b@example.com, invalid") {
			t.Fatalf("preview %s did not show the original cell: %.800s", want, body)
		}
	}

	// The exclusion report stays behind the operator session.
	reportPath := "/campaigns/" + id + "/tasks/" + taskID + "/excluded.xlsx"
	if rec := do(t, handler, http.MethodGet, reportPath, nil, func(*http.Request) {}); rec.Code == http.StatusOK {
		t.Fatal("anonymous report download succeeded")
	}
	rec = do(t, handler, http.MethodGet, reportPath, nil, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("operator report download: status=%d", rec.Code)
	}
	report, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := report.Close(); err != nil {
			t.Error(err)
		}
	}()
	grid, err := report.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	if len(grid) != 4 {
		t.Fatalf("report rows = %d, want header plus three excluded entries: %v", len(grid), grid)
	}
}
