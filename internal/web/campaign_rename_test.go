package web

// Campaign rename contracts: a Settings POST/Redirect/GET that stores the
// trimmed name, fixed validation and storage failures, owner/origin refusals,
// and a rename that reaches every displayed name while links and record ids
// stay ID-based.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// pageHeading returns the normalized text of the page's first h1.
func pageHeading(t *testing.T, body string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for node := range doc.Descendants() {
		if node.Type == html.ElementNode && node.Data == "h1" {
			var text strings.Builder
			for child := range node.Descendants() {
				if child.Type == html.TextNode {
					text.WriteString(child.Data)
				}
			}
			return strings.Join(strings.Fields(text.String()), " ")
		}
	}
	return ""
}

// breadcrumbNav returns the breadcrumb navigation's markup so a test can
// assert a label or an ID-based href inside the trail itself.
func breadcrumbNav(t *testing.T, body string) string {
	t.Helper()
	at := strings.Index(body, `aria-label="breadcrumb"`)
	if at < 0 {
		t.Fatalf("no breadcrumb navigation in %.400s", body)
	}
	open := strings.LastIndex(body[:at], "<nav")
	end := strings.Index(body[at:], "</nav>")
	if open < 0 || end < 0 {
		t.Fatal("malformed breadcrumb navigation")
	}
	return body[open : at+end+len("</nav>")]
}

