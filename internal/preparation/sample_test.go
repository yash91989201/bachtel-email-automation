package preparation_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// sampleValues is one signed-but-noisy sample row for the fixture campaign.
func sampleValues() map[string]string {
	return map[string]string{
		"candidate_email": "Ada@Example.com",
		"candidate_name":  "ada   lovelace",
		"department":      "PRODUCT MANGMENT",
		"certificate":     "CERT-1",
	}
}

// sampleFixture cleans the sample the way the base policy asks: a name and a
// department correction, never a finding.
func sampleFixture(payload preparation.RequestPayload) (string, int) {
	return scriptedResponse(payload, func(row preparation.InputRow) editRow {
		values := make(map[string]string, len(row.Values))
		maps.Copy(values, row.Values)
		values["candidate_name"] = "Ada Lovelace"
		values["department"] = "Product Management"
		return editRow{values: values, corrections: []testNote{
			{Field: "candidate_name", Note: "restored spacing"},
			{Field: "department", Note: "corrected spelling"},
		}}
	}), 0
}

// configuration loads the campaign revision the sample renders with.
func (e *env) configuration() *campaign.Configuration {
	e.t.Helper()
	loaded, err := e.campaigns.LoadConfiguration(context.Background(), e.owner, e.revisionID)
	if err != nil {
		e.t.Fatal(err)
	}
	return loaded
}

// connectAt confirms one simulated connection for an owner, replacing any
// previous selection.
func (e *env) connectAt(owner, root, model, apiKey string) {
	e.t.Helper()
	ctx := context.Background()
	draft, err := e.aiStore.StageValidation(ctx, owner, ai.Proposal{
		Config: ai.Config{Provider: ai.ProviderCustom, APIRoot: root + "/v1", APIFormat: ai.FormatResponses, Model: model},
		APIKey: apiKey,
	})
	if err != nil {
		e.t.Fatalf("stage AI connection: %v", err)
	}
	generation := 0
	if active, err := e.aiStore.Connection(ctx, owner); err == nil {
		generation = active.ConfigurationGeneration
	}
	if _, err := e.aiStore.Confirm(ctx, owner, draft.ID, generation); err != nil {
		e.t.Fatalf("confirm AI connection: %v", err)
	}
}

// sample runs one AI sample over the fixture campaign.
func (e *env) sample(values map[string]string) (preparation.SampleResult, error) {
	e.t.Helper()
	return e.service.Sample(context.Background(), e.owner, e.configuration(), values)
}

// recordCounts counts every collection a sample must never touch.
func (e *env) recordCounts() map[string]int64 {
	e.t.Helper()
	counts := map[string]int64{}
	for _, name := range []string{
		preparation.RunsCollection,
		preparation.JobsCollection,
		intake.TasksCollection,
		intake.CandidatesCollection,
		intake.TaskCandidatesCollection,
		finalization.SnapshotsCollection,
		sending.AttemptsCollection,
		sending.ApprovalsCollection,
		sending.ActionsCollection,
		sending.ResolutionsCollection,
		sending.DispatcherCollection,
		ai.ConnectionsCollection,
		ai.DraftsCollection,
	} {
		total, err := e.app.CountRecords(name)
		if err != nil {
			e.t.Fatalf("count %s: %v", name, err)
		}
		counts[name] = total
	}
	return counts
}

