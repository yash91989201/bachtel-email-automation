package preparation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

const tokenLimitResponse = `{"output_text":"","incomplete_details":{"reason":"max_output_tokens"}}`

func TestPreparationDefaultOutputBudgetCompletesBeyond4096Tokens(t *testing.T) {
	e := newEnv(t)
	e.connectOpenCodeGo(t)
	e.service.SetOptions(preparation.DefaultOptions())
	e.model.setRawResponder(func(payload preparation.RequestPayload, maxOutputTokens int) (string, int) {
		if maxOutputTokens <= ai.DefaultMaxTokens {
			return tokenLimitResponse, http.StatusOK
		}
		text, status := echoResponse(payload)
		if status != 0 {
			return "", status
		}
		return responsesBody(t, text), http.StatusOK
	})
	task := e.validate([][]string{
		{"Email", "Applicant Full Name", "Department", "Certificate", "Status"},
		{"alice@example.com", "Alice", "Engineering", "CERT-1", "SUBMITTED"},
	})
	stop := e.startWorker()
	defer stop()
	run := e.prepare(task)
	completed := e.waitRun(task.ID, run.ID, preparation.RunCompleted)
	if completed.Options.MaxTokens != ai.MaxTokens {
		t.Fatalf("recorded max tokens = %d, want %d", completed.Options.MaxTokens, ai.MaxTokens)
	}
	if completed.Model != "muse-spark-1.3-contributor" || completed.APIFormat != ai.FormatResponses {
		t.Fatalf("captured configuration = %+v, want fixed OpenCode Go Responses selection", completed)
	}
	payloads, models, budgets := e.model.requestSnapshot()
	if len(payloads) != 1 || len(models) != 1 || len(budgets) != 1 {
		t.Fatalf("generation requests = %d, models = %d, budgets = %d; want one each", len(payloads), len(models), len(budgets))
	}
	if models[0] != completed.Model || budgets[0] != ai.MaxTokens {
		t.Fatalf("generation model/budget = %q/%d, want %q/%d", models[0], budgets[0], completed.Model, ai.MaxTokens)
	}
}

