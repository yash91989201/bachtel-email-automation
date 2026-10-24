package web

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// validMappings is one complete mapping line in the delivered textarea
// contract: variable | source header | required|optional|recipient | fallback.
const recipientMapping = "candidate_email | Email | recipient"
const validMappings = "candidate_name | Applicant Full Name | required\n" + recipientMapping

// stubPandoc writes a deterministic converter stand-in: it reports a version
// and emits one fixed body containing a placeholder, so revision tests never
// depend on a pandoc install on the machine running them.
func stubPandoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pandoc")
	script := `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = "--version" ]; then
		echo "pandoc stub 1.0"
		exit 0
	fi
done
printf '%s' '<p>Dear {{candidate_name}}, we received your application.</p>'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// docxUpload builds a minimal readable DOCX package: a zip carrying
// word/document.xml, which both the inspection and the file storage accept.
func docxUpload(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	part, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	document := `<?xml version="1.0"?><w:document xmlns:w="w"><w:body><w:p/></w:body></w:document>`
	if _, err := io.WriteString(part, document); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// newCampaignServer boots the UI behind a stub converter with a provisioned
// operator; the converter path is returned so tests can assert no page leaks
// it.
func newCampaignServer(t *testing.T) (*pocketbase.PocketBase, http.Handler, string) {
	t.Helper()
	cfg := testConfig("http://127.0.0.1:8090")
	converter := stubPandoc(t)
	cfg.PandocPath = converter
	app, handler := newTestServer(t, cfg)
	provisionOperator(t, app)
	return app, handler, converter
}

// revisionBody builds the multipart upload for one content revision.
func revisionBody(t *testing.T, subject, mappings string, document []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subject", subject); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("mappings", mappings); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("document", "body.docx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(document); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func postRevision(t *testing.T, handler http.Handler, cookie *http.Cookie, id, subject, mappings string, document []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := revisionBody(t, subject, mappings, document)
	req := httptest.NewRequest(http.MethodPost, "/campaigns/"+id+"/revisions", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// createCampaign creates a campaign with default result labels and returns
// its id from the redirect.
func createCampaign(t *testing.T, handler http.Handler, cookie *http.Cookie, name string) string {
	t.Helper()
	rec := do(t, handler, http.MethodPost, "/campaigns", url.Values{
		"name":           {name},
		"source_headers": {"Email"},
	}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create campaign: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	id := strings.TrimPrefix(rec.Header().Get("Location"), "/campaigns/")
	id, _, _ = strings.Cut(id, "?")
	id, _, _ = strings.Cut(id, "/")
	if id == "" {
		t.Fatalf("no campaign id in location %q", rec.Header().Get("Location"))
	}
	return id
}

func campaignRecord(t *testing.T, app core.App, id string) *core.Record {
	t.Helper()
	record, err := app.FindRecordById(campaign.CampaignsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// currentConfigurationID returns the campaign's current revision id.
func currentConfigurationID(t *testing.T, app core.App, campaignID string) string {
	t.Helper()
	return campaignRecord(t, app, campaignID).GetString("current_configuration")
}

func configurationRecord(t *testing.T, app core.App, id string) *core.Record {
	t.Helper()
	record, err := app.FindRecordById(campaign.ConfigurationsCollection, id)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func storedLabels(t *testing.T, app core.App, campaignID string) []string {
	t.Helper()
	var labels []string
	if err := campaignRecord(t, app, campaignID).UnmarshalJSONField("result_labels", &labels); err != nil {
		t.Fatal(err)
	}
	return labels
}

// TestCampaignPagesBehindSession covers behaviour 1: the three campaign GETs
// sit behind the operator session and never render a filesystem path or a raw
// Go error value.
func TestCampaignPagesBehindSession(t *testing.T) {
	app, handler, converter := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}

	// Anonymous GETs are bounced to the login page; unsafe methods get 401.
	for _, path := range []string{"/campaigns", "/campaigns/new", "/campaigns/" + id} {
		rec := do(t, handler, http.MethodGet, path, nil, nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Fatalf("anonymous GET %s: status=%d location=%q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, path := range []string{
		"/campaigns",
		"/campaigns/" + id + "/revisions",
		"/campaigns/" + id + "/labels",
		"/campaigns/" + id + "/preview",
	} {
		rec := do(t, handler, http.MethodPost, path, url.Values{}, nil)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "authentication required") {
			t.Fatalf("anonymous POST %s: status=%d body=%q", path, rec.Code, rec.Body.String())
		}
	}

	// A session reaches every page, and no page shows a filesystem path or a
	// raw Go error value.
	for _, path := range []string{"/campaigns", "/campaigns/new", "/campaigns/" + id} {
		rec := do(t, handler, http.MethodGet, path, nil, withCookie(cookie))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%.400s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, leak := range []string{converter, app.DataDir(), "errorString", "sql:", "goroutine "} {
			if strings.Contains(body, leak) {
				t.Fatalf("GET %s leaks %q: %.400s", path, leak, body)
			}
		}
	}
}

// TestCampaignRevisionValidationInline covers behaviour 2: mapping problems
// answer 400 with the message inline and never become the current revision.
func TestCampaignRevisionValidationInline(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")

	currentRevision := func() string {
		t.Helper()
		return currentConfigurationID(t, app, id)
	}

	t.Run("malformed mapping line", func(t *testing.T) {
		rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", "candidate_name - Applicant Full Name", docxUpload(t))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "mapping line 1:") || !strings.Contains(body, "variable | source header") {
			t.Fatalf("inline parse message missing: %.600s", body)
		}
		if got := currentRevision(); got != "" {
			t.Fatalf("current_configuration = %q, want empty", got)
		}
	})

	t.Run("placeholder without a mapping", func(t *testing.T) {
		// The stub converter runs, so Compile reports the unmapped subject
		// placeholder with its own typed message.
		rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", "role | Job Title\n"+recipientMapping, docxUpload(t))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, "no mapping for variable(s): candidate_name") ||
			!strings.Contains(body, "Add a mapping row for each name, or rename the placeholder in the subject or body.") {
			t.Fatalf("inline variable message missing its remedy: %.600s", body)
		}
		if got := currentRevision(); got != "" {
			t.Fatalf("current_configuration = %q, want empty", got)
		}
	})

	t.Run("configured variable without a placeholder", func(t *testing.T) {
		// The documented direction check: a row whose variable never appears in
		// the content is rejected with the remedy that names the row.
		rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}",
			validMappings+"\nrole | Job Title | required", docxUpload(t))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, "configured variable(s) missing from the subject and body: role") ||
			!strings.Contains(body, "Remove each mapping row, or add its placeholder to the subject or body.") {
			t.Fatalf("inline unused message missing its remedy: %.600s", body)
		}
		if got := currentRevision(); got != "" {
			t.Fatalf("current_configuration = %q, want empty", got)
		}
	})

	t.Run("valid revision becomes current", func(t *testing.T) {
		rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", validMappings, docxUpload(t))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if currentRevision() == "" {
			t.Fatal("current_configuration stayed empty after a valid revision")
		}
	})
}

// TestCampaignResultLabelsUpdate covers behaviour 3: duplicate labels are
// rejected without touching storage, and a valid update never changes the
// current revision's content hash.
func TestCampaignResultLabelsUpdate(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	configID := currentConfigurationID(t, app, id)
	hashBefore := configurationRecord(t, app, configID).GetString("content_hash")
	labelsBefore := storedLabels(t, app, id)

	t.Run("duplicate labels are rejected and stored labels stay unchanged", func(t *testing.T) {
		rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/labels", url.Values{
			"result_labels": {"Status,Status"},
		}, withCookie(cookie))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); !strings.Contains(body, "is used more than once") {
			t.Fatalf("inline duplicate message missing: %.600s", body)
		}
		if after := storedLabels(t, app, id); len(after) != len(labelsBefore) {
			t.Fatalf("labels changed to %v, want %v", after, labelsBefore)
		}
	})

	t.Run("valid update keeps the content hash", func(t *testing.T) {
		rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/labels", url.Values{
			"result_labels": {"Email Status, Submitted At, Email Error, Owner"},
		}, withCookie(cookie))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		if after := storedLabels(t, app, id); len(after) != 4 || after[3] != "Owner" {
			t.Fatalf("labels = %v, want four labels ending in Owner", after)
		}
		hashAfter := configurationRecord(t, app, configID).GetString("content_hash")
		if hashAfter != hashBefore {
			t.Fatalf("content hash changed: %q -> %q", hashBefore, hashAfter)
		}
		// Each section reflects its own change: new labels, same revision hash.
		page := do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
		if body := page.Body.String(); !strings.Contains(body, hashBefore) {
			t.Fatalf("template section missing the unchanged hash: %.600s", body)
		}
		page = do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionSettings), nil, withCookie(cookie))
		if body := page.Body.String(); !strings.Contains(body, "Owner") {
			t.Fatalf("settings section missing updated labels: %.600s", body)
		}
	})
}

// TestCampaignPreview covers behaviour 4: revision metadata plus the
// raw-versus-normalized table, and an inline (never 500) message when a
// required sample value is missing.
func TestCampaignPreview(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, "Interview invite {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	configID := currentConfigurationID(t, app, id)
	hash := configurationRecord(t, app, configID).GetString("content_hash")

	preview := func(values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		values.Set("candidate_email", " YASH@EXAMPLE.COM ")
		return do(t, handler, http.MethodPost, "/campaigns/"+id+"/preview", values, withCookie(cookie))
	}

	t.Run("renders revision metadata and raw versus normalized values", func(t *testing.T) {
		rec := preview(url.Values{"candidate_name": {"  yash   kumar  "}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{
			"Preview result",
			">To</p>",
			"yash@example.com",
			"Revision:",
			"<dd>1</dd>",
			hash,
			`status-warning">Not approved`,
			"Raw value",
			"Used value",
			"yash   kumar",
			"Dear   yash   kumar",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("preview missing %q: %.900s", want, body)
			}
		}
	})

	t.Run("missing required sample value is an inline error", func(t *testing.T) {
		rec := preview(url.Values{})
		if rec.Code >= 500 {
			t.Fatalf("missing sample rendered a server error: status=%d body=%.400s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "missing required value(s): candidate_name") || !strings.Contains(body, `data-slot="alert"`) || !strings.Contains(body, `role="alert"`) {
			t.Fatalf("inline missing-value message absent: %.600s", body)
		}
		// The revision metadata stays visible on the error path.
		if !strings.Contains(body, hash) {
			t.Fatalf("error path lost the content hash: %.600s", body)
		}
	})
}