// TestSampleUsesTheRevisionContractAndWritesNothing proves the temporary
// sample sends exactly one mapped row over the captured contract and leaves no
// task, run, job, artifact or decision state behind.
func TestSampleUsesTheRevisionContractAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.model.setResponder(sampleFixture)
	before := e.recordCounts()

	result, err := e.sample(sampleValues())
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if result.PromptVersion != preparation.BasePromptVersion || result.Prompt != preparation.CurrentBasePrompt() {
		t.Fatalf("sample prompt = v%d, want the current base policy", result.PromptVersion)
	}
	if result.Model != "test-model" || result.Provider != ai.ProviderCustom || result.APIRoot != e.model.server.URL+"/v1" {
		t.Fatalf("sample provenance = %+v", result)
	}
	if len(result.Output.Rows) != 1 {
		t.Fatalf("sample rows = %d, want 1", len(result.Output.Rows))
	}
	row := result.Output.Rows[0]
	if row.SourceRow != 1 || row.Values["candidate_name"] != "Ada Lovelace" || row.Values["department"] != "Product Management" {
		t.Fatalf("sample row = %+v", row)
	}
	if len(row.Corrections) != 2 || len(row.Findings) != 0 {
		t.Fatalf("corrections/findings = %+v/%+v", row.Corrections, row.Findings)
	}
	if result.Output.Counts.Accepted != 1 || result.Output.Counts.Held != 0 {
		t.Fatalf("sample entries = %+v, want one accepted sample recipient", result.Output.Counts)
	}

	systems, payloads, models := e.model.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("provider requests = %d, want exactly one", len(payloads))
	}
	if systems[0] != preparation.CurrentBasePrompt() {
		t.Fatal("the sample did not use the base policy as its system message")
	}
	if models[0] != "test-model" {
		t.Fatalf("request model = %q", models[0])
	}
	payload := payloads[0]
	if payload.Instructions != "" {
		t.Fatalf("the sample carried task instructions: %q", payload.Instructions)
	}
	if len(payload.Fields) != 4 || len(payload.Rows) != 1 || payload.Rows[0].ID != "1" {
		t.Fatalf("sample payload = %+v, want one row with the four mapped fields", payload)
	}
	recipient, ok := result.Output.Rows[0].Values["candidate_email"]
	if !ok || recipient != "Ada@Example.com" {
		t.Fatalf("sample recipient = %q, want the supplied casing preserved", recipient)
	}
	if !reflect.DeepEqual(payload.Rows[0].Values, sampleValues()) {
		t.Fatalf("payload values = %v, want the supplied sample values unchanged", payload.Rows[0].Values)
	}
	_, _, tokens := e.model.requestSnapshot()
	if tokens[0] != 2048 {
		t.Fatalf("max output tokens = %d, want the configured bound 2048", tokens[0])
	}

	after := e.recordCounts()
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("%s changed from %d to %d; a sample must not store state", name, want, after[name])
		}
	}
}

// TestSampleMatchesAOneRowTaskBatch proves the sample and a one-row durable
// batch send the same request and produce the same normalized values.
func TestSampleMatchesAOneRowTaskBatch(t *testing.T) {
	e := newEnv(t)
	e.model.setResponder(sampleFixture)
	task := e.validate([][]string{
		{"Email", "Applicant Full Name", "Department", "Certificate", "Status"},
		{sampleValues()["candidate_email"], sampleValues()["candidate_name"], sampleValues()["department"], sampleValues()["certificate"], ""},
	})
	stop := e.startWorker()
	defer stop()
	run := e.prepare(task)
	completed := e.waitRun(task.ID, run.ID, preparation.RunCompleted)
	_, payloads, _ := e.model.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("task payloads = %d, want 1", len(payloads))
	}
	taskPayload := payloads[0]

	result, err := e.sample(sampleValues())
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	_, payloads, _ = e.model.snapshot()
	if len(payloads) != 2 {
		t.Fatalf("payloads = %d, want one task batch and one sample", len(payloads))
	}
	samplePayload := payloads[1]
	if !reflect.DeepEqual(samplePayload.Fields, taskPayload.Fields) {
		t.Fatalf("sample fields = %+v, want the task batch fields %+v", samplePayload.Fields, taskPayload.Fields)
	}
	// The sample uses the synthetic source-row id 1; every other part of the
	// row is the batch row's.
	if len(samplePayload.Rows) != 1 || len(taskPayload.Rows) != 1 {
		t.Fatalf("rows = %d sample, %d task, want one each", len(samplePayload.Rows), len(taskPayload.Rows))
	}
	if samplePayload.Rows[0].ID != "1" || !reflect.DeepEqual(samplePayload.Rows[0].Values, taskPayload.Rows[0].Values) {
		t.Fatalf("sample row = %+v, want one synthetic row carrying the batch values %+v", samplePayload.Rows[0], taskPayload.Rows[0])
	}
	if samplePayload.Instructions != taskPayload.Instructions {
		t.Fatalf("sample instructions = %q, task instructions = %q", samplePayload.Instructions, taskPayload.Instructions)
	}
	if completed.Output == nil || len(completed.Output.Rows) != 1 {
		t.Fatalf("task output = %+v", completed.Output)
	}
	if !reflect.DeepEqual(result.Output.Rows[0].Values, completed.Output.Rows[0].Values) {
		t.Fatalf("sample values = %v, task values = %v", result.Output.Rows[0].Values, completed.Output.Rows[0].Values)
	}
}

