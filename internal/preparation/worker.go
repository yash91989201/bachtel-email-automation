package preparation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Run drains durable preparation work once at startup and on notifications.
// The worker is process-owned: browser closure does not end it, and completed
// evidence never depends on a request context.
func (s *Service) Run(ctx context.Context) {
	recovered := false
	delay := time.Second
	for ctx.Err() == nil {
		var err error
		if !recovered {
			err = s.recoverInterrupted(ctx)
			recovered = err == nil
		}
		if err == nil {
			err = s.processQueued(ctx)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			s.app.Logger().Error("prepare task data", "error", err)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.wake:
				timer.Stop()
			case <-timer.C:
			}
			delay = min(delay*2, time.Minute)
			continue
		}
		delay = time.Second
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
	}
}

// recoverInterrupted marks work interrupted by a restart as incomplete. A
// retry is explicit: interrupted work never publishes partial provider output
// and never looks prepared.
func (s *Service) recoverInterrupted(ctx context.Context) error {
	now := s.options.normalize().Now().UTC()
	var taskIDs []string
	err := s.app.RunInTransaction(func(txApp core.App) error {
		jobs, err := txApp.FindRecordsByFilter(JobsCollection, "state = {:state}", "created", 0, 0, dbx.Params{"state": JobRunning})
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return err
			}
			job.Set("state", JobFailed)
			job.Set("error", ErrInterrupted.Error())
			job.Set("finished_at", now)
			if err := txApp.Save(job); err != nil {
				return err
			}
			run, err := txApp.FindRecordById(RunsCollection, job.GetString("run"))
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if run.GetString("state") != RunRunning && run.GetString("state") != RunQueued {
				continue
			}
			run.Set("state", RunIncomplete)
			run.Set("error", ErrInterrupted.Error())
			run.Set("finished_at", now)
			if err := txApp.Save(run); err != nil {
				return err
			}
			taskIDs = append(taskIDs, job.GetString("task"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		s.notifySettled(taskID)
	}
	return nil
}

// processQueued runs every queued job in creation order. A job-level failure is
// persisted on the run by runJob, so a returned error is a discovery or
// persistence error that warrants a retry of the loop.
func (s *Service) processQueued(ctx context.Context) error {
	jobs, err := s.app.FindRecordsByFilter(JobsCollection, "state = {:state}", "created", 0, 0, dbx.Params{"state": JobQueued})
	if err != nil {
		return err
	}
	var processErr error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.runJob(ctx, job.Id); err != nil {
			if ctx.Err() != nil {
				return err
			}
			processErr = errors.Join(processErr, err)
		}
	}
	return processErr
}

// runJob claims one queued job and executes its captured batches. Every batch
// is validated against the captured input before the next request; nothing is
// published unless the complete response set is valid.
func (s *Service) runJob(ctx context.Context, jobID string) error {
	now := s.options.normalize().Now().UTC()
	attempt := newToken()
	var runRecord *core.Record
	var taskID, ownerID string
	claimed := false
	cancelled := false
	err := s.app.RunInTransaction(func(txApp core.App) error {
		job, err := txApp.FindRecordById(JobsCollection, jobID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if job.GetString("state") != JobQueued {
			return nil
		}
		run, err := txApp.FindRecordById(RunsCollection, job.GetString("run"))
		if err != nil {
			return fmt.Errorf("load the preparation run: %w", err)
		}
		task, err := txApp.FindRecordById(intake.TasksCollection, job.GetString("task"))
		if err != nil {
			return fmt.Errorf("load the prepared task: %w", err)
		}
		if closedOrClosing(task) {
			job.Set("state", JobCancelled)
			job.Set("error", ErrCancelled.Error())
			job.Set("finished_at", now)
			if err := txApp.Save(job); err != nil {
				return err
			}
			run.Set("state", RunCancelled)
			run.Set("error", ErrCancelled.Error())
			run.Set("finished_at", now)
			if err := txApp.Save(run); err != nil {
				return err
			}
			cancelled = true
			taskID = job.GetString("task")
			return nil
		}
		job.Set("state", JobRunning)
		job.Set("attempt", attempt)
		job.Set("started_at", now)
		if err := txApp.Save(job); err != nil {
			return err
		}
		run.Set("state", RunRunning)
		run.Set("started_at", now)
		if err := txApp.Save(run); err != nil {
			return err
		}
		runRecord = run
		taskID = job.GetString("task")
		ownerID = job.GetString("owner")
		claimed = true
		return nil
	})
	if err != nil {
		return err
	}
	if cancelled {
		s.notifySettled(taskID)
		return nil
	}
	if !claimed {
		return nil
	}
	return s.execute(ctx, jobID, runRecord, taskID, ownerID, attempt)
}

// execute sends the captured batches over the captured configuration. A
// provider or validation failure terminates the run with fixed text; a context
// cancellation leaves the claim for restart recovery.
func (s *Service) execute(ctx context.Context, jobID string, run *core.Record, taskID, ownerID, attempt string) error {
	input, err := s.loadInput(ctx, run)
	if err != nil {
		s.app.Logger().Error("read captured preparation input", "run", run.Id, "error", err)
		return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, "the captured preparation input could not be read; start preparation again", err)
	}
	rawConfig := run.GetString("api_root")
	captured := ai.Config{
		Provider:  ai.Provider(run.GetString("provider")),
		APIRoot:   rawConfig,
		APIFormat: ai.APIFormat(run.GetString("api_format")),
		Model:     run.GetString("model"),
	}
	active, err := s.ai.Connection(ctx, ownerID)
	if err != nil {
		s.app.Logger().Error("load confirmed AI connection", "run", run.Id, "error", err)
		return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, ErrConfigurationChanged.Error(), err)
	}
	if !active.Config.Equal(captured) {
		return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, ErrConfigurationChanged.Error(), nil)
	}
	var options RunOptions
	if err := run.UnmarshalJSONField("options", &options); err != nil {
		return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, ErrInvalidResponse.Error(), err)
	}
	if options.BatchSize <= 0 {
		options.BatchSize = DefaultBatchSize
	}
	if options.MaxTokens <= 0 {
		options.MaxTokens = ai.DefaultMaxTokens
	}
	op := operation{
		inference: func(ctx context.Context, config ai.Config, request ai.InferenceRequest) (string, error) {
			return s.ai.CompleteFor(ctx, ownerID, config, request)
		},
		config:       captured,
		prompt:       run.GetString("base_prompt"),
		instructions: run.GetString("instructions"),
		options:      options,
		session:      newToken(),
	}
	results := make([]RowResult, 0, len(input.Rows))
	batches := chunkRows(input.Rows, options.BatchSize)
	for index, batch := range batches {
		if err := ctx.Err(); err != nil {
			return err
		}
		stopped, err := s.stopIfClosed(ctx, jobID, run.Id, attempt, taskID)
		if err != nil || stopped {
			return err
		}
		parsed, err := op.normalize(ctx, input, batch)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if errors.Is(err, ErrInvalidResponse) {
				s.app.Logger().Warn("preparation response rejected", "run", run.Id, "batch", index+1, "reason", InvalidReason(err))
				return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, ErrInvalidResponse.Error(), err)
			}
			s.app.Logger().Warn("preparation request failed", "run", run.Id, "batch", index+1, "error", err)
			return s.finishFailure(ctx, jobID, run.Id, attempt, taskID, failureText(err), err)
		}
		results = append(results, parsed...)
		if err := s.markProgress(ctx, jobID, attempt, index+1); err != nil {
			return err
		}
	}
	output := Derive(input, results)
	return s.publish(ctx, jobID, run.Id, attempt, taskID, output)
}

