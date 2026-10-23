package preparation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Service owns the preparation lifecycle: input capture, durable run creation,
// explicit selection and the server-owned worker that calls the confirmed AI
// connection. It never sends email and never writes candidate, membership or
// attempt records.
type Service struct {
	app       core.App
	tasks     *intake.Service
	campaigns *campaign.Service
	ai        *ai.Store
	options   Options
	wake      chan struct{}
	settled   Settled
}

// NewService returns the preparation service bound to one application.
func NewService(app core.App, tasks *intake.Service, campaigns *campaign.Service, aiStore *ai.Store) *Service {
	return &Service{
		app:       app,
		tasks:     tasks,
		campaigns: campaigns,
		ai:        aiStore,
		options:   DefaultOptions(),
		wake:      make(chan struct{}, 1),
	}
}

// SetOptions binds the inference bounds used by future runs. Production uses
// the defaults; tests may lower the batch size.
func (s *Service) SetOptions(options Options) { s.options = options.normalize() }

// SetSettled binds the closure notice called after one task's preparation work
// reaches a terminal state. It is injected so this package stays independent of
// the sending lifecycle.
func (s *Service) SetSettled(fn Settled) { s.settled = fn }

// Trigger asks the background worker to look for queued work now.
func (s *Service) Trigger() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Prepare starts an automatic preparation run: the first run of a task, a
// retry after failure, or a rerun after a completed run.
func (s *Service) Prepare(ctx context.Context, ownerID, taskID string) (*Run, error) {
	return s.start(ctx, ownerID, taskID, "")
}

// Retry starts a new run after failed or interrupted work. Completed evidence
// is never modified.
func (s *Service) Retry(ctx context.Context, ownerID, taskID string) (*Run, error) {
	return s.start(ctx, ownerID, taskID, KindRetry)
}

// Rerun starts a new run after completed work, capturing the current
// instructions and the currently confirmed AI configuration.
func (s *Service) Rerun(ctx context.Context, ownerID, taskID string) (*Run, error) {
	return s.start(ctx, ownerID, taskID, KindRerun)
}