// TestCampaignDetailApprovalAndHistory covers behaviour 5: the detail page
// shows ApprovalCurrent() and lists older revisions read-only.
func TestCampaignDetailApprovalAndHistory(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	if rec := postRevision(t, handler, cookie, id, "First {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("first revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	firstID := currentConfigurationID(t, app, id)
	firstHash := configurationRecord(t, app, firstID).GetString("content_hash")

	if rec := postRevision(t, handler, cookie, id, "Second {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("second revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	secondID := currentConfigurationID(t, app, id)
	second := configurationRecord(t, app, secondID)
	rec := do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`status-warning">Not approved`,
		"Revision 1",
		"Revision 2",
		firstHash,
		second.GetString("content_hash"),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail page missing %q: %.900s", want, body)
		}
	}
	// The history is read-only: the template section's forms are fixed
	// (sign-out, revision upload and sample preview), never one per stored
	// revision.
	if forms := strings.Count(body, "<form"); forms != 3 {
		t.Fatalf("template section has %d forms, want 3 fixed forms", forms)
	}
}

// mappingTextarea returns the mapping form's textarea value as the page HTML
// carries it.
func mappingTextarea(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `name="mappings"`)
	if !ok {
		t.Fatalf("no mapping textarea in page: %.400s", body)
	}
	_, rest, ok = strings.Cut(rest, ">")
	if !ok {
		t.Fatalf("mapping textarea has no body: %.400s", body)
	}
	value, _, ok := strings.Cut(rest, "</textarea>")
	if !ok {
		t.Fatalf("mapping textarea is not closed: %.400s", body)
	}
	return value
}

