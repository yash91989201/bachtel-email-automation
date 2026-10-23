package web

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/convert"
)

// TestAcceptanceCampaignAuthoringFlow drives the whole authoring journey over
// real HTTP against a real PocketBase app and the packaged DOCX converter:
// create a campaign, upload a body document, see the converted revision, render
// a sample preview from the same entry point sending will use, and confirm that
// a presentation-only change cannot alter the personalized content.
//
// The test is skipped when no converter is installed; `make verify-converter`
// runs it inside the application image where the pinned Pandoc is packaged.
func TestAcceptanceCampaignAuthoringFlow(t *testing.T) {
	if _, err := (convert.Converter{}).Executable(); err != nil {
		t.Skipf("skipping: %v", err)
	}
	env := newAcceptanceEnv(t)
	session := env.signIn()
	cookies := []*http.Cookie{session}

	// Create the campaign: output settings only, no candidate data.
	rec := env.request(http.MethodPost, "/campaigns", cookies, url.Values{
		"name":           {"Software Engineers"},
		"source_headers": {"Email"},
		"result_labels":  {"Status, Submitted At"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create campaign: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	detail, _, _ := strings.Cut(rec.Header().Get("Location"), "#")
	if !strings.HasPrefix(detail, "/campaigns/") {
		t.Fatalf("create campaign location = %q", detail)
	}

	// Upload the body document and the mapping table in one multipart form.
	subject := "Application for {{candidate_name}}"
	mappings := strings.Join([]string{
		"candidate_name | Applicant Full Name | required |",
		"location | Location | optional | Remote",
		recipientMapping,
		"",
	}, "\n")
	fields := map[string]string{"subject": subject, "mappings": mappings}
	rec = env.requestMultipart(http.MethodPost, detail+"/revisions", cookies, fields, "body.docx", fixtureDocument(t))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.600s", rec.Code, rec.Body.String())
	}

	page := env.request(http.MethodGet, detail, cookies, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("campaign page: status=%d", page.Code)
	}
	body := page.Body.String()
	// The stored revision is visible with its identity and unapproved state.
	for _, want := range []string{"Not approved", subject, "pandoc"} {
		if !strings.Contains(body, want) {
			t.Fatalf("campaign page missing %q: %.800s", want, body)
		}
	}
	// A literal "Subject:" line inside the document stays body content: the
	// configured subject is the only email subject.
	revision := storedRevision(t, env, detail)
	if got := revision.GetString("subject"); got != subject {
		t.Fatalf("stored subject = %q, want the configured %q", got, subject)
	}
	if converted := revision.GetString("body_html"); !strings.Contains(converted, "Subject: on-site interview") {
		t.Fatalf("converted body dropped the document's literal Subject line: %q", converted)
	}

	// The sample preview renders through the same entry point sending consumes.
	preview := env.request(http.MethodPost, detail+"/preview", cookies, url.Values{
		"candidate_email": {"ADA@EXAMPLE.COM"},
		"candidate_name":  {"  ada   lovelace "},
		"location":        {""},
	})
	if preview.Code != http.StatusOK {
		t.Fatalf("preview: status=%d body=%.400s", preview.Code, preview.Body.String())
	}
	previewBody := preview.Body.String()
	for _, want := range []string{
		"Application for   ada   lovelace ", // direct sample subject
		"  ada   lovelace ",                 // raw value preserved
		"Remote",                            // optional fallback
	} {
		if !strings.Contains(previewBody, want) {
			t.Fatalf("preview missing %q: %.800s", want, previewBody)
		}
	}
	// Placeholder source text must be gone from the rendered body.
	if strings.Contains(previewBody, "{{candidate_name}}") || strings.Contains(previewBody, "{{location}}") {
		t.Fatalf("preview left a placeholder unreplaced: %.800s", previewBody)
	}

	// An empty required value holds the row and names the variable inline.
	missing := env.request(http.MethodPost, detail+"/preview", cookies, url.Values{
		"candidate_email": {"ADA@EXAMPLE.COM"},
		"candidate_name":  {"   "},
		"location":        {"London"},
	})
	if missing.Code != http.StatusOK || !strings.Contains(missing.Body.String(), "missing required value(s): candidate_name") {
		t.Fatalf("missing required preview: status=%d body=%.400s", missing.Code, missing.Body.String())
	}

	// Understanding the configured variables: a placeholder without a mapping
	// keeps the configuration unready and reports the variable by name.
	badMapping := env.requestMultipart(http.MethodPost, detail+"/revisions", cookies, map[string]string{
		"subject":  "Application for {{candidate_name}} and {{level}}",
		"mappings": "candidate_name | Applicant Full Name | required |\n" + recipientMapping,
	}, "body.docx", fixtureDocument(t))
	if badMapping.Code != http.StatusBadRequest || !strings.Contains(badMapping.Body.String(), "level") {
		t.Fatalf("unmapped variable: status=%d body=%.600s", badMapping.Code, badMapping.Body.String())
	}

	// A presentation-only change keeps the personalized content hash intact.
	hashBefore := storedContentHash(t, env, detail)
	if rec := env.request(http.MethodPost, detail+"/labels", cookies, url.Values{"result_labels": {"Status, Submitted At, Notes"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("labels: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	hashAfter := storedContentHash(t, env, detail)
	if hashBefore == "" || hashBefore != hashAfter {
		t.Fatalf("content hash changed with the result labels: %q -> %q", hashBefore, hashAfter)
	}
	if rec := env.request(http.MethodPost, detail+"/labels", cookies, url.Values{"result_labels": {"Status, Status"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate labels: status=%d, want 400", rec.Code)
	}
}

// storedRevision returns the campaign's current configuration record, so
// assertions cannot be satisfied by page text alone.
func storedRevision(t *testing.T, env *acceptanceEnv, detail string) *core.Record {
	t.Helper()
	loaded, err := env.app.FindRecordById("campaigns", strings.TrimPrefix(detail, "/campaigns/"))
	if err != nil {
		t.Fatal(err)
	}
	revisionID := loaded.GetString("current_configuration")
	if revisionID == "" {
		t.Fatal("campaign has no current configuration")
	}
	revision, err := env.app.FindRecordById("campaign_configurations", revisionID)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

// storedContentHash reads the current revision's content hash straight from the
// database, so the assertion cannot be satisfied by page text alone.
func storedContentHash(t *testing.T, env *acceptanceEnv, detail string) string {
	t.Helper()
	return storedRevision(t, env, detail).GetString("content_hash")
}

// requestMultipart posts one multipart form with a single file field, using the
// same browser-equivalent origin headers as request.
func (env *acceptanceEnv) requestMultipart(method, target string, cookies []*http.Cookie, fields map[string]string, fileName string, file []byte) *httptest.ResponseRecorder {
	env.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			env.t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("document", fileName)
	if err != nil {
		env.t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		env.t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		env.t.Fatal(err)
	}

	req := httptest.NewRequest(method, target, &body)
	req.Host = strings.TrimPrefix(testOrigin, "http://")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)
	return rec
}

// fixtureDocument builds a small DOCX package with the supported semantics and
// a placeholder split across two runs, so the upload exercises conversion,
// sanitization and the run join end to end.
func fixtureDocument(t *testing.T) []byte {
	t.Helper()
	const document = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Application for {{candidate_name}}</w:t></w:r></w:p>
<w:p><w:r><w:t>Subject: on-site interview</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Dear {{cand</w:t></w:r><w:r><w:t xml:space="preserve">idate_name}},</w:t></w:r><w:r><w:t xml:space="preserve"> we received your application</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>Preferred location:</w:t></w:r><w:r><w:t xml:space="preserve"> {{location}}</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Reply with your availability</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>Attach your portfolio</w:t></w:r></w:p>
</w:body></w:document>`
	const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`
	const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
	const documentRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`
	const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/></w:style>
</w:styles>`
	const numbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/><w:lvlJc w:val="left"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"word/document.xml", document},
		{"word/_rels/document.xml.rels", documentRels},
		{"word/styles.xml", styles},
		{"word/numbering.xml", numbering},
	} {
		writer, err := archive.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