// TestCampaignNameSaveFlow covers the success path: a trimmed save through
// POST/Redirect/GET, duplicate names, and Settings loads that write nothing.
func TestCampaignNameSaveFlow(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "October outreach")
	namePath := "/campaigns/" + id + "/name"
	settings := views.CampaignSectionPath(id, views.CampaignSectionSettings)

	// state is everything a save may touch or a page load must leave alone:
	// the stored name, its record metadata, and the revision/attempt/task
	// counts a rename must never create.
	type state struct {
		name          string
		updated       string
		configuration string
		revisions     int64
		attempts      int64
		tasks         int64
	}
	read := func() state {
		t.Helper()
		record := campaignRecord(t, app, id)
		out := state{
			name:          record.GetString("name"),
			updated:       record.GetString("updated"),
			configuration: record.GetString("current_configuration"),
		}
		for _, entry := range []struct {
			collection string
			count      *int64
		}{
			{collection: campaign.ConfigurationsCollection, count: &out.revisions},
			{collection: sending.AttemptsCollection, count: &out.attempts},
			{collection: intake.TasksCollection, count: &out.tasks},
		} {
			count, err := app.CountRecords(entry.collection)
			if err != nil {
				t.Fatal(err)
			}
			*entry.count = count
		}
		return out
	}

	before := read()
	rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"  November outreach  "}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != settings+"?saved=1" {
		t.Fatalf("rename: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	after := read()
	if after.name != "November outreach" {
		t.Fatalf("stored name = %q, want the trimmed name", after.name)
	}
	if after.revisions != before.revisions || after.attempts != before.attempts || after.tasks != before.tasks {
		t.Fatalf("success rename created revisions/attempts/tasks: %+v -> %+v", before, after)
	}

	// The follow-up page announces the save and prefills the saved name.
	saved := fetchTaskPage(t, handler, cookie, settings+"?saved=1")
	for _, want := range []string{"Campaign name saved", "Required. Up to 200 characters. Another campaign may share this name."} {
		if !strings.Contains(saved, want) {
			t.Errorf("success page missing %q", want)
		}
	}
	if tag := inputTag(saved, "campaign-name-input"); !strings.Contains(tag, `value="November outreach"`) {
		t.Errorf("success page input = %s, want the saved name", tag)
	}
	if heading := pageHeading(t, saved); heading != "November outreach" {
		t.Errorf("success page heading = %q, want the saved name", heading)
	}

	// Re-saving the same name and a second campaign choosing the same name
	// both succeed: campaign identity is the record id, not the name.
	if rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"November outreach"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-name save: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	duplicate := createCampaign(t, handler, cookie, "Duplicate target")
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+duplicate+"/name", url.Values{"name": {"November outreach"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("duplicate-name save: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	if got := campaignRecord(t, app, duplicate).GetString("name"); got != "November outreach" {
		t.Fatalf("duplicate campaign name = %q, want the shared name", got)
	}

	// Two plain Settings loads are read-only: no name, metadata, count or
	// processing side effect.
	beforeGET := read()
	plain := fetchTaskPage(t, handler, cookie, settings)
	if tag := inputTag(plain, "campaign-name-input"); !strings.Contains(tag, `value="November outreach"`) {
		t.Errorf("plain Settings input = %s, want the saved name", tag)
	}
	fetchTaskPage(t, handler, cookie, settings+"?saved=1")
	if got := read(); got != beforeGET {
		t.Errorf("Settings GET wrote state: %+v -> %+v", beforeGET, got)
	}
}

// TestCampaignNameRefusalsAndStorageFailure covers the refused saves:
// validation, a failing storage write, and anonymous, foreign, missing or
// cross-origin requests all answer with the documented status and leave the
// stored name untouched.
func TestCampaignNameRefusalsAndStorageFailure(t *testing.T) {
	app, handler, _ := newCampaignServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "October outreach")
	namePath := "/campaigns/" + id + "/name"

	stored := func() string {
		t.Helper()
		return campaignRecord(t, app, id).GetString("name")
	}
	// refused asserts one error response: the Settings panel with its General
	// card, the submitted draft kept in the input, the saved name still in the
	// h1, and the stored name unchanged.
	refused := func(rec *httptest.ResponseRecorder, wantStatus int, draft, message string) {
		t.Helper()
		if rec.Code != wantStatus {
			t.Fatalf("refused save: status=%d, want %d body=%.400s", rec.Code, wantStatus, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{message, `id="campaign-settings-workspace"`, "General", "Save name"} {
			if !strings.Contains(body, want) {
				t.Errorf("refused page missing %q", want)
			}
		}
		if tag := inputTag(body, "campaign-name-input"); !strings.Contains(tag, `value="`+draft+`"`) {
			t.Errorf("refused page input = %s, want draft %q", tag, draft)
		}
		if heading := pageHeading(t, body); heading != "October outreach" {
			t.Errorf("refused page heading = %q, want the saved name", heading)
		}
		if got := stored(); got != "October outreach" {
			t.Fatalf("refused save changed the stored name to %q", got)
		}
	}

	refused(do(t, handler, http.MethodPost, namePath, url.Values{"name": {"   "}}, withCookie(cookie)),
		http.StatusBadRequest, "   ", "a campaign name is required")

	overlong := strings.Repeat("é", campaign.MaxCampaignNameLength+1)
	refused(do(t, handler, http.MethodPost, namePath, url.Values{"name": {overlong}}, withCookie(cookie)),
		http.StatusBadRequest, overlong, "a campaign name can be at most 200 characters")

	// A failing store answers 500 with the fixed retry text, keeps the draft
	// and never changes the stored name.
	if _, err := app.DB().NewQuery(`
		CREATE TRIGGER fail_campaign_name_update BEFORE UPDATE OF name ON campaigns
		BEGIN SELECT RAISE(ABORT, 'storage failure'); END
	`).Execute(); err != nil {
		t.Skipf("this SQLite build cannot install a failure trigger: %v", err)
	}
	rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Draft name"}}, withCookie(cookie))
	if _, err := app.DB().NewQuery("DROP TRIGGER fail_campaign_name_update").Execute(); err != nil {
		t.Fatal(err)
	}
	refused(rec, http.StatusInternalServerError, "Draft name",
		"The campaign name could not be saved; check the name and try again.")

	// Anonymous, foreign, missing and cross-origin requests change nothing.
	if rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Anonymous"}}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous save = %d, want 401", rec.Code)
	}
	provisionOperatorAs(t, app, "second@example.com", "second-password")
	foreign := loginAs(t, handler, "second@example.com", "second-password")
	if rec := do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Foreign"}}, withCookie(foreign)); rec.Code != http.StatusNotFound {
		t.Errorf("foreign save = %d, want 404", rec.Code)
	}

	campaignCount := func() int64 {
		t.Helper()
		count, err := app.CountRecords(campaign.CampaignsCollection)
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	beforeCount := campaignCount()
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+strings.Repeat("f", 15)+"/name",
		url.Values{"name": {"Ghost"}}, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Errorf("missing campaign save = %d, want 404", rec.Code)
	}
	if campaignCount() != beforeCount {
		t.Error("a refused save created a campaign")
	}

	rec = do(t, handler, http.MethodPost, namePath, url.Values{"name": {"Cross-site"}}, func(r *http.Request) {
		r.AddCookie(cookie)
		r.Header.Set("Origin", "http://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin save = %d, want 403", rec.Code)
	}
	if got := stored(); got != "October outreach" {
		t.Fatalf("a refused request changed the stored name to %q", got)
	}
}

// TestCampaignRenameKeepsIdentityAcrossPages covers the rename regression
// surface: lists, headings, breadcrumbs and task context show the new name
// while task identity, workbook detail and every ID-based URL stay put.
func TestCampaignRenameKeepsIdentityAcrossPages(t *testing.T) {
	app, handler := newTaskServer(t)
	cookie := login(t, handler)
	id := createCampaign(t, handler, cookie, "October outreach")
	if rec := postRevision(t, handler, cookie, id, taskSubject, validMappings, docxUpload(t)); rec.Code != http.StatusSeeOther {
		t.Fatalf("create revision: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	taskID := stageTask(t, handler, cookie, id, mixedWorkbook(t))

	task, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	taskName := task.GetString("name")
	configuration := campaignRecord(t, app, id).GetString("current_configuration")
	taskURL := "/campaigns/" + id + "/tasks/" + taskID
	tasksURL := views.CampaignSectionPath(id, views.CampaignSectionTasks)
	settingsURL := views.CampaignSectionPath(id, views.CampaignSectionSettings)
	resultsBefore := do(t, handler, http.MethodGet, taskURL+"/results.xlsx", nil, withCookie(cookie)).Code

	rec := do(t, handler, http.MethodPost, "/campaigns/"+id+"/name", url.Values{"name": {"November outreach"}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != settingsURL+"?saved=1" {
		t.Fatalf("rename: status=%d location=%q body=%.400s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	// The campaign list shows the new name under the campaign's own id.
	list := fetchTaskPage(t, handler, cookie, "/campaigns")
	if !strings.Contains(list, `href="/campaigns/`+id+`">November outreach`) {
		t.Error("campaign list does not show the renamed campaign under its id")
	}
	if strings.Contains(list, "October outreach") {
		t.Error("campaign list still shows the old name")
	}

	// The Tasks section heading and breadcrumb use the new name; the task row
	// and section navigation keep ID-based URLs.
	tasksPage := fetchTaskPage(t, handler, cookie, tasksURL)
	if heading := pageHeading(t, tasksPage); heading != "November outreach" {
		t.Errorf("Tasks heading = %q, want the new campaign name", heading)
	}
	if crumbs := breadcrumbNav(t, tasksPage); !strings.Contains(crumbs, "November outreach") || !strings.Contains(crumbs, `href="/campaigns"`) {
		t.Errorf("Tasks breadcrumb missing the new name or its ID-based links: %s", crumbs)
	}
	for _, link := range []string{
		`href="/campaigns/` + id + `/tasks/` + taskID,
		`href="` + settingsURL + `"`,
		`href="` + tasksURL + `"`,
	} {
		if !strings.Contains(tasksPage, link) {
			t.Errorf("Tasks page lost ID-based link %s", link)
		}
	}

	// The task page shows the renamed campaign in its breadcrumb and Campaign
	// detail; the task heading and workbook detail stay unchanged.
	taskPage := fetchTaskPage(t, handler, cookie, taskURL)
	if heading := pageHeading(t, taskPage); heading != taskName {
		t.Errorf("task heading = %q, want the unchanged task name %q", heading, taskName)
	}
	crumbs := breadcrumbNav(t, taskPage)
	for _, want := range []string{"November outreach", `href="/campaigns/` + id + `"`, `href="` + tasksURL + `"`, taskName} {
		if !strings.Contains(crumbs, want) {
			t.Errorf("task breadcrumb missing %q: %s", want, crumbs)
		}
	}
	for _, want := range []string{`<dt>Workbook</dt><dd>book.xlsx</dd>`, `href="` + tasksURL + `">November outreach</a>`} {
		if !strings.Contains(taskPage, want) {
			t.Errorf("task page missing %q", want)
		}
	}
	if !strings.Contains(taskPage, `href="`+taskURL+`/settings"`) {
		t.Errorf("task page lost the ID-based settings link %s/settings", taskURL)
	}

	// Protected URLs still resolve after the rename.
	for _, path := range []string{taskURL, taskURL + "/settings", settingsURL, tasksURL} {
		if code := do(t, handler, http.MethodGet, path, nil, withCookie(cookie)).Code; code != http.StatusOK {
			t.Errorf("GET %s after rename = %d, want 200", path, code)
		}
	}
	// A staging task has no results workbook yet, so not-found is the normal
	// answer: the status must be identical before and after the rename and
	// never an authentication or origin refusal.
	resultsAfter := do(t, handler, http.MethodGet, taskURL+"/results.xlsx", nil, withCookie(cookie)).Code
	if resultsAfter != resultsBefore || resultsAfter == http.StatusUnauthorized || resultsAfter == http.StatusForbidden {
		t.Errorf("results.xlsx before=%d after=%d", resultsBefore, resultsAfter)
	}

	// The existing records keep their ids, relations and revision pointer.
	renamed := campaignRecord(t, app, id)
	if renamed.Id != id || renamed.GetString("current_configuration") != configuration {
		t.Error("renaming changed the campaign record identity or revision pointer")
	}
	taskAfter, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if taskAfter.Id != taskID || taskAfter.GetString("campaign") != id || taskAfter.GetString("name") != taskName {
		t.Error("renaming changed the task record identity, campaign link or name")
	}

	// Two campaigns renamed to one name stay distinguishable by their distinct
	// ID-based hrefs, headings and settings actions.
	second := createCampaign(t, handler, cookie, "Contractors")
	if rec := do(t, handler, http.MethodPost, "/campaigns/"+second+"/name", url.Values{"name": {"November outreach"}}, withCookie(cookie)); rec.Code != http.StatusSeeOther {
		t.Fatalf("second rename: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	list = fetchTaskPage(t, handler, cookie, "/campaigns")
	for _, pair := range [][2]string{{id, second}, {second, id}} {
		if !strings.Contains(list, `href="/campaigns/`+pair[0]+`">November outreach`) {
			t.Errorf("campaign list does not show campaign %s under its id", pair[0])
		}
		page := fetchTaskPage(t, handler, cookie, views.CampaignSectionPath(pair[0], views.CampaignSectionSettings))
		if heading := pageHeading(t, page); heading != "November outreach" {
			t.Errorf("campaign %s heading = %q, want the shared name", pair[0], heading)
		}
		if !strings.Contains(page, `action="/campaigns/`+pair[0]+`/name"`) {
			t.Errorf("campaign %s settings page does not post to its own id", pair[0])
		}
		if strings.Contains(page, `action="/campaigns/`+pair[1]+`/name"`) {
			t.Errorf("campaign %s settings page leaks campaign %s", pair[0], pair[1])
		}
	}
}