// TestCampaignMappingDraft covers the pre-filled mapping table: a campaign
// without a revision offers its expected headers as required text rows, a
// revision-backed campaign offers none, and a failed submission keeps the
// operator's own text.
func TestCampaignMappingDraft(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)

	rec := do(t, handler, http.MethodPost, "/campaigns", url.Values{
		"name":           {"Draft"},
		"source_headers": {"Candidate Name, Candidate Email, Expected Department/Division"},
	}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create campaign: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	id, _, _ := strings.Cut(strings.TrimPrefix(rec.Header().Get("Location"), "/campaigns/"), "/")

	draft := "candidate_name | Candidate Name | required\n" +
		"candidate_email | Candidate Email | required\n" +
		"expected_department_division | Expected Department/Division | required"

	page := do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("GET campaign: status=%d body=%.400s", page.Code, page.Body.String())
	}
	if got := mappingTextarea(t, page.Body.String()); got != draft {
		t.Fatalf("mapping draft = %q, want %q", got, draft)
	}
	if got := currentConfigurationID(t, app, id); got != "" {
		t.Fatalf("loading the page created revision %q", got)
	}

	// A rejected submission keeps the operator's own lines, not the draft.
	submitted := "candidate_name - Applicant Full Name"
	rec = postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", submitted, docxUpload(t))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("failed revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if got := mappingTextarea(t, rec.Body.String()); got != submitted {
		t.Fatalf("mapping textarea after failure = %q, want the submitted %q", got, submitted)
	}

	// A campaign with a revision gets no draft: the operator's mappings live in
	// the revision.
	if rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	page = do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("GET revision campaign: status=%d body=%.400s", page.Code, page.Body.String())
	}
	if got := mappingTextarea(t, page.Body.String()); got != "" {
		t.Fatalf("mapping textarea = %q, want empty once a revision exists", got)
	}
}