// TestSampleRefusesUnusableSetup proves absent or unreadable credentials block
// only the sample and never call the provider.
func TestSampleRefusesUnusableSetup(t *testing.T) {
	e := newEnv(t)
	other := newOperator(t, e.app, "no-ai@example.com")
	if _, err := e.service.Sample(context.Background(), other, e.configuration(), sampleValues()); !errors.Is(err, preparation.ErrSetup) {
		t.Fatalf("sample without a connection = %v, want ErrSetup", err)
	}
	_, payloads, _ := e.model.snapshot()
	if len(payloads) != 0 {
		t.Fatalf("an unconfigured sample called the provider: %d", len(payloads))
	}

	active, err := e.aiStore.Connection(context.Background(), e.owner)
	if err != nil {
		t.Fatal(err)
	}
	record, err := e.app.FindRecordById(ai.ConnectionsCollection, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("credential", "v1.not-a-real-sealed-key")
	if err := e.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sample(sampleValues()); !errors.Is(err, preparation.ErrSetup) {
		t.Fatalf("sample with an unreadable credential = %v, want ErrSetup", err)
	}
	_, payloads, _ = e.model.snapshot()
	if len(payloads) != 0 {
		t.Fatalf("an unreadable credential called the provider: %d", len(payloads))
	}
}

// TestSampleDiscardsResultWhenTheConnectionChanges proves a configuration or
// credential change during inference discards the result, keeps the captured
// credential for the in-flight request and never falls back to the new
// selection.
func TestSampleDiscardsResultWhenTheConnectionChanges(t *testing.T) {
	for _, tc := range []struct {
		name string
		// change installs the new selection and reports the endpoint that will
		// serve the next explicit action together with its expected key.
		change func(t *testing.T, e *env) (active *fakeModel, key string)
	}{
		{
			name: "credential repair",
			change: func(t *testing.T, e *env) (*fakeModel, string) {
				t.Helper()
				e.connectAt(e.owner, e.model.server.URL, "test-model", "repaired-key")
				return e.model, "Bearer repaired-key"
			},
		},
		{
			name: "configuration switch",
			change: func(t *testing.T, e *env) (*fakeModel, string) {
				t.Helper()
				replacement := newFakeModel(t)
				replacement.setResponder(sampleFixture)
				e.connectAt(e.owner, replacement.server.URL, "other-model", "replacement-key")
				return replacement, "Bearer replacement-key"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.model.setResponder(sampleFixture)
			block := make(chan struct{})
			e.model.setBlock(block)
			type outcome struct {
				result preparation.SampleResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := e.sample(sampleValues())
				done <- outcome{result: result, err: err}
			}()
			waitFor(t, "the blocked sample request", func() bool {
				_, payloads, _ := e.model.snapshot()
				return len(payloads) == 1
			})
			active, key := tc.change(t, e)
			close(block)
			got := <-done
			if !errors.Is(got.err, preparation.ErrSampleChanged) {
				t.Fatalf("sample err = %v, want ErrSampleChanged", got.err)
			}
			if got.result.Output.Rows != nil {
				t.Fatalf("a discarded sample returned rows: %+v", got.result.Output.Rows)
			}
			_, payloads, _ := e.model.snapshot()
			if len(payloads) != 1 {
				t.Fatalf("provider requests = %d, want no automatic rerun", len(payloads))
			}
			auths := e.model.authSnapshot()
			if last := auths[len(auths)-1]; last != "Bearer test-key" {
				t.Fatalf("in-flight credential = %q, want the captured key", last)
			}
			// The in-flight request never moved to the newly confirmed
			// destination or model: only its validation probe reached it.
			if active != e.model {
				if _, samplePayloads, _ := active.snapshot(); len(samplePayloads) != 0 {
					t.Fatalf("the in-flight sample reached the replacement endpoint: %d", len(samplePayloads))
				}
			}

			// A new explicit action uses the changed selection.
			if _, err := e.service.Sample(context.Background(), e.owner, e.configuration(), sampleValues()); err != nil {
				t.Fatalf("second sample: %v", err)
			}
			auths = active.authSnapshot()
			if last := auths[len(auths)-1]; last != key {
				t.Fatalf("second sample credential = %q, want %q", last, key)
			}
		})
	}
}

// TestSampleAdmissionRefusesBusyWork proves one active sample per owner, four
// across the process, and no provider call or queue for a refusal.
func TestSampleAdmissionRefusesBusyWork(t *testing.T) {
	e := newEnv(t)
	e.model.setResponder(sampleFixture)
	block := make(chan struct{})
	e.model.setBlock(block)
	configuration := e.configuration()

	start := func(owner string) chan error {
		done := make(chan error, 1)
		go func() {
			_, err := e.service.Sample(context.Background(), owner, configuration, sampleValues())
			done <- err
		}()
		return done
	}
	first := start(e.owner)
	waitFor(t, "the first sample request", func() bool {
		_, payloads, _ := e.model.snapshot()
		return len(payloads) == 1
	})
	if _, err := e.service.Sample(context.Background(), e.owner, configuration, sampleValues()); !errors.Is(err, preparation.ErrSampleBusy) {
		t.Fatalf("repeat sample = %v, want ErrSampleBusy", err)
	}
	_, payloads, _ := e.model.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("a busy refusal called the provider: %d requests", len(payloads))
	}

	// Three more owners fill the process bound; the fifth owner is refused.
	var othersDone []chan error
	for i := 1; i <= 3; i++ {
		owner := newOperator(t, e.app, fmt.Sprintf("sample-%d@example.com", i))
		e.connectAt(owner, e.model.server.URL, "test-model", "test-key")
		othersDone = append(othersDone, start(owner))
	}
	waitFor(t, "four active samples", func() bool {
		_, payloads, _ := e.model.snapshot()
		return len(payloads) == 4
	})
	refused := newOperator(t, e.app, "sample-refused@example.com")
	if _, err := e.service.Sample(context.Background(), refused, configuration, sampleValues()); !errors.Is(err, preparation.ErrSampleBusy) {
		t.Fatalf("fifth owner sample = %v, want ErrSampleBusy", err)
	}
	close(block)
	for _, done := range append(othersDone, first) {
		if err := <-done; err != nil {
			t.Fatalf("admitted sample failed: %v", err)
		}
	}
	// Every slot is released: a fresh sample is admitted.
	if _, err := e.sample(sampleValues()); err != nil {
		t.Fatalf("sample after release: %v", err)
	}
}

