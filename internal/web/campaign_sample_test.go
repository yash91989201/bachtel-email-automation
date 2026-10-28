package web

// Campaign AI sample tests. They drive the real HTTP handlers, a real
// PocketBase app on temporary data and a simulated provider; no email is sent,
// no live credential is used and no sample is ever stored.

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// samplePandoc writes a deterministic converter that emits one body using both
// the required name and the optional department.
func samplePandoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pandoc")
	script := `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = "--version" ]; then
		echo "pandoc stub 1.0"
		exit 0
	fi
done
printf '%s' '<p>Dear {{candidate_name}}, welcome to {{department}}.</p>'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// sampleModel simulates the Responses API for the sample action. It records the
// preparation payloads so tests assert what was sent, and can block or fail to
// exercise cancellation, busy refusal and provider errors.
type sampleModel struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	payloads []preparation.RequestPayload
	respond  func(preparation.RequestPayload) string
	block    chan struct{}
	failure  bool
}

func newSampleModel(t *testing.T) *sampleModel {
	t.Helper()
	model := &sampleModel{t: t}
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
		if strings.Contains(user, `"probe"`) {
			writeSampleEnvelope(t, w, aiProbeOutput)
			return
		}
		var payload preparation.RequestPayload
		if err := json.Unmarshal([]byte(user), &payload); err != nil {
			t.Errorf("decode sample payload: %v", err)
		}
		model.mu.Lock()
		model.payloads = append(model.payloads, payload)
		respond := model.respond
		block := model.block
		failure := model.failure
		model.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		if failure {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if respond == nil {
			respond = sampleNormalize
		}
		writeSampleEnvelope(t, w, respond(payload))
	}))
	t.Cleanup(model.server.Close)
	return model
}

// writeSampleEnvelope wraps assistant text in the Responses API shape.
func writeSampleEnvelope(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"output": []map[string]any{{
			"type":    "message",
			"content": []map[string]string{{"type": "output_text", "text": text}},
		}},
	}); err != nil {
		t.Errorf("encode simulated response: %v", err)
	}
}

func (m *sampleModel) setResponder(respond func(preparation.RequestPayload) string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.respond = respond
}

func (m *sampleModel) setBlock(block chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.block = block
}

func (m *sampleModel) setFailure(failure bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failure = failure
}

func (m *sampleModel) snapshot() []preparation.RequestPayload {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]preparation.RequestPayload(nil), m.payloads...)
}

// sampleRows builds one complete response from a per-row edit.
func sampleRows(payload preparation.RequestPayload, edit func(map[string]string) (map[string]string, []map[string]any, []map[string]any)) string {
	rows := make([]map[string]any, 0, len(payload.Rows))
	for _, row := range payload.Rows {
		values := make(map[string]string, len(row.Values))
		maps.Copy(values, row.Values)
		changed, corrections, findings := edit(values)
		if changed != nil {
			values = changed
		}
		entry := map[string]any{"id": row.ID, "values": values}
		if len(corrections) > 0 {
			entry["corrections"] = corrections
		}
		if len(findings) > 0 {
			entry["findings"] = findings
		}
		rows = append(rows, entry)
	}
	encoded, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// sampleNormalize trims the values, cleans the name and records a correction.
func sampleNormalize(payload preparation.RequestPayload) string {
	return sampleRows(payload, func(values map[string]string) (map[string]string, []map[string]any, []map[string]any) {
		values["candidate_name"] = "Ada Lovelace"
		return values, []map[string]any{{"field": "candidate_name", "note": "restored spacing"}}, nil
	})
}

// sampleEnv is one campaign with a role-mapping revision, a confirmed
// simulated connection and a signed-in operator.
type sampleEnv struct {
	app        *pocketbase.PocketBase
	handler    http.Handler
	services   Services
	campaigns  *campaign.Service
	owner      string
	campaignID string
	revision   *campaign.Configuration
	model      *sampleModel
	cookie     *http.Cookie
}

func newSampleEnv(t *testing.T) *sampleEnv {
	t.Helper()
	cfg := testConfig("http://127.0.0.1:8090")
	cfg.PandocPath = samplePandoc(t)
	cfg.Intake = intake.DefaultLimits()
	app, handler, services := newTestServerWithServices(t, cfg)
	provisionOperator(t, app)
	owner := operatorID(t, app)
	ctx := context.Background()
	campaigns := campaign.NewService(app, cfg.PandocPath)
	created, err := campaigns.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Sample campaign",
		SourceHeaders: []string{"Email", "Applicant Full Name", "Department"},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		MappingVersion: campaign.CurrentMappingVersion,
		Subject:        "Interview invite {{candidate_name}}",
		Document:       docxUpload(t),
		DocumentName:   "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Required: true, Recipient: true},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true},
			{Variable: "department", Header: "Department", Fallback: "the team"},
		},
	})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	e := &sampleEnv{
		app:        app,
		handler:    handler,
		services:   services,
		campaigns:  campaigns,
		owner:      owner,
		campaignID: created.ID,
		revision:   revision,
		model:      newSampleModel(t),
	}
	draft, err := services.AI.StageValidation(ctx, owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: e.model.server.URL + "/v1", APIFormat: ai.FormatResponses, Model: "exact-model"},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("stage AI connection: %v", err)
	}
	if _, err := services.AI.Confirm(ctx, owner, draft.ID, 0); err != nil {
		t.Fatalf("confirm AI connection: %v", err)
	}
	e.cookie = login(t, handler)
	return e
}

// sampleForm is the operator's submitted sample for the current revision.
func (e *sampleEnv) sampleForm() url.Values {
	return url.Values{
		"revision":        {strconv.Itoa(e.revision.Revision)},
		"candidate_email": {"Ada@Example.com"},
		"candidate_name":  {"ada   lovelace"},
		"department":      {""},
	}
}

// postSample submits one AI sample action.
func (e *sampleEnv) postSample(form url.Values) *httptest.ResponseRecorder {
	return do(e.model.t, e.handler, http.MethodPost, "/campaigns/"+e.campaignID+"/preview/normalize", form, withCookie(e.cookie))
}

// storedState counts every collection a sample must leave untouched.
func (e *sampleEnv) storedState(t *testing.T) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, name := range []string{
		intake.TasksCollection,
		intake.CandidatesCollection,
		intake.TaskCandidatesCollection,
		preparation.RunsCollection,
		preparation.JobsCollection,
		finalization.SnapshotsCollection,
		sending.AttemptsCollection,
		sending.ApprovalsCollection,
		sending.ActionsCollection,
	} {
		total, err := e.app.CountRecords(name)
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		counts[name] = total
	}
	return counts
}

// TestCampaignAISampleRendersPreparedComparison covers the successful action:
// provenance, comparisons, corrections, fallback visibility and untouched
// stored state.
func TestCampaignAISampleRendersPreparedComparison(t *testing.T) {
	e := newSampleEnv(t)
	before := e.storedState(t)
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%.600s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"AI-prepared sample",
		"exact-model",
		e.model.server.URL + "/v1",
		"version 1",
		"Illustrative only",
		"Corrections applied",
		"Applicant Full Name: restored spacing",
		"Original",
		"AI-prepared",
		"Effective",
		"ada   lovelace",
		"Ada Lovelace",
		"the team",
		"Interview invite Ada Lovelace",
		"ada@example.com",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("AI sample result is missing %q: %.900s", want, body)
		}
	}
	if strings.Contains(body, "Needs review") {
		t.Fatalf("a corrected sample must not need review: %.600s", body)
	}

	payloads := e.model.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("provider payloads = %d, want exactly one", len(payloads))
	}
	payload := payloads[0]
	if len(payload.Rows) != 1 || payload.Rows[0].Values["candidate_name"] != "ada   lovelace" || payload.Rows[0].Values["candidate_email"] != "Ada@Example.com" {
		t.Fatalf("the provider payload was rewritten: %+v", payload.Rows)
	}
	if payload.Instructions != "" {
		t.Fatalf("the sample carried task instructions: %q", payload.Instructions)
	}
	if len(payload.Fields) != 3 {
		t.Fatalf("payload fields = %+v, want only the mapped fields", payload.Fields)
	}
	for _, field := range payload.Fields {
		if field.ID == "subject" || field.ID == "body" {
			t.Fatalf("template content reached the provider: %+v", payload.Fields)
		}
	}
	after := e.storedState(t)
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("%s changed from %d to %d; a sample must not store state", name, want, after[name])
		}
	}
}

// TestCampaignAISampleDirectPreviewStillDoesNotCallAI proves the direct action
// keeps its previous behavior and never spends provider usage.
func TestCampaignAISampleDirectPreviewStillDoesNotCallAI(t *testing.T) {
	e := newSampleEnv(t)
	form := e.sampleForm()
	delete(form, "revision")
	rec := do(t, e.handler, http.MethodPost, "/campaigns/"+e.campaignID+"/preview", form, withCookie(e.cookie))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Preview result", "Raw value", "Used value", "ada   lovelace"} {
		if !strings.Contains(body, want) {
			t.Fatalf("direct preview is missing %q: %.600s", want, body)
		}
	}
	if strings.Contains(body, "AI-prepared sample") {
		t.Fatal("direct rendering must not present itself as AI-prepared")
	}
	if payloads := e.model.snapshot(); len(payloads) != 0 {
		t.Fatalf("direct rendering called the provider %d times", len(payloads))
	}
}

// TestCampaignAISampleRefusalsMakeNoProviderCall covers the pre-inference gate:
// revision identity, size, required values, single recipient, ownership and
// authentication.
func TestCampaignAISampleRefusalsMakeNoProviderCall(t *testing.T) {
	e := newSampleEnv(t)
	for _, tc := range []struct {
		name   string
		mutate func(url.Values)
		status int
		want   string
	}{
		{
			name:   "stale revision",
			mutate: func(form url.Values) { form.Set("revision", strconv.Itoa(e.revision.Revision+1)) },
			status: http.StatusConflict,
			want:   "revision changed",
		},
		{
			name:   "missing revision identity",
			mutate: func(form url.Values) { form.Del("revision") },
			status: http.StatusConflict,
			want:   "revision changed",
		},
		{
			name:   "missing required value",
			mutate: func(form url.Values) { form.Set("candidate_name", "   ") },
			status: http.StatusBadRequest,
			want:   "missing required value(s): candidate_name",
		},
		{
			name:   "recipient list",
			mutate: func(form url.Values) { form.Set("candidate_email", "ada@example.com, grace@example.com") },
			status: http.StatusBadRequest,
			want:   "recipient must be a single valid email address",
		},
		{
			name:   "recipient display name",
			mutate: func(form url.Values) { form.Set("candidate_email", "Ada <ada@example.com>") },
			status: http.StatusBadRequest,
			want:   "recipient must be a single valid email address",
		},
		{
			name:   "recipient header control character",
			mutate: func(form url.Values) { form.Set("candidate_email", "ada@example.com\r\nBcc: x@example.com") },
			status: http.StatusBadRequest,
			want:   "recipient must be a single valid email address",
		},
		{
			name:   "oversized value",
			mutate: func(form url.Values) { form.Set("candidate_name", strings.Repeat("a", preparation.MaxValueLength+1)) },
			status: http.StatusBadRequest,
			want:   "longer than the accepted cell size",
		},
		{
			name: "oversized form",
			mutate: func(form url.Values) {
				form.Set("candidate_name", strings.Repeat("a", (1<<20)+1))
			},
			status: http.StatusBadRequest,
			want:   "could not be read",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := e.sampleForm()
			tc.mutate(form)
			rec := e.postSample(form)
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d: %.400s", rec.Code, tc.status, rec.Body.String())
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.want) {
				t.Fatalf("refusal is missing %q: %.600s", tc.want, body)
			}
		})
	}
	if payloads := e.model.snapshot(); len(payloads) != 0 {
		t.Fatalf("a refused sample called the provider %d times", len(payloads))
	}

	t.Run("unauthenticated", func(t *testing.T) {
		rec := do(t, e.handler, http.MethodPost, "/campaigns/"+e.campaignID+"/preview/normalize", e.sampleForm(), nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})
	t.Run("foreign campaign", func(t *testing.T) {
		// Another owner's campaign is indistinguishable from a missing one.
		other := provisionOperatorAs(t, e.app, "second@example.com", "correct-horse-battery-staple")
		campaigns := campaign.NewService(e.app, "")
		created, err := campaigns.CreateCampaign(context.Background(), other, campaign.CreateCampaignInput{Name: "Other", SourceHeaders: []string{"Email"}})
		if err != nil {
			t.Fatal(err)
		}
		rec := do(t, e.handler, http.MethodPost, "/campaigns/"+created.ID+"/preview/normalize", e.sampleForm(), withCookie(e.cookie))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404 for another owner's campaign: %.300s", rec.Code, rec.Body.String())
		}
	})
}

// TestCampaignAISampleWithoutConnectionRefuses proves a missing confirmed
// connection blocks only the AI action and spends no provider usage.
func TestCampaignAISampleWithoutConnectionRefuses(t *testing.T) {
	e := newSampleEnv(t)
	active, err := e.services.AI.Connection(context.Background(), e.owner)
	if err != nil {
		t.Fatal(err)
	}
	record, err := e.app.FindRecordById(ai.ConnectionsCollection, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.app.Delete(record); err != nil {
		t.Fatal(err)
	}
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %.400s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, preparation.ErrSetup.Error()) {
		t.Fatalf("the setup refusal is missing: %.600s", body)
	}
	if payloads := e.model.snapshot(); len(payloads) != 0 {
		t.Fatalf("an unusable setup called the provider %d times", len(payloads))
	}
}

// TestCampaignAISampleFindingNeedsReview proves an unresolved finding in an
// optional field still labels the sample Needs review.
func TestCampaignAISampleFindingNeedsReview(t *testing.T) {
	e := newSampleEnv(t)
	e.model.setResponder(func(payload preparation.RequestPayload) string {
		return sampleRows(payload, func(values map[string]string) (map[string]string, []map[string]any, []map[string]any) {
			return values, nil, []map[string]any{{"field": "department", "scope": "shared", "note": "the department is ambiguous"}}
		})
	})
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Needs review", "Department: the department is ambiguous", "Illustrative only"} {
		if !strings.Contains(body, want) {
			t.Fatalf("finding result is missing %q: %.600s", want, body)
		}
	}
}

// TestCampaignAISampleRenderRefusalKeepsComparison proves prepared values that
// the renderer refuses show the error and the comparison, never a successful
// email.
func TestCampaignAISampleRenderRefusalKeepsComparison(t *testing.T) {
	e := newSampleEnv(t)
	e.model.setResponder(func(payload preparation.RequestPayload) string {
		return sampleRows(payload, func(values map[string]string) (map[string]string, []map[string]any, []map[string]any) {
			values["candidate_name"] = ""
			return values, nil, nil
		})
	})
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"missing required value(s): candidate_name", "Effective", "candidate_name"} {
		if !strings.Contains(body, want) {
			t.Fatalf("refused render is missing %q: %.600s", want, body)
		}
	}
	for _, absent := range []string{"Interview invite", ">To</p>"} {
		if strings.Contains(body, absent) {
			t.Fatalf("a refused render showed %q as a rendered email", absent)
		}
	}
}

// TestCampaignAISampleEscapesAndRefusesUnsafePreparedContent proves prepared
// values stay inert and a header-breaking subject is refused.
func TestCampaignAISampleEscapesAndRefusesUnsafePreparedContent(t *testing.T) {
	t.Run("markup stays text", func(t *testing.T) {
		e := newSampleEnv(t)
		e.model.setResponder(func(payload preparation.RequestPayload) string {
			return sampleRows(payload, func(values map[string]string) (map[string]string, []map[string]any, []map[string]any) {
				values["candidate_name"] = `<script>alert(1)</script>`
				return values, nil, nil
			})
		})
		rec := e.postSample(e.sampleForm())
		body := rec.Body.String()
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatal("prepared markup reached the page unescaped")
		}
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("the comparison does not show the inert prepared value: %.600s", body)
		}
	})
	t.Run("subject line break is refused", func(t *testing.T) {
		e := newSampleEnv(t)
		e.model.setResponder(func(payload preparation.RequestPayload) string {
			return sampleRows(payload, func(values map[string]string) (map[string]string, []map[string]any, []map[string]any) {
				values["candidate_name"] = "Ada\r\nBcc: evil@example.com"
				return values, nil, nil
			})
		})
		rec := e.postSample(e.sampleForm())
		body := rec.Body.String()
		if !strings.Contains(body, "rendered subject contains control characters or line breaks") {
			t.Fatalf("a header-breaking subject was not refused: %.600s", body)
		}
		if strings.Contains(body, ">Subject<") || strings.Contains(body, ">Body<") {
			t.Fatal("the refused subject was rendered as an email")
		}
	})
}

// TestCampaignAISampleProviderFailureIsSafeAndTemporary proves a provider error
// is reported without sample content, storage or a retry.
func TestCampaignAISampleProviderFailureIsSafeAndTemporary(t *testing.T) {
	e := newSampleEnv(t)
	before := e.storedState(t)
	e.model.setFailure(true)
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400: %.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "the AI endpoint returned an error") && !strings.Contains(body, "retry") {
		t.Fatalf("provider failure has no actionable text: %.600s", body)
	}
	for _, leak := range []string{"ada   lovelace", "Ada@Example.com"} {
		if strings.Contains(body, leak) {
			t.Fatalf("a failed sample echoed the input (%q): %.600s", leak, body)
		}
	}
	if payloads := e.model.snapshot(); len(payloads) != 1 {
		t.Fatalf("provider requests = %d, want one without automatic retry", len(payloads))
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	after := e.storedState(t)
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("%s changed from %d to %d after a failed sample", name, want, after[name])
		}
	}
	// The next explicit action works: nothing was left behind.
	e.model.setFailure(false)
	if rec := e.postSample(e.sampleForm()); rec.Code != http.StatusOK {
		t.Fatalf("sample after a failure: status=%d body=%.400s", rec.Code, rec.Body.String())
	}
}

// TestCampaignAISampleRefusesConcurrentWork proves the handler refuses a second
// sample while one is active, without a provider call or a queue.
func TestCampaignAISampleRefusesConcurrentWork(t *testing.T) {
	e := newSampleEnv(t)
	block := make(chan struct{})
	e.model.setBlock(block)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- e.postSample(e.sampleForm())
	}()
	waitForSample(t, "the active sample request", func() bool { return len(e.model.snapshot()) == 1 })
	rec := e.postSample(e.sampleForm())
	if rec.Code != http.StatusConflict {
		t.Fatalf("busy refusal status=%d, want 409: %.400s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "another AI sample is already running") {
		t.Fatalf("busy refusal text is missing: %.600s", body)
	}
	if payloads := e.model.snapshot(); len(payloads) != 1 {
		t.Fatalf("a busy refusal called the provider: %d requests", len(payloads))
	}
	close(block)
	first := <-done
	if first.Code != http.StatusOK {
		t.Fatalf("the admitted sample failed: status=%d body=%.400s", first.Code, first.Body.String())
	}
}

// TestCampaignAISampleDiscardsResultForAChangedRevision proves a revision
// replaced while inference runs never returns a current-looking result.
func TestCampaignAISampleDiscardsResultForAChangedRevision(t *testing.T) {
	e := newSampleEnv(t)
	block := make(chan struct{})
	e.model.setBlock(block)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- e.postSample(e.sampleForm())
	}()
	waitForSample(t, "the active sample request", func() bool { return len(e.model.snapshot()) == 1 })
	if _, _, err := e.campaigns.CreateRevision(context.Background(), e.owner, e.campaignID, campaign.RevisionInput{
		MappingVersion: campaign.CurrentMappingVersion,
		Subject:        "Updated invite {{candidate_name}}",
		Document:       docxUpload(t),
		DocumentName:   "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Required: true, Recipient: true},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true},
			{Variable: "department", Header: "Department", Fallback: "the team"},
		},
	}); err != nil {
		t.Fatalf("create the replacement revision: %v", err)
	}
	close(block)
	rec := <-done
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d, want 409: %.400s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "revision changed") {
		t.Fatalf("the stale sample was not invalidated: %.600s", body)
	}
	for _, absent := range []string{"AI-prepared sample", "Interview invite"} {
		if strings.Contains(body, absent) {
			t.Fatalf("a stale result presented %q as current", absent)
		}
	}
}

// waitForSample polls one sample condition with a bounded deadline.
func waitForSample(t *testing.T, what string, condition func() bool) {
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