// TestCampaignMappingHelpExample covers the worked example beside the mapping
// form: it follows the campaign's own expected headers so the help never
// teaches a variable name the draft does not use, and it falls back to a
// generic name when no header can form one.
func TestCampaignMappingHelpExample(t *testing.T) {
	_, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)

	tests := []struct {
		name   string
		header string
		line   string
	}{
		{name: "campaign headers", header: "Candidate Name, Candidate Email", line: "candidate_name | Candidate Name | required"},
		{name: "no usable header", header: "----, 2024 Region", line: "variable_name | Header | required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := do(t, handler, http.MethodPost, "/campaigns", url.Values{
				"name":           {test.name},
				"source_headers": {test.header},
			}, withCookie(cookie))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("create campaign: status=%d body=%.400s", rec.Code, rec.Body.String())
			}
			id, _, _ := strings.Cut(strings.TrimPrefix(rec.Header().Get("Location"), "/campaigns/"), "/")

			page := do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
			if page.Code != http.StatusOK {
				t.Fatalf("GET campaign: status=%d body=%.400s", page.Code, page.Body.String())
			}
			body := page.Body.String()
			if !strings.Contains(body, test.line) {
				t.Fatalf("example line %q missing: %.600s", test.line, body)
			}
			if !strings.Contains(body, "must match the") {
				t.Fatalf("hint does not state the placeholder rule: %.600s", body)
			}
			// The shipped example name must not survive anywhere on the page: it
			// is the name operators copied into their documents.
			if strings.Contains(body, "full_name") {
				t.Fatalf("page still shows the generic full_name example: %.600s", body)
			}
		})
	}
}

// New authoring rejects a legacy format-shaped line instead of reading the
// former format column as fallback text.
func TestCampaignMappingRejectsLegacyFormatShape(t *testing.T) {
	_, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Legacy shape")
	for label, raw := range map[string]string{
		"five columns":   "candidate_name | Applicant Full Name | required | name | Remote",
		"format in four": "candidate_name | Applicant Full Name | required | name",
	} {
		t.Run(label, func(t *testing.T) {
			rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", raw, docxUpload(t))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, "format") {
				t.Fatalf("remedy does not name the removed format column: %.600s", body)
			}
		})
	}
}

func TestCampaignRecipientMapping(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Recipient mapping")
	name := "candidate_name | Applicant Full Name | required"
	for label, raw := range map[string]string{
		"missing recipient":  name,
		"two recipients":     name + "\n" + recipientMapping + "\nother_email | Other Email | recipient",
		"conflicting format": name + "\ncandidate_email | Email | recipient | name",
		"recipient fallback": name + "\ncandidate_email | Email | recipient | fallback@example.com",
	} {
		t.Run(label, func(t *testing.T) {
			rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", raw, docxUpload(t))
			if rec.Code != http.StatusBadRequest || currentConfigurationID(t, app, id) != "" {
				t.Fatalf("invalid recipient mapping: status=%d current=%q", rec.Code, currentConfigurationID(t, app, id))
			}
			if !strings.Contains(rec.Body.String(), "recipient") && !strings.Contains(rec.Body.String(), "fallback") {
				t.Fatalf("missing actionable validation message: %.600s", rec.Body.String())
			}
		})
	}
	// Omitting the recipient's format defaults it to email; the DOCX uses only candidate_name.
	rec := postRevision(t, handler, cookie, id, "Hello {{candidate_name}}", name+"\ncandidate_email | Email | recipient", docxUpload(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("recipient without body placeholder: status=%d body=%.600s", rec.Code, rec.Body.String())
	}
	page := do(t, handler, http.MethodGet, views.CampaignSectionPath(id, views.CampaignSectionTemplate), nil, withCookie(cookie))
	if !strings.Contains(page.Body.String(), `name="candidate_email"`) {
		t.Fatal("sample input for recipient-only variable is missing")
	}
	rec = do(t, handler, http.MethodPost, "/campaigns/"+id+"/preview", url.Values{
		"candidate_name": {"Ada"}, "candidate_email": {"Ada <ada@example.com>"},
	}, withCookie(cookie))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), campaign.ErrRecipientInvalid.Error()) {
		t.Fatalf("invalid recipient preview: status=%d body=%.600s", rec.Code, rec.Body.String())
	}
}
