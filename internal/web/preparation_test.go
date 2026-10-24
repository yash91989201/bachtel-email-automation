package web

// Preparation operator-surface tests. They drive the real HTTP handler, a real
// PocketBase app on temporary data and a simulated provider; no email is sent
// and no live credential is used.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// preparationModel simulates the Responses API for the web workflow.
type preparationModel struct {
	server *httptest.Server
	mu     sync.Mutex
	rows   int
}

func newPreparationModel(t *testing.T) *preparationModel {
	t.Helper()
	model := &preparationModel{}
	model.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		user := ""
		if len(body.Input) > 1 && len(body.Input[1].Content) > 0 {
			user = body.Input[1].Content[0].Text
		}
		text := `{"values":[{"id":"probe","value":"probe@example.test"}]}`
		if !strings.Contains(user, `"probe"`) {
			var payload preparation.RequestPayload
			if err := json.Unmarshal([]byte(user), &payload); err != nil {
				t.Errorf("decode preparation payload: %v", err)
			}
			model.mu.Lock()
			model.rows += len(payload.Rows)
			model.mu.Unlock()
			rows := make([]map[string]any, 0, len(payload.Rows))
			for _, row := range payload.Rows {
				rows = append(rows, map[string]any{"id": row.ID, "values": row.Values})
			}
			encoded, err := json.Marshal(map[string]any{"rows": rows})
			if err != nil {
				t.Errorf("encode response: %v", err)
			}
			text = string(encoded)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"output": []map[string]any{{
				"type":    "message",
				"content": []map[string]string{{"type": "output_text", "text": text}},
			}},
		}); err != nil {
			t.Errorf("encode envelope: %v", err)
		}
	}))
	t.Cleanup(model.server.Close)
	return model
}

type preparationServer struct {
	app      *pocketbase.PocketBase
	handler  http.Handler
	services Services
	owner    string
	campaign string
	revision string
	task     *intake.Task
	model    *preparationModel
}

func newPreparationServer(t *testing.T) *preparationServer {
	t.Helper()
	return newPreparationServerFor(t, campaign.MappingVersionLegacy, [][]string{
		{"Email", "Applicant Full Name", "Department"},
		{"alice@example.com", "Alice Smith", "Engineering"},
		{"bob@example.com", "Bob Stone", "Sales"},
	})
}

// newRolePreparationServer builds the same workflow for a current role-mapping
// revision, where validation records source rows only and the prepared review is
// the first backend classification.
func newRolePreparationServer(t *testing.T) *preparationServer {
	t.Helper()
	return newPreparationServerFor(t, campaign.CurrentMappingVersion, [][]string{
		{"Email", "Applicant Full Name", "Department"},
		{"alice@example.com", "Alice Smith", "Engineering"},
		{"bob@example.com", "", "Sales"},
	})
}