func TestPreparationTokenLimitFailureIsPersistedAndRetryUsesNewBudget(t *testing.T) {
	e := newEnv(t)
	e.connectOpenCodeGo(t)
	e.service.SetOptions(preparation.Options{BatchSize: 1, MaxTokens: ai.DefaultMaxTokens})
	e.model.setRawResponder(func(payload preparation.RequestPayload, maxOutputTokens int) (string, int) {
		if maxOutputTokens <= ai.DefaultMaxTokens && len(payload.Rows) == 1 && payload.Rows[0].ID == "3" {
			return tokenLimitResponse, http.StatusOK
		}
		text, status := echoResponse(payload)
		if status != 0 {
			return "", status
		}
		return responsesBody(t, text), http.StatusOK
	})
	task := e.validate([][]string{
		{"Email", "Applicant Full Name", "Department", "Certificate", "Status"},
		{"alice@example.com", "Alice", "Engineering", "CERT-1", "SUBMITTED"},
		{"bob@example.com", "Bob", "Sales", "CERT-2", "SUBMITTED"},
	})
	stop := e.startWorker()
	defer stop()
	first := e.prepare(task)
	failed := e.waitRun(task.ID, first.ID, preparation.RunFailed)
	const expectedFailure = "the AI response reached its output token limit; nothing was published; retry preparation"
	if failed.Error != expectedFailure {
		t.Fatalf("persisted failure = %q, want %q", failed.Error, expectedFailure)
	}
	if failed.Options.BatchSize != 1 || failed.Options.MaxTokens != ai.DefaultMaxTokens {
		t.Fatalf("failed run options = %+v", failed.Options)
	}
	if failed.Output != nil || failed.Counts.Entries != 0 || failed.Selected {
		t.Fatalf("failed run published or selected partial results: %+v", failed)
	}
	if selected := e.loadTask(task.ID).SelectedPreparationRunID; selected != "" {
		t.Fatalf("failed run was selected: %q", selected)
	}
	payloads, models, budgets := e.model.requestSnapshot()
	if len(payloads) != 2 || len(models) != 2 || len(budgets) != 2 {
		t.Fatalf("requests before explicit retry = %d, models = %d, budgets = %d; want 2 each", len(payloads), len(models), len(budgets))
	}
	for index := range payloads {
		if len(payloads[index].Rows) != 1 || models[index] != failed.Model || budgets[index] != ai.DefaultMaxTokens {
			t.Fatalf("request %d = rows:%d model:%q budget:%d; want one row, captured model and %d tokens", index+1, len(payloads[index].Rows), models[index], budgets[index], ai.DefaultMaxTokens)
		}
	}

	e.service.SetOptions(preparation.DefaultOptions())
	retried, err := e.service.Retry(context.Background(), e.owner, task.ID)
	if err != nil {
		t.Fatalf("explicit retry: %v", err)
	}
	if retried.ID == failed.ID || retried.Kind != preparation.KindRetry {
		t.Fatalf("retry = %+v, want a new retry run", retried)
	}
	if retried.Options.BatchSize != preparation.DefaultOptions().BatchSize || retried.Options.MaxTokens != ai.MaxTokens {
		t.Fatalf("retry options = %+v, want current default batch and %d tokens", retried.Options, ai.MaxTokens)
	}
	completed := e.waitRun(task.ID, retried.ID, preparation.RunCompleted)
	if completed.Model != failed.Model || completed.Provider != failed.Provider || completed.APIRoot != failed.APIRoot || completed.APIFormat != failed.APIFormat {
		t.Fatalf("retry changed the captured AI configuration: failed=%+v retry=%+v", failed, completed)
	}
	original, err := e.service.LoadRun(context.Background(), e.owner, task.ID, failed.ID)
	if err != nil {
		t.Fatalf("reload failed run: %v", err)
	}
	if original.State != preparation.RunFailed || original.Error != expectedFailure || original.Output != nil ||
		original.Options.BatchSize != 1 || original.Options.MaxTokens != ai.DefaultMaxTokens {
		t.Fatalf("explicit retry changed prior failure evidence or options: %+v", original)
	}
	allPayloads, allModels, allBudgets := e.model.requestSnapshot()
	if len(allPayloads) != 3 || len(allModels) != 3 || len(allBudgets) != 3 {
		t.Fatalf("requests after one explicit retry = %d, models = %d, budgets = %d; want 3 each", len(allPayloads), len(allModels), len(allBudgets))
	}
	if len(allPayloads[2].Rows) != 2 || allModels[2] != completed.Model || allBudgets[2] != ai.MaxTokens {
		t.Fatalf("retry request = rows:%d model:%q budget:%d, want all rows, captured model and %d tokens", len(allPayloads[2].Rows), allModels[2], allBudgets[2], ai.MaxTokens)
	}
}

func (e *env) connectOpenCodeGo(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	draft, err := e.aiStore.StageValidation(ctx, e.owner, ai.Proposal{
		Config: ai.Config{
			Provider: ai.ProviderOpenCodeGo,
			APIRoot:  e.model.server.URL + "/v1",
			Model:    "muse-spark-1.3-contributor",
		},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("stage OpenCode Go selection: %v", err)
	}
	active, err := e.aiStore.Connection(ctx, e.owner)
	if err != nil {
		t.Fatalf("load prior AI connection: %v", err)
	}
	if _, err := e.aiStore.Confirm(ctx, e.owner, draft.ID, active.ConfigurationGeneration); err != nil {
		t.Fatalf("confirm OpenCode Go selection: %v", err)
	}
}

func responsesBody(t *testing.T, text string) string {
	t.Helper()
	encodedText, err := json.Marshal(text)
	if err != nil {
		t.Fatalf("encode model output: %v", err)
	}
	return `{"output":[{"type":"message","content":[{"type":"output_text","text":` + string(encodedText) + `}]}]}`
}
