package preparation_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

const probeOutput = `{"values":[{"id":"probe","value":"probe@example.test"}]}`

// env is one temporary application, a campaign with four mapped columns and
// one unmapped control column, a validated task, and a simulated provider.
type env struct {
	t          *testing.T
	app        core.App
	campaigns  *campaign.Service
	tasks      *intake.Service
	aiStore    *ai.Store
	service    *preparation.Service
	owner      string
	campaignID string
	revisionID string
	model      *fakeModel
}

func newEnv(t *testing.T) *env {
	t.Helper()
	app := testutil.NewApp(t)
	campaigns := campaign.NewServiceWithConverter(app, convert.Converter{Path: stubPandoc(t)})
	tasks := intake.NewService(app, campaigns, intake.Limits{})
	ctx := context.Background()
	owner := newOperator(t, app, "owner@example.com")
	created, err := campaigns.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Test campaign",
		SourceHeaders: []string{"Email", "Applicant Full Name", "Department", "Certificate", "Status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		Subject:      "Application for {{candidate_name}}",
		Document:     docxFixture(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName},
			{Variable: "department", Header: "Department", Format: campaign.FormatText},
			{Variable: "certificate", Header: "Certificate", Format: campaign.FormatText},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	aiStore := ai.NewStore(app, func() ([]byte, error) { return testKey, nil })
	aiStore.SetValidationOptions(ai.ValidationOptions{Timeout: 5 * time.Second, MaxResponseBytes: 1 << 20, AllowPrivate: true})
	e := &env{
		t:          t,
		app:        app,
		campaigns:  campaigns,
		tasks:      tasks,
		aiStore:    aiStore,
		service:    preparation.NewService(app, tasks, campaigns, aiStore),
		owner:      owner,
		campaignID: created.ID,
		revisionID: revision.ID,
		model:      newFakeModel(t),
	}
	e.service.SetOptions(preparation.Options{BatchSize: 2, MaxTokens: 2048})
	store := sending.NewStore(app, sending.StoreOptions{})
	e.service.SetSettled(func(taskID string) {
		if err := store.SettleClosure(context.Background(), taskID); err != nil {
			t.Errorf("settle closure: %v", err)
		}
	})
	e.connect()
	return e
}

// connect confirms the simulated provider under the default model.
func (e *env) connect() {
	e.t.Helper()
	e.connectModel("test-model")
}

func (e *env) connectModel(model string) {
	e.t.Helper()
	ctx := context.Background()
	draft, err := e.aiStore.StageValidation(ctx, e.owner, ai.Proposal{
		Config: ai.Config{
			Provider:  ai.ProviderCustom,
			APIRoot:   e.model.server.URL + "/v1",
			APIFormat: ai.FormatResponses,
			Model:     model,
		},
		APIKey: "test-key",
	})
	if err != nil {
		e.t.Fatalf("stage AI connection: %v", err)
	}
	active, err := e.aiStore.Connection(ctx, e.owner)
	generation := 0
	if err == nil {
		generation = active.ConfigurationGeneration
	}
	if _, err := e.aiStore.Confirm(ctx, e.owner, draft.ID, generation); err != nil {
		e.t.Fatalf("confirm AI connection: %v", err)
	}
}

func newOperator(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

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
printf '%s' '<p>Dear {{candidate_name}} ({{department}}, {{certificate}}), we received your application at {{candidate_email}}.</p>'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func docxFixture(t *testing.T) []byte {
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

// workbook builds one Candidates worksheet. rows[0] is worksheet row 1.
func workbook(t *testing.T, rows [][]string) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := file.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		values := row
		if err := file.SetSheetRow("Candidates", cell, &values); err != nil {
			t.Fatal(err)
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// validate stages and validates one workbook against the pinned revision.
func (e *env) validate(rows [][]string) *intake.Task {
	e.t.Helper()
	ctx := context.Background()
	task, err := e.tasks.Stage(ctx, e.owner, e.campaignID, "book.xlsx", workbook(e.t, rows), "")
	if err != nil {
		e.t.Fatalf("stage workbook: %v", err)
	}
	if err := e.tasks.Validate(ctx, e.owner, task.ID, "Candidates", 1); err != nil {
		e.t.Fatalf("validate workbook: %v", err)
	}
	return e.loadTask(task.ID)
}

func (e *env) loadTask(id string) *intake.Task {
	e.t.Helper()
	task, err := e.tasks.LoadTask(context.Background(), e.owner, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return task
}

func (e *env) startWorker() func() {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.service.Run(ctx)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			e.t.Error("preparation worker did not stop")
		}
	}
}

func (e *env) waitRun(taskID, runID, state string) *preparation.Run {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last *preparation.Run
	for time.Now().Before(deadline) {
		run, err := e.service.LoadRun(context.Background(), e.owner, taskID, runID)
		if err == nil {
			last = run
			if run.State == state {
				return run
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatalf("run %s did not reach %q; last state %+v", runID, state, last)
	return nil
}

func (e *env) candidateRows(taskID string) []intake.RowSnapshot {
	e.t.Helper()
	rows, err := e.tasks.Rows(context.Background(), e.owner, taskID)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// fakeModel simulates the Responses API. It records every system prompt and
// payload so a test can assert what was sent, and delegates the response body
// to one scripted function.
type fakeModel struct {
	t               *testing.T
	server          *httptest.Server
	mu              sync.Mutex
	systems         []string
	payloads        []preparation.RequestPayload
	models          []string
	maxOutputTokens []int
	auths           []string
	respond         func(payload preparation.RequestPayload) (string, int)
	respondRaw      func(payload preparation.RequestPayload, maxOutputTokens int) (string, int)
	block           chan struct{}
}

func newFakeModel(t *testing.T) *fakeModel {
	t.Helper()
	fake := &fakeModel{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("missing bearer credential")
		}
		var body struct {
			Model           string `json:"model"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Input           []struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		user := ""
		system := ""
		if len(body.Input) > 1 && len(body.Input[1].Content) > 0 {
			user = body.Input[1].Content[0].Text
		}
		if len(body.Input) > 0 && len(body.Input[0].Content) > 0 {
			system = body.Input[0].Content[0].Text
		}
		if strings.Contains(user, `"probe"`) {
			writeResponses(w, probeOutput)
			return
		}
		var payload preparation.RequestPayload
		if err := json.Unmarshal([]byte(user), &payload); err != nil {
			t.Errorf("decode preparation payload: %v", err)
		}
		fake.mu.Lock()
		fake.auths = append(fake.auths, r.Header.Get("Authorization"))
		fake.systems = append(fake.systems, system)
		fake.payloads = append(fake.payloads, payload)
		fake.models = append(fake.models, body.Model)
		fake.maxOutputTokens = append(fake.maxOutputTokens, body.MaxOutputTokens)
		block := fake.block
		respond := fake.respond
		respondRaw := fake.respondRaw
		fake.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		if respondRaw != nil {
			response, status := respondRaw(payload, body.MaxOutputTokens)
			if status == 0 {
				status = http.StatusOK
			}
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := io.WriteString(w, response); err != nil {
				t.Errorf("write raw response: %v", err)
			}
			return
		}
		if respond == nil {
			respond = echoResponse
		}
		text, status := respond(payload)
		if status == 0 {
			status = http.StatusOK
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		writeResponses(w, text)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeModel) setResponder(respond func(payload preparation.RequestPayload) (string, int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respond = respond
}

func (f *fakeModel) setRawResponder(respond func(payload preparation.RequestPayload, maxOutputTokens int) (string, int)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respondRaw = respond
}

func (f *fakeModel) setBlock(block chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block = block
}

func (f *fakeModel) snapshot() ([]string, []preparation.RequestPayload, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.systems...), append([]preparation.RequestPayload(nil), f.payloads...), append([]string(nil), f.models...)
}

func (f *fakeModel) authSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

func (f *fakeModel) requestSnapshot() ([]preparation.RequestPayload, []string, []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]preparation.RequestPayload(nil), f.payloads...), append([]string(nil), f.models...), append([]int(nil), f.maxOutputTokens...)
}

func writeResponses(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"output": []map[string]any{{
			"type":    "message",
			"content": []map[string]string{{"type": "output_text", "text": text}},
		}},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
	}
}

// testRow and testNote mirror the required response shape.
type testRow struct {
	ID          string            `json:"id"`
	Values      map[string]string `json:"values"`
	Corrections []testNote        `json:"corrections,omitempty"`
	Findings    []testFinding     `json:"findings,omitempty"`
}

type testNote struct {
	Field string `json:"field"`
	Note  string `json:"note"`
}

type testFinding struct {
	Field    string `json:"field"`
	Scope    string `json:"scope"`
	Position int    `json:"position,omitempty"`
	Note     string `json:"note"`
}

// editRow is one scripted row response.
type editRow struct {
	values      map[string]string
	corrections []testNote
	findings    []testFinding
}

// scriptedResponse builds one complete response from a per-row edit function.
func scriptedResponse(payload preparation.RequestPayload, edit func(row preparation.InputRow) editRow) string {
	rows := make([]testRow, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		changed := edit(row)
		values := changed.values
		if values == nil {
			values = row.Values
		}
		rows = append(rows, testRow{ID: row.ID, Values: values, Corrections: changed.corrections, Findings: changed.findings})
	}
	data, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func echoResponse(payload preparation.RequestPayload) (string, int) {
	return scriptedResponse(payload, func(preparation.InputRow) editRow { return editRow{} }), 0
}

// waitFor polls one condition with a bounded deadline.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// prepare starts one run and returns it.
func (e *env) prepare(task *intake.Task) *preparation.Run {
	e.t.Helper()
	run, err := e.service.Prepare(context.Background(), e.owner, task.ID)
	if err != nil {
		e.t.Fatalf("prepare: %v", err)
	}
	return run
}

// isNotFound reports the fixed ownership refusal.
func isNotFound(err error) bool { return errors.Is(err, preparation.ErrNotFound) }