// newPreparationServerFor provisions the AI connection, one revision and one
// validated task for a stub provider. The rows are the workbook's data rows.
func newPreparationServerFor(t *testing.T, mappingVersion int, rows [][]string) *preparationServer {
	t.Helper()
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.PandocPath = stubPandoc(t)
	cfg.Intake = intake.DefaultLimits()
	app, handler, services := newTestServerWithServices(t, cfg)
	provisionOperator(t, app)
	owner := operatorID(t, app)
	ctx := context.Background()
	campaigns := campaign.NewService(app, cfg.PandocPath)
	tasks := intake.NewService(app, campaigns, cfg.Intake)

	created, err := campaigns.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Preparation campaign",
		SourceHeaders: []string{"Email", "Applicant Full Name", "Department"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mappings := []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "candidate_name", Header: "Applicant Full Name", Required: true},
		{Variable: "department", Header: "Department"},
	}
	if mappingVersion < campaign.MappingVersionRole {
		// A legacy revision stores an explicit format per mapping.
		mappings[0].Format = campaign.FormatEmail
		mappings[1].Format = campaign.FormatName
		mappings[2].Format = campaign.FormatText
	}
	revision, _, err := campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		Subject:        taskSubject + " — {{department}}",
		Document:       docxUpload(t),
		DocumentName:   "body.docx",
		Mappings:       mappings,
		MappingVersion: mappingVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	model := newPreparationModel(t)
	draft, err := services.AI.StageValidation(ctx, owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: model.server.URL + "/v1", APIFormat: ai.FormatResponses, Model: "web-model"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := services.AI.Confirm(ctx, owner, draft.ID, 0); err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Stage(ctx, owner, created.ID, "book.xlsx", taskWorkbook(t, "Sheet1", rows))
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.Validate(ctx, owner, task.ID, "Sheet1", 1); err != nil {
		t.Fatal(err)
	}
	loaded, err := tasks.LoadTask(ctx, owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	services.Preparation.SetOptions(preparation.Options{BatchSize: 20, MaxTokens: 2048})
	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		services.Preparation.Run(workerCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("preparation worker did not stop")
		}
	})
	return &preparationServer{
		app:      app,
		handler:  handler,
		services: services,
		owner:    owner,
		campaign: created.ID,
		revision: revision.ID,
		task:     loaded,
		model:    model,
	}
}

func (s *preparationServer) path(suffix string) string {
	return "/campaigns/" + s.campaign + "/tasks/" + s.task.ID + suffix
}

// pagePath is the Recipients section, where preparation controls live.
func (s *preparationServer) pagePath() string {
	return s.path("/" + views.TaskSectionRecipients)
}

func (s *preparationServer) latestRun(t *testing.T) *preparation.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := s.services.Preparation.RunList(context.Background(), s.owner, s.task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 && runs[0].State == preparation.RunCompleted {
			return runs[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("preparation run did not complete")
	return nil
}

func TestPreparationWorkflowInTaskWorkspace(t *testing.T) {
	server := newPreparationServer(t)
	cookie := login(t, server.handler)

	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/prepare"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	run := server.latestRun(t)
	if run.Model != "web-model" || run.State != preparation.RunCompleted {
		t.Fatalf("run = %+v", run)
	}

	candidatesBefore, err := server.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task = {:task}", "", 0, 0, dbx.Params{"task": server.task.ID})
	if err != nil {
		t.Fatal(err)
	}

	page := do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("task page: status=%d", page.Code)
	}
	body := page.Body.String()
	for _, want := range []string{
		"AI data preparation",
		"Prepare mapped field values",
		"Application base prompt · version 1",
		"Only campaign-mapped values are sent",
		"web-model",
		"Run preparation again",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("task page is missing %q", want)
		}
	}
	if strings.Contains(body, "Prepare mapped data</button>") {
		t.Fatal("an initial Prepare action remained visible after a completed run")
	}

	instructions := "Expand the Department abbreviation in this task."
	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/instructions"), url.Values{"instructions": {instructions}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save instructions: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	page = do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	if !strings.Contains(page.Body.String(), instructions) {
		t.Fatal("saved instructions are not displayed")
	}
	reloaded, err := server.services.Preparation.LoadRun(context.Background(), server.owner, server.task.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Instructions != "" {
		t.Fatal("saving a draft changed completed run evidence")
	}

	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/select"), url.Values{"run": {run.ID}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select run: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	page = do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	if !strings.Contains(page.Body.String(), "selected") {
		t.Fatal("selected run is not identified on the page")
	}

	candidatesAfter, err := server.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task = {:task}", "", 0, 0, dbx.Params{"task": server.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidatesAfter) != len(candidatesBefore) {
		t.Fatal("the preparation workflow changed legacy candidate rows")
	}
}

