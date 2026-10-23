package web

// Task intake routes. Every handler runs behind requireOperator, every
// operator-fixable failure re-renders the page inline with status 400, and no
// handler here sends email: a validated task is ready, and starting belongs to
// a later change.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// registerTaskRoutes mounts the task intake UI on the shared mux. Every route
// runs behind requireOperator.
func (s *server) registerTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /campaigns/{id}/tasks", s.requireOperator(s.handleTaskList))
	mux.HandleFunc("GET /campaigns/{id}/tasks/new", s.requireOperator(s.handleTaskNew))
	mux.HandleFunc("POST /campaigns/{id}/tasks", s.requireOperator(s.handleTaskCreate))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}", s.requireOperator(s.handleTaskDetail))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/sending-type", s.requireOperator(s.handleTaskSendingType))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/select", s.requireOperator(s.handleTaskSelect))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/revalidate", s.requireOperator(s.handleTaskRevalidate))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preview", s.requireOperator(s.handleTaskPreview))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/excluded.xlsx", s.requireOperator(s.handleTaskExcluded))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/results.xlsx", s.requireOperator(s.handleTaskResultsDownload))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/results", s.requireOperator(s.handleTaskResultsGenerate))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/results/delete", s.requireOperator(s.handleTaskResultsDelete))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/close", s.requireOperator(s.handleTaskClose))
}

// handleTaskList renders the campaign workspace on its Tasks section. The
// dedicated route stays for old links; the content and navigation are the same
// as the campaign root.
func (s *server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	data, err := s.campaignPageData(r, r.PathValue("id"), views.CampaignSectionTasks, nil)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.CampaignPage(data))
}

// handleTaskNew renders the guided upload form with campaign context, the
// current mapped columns and the configured workbook limits.
func (s *server) handleTaskNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, r.PathValue("id"))
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskUpload(s.taskUploadData(ctx, ownerID, loaded.ID, loaded.Name, "")))
}

// handleTaskCreate stores one uploaded workbook as a staging task. The body is
// bounded before any part is read, and only fixed intake messages ever reach
// the form again.
func (s *server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	id := r.PathValue("id")
	loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, id)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	// One workbook plus about 1 MiB of multipart framing: the bound applies
	// before any part is read into memory.
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Intake.MaxUploadBytes+(1<<20))

	sendingType := ""
	uploadError := func(message string) {
		data := s.taskUploadData(ctx, ownerID, loaded.ID, loaded.Name, message)
		data.SendingType = sendingType
		s.renderWithStatus(w, r, http.StatusBadRequest, views.TaskUpload(data))
	}

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			uploadError(intake.ErrUploadTooLarge.Error())
		} else {
			uploadError("the form could not be read; choose an XLSX workbook and try again")
		}
		return
	}
	sendingType = r.FormValue("sending_type")
	if sendingType != intake.TaskSendingTypeDirect && sendingType != intake.TaskSendingTypeScheduled {
		uploadError("choose Direct or Scheduled sending")
		return
	}
	file, header, err := r.FormFile("workbook")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			uploadError(intake.ErrNoUpload.Error())
		} else {
			s.app.Logger().Error("read workbook part", "campaign", id, "error", err)
			uploadError("the form could not be read; choose an XLSX workbook and try again")
		}
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			s.app.Logger().Warn("close uploaded file", "error", err)
		}
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			uploadError(intake.ErrUploadTooLarge.Error())
		} else {
			s.app.Logger().Error("read workbook body", "campaign", id, "error", err)
			uploadError("the uploaded workbook could not be read; try again")
		}
		return
	}
	task, err := s.tasks.StageWithSendingType(ctx, ownerID, id, header.Filename, data, sendingType)
	if err != nil {
		if message := intakeErrorMessage(err); message != "" {
			uploadError(message)
			return
		}
		s.app.Logger().Error("stage task", "campaign", id, "error", err)
		uploadError("the workbook could not be uploaded; try again")
		return
	}
	http.Redirect(w, r, taskPath(task)+"#recipients", http.StatusSeeOther)
}

