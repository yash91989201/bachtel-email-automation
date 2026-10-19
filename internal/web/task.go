package web

// Task intake routes. Every handler runs behind requireOperator, every
// operator-fixable failure re-renders the page inline with status 400, and no
// handler here sends email: a validated task is ready, and starting belongs to
// a later change.

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
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
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/select", s.requireOperator(s.handleTaskSelect))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/revalidate", s.requireOperator(s.handleTaskRevalidate))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preview", s.requireOperator(s.handleTaskPreview))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/excluded.xlsx", s.requireOperator(s.handleTaskExcluded))
}

// handleTaskList lists a campaign's tasks with their status and counts.
func (s *server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	loaded, err := s.campaigns.LoadCampaign(ctx, id)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	tasks, err := s.tasks.ListTasks(ctx, id)
	if err != nil {
		s.app.Logger().Error("list tasks", "campaign", id, "error", err)
		http.Error(w, "Unable to load tasks", http.StatusInternalServerError)
		return
	}
	data := views.TaskListData{CampaignID: loaded.ID, CampaignName: loaded.Name}
	for _, task := range tasks {
		data.Tasks = append(data.Tasks, views.TaskListRow{
			ID:        task.ID,
			Status:    task.Status,
			Sheet:     task.Sheet,
			HeaderRow: task.HeaderRow,
			Total:     task.Counts.Total,
			Eligible:  task.Counts.Eligible,
		})
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskList(data))
}

// handleTaskNew renders the upload form with the configured workbook limits.
func (s *server) handleTaskNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loaded, err := s.campaigns.LoadCampaign(ctx, r.PathValue("id"))
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskUpload(s.taskUploadData(loaded.ID, loaded.Name, "")))
}

// handleTaskCreate stores one uploaded workbook as a staging task. The body is
// bounded before any part is read, and only fixed intake messages ever reach
// the form again.
func (s *server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	loaded, err := s.campaigns.LoadCampaign(ctx, id)
	if err != nil {
		s.campaignLoadFailure(w, r, err)
		return
	}
	// One workbook plus about 1 MiB of multipart framing: the bound applies
	// before any part is read into memory.
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Intake.MaxUploadBytes+(1<<20))

	uploadError := func(message string) {
		s.renderWithStatus(w, r, http.StatusBadRequest, views.TaskUpload(s.taskUploadData(loaded.ID, loaded.Name, message)))
	}

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			uploadError(intake.ErrUploadTooLarge.Error())
		} else {
			uploadError("the form could not be read; choose an XLSX workbook and try again")
		}
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
	task, err := s.tasks.Stage(ctx, id, header.Filename, data)
	if err != nil {
		if message := intakeErrorMessage(err); message != "" {
			uploadError(message)
			return
		}
		s.app.Logger().Error("stage task", "campaign", id, "error", err)
		uploadError("the workbook could not be uploaded; try again")
		return
	}
	http.Redirect(w, r, "/campaigns/"+id+"/tasks/"+task.ID, http.StatusSeeOther)
}

// handleTaskDetail shows the selection form for a staging task and the review
// page for a ready one.
func (s *server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	data, err := s.taskPageData(r, task)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	s.renderWithStatus(w, r, http.StatusOK, views.TaskPage(data))
}

// handleTaskSelect confirms the worksheet and header row of a staging task.
// Success redirects to the task page; a validation failure re-renders it with
// the fixed message inline and status 400.
func (s *server) handleTaskSelect(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	headerRow, err := strconv.Atoi(strings.TrimSpace(r.FormValue("header_row")))
	if err != nil {
		s.renderTaskMessage(w, r, task, intake.ErrInvalidHeaderRow.Error(), http.StatusBadRequest)
		return
	}
	if err := s.tasks.Validate(ctx, task.ID, r.FormValue("sheet"), headerRow); err != nil {
		if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
			s.renderTaskMessage(w, r, task, validation.Message, http.StatusBadRequest)
			return
		}
		s.taskLoadFailure(w, r, err)
		return
	}
	http.Redirect(w, r, "/campaigns/"+task.CampaignID+"/tasks/"+task.ID, http.StatusSeeOther)
}

// handleTaskRevalidate rebuilds the snapshot against the campaign's current
// revision. Success redirects; a validation failure stays inline with 400.
func (s *server) handleTaskRevalidate(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if err := s.tasks.Revalidate(r.Context(), task.ID); err != nil {
		if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
			s.renderTaskMessage(w, r, task, validation.Message, http.StatusBadRequest)
			return
		}
		s.taskLoadFailure(w, r, err)
		return
	}
	http.Redirect(w, r, "/campaigns/"+task.CampaignID+"/tasks/"+task.ID, http.StatusSeeOther)
}

