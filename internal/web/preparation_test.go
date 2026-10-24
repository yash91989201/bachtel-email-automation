package web

// Preparation operator-surface tests. They drive the real HTTP handler, a real
// PocketBase app on temporary data and a simulated provider; no email is sent
// and no live credential is used.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
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
	revision, _, err := campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		Subject:      taskSubject + " — {{department}}",
		Document:     docxUpload(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName},
			{Variable: "department", Header: "Department", Format: campaign.FormatText},
		},
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
	task, err := tasks.Stage(ctx, owner, created.ID, "book.xlsx", taskWorkbook(t, "Sheet1", [][]string{
		{"Email", "Applicant Full Name", "Department"},
		{"alice@example.com", "Alice Smith", "Engineering"},
		{"bob@example.com", "Bob Stone", "Sales"},
	}))
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