// handleTaskDetail renders the one task workspace. Without a recognized
// fragment the selected section is Overview.
func (s *server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	data, err := s.taskPageData(r, task, "")
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskPage(data))
}

// handleTaskSendingType persists the shared task preference. GET navigation
// cannot change it and saving it never starts mail or creates a schedule.
func (s *server) handleTaskSendingType(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid sending type form", http.StatusBadRequest)
		return
	}
	if err := s.tasks.SetSendingType(r.Context(), recordFromContext(r).Id, task.ID, r.PostForm.Get("sending_type")); err != nil {
		if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
			s.renderTaskMessage(w, r, task, views.TaskSectionSending, validation.Message, http.StatusBadRequest)
			return
		}
		s.taskLoadFailure(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#sending", http.StatusSeeOther)
}

// handleTaskSelect confirms the worksheet and header row of a staging task.
// Success redirects to Recipients; a validation failure re-renders it inline.
func (s *server) handleTaskSelect(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	headerRow, err := strconv.Atoi(strings.TrimSpace(r.FormValue("header_row")))
	if err != nil {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, intake.ErrInvalidHeaderRow.Error(), http.StatusBadRequest)
		return
	}
	sheet := r.FormValue("sheet")
	if err := s.tasks.Validate(ctx, ownerID, task.ID, sheet, headerRow); err != nil {
		if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
			s.renderTaskMessageWith(w, r, task, views.TaskSectionRecipients, validation.Message, http.StatusBadRequest, func(data *views.TaskPageData) {
				data.SheetDraft = sheet
				data.HeaderRowDraft = headerRow
			})
			return
		}
		s.taskLoadFailure(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#recipients", http.StatusSeeOther)
}

// handleTaskRevalidate rebuilds the snapshot against the campaign's current
// revision. An enabled schedule refuses the change inside the service
// transaction; the message is shown at Recipients.
func (s *server) handleTaskRevalidate(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if err := s.tasks.Revalidate(r.Context(), recordFromContext(r).Id, task.ID); err != nil {
		if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
			s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, validation.Message, http.StatusBadRequest)
			return
		}
		s.taskLoadFailure(w, r, err)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#recipients", http.StatusSeeOther)
}

// handleTaskPreview renders one admitted row through the saved revision as an
// htmx partial. A row outside this task, a render failure, or an unexpected
// error is reported with a fixed message, never as a 500 page.
func (s *server) handleTaskPreview(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	data := views.TaskPreviewData{}
	rowID := strings.TrimSpace(r.FormValue("row"))

	rows, err := s.tasks.Rows(ctx, ownerID, task.ID)
	if err != nil {
		s.app.Logger().Error("list task rows", "task", task.ID, "error", err)
		data.Error = "the preview could not be rendered for this row; try again"
		s.renderWithStatus(w, r, http.StatusOK, views.TaskPreviewResult(data))
		return
	}
	available := false
	for _, row := range rows {
		if row.ID == rowID {
			available = true
			break
		}
	}
	if !available {
		data.Error = "that row is not part of this task; choose one of the rows listed above"
		s.renderWithStatus(w, r, http.StatusOK, views.TaskPreviewResult(data))
		return
	}
	preview, err := s.tasks.Preview(ctx, ownerID, task.ID, rowID)
	if err != nil {
		data.Error = s.taskPreviewMessage(err)
		s.renderWithStatus(w, r, http.StatusOK, views.TaskPreviewResult(data))
		return
	}
	data.SourceRow = preview.SourceRow
	data.RecipientPosition = preview.RecipientIndex
	data.OriginalRecipient = preview.OriginalRecipient
	data.ContentHash = preview.ContentHash
	data.Approved = preview.Approved
	data.Recipient = preview.Rendered.Recipient
	data.Subject = preview.Rendered.Subject
	data.Body = preview.Rendered.Body
	for _, value := range preview.Rendered.Values {
		data.Values = append(data.Values, views.PreviewValue{
			Variable:   value.Variable,
			Header:     value.Header,
			Raw:        value.Raw,
			Normalized: value.Normalized,
		})
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskPreviewResult(data))
}