// publish stores the validated output and completes the run in one short
// transaction. A job whose attempt token changed, or a task that closed while
// the provider answered, never publishes.
func (s *Service) publish(ctx context.Context, jobID, runID, attempt, taskID string, output Output) error {
	data, err := json.Marshal(output)
	if err != nil {
		return s.finishFailure(ctx, jobID, runID, attempt, taskID, ErrInvalidResponse.Error(), err)
	}
	now := s.options.normalize().Now().UTC()
	var settled bool
	err = s.app.RunInTransaction(func(txApp core.App) error {
		job, err := txApp.FindRecordById(JobsCollection, jobID)
		if err != nil {
			return err
		}
		if job.GetString("state") != JobRunning || job.GetString("attempt") != attempt {
			// A stale response from an older claim is discarded.
			return nil
		}
		run, err := txApp.FindRecordById(RunsCollection, runID)
		if err != nil {
			return err
		}
		if run.GetString("state") != RunRunning {
			return nil
		}
		task, err := txApp.FindRecordById(intake.TasksCollection, taskID)
		if err != nil {
			return err
		}
		if closedOrClosing(task) {
			job.Set("state", JobCancelled)
			job.Set("error", ErrCancelled.Error())
			job.Set("finished_at", now)
			if err := txApp.Save(job); err != nil {
				return err
			}
			run.Set("state", RunCancelled)
			run.Set("error", ErrCancelled.Error())
			run.Set("finished_at", now)
			if err := txApp.Save(run); err != nil {
				return err
			}
			settled = true
			return nil
		}
		file, err := filesystem.NewFileFromBytes(data, "preparation-output.json")
		if err != nil {
			return fmt.Errorf("store the preparation output: %w", err)
		}
		run.Set("output", file)
		run.Set("output_digest", digestHex(data))
		run.Set("counts", output.Counts)
		run.Set("state", RunCompleted)
		run.Set("error", "")
		run.Set("finished_at", now)
		if err := txApp.Save(run); err != nil {
			return err
		}
		job.Set("state", JobDone)
		job.Set("batch_done", job.GetInt("batch_total"))
		job.Set("error", "")
		job.Set("finished_at", now)
		if err := txApp.Save(job); err != nil {
			return err
		}
		// Publication selects the completed run as prepared evidence when the
		// task is still runnable and no dataset is finalized. A finalized
		// dataset is never replaced because AI finished; that stays an explicit
		// operator choice. Lifecycle refusals skip selection and still settle.
		if task.GetString("selected_final_snapshot") == "" && runnableTask(intakeTask(task)) == nil {
			if selectPreparedRun(task, run.Id) {
				if err := txApp.Save(task); err != nil {
					return err
				}
			}
		}
		settled = true
		return nil
	})
	if err != nil {
		return err
	}
	if settled {
		s.notifySettled(taskID)
	}
	return nil
}

