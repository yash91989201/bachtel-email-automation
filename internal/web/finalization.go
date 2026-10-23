package web

// Final sending snapshot routes. Every handler runs behind requireOperator.
// Dataset approval, final upload and downloads never send email or authorize a
// test; a snapshot is evidence that later actions recheck.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// registerFinalizationRoutes mounts the final-data actions on the task page.
func (s *server) registerFinalizationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/finalization/approve", s.requireOperator(s.handleFinalizationApprove))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/finalization/upload", s.requireOperator(s.handleFinalizationUpload))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/preparation/sending.xlsx", s.requireOperator(s.handlePreparedSheet))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/finalization/sending.xlsx", s.requireOperator(s.handleFinalSheet))
	mux.HandleFunc("GET /campaigns/{id}/tasks/{task}/finalization/exclusions.xlsx", s.requireOperator(s.handleFinalExclusions))
}

// handleFinalizationApprove seals the selected prepared run as the task's final
// sending dataset. It requires explicit confirmation and never calls AI.
func (s *server) handleFinalizationApprove(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.finalization == nil {
		http.Error(w, "Final sending data is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "the finalization form could not be read; reload this page", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("confirm_approve") != "yes" {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "confirm that you reviewed and accept the selected prepared values", http.StatusBadRequest)
		return
	}
	actionID := strings.TrimSpace(r.PostForm.Get("action_id"))
	if len(actionID) < 16 || len(actionID) > 128 {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "a valid action ID is required; reload this page", http.StatusBadRequest)
		return
	}
	ownerID := recordFromContext(r).Id
	if _, err := s.finalization.ApproveAsIs(r.Context(), ownerID, task.ID, actionID, ownerID); err != nil {
		s.app.Logger().Warn("approve prepared data as-is rejected", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, finalizationMessage(err), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#recipients", http.StatusSeeOther)
}

// handleFinalizationUpload accepts an operator-edited final sheet. Its body is
// bounded before any part is read; only deterministic backend validation runs.
func (s *server) handleFinalizationUpload(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.finalization == nil {
		http.Error(w, "Final sending data is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Intake.MaxUploadBytes+(1<<20))
	reject := func(message string) {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, message, http.StatusBadRequest)
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if _, maxErr := errors.AsType[*http.MaxBytesError](err); maxErr {
			reject(intake.ErrUploadTooLarge.Error())
		} else {
			reject("the form could not be read; choose an edited final XLSX workbook and try again")
		}
		return
	}
	actionID := strings.TrimSpace(r.FormValue("action_id"))
	if len(actionID) < 16 || len(actionID) > 128 {
		reject("a valid action ID is required; reload this page")
		return
	}
	file, header, err := r.FormFile("workbook")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			reject("choose an edited final XLSX workbook to upload")
		} else {
			reject("the form could not be read; choose an edited final XLSX workbook and try again")
		}
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			s.app.Logger().Warn("close final workbook", "error", err)
		}
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		reject("the uploaded final workbook could not be read; try again")
		return
	}
	ownerID := recordFromContext(r).Id
	if _, err := s.finalization.AcceptUpload(r.Context(), ownerID, task.ID, actionID, ownerID, header.Filename, data); err != nil {
		s.app.Logger().Warn("final upload rejected", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, finalizationMessage(err), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, taskPath(task)+"#recipients", http.StatusSeeOther)
}

// handlePreparedSheet serves the prepared sending sheet of the selected run.
func (s *server) handlePreparedSheet(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.finalization == nil {
		http.NotFound(w, r)
		return
	}
	data, filename, err := s.finalization.PreparedSheet(r.Context(), recordFromContext(r).Id, task.ID)
	if err != nil {
		s.finalDownloadFailure(w, r, task, err, "prepared sending sheet")
		return
	}
	s.writeDownload(w, xlsxContentType, filename, data)
}

// handleFinalSheet serves one snapshot's accepted-final workbook.
func (s *server) handleFinalSheet(w http.ResponseWriter, r *http.Request) {
	s.serveSnapshotArtifact(w, r, true)
}

// handleFinalExclusions serves one snapshot's exclusion report.
func (s *server) handleFinalExclusions(w http.ResponseWriter, r *http.Request) {
	s.serveSnapshotArtifact(w, r, false)
}

func (s *server) serveSnapshotArtifact(w http.ResponseWriter, r *http.Request, sheet bool) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.finalization == nil {
		http.NotFound(w, r)
		return
	}
	snapshotID := strings.TrimSpace(r.URL.Query().Get("snapshot"))
	if snapshotID == "" {
		snapshotID = task.SelectedFinalSnapshotID
	}
	if snapshotID == "" {
		http.NotFound(w, r)
		return
	}
	ownerID := recordFromContext(r).Id
	var (
		data     []byte
		filename string
		err      error
	)
	if sheet {
		data, filename, err = s.finalization.FinalSheet(r.Context(), ownerID, task.ID, snapshotID)
	} else {
		data, filename, err = s.finalization.ExclusionSheet(r.Context(), ownerID, task.ID, snapshotID)
	}
	if err != nil {
		s.finalDownloadFailure(w, r, task, err, "final workbook")
		return
	}
	s.writeDownload(w, xlsxContentType, filename, data)
}

