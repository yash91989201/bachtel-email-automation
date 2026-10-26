package web

// Task naming on the operator surface: the optional upload field, its draft
// retention on every refused upload, and the saved name in task/list views.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestTaskUploadNameFlow(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "Engineers")
	data := taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name"},
		{"ada@example.com", "Ada Lovelace"},
	})

	form := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/new")
	for _, want := range []string{"Task name (optional)", "Leave blank to use the workbook filename", "255 characters"} {
		if !strings.Contains(form, want) {
			t.Errorf("upload form is missing %q", want)
		}
	}
	if strings.Contains(form, "maxlength") {
		t.Error("the upload name must not carry a client-side length limit")
	}

	named := uploadSubmission(t, handler, cookie, id,
		url.Values{"sending_type": {"direct"}, "name": {"  October outreach  "}}, "recipients.xlsx", data)
	if named.Code != http.StatusSeeOther {
		t.Fatalf("named upload: status=%d body=%.400s", named.Code, named.Body.String())
	}
	namedID := taskIDFromRedirect(t, named, id)
	page := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/"+namedID)
	if !strings.Contains(page, "October outreach") {
		t.Error("the saved task name is missing from the task page")
	}
	list := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks")
	for _, want := range []string{"October outreach", "Workbook recipients.xlsx"} {
		if !strings.Contains(list, want) {
			t.Errorf("task list is missing %q", want)
		}
	}

	unnamed := uploadSubmission(t, handler, cookie, id,
		url.Values{"sending_type": {"direct"}}, "blank-name.xlsx", data)
	if unnamed.Code != http.StatusSeeOther {
		t.Fatalf("unnamed upload: status=%d body=%.400s", unnamed.Code, unnamed.Body.String())
	}
	if list := fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks"); !strings.Contains(list, "blank-name.xlsx") {
		t.Error("a blank name must default to the workbook filename")
	}

	before := taskCount(t, app)
	tooLong := strings.Repeat("é", 256)
	refused := uploadSubmission(t, handler, cookie, id,
		url.Values{"sending_type": {"direct"}, "name": {tooLong}}, "book.xlsx", data)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("long name: status=%d, want 400", refused.Code)
	}
	for _, want := range []string{"at most 255 characters", tooLong} {
		if !strings.Contains(refused.Body.String(), want) {
			t.Errorf("refused upload is missing %q", want)
		}
	}
	if after := taskCount(t, app); after != before {
		t.Fatalf("refused name created a task: before=%d after=%d", before, after)
	}

	for _, tc := range []struct {
		name   string
		fields url.Values
		file   string
		data   []byte
		want   string
	}{
		{
			name:   "missing sending preference",
			fields: url.Values{"name": {"Draft name"}},
			file:   "book.xlsx", data: data,
			want: "choose Direct or Scheduled sending",
		},
		{
			name:   "missing workbook",
			fields: url.Values{"sending_type": {"direct"}, "name": {"Draft name"}},
			want:   "choose an XLSX workbook",
		},
		{
			name:   "invalid workbook",
			fields: url.Values{"sending_type": {"direct"}, "name": {"Draft name"}},
			file:   "book.xlsx", data: []byte("not a workbook"),
			want: "workbook",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := uploadSubmission(t, handler, cookie, id, tc.fields, tc.file, tc.data)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, `value="Draft name"`) {
				t.Error("the submitted name must be retained on the refused form")
			}
			if !strings.Contains(strings.ToLower(body), strings.ToLower(tc.want)) {
				t.Errorf("refused form is missing %q", tc.want)
			}
		})
	}

	markup := uploadSubmission(t, handler, cookie, id,
		url.Values{"sending_type": {"direct"}, "name": {"<b>October</b>"}}, "markup.xlsx", data)
	if markup.Code != http.StatusSeeOther {
		t.Fatalf("markup-name upload: status=%d body=%.400s", markup.Code, markup.Body.String())
	}
	markupID := taskIDFromRedirect(t, markup, id)
	page = fetchTaskPage(t, handler, cookie, "/campaigns/"+id+"/tasks/"+markupID)
	if strings.Contains(page, "<b>October</b>") {
		t.Error("a task name supplied active markup")
	}
	if !strings.Contains(page, "&lt;b&gt;October&lt;/b&gt;") {
		t.Error("markup-like task name must be displayed as text")
	}
}