// TestSampleDeadlineAndCancellationReleaseCapacity proves the sample is bounded,
// cancelled work stops without background continuation and capacity returns.
func TestSampleDeadlineAndCancellationReleaseCapacity(t *testing.T) {
	e := newEnv(t)
	e.model.setResponder(sampleFixture)
	e.service.SetOptions(preparation.Options{BatchSize: 2, MaxTokens: 2048, SampleDeadline: 60 * time.Millisecond})

	block := make(chan struct{})
	e.model.setBlock(block)
	if _, err := e.sample(sampleValues()); !errors.Is(err, preparation.ErrSampleTimeout) {
		t.Fatalf("sample deadline = %v, want ErrSampleTimeout", err)
	}
	_, payloads, _ := e.model.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("a timed-out sample made %d requests, want no retry", len(payloads))
	}
	close(block)
	if _, err := e.sample(sampleValues()); err != nil {
		t.Fatalf("sample after the deadline released capacity: %v", err)
	}

	cancelled := make(chan struct{})
	e.model.setBlock(cancelled)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.service.Sample(ctx, e.owner, e.configuration(), sampleValues())
		done <- err
	}()
	waitFor(t, "the cancelable sample request", func() bool {
		_, payloads, _ := e.model.snapshot()
		return len(payloads) == 2
	})
	cancel()
	if err := <-done; err == nil || errors.Is(err, preparation.ErrSampleTimeout) {
		t.Fatalf("cancelled sample err = %v, want a cancellation error", err)
	}
	close(cancelled)
	if _, err := e.sample(sampleValues()); err != nil {
		t.Fatalf("sample after cancellation released capacity: %v", err)
	}
	_, payloads, _ = e.model.snapshot()
	if len(payloads) != 3 {
		t.Fatalf("provider requests = %d, want one per explicit action", len(payloads))
	}
}