// handleTaskExcluded serves the private exclusion report as a download. A task
// without a report (a staging task) is a plain 404; the bytes never travel
// through a public file URL.
func (s *server) handleTaskExcluded(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if task.Status == intake.TaskStaging {
		http.NotFound(w, r)
		return
	}
	data, filename, err := s.tasks.ExclusionReport(r.Context(), recordFromContext(r).Id, task.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.app.Logger().Error("load exclusion report", "task", task.ID, "error", err)
		http.Error(w, "Unable to load the exclusion report", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(data); err != nil {
		s.app.Logger().Warn("write exclusion report response", "error", err)
	}
}

// handleTaskResultsDownload serves a ready task-results workbook. An artifact
// that is still generating, failed or absent is never presented as complete.
func (s *server) handleTaskResultsDownload(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	data, filename, err := s.exports.DownloadTask(r.Context(), recordFromContext(r).Id, task.ID)
	if err != nil {
		if errors.Is(err, exports.ErrNotReady) {
			http.NotFound(w, r)
			return
		}
		s.app.Logger().Error("download task results", "task", task.ID, "error", err)
		http.Error(w, "Unable to load the results workbook", http.StatusInternalServerError)
		return
	}
	s.writeDownload(w, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", filename, data)
}

// handleTaskResultsGenerate regenerates the results workbook from retained
// records. It never creates or retries an email attempt.
func (s *server) handleTaskResultsGenerate(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	err := s.exports.GenerateTask(r.Context(), recordFromContext(r).Id, task.ID, true)
	switch {
	case err == nil:
		http.Redirect(w, r, taskPath(task)+"#results", http.StatusSeeOther)
	case errors.Is(err, exports.ErrInProgress):
		s.renderTaskMessage(w, r, task, views.TaskSectionResults, "result generation is already in progress; reload this page shortly", http.StatusConflict)
	case errors.Is(err, exports.ErrNotTerminal):
		s.renderTaskMessage(w, r, task, views.TaskSectionResults, err.Error(), http.StatusBadRequest)
	default:
		s.app.Logger().Error("generate task results", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionResults, "the results workbook could not be generated; retry", http.StatusInternalServerError)
	}
}

// handleTaskResultsDelete removes only the generated workbook. Candidate
// eligibility, uploads and attempt history stay intact and regeneration is
// still possible.
func (s *server) handleTaskResultsDelete(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(r.FormValue("confirm")) != "delete" {
		s.renderTaskMessage(w, r, task, views.TaskSectionResults, "confirm that only the generated workbook is removed", http.StatusBadRequest)
		return
	}
	if err := s.exports.DeleteTask(r.Context(), recordFromContext(r).Id, task.ID); err != nil {
		s.app.Logger().Error("delete task results", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionResults, "the generated workbook could not be removed; retry", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#results", http.StatusSeeOther)
}

// loadCampaignTask loads the path's task and confirms it belongs to the
// campaign in the path, so a task id is never usable from another campaign.
func (s *server) loadCampaignTask(w http.ResponseWriter, r *http.Request) (*intake.Task, bool) {
	task, err := s.tasks.LoadTask(r.Context(), recordFromContext(r).Id, r.PathValue("task"))
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return nil, false
	}
	if task.CampaignID != r.PathValue("id") {
		http.NotFound(w, r)
		return nil, false
	}
	return task, true
}

func (s *server) writeDownload(w http.ResponseWriter, contentType, filename string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(data); err != nil {
		s.app.Logger().Warn("write download response", "error", err)
	}
}

// taskLoadFailure answers a failed task load without leaking storage detail:
// an unknown id is a plain 404, anything else is logged and answered
// generically.
// handleTaskClose performs the explicit terminal Close task action. The
// confirmation is required here so a stale or scripted post cannot close work;
// the sending service rechecks ownership, state and the action ID.
func (s *server) handleTaskClose(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid Close task form", http.StatusBadRequest)
		return
	}
	if r.FormValue("confirm_close") != "yes" {
		http.Error(w, "Confirm that closing this task is terminal", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("action_id"))
	if len(id) < 16 || len(id) > 128 {
		http.Error(w, "A valid action ID is required; reload this page", http.StatusBadRequest)
		return
	}
	ownerID := recordFromContext(r).Id
	closeTask := func() error {
		_, err := s.sending.CloseTask(r.Context(), ownerID, task.ID, ownerID, id)
		return err
	}
	var err error
	if s.dispatcher != nil {
		err = s.dispatcher.Synchronize(closeTask)
	} else {
		err = closeTask()
	}
	if err != nil {
		s.app.Logger().Warn("close task rejected", "task", task.ID, "error", err)
		s.renderTaskMessageWith(w, r, task, views.TaskSectionSending, s.sendingRefusalMessage(err), http.StatusBadRequest, nil)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#overview", http.StatusSeeOther)
}

func (s *server) taskLoadFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, intake.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.app.Logger().Error("load task", "error", err)
	http.Error(w, "Unable to load the task", http.StatusInternalServerError)
}

// taskPageData builds the one task workspace view model: identity, the saved
// and current revision, current mailbox approval, validation evidence, fresh
// sending facts, recovery rows, results state and the derived status, blockers
// and controls. Loading it writes nothing and creates no attempt.
func (s *server) taskPageData(r *http.Request, task *intake.Task, section string) (views.TaskPageData, error) {
	ctx := r.Context()
	ownerID := recordFromContext(r).Id
	data := views.TaskPageData{
		CampaignID:      task.CampaignID,
		CampaignName:    task.CampaignName,
		TaskID:          task.ID,
		Section:         section,
		Filename:        task.SourceName,
		Reference:       presentation.ShortRef(task.ID),
		UploadedAt:      presentation.Timestamp(task.UploadedAt),
		StoredStatus:    task.Status,
		Closed:          task.Closed(),
		Closing:         task.Closing(),
		ClosedAt:        presentation.Timestamp(task.ClosedAt),
		ClosedBy:        task.ClosedBy,
		CanClose:        !task.Closed() && !task.Closing(),
		ValidationError: task.ValidationError,
		DefaultSheet:    task.Sheet,
		HeaderRow:       task.HeaderRow,
		Worksheet:       task.Sheet,
		ScheduleAt:      presentation.Timestamp(task.ScheduleAt),
		ScheduleState:   task.ScheduleState,
		SendingType:     "",
		ScheduleMinDate: sending.DisplayDate(time.Now()),
		ResultLabels:    task.ResultLabels,
		ActionID:        actionID(),
	}
	data.SendingType = task.SendingType
	if task.ScheduleState == intake.ScheduleScheduled || task.ScheduleState == intake.ScheduleMissed {
		data.SendingType = views.TaskSendingTypeScheduled
	}
	// This query opens the editor only. Direct/Scheduled always come from storage.
	if r.URL.Query().Get("sending_type") == views.TaskSendingTypeChoose {
		data.SendingType = views.TaskSendingTypeChoose
	}
	if task.ScheduleState == intake.ScheduleScheduled && !task.ScheduleAt.IsZero() {
		local := task.ScheduleAt.In(sending.ScheduleZone)
		data.ScheduleDraftDate = local.Format("2006-01-02")
		data.ScheduleDraftTime = local.Format("15:04")
	}
	if data.Filename == "" {
		data.Filename = "Workbook"
	}
	for _, sheet := range task.Sheets {
		data.Sheets = append(data.Sheets, views.TaskSheetOption{
			Name:     sheet.Name,
			Visible:  sheet.Visible,
			Selected: sheet.Name == task.Sheet,
		})
	}

	loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, task.CampaignID)
	if err != nil {
		return data, err
	}
	data.CampaignName = loaded.Name
	var current *campaign.Configuration
	if loaded.CurrentRevisionID != "" {
		current, err = s.campaigns.LoadConfiguration(ctx, ownerID, loaded.CurrentRevisionID)
		if err != nil {
			return data, err
		}
		data.HasCurrentRevision = true
		data.CurrentRevision = current.Revision
	}
	if task.ConfigurationID != "" {
		saved, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID)
		if err != nil {
			return data, err
		}
		data.HasSavedRevision = true
		data.SavedRevision = saved.Revision
	}
	sender, senderStatus := s.mailboxSummary(ctx, ownerID)
	data.Sender = sender
	data.SenderStatus = senderStatus
	mailboxUsable := senderStatus == string(mailbox.StatusConnected)

	data.Counts = views.TaskCounts{
		Total:            task.Counts.Total,
		Eligible:         task.Counts.Eligible,
		Ineligible:       task.Counts.Ineligible,
		Duplicate:        task.Counts.Duplicate,
		Invalid:          task.Counts.Invalid,
		AlreadySubmitted: task.Counts.AlreadySubmitted,
		AlreadyScheduled: task.Counts.AlreadyScheduled,
		ReviewRequired:   task.Counts.ReviewRequired,
	}
	summary := presentation.Summarize(task.Counts)
	data.Summary = views.TaskSummaryView{
		SourceRows:           summary.SourceRows,
		Total:                summary.Total,
		Eligible:             summary.Eligible,
		ExcludedAtValidation: summary.ExcludedAtValidation,
	}
	for _, category := range summary.Categories {
		data.Categories = append(data.Categories, views.TaskCategory{Label: category.Label, Count: category.Count})
	}

	facts := presentation.Facts{
		Staging:         task.Status == intake.TaskStaging && !task.Closed() && !task.Closing(),
		ValidationError: task.ValidationError,
		Status:          task.Status,
		HasRevision:     data.HasCurrentRevision,
		MailboxUsable:   mailboxUsable,
		ScheduleState:   task.ScheduleState,
		Closed:          data.Closed,
		Closing:         data.Closing,
	}
	facts.Stale = task.Status == intake.TaskReady && (current == nil || task.ContentHash != current.ContentHash)
	data.Stale = facts.Stale

	var progress *sending.Progress
	if task.Status != intake.TaskStaging {
		progress, err = s.sending.Preview(ctx, ownerID, task.ID)
		if err != nil {
			s.app.Logger().Error("load sending progress", "task", task.ID, "error", err)
			data.Error = "Sending and approval metadata is unavailable; retry this page."
		}
	}
	if progress != nil {
		facts.Approved = progress.Approved
		facts.RestoreReview = progress.RestoreReview
		facts.Eligible = progress.Eligible
		facts.Remaining = progress.Remaining
		facts.Submitted = progress.Submitted
		facts.Failed = progress.Failed
		facts.RetryableFailed = progress.RetryableFailed
		facts.Uncertain = progress.Held
		facts.Held = progress.Held
		data.Eligible = progress.Eligible
		data.Remaining = progress.Remaining
		data.Submitted = progress.Submitted
		data.Failed = progress.Failed
		data.RetryableFailed = progress.RetryableFailed
		data.Held = progress.Held
		data.InFlight = progress.InFlight
		data.Reserved = progress.Reserved
		data.Invalid = progress.Invalid
		data.Duplicate = progress.Duplicate
		data.Ineligible = progress.Ineligible
		data.Approved = progress.Approved
		data.RestoreReview = progress.RestoreReview
		data.Hash = progress.ContentHash
		data.LatestTest = progress.LatestSuccessfulTestID
		data.Summary.CurrentEligible = progress.Remaining
		if progress.Reason != "" {
			data.StatusExplanation = progress.Reason
		}
		for _, attempt := range progress.Recent {
			when := attempt.FinishedAt
			if when.IsZero() {
				when = attempt.StartedAt
			}
			data.Recent = append(data.Recent, views.TaskAttemptRow{
				ID:        attempt.ID,
				Kind:      attempt.Kind,
				Recipient: attempt.Recipient,
				State:     attempt.State,
				Reason:    attempt.Reason,
				Time:      presentation.Timestamp(when),
				Test:      attempt.Kind == sending.KindTest,
				HTMLHref:  "/campaigns/" + task.CampaignID + "/attempts/" + attempt.ID + "/email.html",
			})
		}
		if connection, e := s.mailboxes.Current(ctx, ownerID); e == nil {
			settings, e := s.sending.DispatcherState(ctx, connection.ID, ownerID)
			if e != nil {
				return data, e
			}
			data.Rate = settings.Rate
			data.Cooldown = presentation.Timestamp(settings.CooldownUntil)
			data.CooldownReason = settings.Reason
		}
	}

	if artifact, err := s.exports.TaskArtifact(ctx, ownerID, task.ID); err != nil {
		return data, err
	} else if artifact != nil {
		data.ResultsState = artifact.State
		data.ResultsError = artifact.Error
		data.ResultsReady = artifact.Ready
		data.ResultsAsOf = presentation.Timestamp(artifact.AsOf)
		facts.ResultState = artifact.State
		facts.ResultReady = artifact.Ready
	}

	if task.Status != intake.TaskStaging {
		manifest, err := s.tasks.Manifest(ctx, ownerID, task.ID)
		if err != nil {
			return data, err
		}
		for _, row := range manifest.Rows {
			if !row.Excluded() {
				continue
			}
			reasons := make([]string, 0, len(row.Reasons))
			for _, code := range row.Reasons {
				reasons = append(reasons, intake.ReasonText(code))
			}
			data.HeldRows = append(data.HeldRows, views.TaskHeldRow{
				Row:       row.Row,
				Position:  row.Position,
				Recipient: row.Recipient,
				Reason:    strings.Join(reasons, "; "),
			})
		}
		if progress != nil {
			for _, member := range progress.Members {
				row := views.TaskAdmittedRow{
					ID:        member.ID,
					Row:       member.SourceRow,
					Position:  member.RecipientIndex,
					Recipient: member.Recipient,
					State:     member.State,
					Reason:    member.Reason,
				}
				if attempt := member.LatestAttempt; attempt != nil {
					when := attempt.FinishedAt
					if when.IsZero() {
						when = attempt.StartedAt
					}
					row.AttemptID = attempt.ID
					row.AttemptRecipient = attempt.Recipient
					row.AttemptState = attempt.State
					row.AttemptTime = presentation.Timestamp(when)
					row.AttemptReason = attempt.Reason
				}
				data.Rows = append(data.Rows, row)
				if !member.ResolvedAt.IsZero() {
					data.Resolutions = append(data.Resolutions, views.TaskResolutionRow{
						Row:   member.SourceRow,
						Actor: member.ResolutionActor,
						Basis: member.ResolutionBasis,
						Time:  presentation.Timestamp(member.ResolvedAt),
					})
				}
			}
			for _, row := range data.Rows {
				if row.State != intake.RowEligible {
					continue
				}
				data.PreviewRowID = row.ID
				preview, err := s.tasks.Preview(ctx, ownerID, task.ID, row.ID)
				if err != nil {
					data.Preview = views.TaskPreviewData{Error: s.taskPreviewMessage(err), SourceRow: row.Row}
				} else {
					data.Preview = taskPreviewData(preview)
				}
				break
			}
		}
	}

	status := presentation.MainStatus(facts)
	data.StatusLabel = status.Label
	data.StatusClass = statusClass(status)
	if data.StatusExplanation == "" {
		data.StatusExplanation = status.Explanation
	}
	for _, blocker := range presentation.Blockers(facts) {
		data.Blockers = append(data.Blockers, views.TaskBlocker{
			Label:   blocker.Label,
			Remedy:  blocker.Remedy,
			Section: blocker.Section,
			Href:    blockerHref(task, blocker),
		})
	}
	for _, step := range presentation.Lifecycle(facts) {
		data.Lifecycle = append(data.Lifecycle, views.LifecycleStep{Name: step.Name, State: step.State, Complete: step.Complete})
	}
	next := presentation.NextAction(facts)
	data.NextLabel = next.Label
	data.NextHref = nextHref(task, next)
	data.NextLabel = sendingTypeNextLabel(data.NextLabel, task.SendingType)

	if section == "" {
		data.Section = views.TaskSectionOverview
	}
	data.ResultEligible = facts.ResultEligible()
	controls := presentation.AvailableControls(facts)
	data.Controls = views.TaskControls{
		Validate:       controls.Validate,
		Revalidate:     controls.Revalidate,
		Start:          controls.Start,
		Schedule:       controls.Schedule,
		SendNowInstead: controls.SendNowInstead,
		EditSchedule:   controls.EditSchedule,
		ClearSchedule:  controls.ClearSchedule,
		Stop:           controls.Stop,
		Resume:         controls.Resume,
		RetryAll:       controls.RetryAll,
		RetryRow:       controls.RetryRow,
		ResendRow:      controls.ResendRow,
		ResolveRow:     controls.ResolveRow,
		GenerateResult: controls.GenerateResult,
		RetryResult:    controls.RetryResult,
	}
	if data.Error != "" {
		data.StatusLabel = "Status unavailable"
		data.StatusClass = "status-warning"
		data.StatusExplanation = data.Error
		data.Controls = views.TaskControls{}
	}
	return data, nil
}

// taskPreviewData maps one rendered preview to its partial view model.
func taskPreviewData(preview *intake.Preview) views.TaskPreviewData {
	data := views.TaskPreviewData{
		SourceRow:         preview.SourceRow,
		RecipientPosition: preview.RecipientIndex,
		OriginalRecipient: preview.OriginalRecipient,
		ContentHash:       preview.ContentHash,
		Approved:          preview.Approved,
		Recipient:         preview.Rendered.Recipient,
		Subject:           preview.Rendered.Subject,
		Body:              preview.Rendered.Body,
	}
	for _, value := range preview.Rendered.Values {
		data.Values = append(data.Values, views.PreviewValue{
			Variable:   value.Variable,
			Header:     value.Header,
			Raw:        value.Raw,
			Normalized: value.Normalized,
		})
	}
	return data
}

// taskPath is the canonical task page root.
func taskPath(task *intake.Task) string {
	return "/campaigns/" + task.CampaignID + "/tasks/" + task.ID
}

// sendingTypeNextLabel specializes first-start navigation without replacing
// prerequisite, progress, saved-schedule or recovery actions.
func sendingTypeNextLabel(label, sendingType string) string {
	if label == "Choose sending time" && sendingType == intake.TaskSendingTypeDirect {
		return "Send"
	}
	return label
}

// nextHref resolves a next action to a navigable destination. External
// remedies leave the task workspace; internal ones select a task section.
func nextHref(task *intake.Task, next presentation.NextLink) string {
	switch next.Destination {
	case presentation.DestCampaignTemplate:
		return "/campaigns/" + task.CampaignID + "#template"
	case presentation.DestCampaignTasks:
		return "/campaigns/" + task.CampaignID + "#tasks"
	case presentation.DestMailbox:
		return "/account"
	}
	if next.Section != "" {
		return taskPath(task) + "#" + next.Section
	}
	return taskPath(task) + "#" + views.TaskSectionRecipients
}

// blockerHref resolves a blocker remedy to its destination.
func blockerHref(task *intake.Task, blocker presentation.Blocker) string {
	if blocker.Destination == presentation.DestMailbox {
		return "/account"
	}
	if blocker.Section != "" {
		return taskPath(task) + "#" + blocker.Section
	}
	return ""
}

// renderTaskMessage re-renders the task workspace on one section with a fixed
// message inline and an explicit status. The task is reloaded first so a
// failure recorded on it stays visible.
func (s *server) renderTaskMessage(w http.ResponseWriter, r *http.Request, task *intake.Task, section, message string, status int) {
	s.renderTaskMessageWith(w, r, task, section, message, status, nil)
}

// renderTaskMessageWith is renderTaskMessage plus a view-model adjustment, so a
// refused form keeps the operator's submitted non-secret input.
func (s *server) renderTaskMessageWith(w http.ResponseWriter, r *http.Request, task *intake.Task, section, message string, status int, adjust func(*views.TaskPageData)) {
	if fresh, err := s.tasks.LoadTask(r.Context(), recordFromContext(r).Id, task.ID); err == nil {
		task = fresh
	}
	data, err := s.taskPageData(r, task, section)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	data.Section = section
	data.IgnoreHash = true
	data.Error = message
	if adjust != nil {
		adjust(&data)
	}
	if data.ValidationError == message {
		data.ValidationError = ""
	}
	s.renderWithStatus(w, r, status, views.TaskPage(data))
}

// taskUploadData builds the upload page view model with campaign context, the
// current mapped columns and the configured workbook limits.
func (s *server) taskUploadData(ctx context.Context, ownerID, campaignID, campaignName, message string) views.TaskUploadData {
	data := views.TaskUploadData{
		CampaignID:        campaignID,
		CampaignName:      campaignName,
		Error:             message,
		MaxUploadBytes:    s.cfg.Intake.MaxUploadBytes,
		UnzipSizeLimit:    s.cfg.Intake.UnzipSizeLimit,
		UnzipXMLSizeLimit: s.cfg.Intake.UnzipXMLSizeLimit,
	}
	if loaded, err := s.campaigns.LoadCampaign(ctx, ownerID, campaignID); err == nil && loaded.CurrentRevisionID != "" {
		if configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, loaded.CurrentRevisionID); err == nil {
			data.HasRevision = true
			data.Revision = configuration.Revision
			for _, mapping := range configuration.Mappings {
				data.MappedColumns = append(data.MappedColumns, mapping.Header)
			}
		}
	}
	return data
}

// taskPreviewMessage keeps operator-fixable preview failures inline: the
// typed campaign render failures name the missing variable or the subject
// problem with fixed text, and intake rejections contribute their fixed
// messages. Everything unexpected is logged and replaced so no internal
// detail reaches the partial.
func (s *server) taskPreviewMessage(err error) string {
	var missing *campaign.MissingRequiredError
	var control *campaign.SubjectControlError
	if errors.As(err, &missing) || errors.As(err, &control) {
		return err.Error()
	}
	if message := intakeErrorMessage(err); message != "" {
		return message
	}
	s.app.Logger().Error("render task preview", "error", err)
	return "the preview could not be rendered for this row; try again"
}

// intakeErrorMessage maps an intake rejection to its fixed operator-facing
// message: a typed validation failure carries its own message, and the
// documented workbook sentinels contribute their fixed text. The underlying
// error may quote a reader diagnostic or a path, so it is never returned.
func intakeErrorMessage(err error) string {
	if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
		return validation.Message
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return intake.ErrUploadTooLarge.Error()
	}
	for _, sentinel := range []error{
		intake.ErrNoUpload,
		intake.ErrUploadTooLarge,
		intake.ErrExpandedTooLarge,
		intake.ErrWorksheetTooLarge,
		intake.ErrUnsupportedWorkbook,
		intake.ErrCorruptWorkbook,
		intake.ErrTooManyRows,
		intake.ErrUnknownSheet,
		intake.ErrInvalidHeaderRow,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return ""
}