// markProgress records completed batches. A lost attempt stops the loop, so a
// superseded claim performs no further provider calls.
func (s *Service) markProgress(ctx context.Context, jobID, attempt string, done int) error {
	job, err := s.app.FindRecordById(JobsCollection, jobID)
	if err != nil {
		return err
	}
	if job.GetString("state") != JobRunning || job.GetString("attempt") != attempt {
		return nil
	}
	job.Set("batch_done", done)
	return s.app.Save(job)
}

// stopIfClosed abandons the claim when the task requested closure. It reports
// whether the claim ended.
func (s *Service) stopIfClosed(ctx context.Context, jobID, runID, attempt, taskID string) (bool, error) {
	task, err := s.app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	if !closedOrClosing(task) {
		return false, nil
	}
	now := s.options.normalize().Now().UTC()
	stopped := false
	err = s.app.RunInTransaction(func(txApp core.App) error {
		job, err := txApp.FindRecordById(JobsCollection, jobID)
		if err != nil {
			return err
		}
		if job.GetString("state") != JobRunning || job.GetString("attempt") != attempt {
			return nil
		}
		job.Set("state", JobCancelled)
		job.Set("error", ErrCancelled.Error())
		job.Set("finished_at", now)
		if err := txApp.Save(job); err != nil {
			return err
		}
		run, err := txApp.FindRecordById(RunsCollection, runID)
		if err != nil {
			return err
		}
		if run.GetString("state") == RunRunning {
			run.Set("state", RunCancelled)
			run.Set("error", ErrCancelled.Error())
			run.Set("finished_at", now)
			if err := txApp.Save(run); err != nil {
				return err
			}
		}
		stopped = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if stopped {
		s.notifySettled(taskID)
	}
	return stopped, nil
}

// finishFailure records a terminal run failure in one transaction. A superseded
// claim records nothing.
func (s *Service) finishFailure(ctx context.Context, jobID, runID, attempt, taskID, message string, cause error) error {
	if cause != nil {
		s.app.Logger().Warn("preparation failed", "run", runID, "error", cause)
	}
	now := s.options.normalize().Now().UTC()
	settled := false
	err := s.app.RunInTransaction(func(txApp core.App) error {
		job, err := txApp.FindRecordById(JobsCollection, jobID)
		if err != nil {
			return err
		}
		if job.GetString("state") != JobRunning || job.GetString("attempt") != attempt {
			return nil
		}
		job.Set("state", JobFailed)
		job.Set("error", message)
		job.Set("finished_at", now)
		if err := txApp.Save(job); err != nil {
			return err
		}
		run, err := txApp.FindRecordById(RunsCollection, runID)
		if err != nil {
			return err
		}
		if run.GetString("state") == RunRunning {
			run.Set("state", RunFailed)
			run.Set("error", message)
			run.Set("finished_at", now)
			if err := txApp.Save(run); err != nil {
				return err
			}
		}
		settled = true
		return nil
	})
	if err != nil {
		return err
	}
	if settled {
		s.notifySettled(taskID)
	}
	return nil
}

// notifySettled reports a terminal preparation state to the closure lifecycle.
func (s *Service) notifySettled(taskID string) {
	if s.settled == nil || taskID == "" {
		return
	}
	s.settled(taskID)
}

// closedOrClosing reports a terminal or in-progress closure.
func closedOrClosing(task *core.Record) bool {
	return task.GetString("closed_at") != "" || task.GetString("closure_requested_at") != ""
}

// chunkRows splits one captured input into bounded request batches.
func chunkRows(rows []InputRow, size int) [][]InputRow {
	if size <= 0 {
		size = DefaultBatchSize
	}
	var batches [][]InputRow
	for start := 0; start < len(rows); start += size {
		end := min(start+size, len(rows))
		batches = append(batches, rows[start:end])
	}
	return batches
}

// failureText maps one provider or validation failure to fixed operator-facing
// text. Raw provider bodies never reach a page or a stored run.
func failureText(err error) string {
	switch {
	case errors.Is(err, ai.ErrNotConfigured), errors.Is(err, ai.ErrCredentialUnreadable):
		return ErrSetup.Error()
	case errors.Is(err, ai.ErrAuthentication):
		return "the AI endpoint rejected the saved API key; repair the key in Account settings and retry preparation"
	case errors.Is(err, ai.ErrModelUnavailable):
		return "the selected model is not available at the confirmed endpoint; repair the AI connection and retry preparation"
	case errors.Is(err, ai.ErrThrottled):
		return "the AI endpoint is throttling requests; retry preparation later"
	case errors.Is(err, ai.ErrEndpointTimeout):
		return "the AI endpoint did not answer within the allowed time; retry preparation"
	case errors.Is(err, ai.ErrResponseTooLarge):
		return "the AI response exceeded the allowed size; retry preparation"
	case errors.Is(err, ai.ErrEndpointUnsafe):
		return "the confirmed AI endpoint is not usable; fix the AI connection in Account settings"
	case errors.Is(err, ai.ErrOutputLimit):
		return "the AI response reached its output token limit; nothing was published; retry preparation"
	case errors.Is(err, ai.ErrOutputContract), errors.Is(err, ErrInvalidResponse):
		return ErrInvalidResponse.Error()
	case errors.Is(err, ai.ErrInvalidSetup):
		return ErrSetup.Error()
	default:
		return "the AI endpoint returned an error; retry preparation"
	}
}