func TestPreparationRoutesRefuseForeignOperator(t *testing.T) {
	server := newPreparationServer(t)
	provisionOperatorAs(t, server.app, "foreign@example.com", "correct-horse-battery-staple")
	cookie := loginAs(t, server.handler, "foreign@example.com", "correct-horse-battery-staple")
	for _, suffix := range []string{"/preparation/prepare", "/preparation/retry", "/preparation/rerun"} {
		rec := do(t, server.handler, http.MethodPost, server.path(suffix), url.Values{}, withCookie(cookie))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s by a foreign operator: status=%d, want 404", suffix, rec.Code)
		}
	}
	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/instructions"), url.Values{"instructions": {"x"}}, withCookie(cookie))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign instructions: status=%d, want 404", rec.Code)
	}
}

// TestPreparationShowsStoredFindings seeds a completed run with a finding so the
// page rendering of corrections, findings and held entries is exercised.
func TestPreparationShowsStoredFindings(t *testing.T) {
	server := newPreparationServer(t)
	cookie := login(t, server.handler)
	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/prepare"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare: status=%d", rec.Code)
	}
	run := server.latestRun(t)

	record, err := server.app.FindRecordById(preparation.RunsCollection, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	output := preparation.Output{
		Version: preparation.OutputVersion,
		Rows: []preparation.RowResult{{
			SourceRow: 2,
			Values: map[string]string{
				"candidate_email": "alice@example.com",
				"candidate_name":  "Alice Smith",
				"department":      "6lpa",
			},
			Corrections: []preparation.Correction{{Row: 2, Field: "candidate_name", Header: "Applicant Full Name", Note: "restored spacing"}},
			Findings:    []preparation.Finding{{Row: 2, Field: "department", Header: "Department", Scope: preparation.ScopeShared, Note: "compensation in a department column"}},
		}},
		Entries: []preparation.Entry{{SourceRow: 2, Position: 1, Recipient: "alice@example.com", Outcome: preparation.OutcomeHeld, Reasons: []string{"Department: compensation in a department column"}}},
		Counts:  preparation.Counts{SourceRows: 1, Entries: 1, Held: 1, Corrections: 1, Findings: 1},
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("output_digest", "digest")
	record.Set("counts", output.Counts)
	file, err := filesystem.NewFileFromBytes(data, "preparation-output.json")
	if err != nil {
		t.Fatal(err)
	}
	record.Set("output", file)
	if err := server.app.Save(record); err != nil {
		t.Fatal(err)
	}

	page := do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	body := page.Body.String()
	for _, want := range []string{
		"compensation in a department column",
		"restored spacing",
		"Prepared entries held by findings",
		"Unresolved findings",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
}

// Publication selects the completed run without any explicit select request:
// the stored task points at the run, and the first page load already shows the
// selected review with approve form and both prepared downloads.
func TestPreparationAutoSelectionShowsReviewWithoutSelect(t *testing.T) {
	server := newRolePreparationServer(t)
	cookie := login(t, server.handler)

	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/prepare"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	run := server.latestRun(t)

	taskRecord, err := server.app.FindRecordById(intake.TasksCollection, server.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected := taskRecord.GetString("selected_preparation_run"); selected != run.ID {
		t.Fatalf("selected_preparation_run = %q, want auto-selected %q", selected, run.ID)
	}

	// No preparation/select request happens before this page load.
	page := do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("task page: status=%d", page.Code)
	}
	body := page.Body.String()
	heading := "Selected prepared data · backend validation"
	headingAt := strings.Index(body, heading)
	if headingAt < 0 {
		t.Fatalf("task page is missing the selected review heading %q: %.1500s", heading, body)
	}
	// The counts paragraph ends at the first </p> after the heading.
	reviewLine := body[headingAt:]
	if end := strings.Index(reviewLine, "</p>"); end >= 0 {
		reviewLine = reviewLine[:end]
	}
	for _, want := range []string{"source rows", "recipient entries", "accepted", "held"} {
		if !strings.Contains(reviewLine, want) {
			t.Fatalf("prepared review line is missing %q: %.400s", want, reviewLine)
		}
	}
	for _, want := range []string{
		`action="` + server.path("/finalization/approve") + `"`,
		`href="` + server.path("/preparation/sending.xlsx") + `"`,
		`href="` + server.path("/excluded.xlsx") + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("task page is missing %q: %.1500s", want, body)
		}
	}

	for _, suffix := range []string{"/preparation/sending.xlsx", "/excluded.xlsx"} {
		download := do(t, server.handler, http.MethodGet, server.path(suffix), nil, withCookie(cookie))
		if download.Code != http.StatusOK || download.Body.Len() == 0 {
			t.Fatalf("%s: status=%d len=%d body=%.300s", suffix, download.Code, download.Body.Len(), download.Body.String())
		}
	}
}

// Explicit selection stays available for history: re-selecting the selected run
// changes nothing, an older run still offers its select form after a rerun, and
// selecting that older run replaces a final snapshot by clearing it.
func TestPreparationExplicitSelectKeepsHistoryAndClearsSnapshot(t *testing.T) {
	server := newRolePreparationServer(t)
	cookie := login(t, server.handler)

	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/prepare"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	first := server.latestRun(t)

	// Re-selecting the automatically selected run still redirects and keeps
	// the stored selection.
	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/select"), url.Values{"run": {first.ID}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select current run: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	taskRecord, err := server.app.FindRecordById(intake.TasksCollection, server.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected := taskRecord.GetString("selected_preparation_run"); selected != first.ID {
		t.Fatalf("selected_preparation_run after reselect = %q, want %q", selected, first.ID)
	}

	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/rerun"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("rerun: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	second := server.latestRun(t)
	if second.ID == first.ID {
		t.Fatalf("rerun returned the first run %q", first.ID)
	}
	taskRecord, err = server.app.FindRecordById(intake.TasksCollection, server.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected := taskRecord.GetString("selected_preparation_run"); selected != second.ID {
		t.Fatalf("selected_preparation_run after rerun = %q, want the new run %q", selected, second.ID)
	}

	// The older run row still offers explicit selection.
	page := do(t, server.handler, http.MethodGet, server.pagePath(), nil, withCookie(cookie))
	if page.Code != http.StatusOK {
		t.Fatalf("task page: status=%d", page.Code)
	}
	selectForm := regexp.MustCompile(`<form method="post" action="[^"]*/preparation/select">\s*<input type="hidden" name="run" value="` + regexp.QuoteMeta(first.ID) + `"`)
	if !selectForm.MatchString(page.Body.String()) {
		t.Fatalf("older run %s has no select form on the page: %.1500s", first.ID, page.Body.String())
	}

	// A final upload selects a snapshot; selecting the older run clears it.
	uploadFinalRows(t, server.handler, cookie, server.campaign, server.task.ID, "auto-select-clear-final-123456", [][]string{
		{"Email", "Applicant Full Name", "Department"},
		{"alice@example.com", "Alice Smith", "Engineering"},
		{"not-an-address", "Carol Danvers", "Sales"},
	})
	taskRecord, err = server.app.FindRecordById(intake.TasksCollection, server.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if taskRecord.GetString("selected_final_snapshot") == "" {
		t.Fatal("final upload did not select a snapshot")
	}

	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/select"), url.Values{"run": {first.ID}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select older run: status=%d body=%.300s", rec.Code, rec.Body.String())
	}
	taskRecord, err = server.app.FindRecordById(intake.TasksCollection, server.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if selected := taskRecord.GetString("selected_preparation_run"); selected != first.ID {
		t.Fatalf("selected_preparation_run = %q, want older run %q", selected, first.ID)
	}
	if snapshot := taskRecord.GetString("selected_final_snapshot"); snapshot != "" {
		t.Fatalf("selected_final_snapshot = %q, want empty", snapshot)
	}
	if digest := taskRecord.GetString("final_snapshot_digest"); digest != "" {
		t.Fatalf("final_snapshot_digest = %q, want empty", digest)
	}
}

// The exclusion download resolves the selected final snapshot first, then the
// selected prepared review, and is unavailable before either exists.
func TestRoleMappingExclusionDownloadPrecedence(t *testing.T) {
	server := newRolePreparationServer(t)
	cookie := login(t, server.handler)
	reportPath := server.path("/excluded.xlsx")

	// Preflight records source rows only: there is no report to download.
	if rec := do(t, server.handler, http.MethodGet, reportPath, nil, withCookie(cookie)); rec.Code != http.StatusNotFound {
		t.Fatalf("preflight report download = %d, want 404", rec.Code)
	}

	rec := do(t, server.handler, http.MethodPost, server.path("/preparation/prepare"), url.Values{}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("prepare: %d %.300s", rec.Code, rec.Body.String())
	}
	run := server.latestRun(t)
	rec = do(t, server.handler, http.MethodPost, server.path("/preparation/select"), url.Values{"run": {run.ID}}, withCookie(cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("select: %d %.300s", rec.Code, rec.Body.String())
	}
	prepared := exclusionReportRows(t, do(t, server.handler, http.MethodGet, reportPath, nil, withCookie(cookie)))
	if len(prepared) != 2 {
		t.Fatalf("prepared report rows = %d, want header plus the held empty name: %v", len(prepared), prepared)
	}
	if !strings.Contains(prepared[1][columnIndex(t, prepared[0], "Reason")], "required") {
		t.Fatalf("prepared report reason = %v", prepared[1])
	}

	// A final upload with its own held entry wins over the prepared review.
	uploadFinalRows(t, server.handler, cookie, server.campaign, server.task.ID, "precedence-final-123456", [][]string{
		{"Email", "Applicant Full Name", "Department"},
		{"alice@example.com", "Alice Smith", "Engineering"},
		{"not-an-address", "Carol Danvers", "Sales"},
	})
	final := exclusionReportRows(t, do(t, server.handler, http.MethodGet, reportPath, nil, withCookie(cookie)))
	if len(final) != 2 {
		t.Fatalf("final report rows = %d, want header plus the rejected upload entry: %v", len(final), final)
	}
	recipient := columnIndex(t, final[0], "Recipient")
	if final[1][recipient] != "not-an-address" {
		t.Fatalf("final report entry = %v, want the final upload's rejected recipient", final[1])
	}
	for _, row := range final[1:] {
		if row[recipient] == "bob@example.com" {
			t.Fatalf("final report still names the prepared review's held entry: %v", row)
		}
	}
}

// exclusionReportRows opens one downloaded exclusion workbook.
func exclusionReportRows(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("exclusion download = %d %.300s", rec.Code, rec.Body.String())
	}
	book, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	grid, err := book.GetRows(book.GetSheetList()[0])
	if err != nil {
		t.Fatal(err)
	}
	return grid
}

// columnIndex finds one report column by its label.
func columnIndex(t *testing.T, header []string, label string) int {
	t.Helper()
	for index, value := range header {
		if value == label {
			return index
		}
	}
	t.Fatalf("report header %q missing from %v", label, header)
	return -1
}

// uploadFinalRows posts an operator-authored sending sheet, header included, as
// the final dataset.
func uploadFinalRows(t *testing.T, handler http.Handler, cookie *http.Cookie, campaignID, taskID, actionID string, rows [][]string) {
	t.Helper()
	sheet := excelize.NewFile()
	if err := sheet.SetSheetName("Sheet1", finalization.SendingSheetName); err != nil {
		t.Fatal(err)
	}
	for rowIndex, values := range rows {
		cells := make([]any, len(values))
		for index, value := range values {
			cells[index] = value
		}
		cell, err := excelize.CoordinatesToCellName(1, rowIndex+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := sheet.SetSheetRow(finalization.SendingSheetName, cell, &cells); err != nil {
			t.Fatal(err)
		}
	}
	data, err := sheet.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := sheet.Close(); err != nil {
		t.Fatal(err)
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("action_id", actionID); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("workbook", "final.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data.Bytes()); err != nil {
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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("final upload = %d %.600s", rec.Code, rec.Body.String())
	}
}