// start captures the input and the exact configuration before creating the
// durable run and job in one short transaction. Network calls never happen in
// the transaction; the worker performs them from the captured input.
func (s *Service) start(ctx context.Context, ownerID, taskID, requestedKind string) (*Run, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil, ErrNotFound
	}
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if err := runnableTask(task); err != nil {
		return nil, err
	}
	runs, err := s.runRecords(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	for _, record := range runs {
		switch record.GetString("state") {
		case RunQueued, RunRunning:
			return nil, ErrInProgress
		}
	}
	kind, err := resolveKind(requestedKind, runs)
	if err != nil {
		return nil, err
	}
	configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID)
	if err != nil {
		return nil, err
	}
	if task.ConfigurationID == "" || task.ContentHash == "" || configuration.ContentHash != task.ContentHash {
		return nil, ErrStale
	}
	rows, err := s.tasks.MappedRows(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrEmpty
	}
	input, err := BuildInput(configuration, task.SourceDigest, rows)
	if err != nil {
		return nil, err
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode the preparation input: %w", err)
	}
	connection, err := s.ai.Connection(ctx, ownerID)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			return nil, ErrSetup
		}
		return nil, err
	}
	// Resolve the credential before the run exists: an unusable deployment
	// key must block preparation instead of failing every batch later.
	if _, err := s.ai.Credential(ctx, ownerID); err != nil {
		return nil, ErrSetup
	}
	options := s.options.normalize()
	prompt := CurrentBasePrompt()
	batchTotal := (len(input.Rows) + options.BatchSize - 1) / options.BatchSize

	var run *Run
	err = s.app.RunInTransaction(func(txApp core.App) error {
		currentRecord, err := ownedTaskRecord(txApp, ownerID, taskID)
		if err != nil {
			return err
		}
		if err := runnableTask(intakeTask(currentRecord)); err != nil {
			return err
		}
		active, err := activeRunRecord(txApp, taskID)
		if err != nil {
			return err
		}
		if active != nil {
			return ErrInProgress
		}
		collection, err := txApp.FindCollectionByNameOrId(RunsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", RunsCollection, err)
		}
		record := core.NewRecord(collection)
		record.Set("owner", ownerID)
		record.Set("task", taskID)
		record.Set("kind", kind)
		record.Set("state", RunQueued)
		record.Set("provider", string(connection.Config.Provider))
		record.Set("api_root", connection.Config.APIRoot)
		record.Set("api_format", string(connection.Config.APIFormat))
		record.Set("model", connection.Config.Model)
		record.Set("configuration_generation", connection.ConfigurationGeneration)
		record.Set("credential_generation", connection.CredentialGeneration)
		record.Set("configuration", configuration.ID)
		record.Set("content_hash", configuration.ContentHash)
		record.Set("source_digest", task.SourceDigest)
		record.Set("base_prompt_version", BasePromptVersion)
		record.Set("base_prompt", prompt)
		record.Set("instructions", task.PreparationInstructions)
		record.Set("options", RunOptions{BatchSize: options.BatchSize, MaxTokens: options.MaxTokens, Temperature: options.Temperature})
		record.Set("input_digest", digestHex(inputBytes))
		record.Set("counts", Counts{})
		inputFile, err := filesystem.NewFileFromBytes(inputBytes, "preparation-input.json")
		if err != nil {
			return fmt.Errorf("store the preparation input: %w", err)
		}
		record.Set("input", inputFile)
		if err := txApp.Save(record); err != nil {
			return err
		}

		jobs, err := txApp.FindCollectionByNameOrId(JobsCollection)
		if err != nil {
			return fmt.Errorf("%s collection is missing; run migrations first: %w", JobsCollection, err)
		}
		job := core.NewRecord(jobs)
		job.Set("owner", ownerID)
		job.Set("task", taskID)
		job.Set("run", record.Id)
		job.Set("state", JobQueued)
		job.Set("attempt", newToken())
		job.Set("batch_total", batchTotal)
		job.Set("batch_done", 0)
		if err := txApp.Save(job); err != nil {
			return err
		}
		run = runFromRecord(record)
		run.BatchTotal = batchTotal
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Trigger()
	return run, nil
}

// SetInstructions saves the editable task-instruction draft. It never changes
// a completed run; only a later explicit run captures the new text.
func (s *Service) SetInstructions(ctx context.Context, ownerID, taskID, text string) error {
	if len(text) > MaxInstructions {
		return fmt.Errorf("task instructions must be at most %d bytes", MaxInstructions)
	}
	if err := s.app.RunInTransaction(func(txApp core.App) error {
		record, err := ownedTaskRecord(txApp, ownerID, taskID)
		if err != nil {
			return err
		}
		task := intakeTask(record)
		if task.Closed() || task.Closing() {
			return ErrClosed
		}
		record.Set("preparation_instructions", text)
		return txApp.Save(record)
	}); err != nil {
		return err
	}
	return nil
}

// SelectRun marks one completed run as the prepared evidence selected for
// review. Selection is preparation-only: it changes no candidate, membership,
// attempt, schedule or approval record.
func (s *Service) SelectRun(ctx context.Context, ownerID, taskID, runID string) (*Run, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	if err := runnableTask(task); err != nil {
		return nil, err
	}
	run, err := s.runRecord(ctx, ownerID, taskID, runID)
	if err != nil {
		return nil, err
	}
	if run.GetString("state") != RunCompleted {
		return nil, ErrNotCompleted
	}
	err = s.app.RunInTransaction(func(txApp core.App) error {
		currentRecord, err := ownedTaskRecord(txApp, ownerID, taskID)
		if err != nil {
			return err
		}
		if err := runnableTask(intakeTask(currentRecord)); err != nil {
			return err
		}
		selected, err := txApp.FindFirstRecordByFilter(
			RunsCollection,
			"id = {:id} && owner = {:owner} && task = {:task}",
			dbx.Params{"id": runID, "owner": ownerID, "task": taskID},
		)
		if err != nil {
			return ErrNotFound
		}
		if selected.GetString("state") != RunCompleted {
			return ErrNotCompleted
		}
		// Changing selected prepared evidence invalidates any finalized dataset
		// and its approvals. The snapshot record stays as history; the task no
		// longer points at it, so no approval can authorize the old values.
		if currentRecord.GetString("selected_preparation_run") != selected.Id {
			currentRecord.Set("selected_final_snapshot", "")
			currentRecord.Set("final_snapshot_digest", "")
			currentRecord.Set("finalization_error", "")
		}
		currentRecord.Set("selected_preparation_run", selected.Id)
		return txApp.Save(currentRecord)
	})
	if err != nil {
		return nil, err
	}
	mapped := runFromRecord(run)
	mapped.Selected = true
	return mapped, nil
}

// RunList returns one task's runs newest first with their batch progress.
func (s *Service) RunList(ctx context.Context, ownerID, taskID string) ([]*Run, error) {
	if _, err := s.taskRecord(ctx, ownerID, taskID); err != nil {
		return nil, err
	}
	records, err := s.runRecords(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	jobs, err := s.app.FindRecordsByFilter(JobsCollection, "task = {:task} && owner = {:owner}", "", 0, 0, dbx.Params{"task": taskID, "owner": ownerID})
	if err != nil {
		return nil, err
	}
	byRun := make(map[string]*core.Record, len(jobs))
	for _, job := range jobs {
		byRun[job.GetString("run")] = job
	}
	runs := make([]*Run, 0, len(records))
	for _, record := range records {
		runs = append(runs, withJob(runFromRecord(record), byRun[record.Id]))
	}
	return runs, nil
}

// LoadRun returns one run and, when completed, its published output evidence.
func (s *Service) LoadRun(ctx context.Context, ownerID, taskID, runID string) (*Run, error) {
	record, err := s.runRecord(ctx, ownerID, taskID, runID)
	if err != nil {
		return nil, err
	}
	run := runFromRecord(record)
	if job, err := s.jobRecord(ctx, record.Id); err == nil {
		withJob(run, job)
	}
	if run.State == RunCompleted {
		output, err := s.loadOutput(ctx, record)
		if err != nil {
			return nil, err
		}
		run.Output = &output
	}
	return run, nil
}

// runnableTask refuses preparation outside an unstarted, unclosed, unscheduled
// validated task. The same guard is repeated inside the creating transaction,
// so a stale page is never authority.
func runnableTask(task *intake.Task) error {
	if task.Closed() || task.Closing() {
		return ErrClosed
	}
	if task.Status == intake.TaskStaging {
		return ErrNotReady
	}
	if task.Status != intake.TaskReady {
		return ErrStarted
	}
	if task.ScheduleState == intake.ScheduleScheduled {
		return ErrScheduled
	}
	if task.ScheduleState == intake.ScheduleFired {
		return ErrStarted
	}
	return nil
}

// resolveKind selects the run kind and refuses an explicit retry or rerun that
// has no matching completed or failed work.
func resolveKind(requested string, runs []*core.Record) (string, error) {
	latest := ""
	if len(runs) > 0 {
		latest = runs[0].GetString("state")
	}
	completed := false
	for _, run := range runs {
		if run.GetString("state") == RunCompleted {
			completed = true
			break
		}
	}
	switch requested {
	case KindRetry:
		if latest != RunFailed && latest != RunIncomplete {
			return "", ErrNotFailed
		}
		return KindRetry, nil
	case KindRerun:
		if !completed {
			return "", ErrNotCompleted
		}
		return KindRerun, nil
	case "":
		switch {
		case latest == RunFailed || latest == RunIncomplete:
			return KindRetry, nil
		case completed:
			return KindRerun, nil
		default:
			return KindInitial, nil
		}
	default:
		return "", fmt.Errorf("unsupported preparation kind")
	}
}

// intakeTask maps a task record without loading its campaign name.
func intakeTask(record *core.Record) *intake.Task {
	// The record shape is owned by intake; LoadTask maps it through the same
	// helper. Here only the lifecycle fields are needed.
	return &intake.Task{
		ID:                       record.Id,
		CampaignID:               record.GetString("campaign"),
		Status:                   record.GetString("status"),
		ScheduleState:            record.GetString("schedule_state"),
		ClosureRequestedAt:       record.GetDateTime("closure_requested_at").Time().UTC(),
		ClosedAt:                 record.GetDateTime("closed_at").Time().UTC(),
		PreparationInstructions:  record.GetString("preparation_instructions"),
		SelectedPreparationRunID: record.GetString("selected_preparation_run"),
	}
}

// digestHex is the canonical artifact digest.
func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newToken returns one random claim token.
func newToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// A token is a freshness guard, not a security boundary: a repeated
		// value would only weaken stale-response detection, never expose data.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