// xlsxContentType is the private workbook download type.
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (s *server) finalDownloadFailure(w http.ResponseWriter, r *http.Request, task *intake.Task, err error, label string) {
	if errors.Is(err, finalization.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.app.Logger().Error("download "+label, "task", task.ID, "error", err)
	http.Error(w, "Unable to load the "+label, http.StatusInternalServerError)
}

// finalizationData builds the final-dataset state of one task.
func (s *server) finalizationData(ctx context.Context, ownerID string, task *intake.Task, prep views.TaskPreparationView) views.TaskFinalizationView {
	data := views.TaskFinalizationView{Available: s.finalization != nil}
	if s.finalization == nil {
		return data
	}
	if task.ConfigurationID != "" {
		if configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID); err == nil {
			data.Required = configuration.MappingVersion >= campaign.MappingVersionRole
		}
	}
	if data.Required || task.SelectedFinalSnapshotID != "" || prep.EvidenceRunID != "" {
		data.Required = true
	}
	data.CanFinalize = finalizationAvailable(task)
	data.CanUpload = data.CanFinalize
	data.BlockedReason = finalizationBlockedReason(task)
	data.Error = task.FinalizationError
	if prep.EvidenceRunID != "" {
		data.PreparedReady = true
		data.PreparedAccepted = prep.Counts.Accepted
		if prep.EvidenceSelected && prep.Counts.Accepted > 0 {
			data.PreparedHref = "/campaigns/" + task.CampaignID + "/tasks/" + task.ID + "/preparation/sending.xlsx"
		}
	}
	data.CanApprove = data.CanFinalize && prep.EvidenceSelected && prep.Counts.Accepted >= 0 && prep.EvidenceRunID != ""
	snapshots, err := s.finalization.List(ctx, ownerID, task.ID)
	if err != nil {
		s.app.Logger().Error("list final snapshots", "task", task.ID, "error", err)
		data.CanApprove = false
		data.CanUpload = false
		data.BlockedReason = "Final snapshot evidence is unavailable; reload this page."
		return data
	}
	for _, snapshot := range snapshots {
		row := views.TaskFinalSnapshot{
			ID:         snapshot.ID,
			Reference:  presentation.ShortRef(snapshot.ID),
			Kind:       finalizationKindLabel(snapshot.Kind),
			CreatedAt:  presentation.Timestamp(snapshot.CreatedAt),
			Actor:      snapshot.Actor,
			Accepted:   snapshot.Counts.Accepted,
			Excluded:   snapshot.Counts.Excluded,
			Digest:     snapshot.Digest,
			Selected:   snapshot.ID == task.SelectedFinalSnapshotID,
			SheetHref:  "/campaigns/" + task.CampaignID + "/tasks/" + task.ID + "/finalization/sending.xlsx?snapshot=" + snapshot.ID,
			ReportHref: "/campaigns/" + task.CampaignID + "/tasks/" + task.ID + "/finalization/exclusions.xlsx?snapshot=" + snapshot.ID,
		}
		data.History = append(data.History, row)
		if row.Selected {
			data.Selected = row
			data.HasSelected = true
		}
	}
	if data.HasSelected && data.Selected.Accepted == 0 {
		data.CanApprove = false
	}
	return data
}

// finalizationAvailable reports whether final data may change right now.
func finalizationAvailable(task *intake.Task) bool {
	switch {
	case task.Closed() || task.Closing():
		return false
	case task.Status != intake.TaskReady:
		return false
	case task.ScheduleState == intake.ScheduleScheduled, task.ScheduleState == intake.ScheduleFired:
		return false
	default:
		return true
	}
}

// finalizationBlockedReason is the fixed remedy for an unavailable final-data
// change.
func finalizationBlockedReason(task *intake.Task) string {
	switch {
	case task.Closed() || task.Closing():
		return "This task is closed. Final sending data is frozen; upload a new task for different data."
	case task.Status == intake.TaskStaging:
		return "Validate the workbook before finalizing sending data."
	case task.Status != intake.TaskReady:
		return "Final sending data is frozen because candidate sending has started. Retry, resend and recovery use the saved snapshot."
	case task.ScheduleState == intake.ScheduleScheduled:
		return "Clear the scheduled start before changing final sending data. Clearing it never transfers its authorization to new data."
	case task.ScheduleState == intake.ScheduleFired:
		return "Final sending data is frozen because candidate sending has started. Upload a new task for different data."
	default:
		return ""
	}
}

// finalizationKindLabel names one snapshot source.
func finalizationKindLabel(kind string) string {
	switch kind {
	case finalization.KindPrepared:
		return "Prepared data (Approve as-is)"
	case finalization.KindUpload:
		return "Edited final upload"
	default:
		return kind
	}
}

// finalizationMessage maps one refusal to fixed operator-facing text.
func finalizationMessage(err error) string {
	if structural, ok := errors.AsType[*finalization.StructuralError](err); ok {
		return structural.Message
	}
	for _, sentinel := range []error{
		finalization.ErrNotFound,
		finalization.ErrNotReady,
		finalization.ErrStarted,
		finalization.ErrScheduled,
		finalization.ErrClosed,
		finalization.ErrNoPrepared,
		finalization.ErrStale,
		finalization.ErrTooManyEntries,
		finalization.ErrNoSnapshot,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return "the final sending data could not be accepted; reload this page and try again"
}