// handleTaskPreview renders one admitted row through the pinned revision as an
// htmx partial. A row outside this task, a render failure, or any unexpected
// error is reported with a fixed message at status 200, never as a 500 page.
func (s *server) handleTaskPreview(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	data := views.TaskPreviewData{}
	rowID := strings.TrimSpace(r.FormValue("row"))

	rows, err := s.tasks.Rows(ctx, task.ID)
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
	preview, err := s.tasks.Preview(ctx, task.ID, rowID)
	if err != nil {
		data.Error = s.taskPreviewMessage(err)
		s.renderWithStatus(w, r, http.StatusOK, views.TaskPreviewResult(data))
		return
	}
	data.SourceRow = preview.SourceRow
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
	data, filename, err := s.tasks.ExclusionReport(r.Context(), task.ID)
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

// loadCampaignTask loads the path's task and confirms it belongs to the
// campaign in the path, so a task id is never usable from another campaign.
func (s *server) loadCampaignTask(w http.ResponseWriter, r *http.Request) (*intake.Task, bool) {
	task, err := s.tasks.LoadTask(r.Context(), r.PathValue("task"))
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

// taskLoadFailure answers a failed task load without leaking storage detail:
// an unknown id is a plain 404, anything else is logged and answered
// generically.
func (s *server) taskLoadFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	s.app.Logger().Error("load task", "error", err)
	http.Error(w, "Unable to load the task", http.StatusInternalServerError)
}

// taskPageData builds the safe view model for one task page: the recorded
// selection state for a staging task, and the pinned revision, counts, held
// rows and admitted rows for a ready one.
func (s *server) taskPageData(r *http.Request, task *intake.Task) (views.TaskPageData, error) {
	data := views.TaskPageData{
		CampaignID:      task.CampaignID,
		CampaignName:    task.CampaignName,
		TaskID:          task.ID,
		Status:          task.Status,
		ValidationError: task.ValidationError,
		DefaultSheet:    task.Sheet,
		HeaderRow:       task.HeaderRow,
		ScheduleAt:      sending.DisplayTime(task.ScheduleAt),
		ScheduleState:   task.ScheduleState,
	}
	for _, sheet := range task.Sheets {
		data.Sheets = append(data.Sheets, views.TaskSheetOption{
			Name:     sheet.Name,
			Visible:  sheet.Visible,
			Selected: sheet.Name == task.Sheet,
		})
	}
	if task.Status == intake.TaskStaging {
		return data, nil
	}

	ctx := r.Context()
	configuration, err := s.campaigns.LoadConfiguration(ctx, task.ConfigurationID)
	if err != nil {
		return data, err
	}
	preflight, err := s.tasks.Preflight(ctx, task.ID)
	if err != nil {
		return data, err
	}
	manifest, err := s.tasks.Manifest(ctx, task.ID)
	if err != nil {
		return data, err
	}
	rows, err := s.tasks.Rows(ctx, task.ID)
	if err != nil {
		return data, err
	}

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
	data.ResultLabels = task.ResultLabels
	data.Revision = configuration.Revision
	data.ContentHash = task.ContentHash
	data.Approved = configuration.ApprovalCurrent()
	data.Stale = preflight.Stale
	for _, row := range manifest.Rows {
		if !row.Excluded() {
			continue
		}
		reasons := make([]string, 0, len(row.Reasons))
		for _, code := range row.Reasons {
			reasons = append(reasons, intake.ReasonText(code))
		}
		data.HeldRows = append(data.HeldRows, views.TaskHeldRow{
			Row:    row.Row,
			Reason: strings.Join(reasons, "; "),
		})
	}
	for _, row := range rows {
		data.Rows = append(data.Rows, views.TaskAdmittedRow{
			ID:     row.ID,
			Row:    row.SourceRow,
			State:  row.State,
			Reason: row.Reason,
		})
	}
	return data, nil
}

// renderTaskMessage re-renders one task page with a fixed message inline and
// an explicit status. The task is reloaded first so a failure recorded on it
// stays visible; an identical recorded message is dropped to avoid showing the
// same text twice.
func (s *server) renderTaskMessage(w http.ResponseWriter, r *http.Request, task *intake.Task, message string, status int) {
	if fresh, err := s.tasks.LoadTask(r.Context(), task.ID); err == nil {
		task = fresh
	}
	data, err := s.taskPageData(r, task)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	data.Error = message
	if data.ValidationError == message {
		data.ValidationError = ""
	}
	s.renderWithStatus(w, r, status, views.TaskPage(data))
}

// taskUploadData builds the upload page view model with the configured
// workbook limits, rendered as literal byte counts.
func (s *server) taskUploadData(campaignID, campaignName, message string) views.TaskUploadData {
	return views.TaskUploadData{
		CampaignID:        campaignID,
		CampaignName:      campaignName,
		Error:             message,
		MaxUploadBytes:    s.cfg.Intake.MaxUploadBytes,
		UnzipSizeLimit:    s.cfg.Intake.UnzipSizeLimit,
		UnzipXMLSizeLimit: s.cfg.Intake.UnzipXMLSizeLimit,
	}
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
