package web

// Preparation evidence for the task workspace. The view model is derived from
// stored runs; nothing here is cached page state, and rendering never calls the
// provider.

import (
	"context"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Evidence list bounds: the page shows the newest entries with an explicit
// count of what is not listed, so a large run never crowds out the controls.
const (
	preparationNoteLimit  = 100
	preparationEntryLimit = 100
)

// preparationData builds the preparation section model of one task.
func (s *server) preparationData(ctx context.Context, ownerID string, task *intake.Task) views.TaskPreparationView {
	data := views.TaskPreparationView{
		Available:     s.preparation != nil,
		PromptVersion: preparation.BasePromptVersion,
		Prompt:        preparation.CurrentBasePrompt(),
		Instructions:  task.PreparationInstructions,
	}
	if s.preparation == nil {
		return data
	}
	if connection, err := s.aiConnection(ctx, ownerID); err == nil && connection != nil {
		data.Configured = true
		data.ConnectionLabel = connection.Config.Provider.Label() + " · " + connection.Config.APIFormat.Label() + " · " + connection.Config.Model
		data.Endpoint = connection.Config.APIRoot
	}
	manageable, blocked := preparationAvailability(task)
	data.CanManage = manageable
	data.BlockedReason = blocked
	runs, err := s.preparation.RunList(ctx, ownerID, task.ID)
	if err != nil {
		s.app.Logger().Error("list preparation runs", "task", task.ID, "error", err)
		data.BlockedReason = "Preparation evidence is unavailable; reload this page."
		data.CanManage = false
		return data
	}
	active := false
	var completed *preparation.Run
	for _, run := range runs {
		if run.State == preparation.RunQueued || run.State == preparation.RunRunning {
			active = true
		}
		if completed == nil && run.State == preparation.RunCompleted {
			completed = run
		}
		data.Runs = append(data.Runs, preparationRunView(run, manageable && !active))
	}
	data.SelectedRunID = task.SelectedPreparationRunID
	if active {
		data.BlockedReason = "Preparation is queued or running. This page shows the durable state; closing the browser does not stop it."
	}
	latest := (*preparation.Run)(nil)
	if len(runs) > 0 {
		latest = runs[0]
	}
	data.CanPrepare = manageable && !active && len(runs) == 0
	data.CanRetry = manageable && !active && latest != nil && (latest.State == preparation.RunFailed || latest.State == preparation.RunIncomplete)
	data.CanRerun = manageable && !active && completed != nil
	for index := range data.Runs {
		if data.Runs[index].ID != task.SelectedPreparationRunID {
			continue
		}
		data.Runs[index].Selected = true
	}
	evidence := completed
	if task.SelectedPreparationRunID != "" {
		for _, run := range runs {
			if run.ID == task.SelectedPreparationRunID && run.State == preparation.RunCompleted {
				evidence = run
				break
			}
		}
	}
	if evidence == nil {
		return data
	}
	loaded, err := s.preparation.LoadRun(ctx, ownerID, task.ID, evidence.ID)
	if err != nil {
		s.app.Logger().Error("load preparation evidence", "task", task.ID, "run", evidence.ID, "error", err)
		data.BlockedReason = "The selected preparation output could not be read; prepare the data again."
		return data
	}
	if loaded.Output == nil {
		return data
	}
	data.EvidenceRunID = loaded.ID
	data.EvidenceSelected = loaded.ID == task.SelectedPreparationRunID
	data.Counts = views.TaskPreparationCounts{
		SourceRows:  loaded.Output.Counts.SourceRows,
		Entries:     loaded.Output.Counts.Entries,
		Accepted:    loaded.Output.Counts.Accepted,
		Held:        loaded.Output.Counts.Held,
		Corrections: loaded.Output.Counts.Corrections,
		Findings:    loaded.Output.Counts.Findings,
	}
	for _, row := range loaded.Output.Rows {
		for _, correction := range row.Corrections {
			if len(data.Corrections) >= preparationNoteLimit {
				data.CorrectionsHidden++
				continue
			}
			data.Corrections = append(data.Corrections, views.TaskPreparationNote{
				Row:   correction.Row,
				Field: correction.Header,
				Note:  correction.Note,
				Scope: "correction",
			})
		}
		for _, finding := range row.Findings {
			if len(data.Findings) >= preparationNoteLimit {
				data.FindingsHidden++
				continue
			}
			data.Findings = append(data.Findings, views.TaskPreparationNote{
				Row:      finding.Row,
				Position: finding.Position,
				Field:    finding.Header,
				Scope:    finding.Scope,
				Note:     finding.Note,
			})
		}
	}
	for _, entry := range loaded.Output.Entries {
		if entry.Outcome != preparation.OutcomeHeld {
			continue
		}
		if len(data.Held) >= preparationEntryLimit {
			data.HeldHidden++
			continue
		}
		data.Held = append(data.Held, views.TaskPreparationHeld{
			Row:       entry.SourceRow,
			Position:  entry.Position,
			Recipient: entry.Recipient,
			Reasons:   strings.Join(entry.Reasons, "; "),
		})
	}
	return data
}

// preparationRunView maps one run to its table row.
func preparationRunView(run *preparation.Run, canSelect bool) views.TaskPreparationRun {
	row := views.TaskPreparationRun{
		ID:            run.ID,
		Kind:          run.Kind,
		State:         run.State,
		Model:         run.Model,
		Provider:      run.Provider.Label(),
		APIRoot:       run.APIRoot,
		APIFormat:     run.APIFormat.Label(),
		PromptVersion: run.PromptVersion,
		Instructions:  run.Instructions,
		CreatedAt:     presentation.Timestamp(run.CreatedAt),
		StartedAt:     presentation.Timestamp(run.StartedAt),
		FinishedAt:    presentation.Timestamp(run.FinishedAt),
		Error:         run.Error,
		Accepted:      run.Counts.Accepted,
		Held:          run.Counts.Held,
		Corrections:   run.Counts.Corrections,
		Findings:      run.Counts.Findings,
		CanSelect:     canSelect && run.State == preparation.RunCompleted,
	}
	if run.BatchTotal > 0 {
		row.Batches = strconv.Itoa(run.BatchDone) + "/" + strconv.Itoa(run.BatchTotal)
	}
	return row
}

// preparationAvailability reports whether prepared data may change, and the
// fixed reason when it may not.
func preparationAvailability(task *intake.Task) (bool, string) {
	switch {
	case task.Closed() || task.Closing():
		return false, "This task is closed. Preparation is unavailable; upload a new task for further work."
	case task.Status == intake.TaskStaging:
		return false, "Validate the workbook before preparing data."
	case task.Status != intake.TaskReady:
		return false, "Preparation is only available before sending starts. Upload a new task for further work."
	case task.ScheduleState == intake.ScheduleScheduled:
		return false, "Clear the scheduled start before preparing data."
	case task.ScheduleState == intake.ScheduleFired:
		return false, "Preparation is only available before sending starts. Upload a new task for further work."
	default:
		return true, ""
	}
}
