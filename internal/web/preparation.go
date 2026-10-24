package web

// AI data-preparation routes. Every handler runs behind requireOperator and
// every operator-fixable refusal re-renders the task page inline. Nothing here
// sends email, publishes candidates or authorizes a start: a preparation run is
// evidence for later review.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// registerPreparationRoutes mounts the preparation actions on the task page.
func (s *server) registerPreparationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preparation/prepare", s.requireOperator(s.handlePreparationStart))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preparation/retry", s.requireOperator(s.handlePreparationStart))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preparation/rerun", s.requireOperator(s.handlePreparationStart))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preparation/select", s.requireOperator(s.handlePreparationSelect))
	mux.HandleFunc("POST /campaigns/{id}/tasks/{task}/preparation/instructions", s.requireOperator(s.handlePreparationInstructions))
}

// handlePreparationStart starts an initial run, an explicit retry or an
// explicit rerun. The run is durable before the redirect: the worker owns it,
// not the browser request.
func (s *server) handlePreparationStart(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.preparation == nil {
		http.Error(w, "AI data preparation is unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerID := recordFromContext(r).Id
	var err error
	switch {
	case strings.HasSuffix(r.URL.Path, "/retry"):
		_, err = s.preparation.Retry(r.Context(), ownerID, task.ID)
	case strings.HasSuffix(r.URL.Path, "/rerun"):
		_, err = s.preparation.Rerun(r.Context(), ownerID, task.ID)
	default:
		_, err = s.preparation.Prepare(r.Context(), ownerID, task.ID)
	}
	if err != nil {
		s.app.Logger().Warn("preparation start rejected", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, preparationMessage(err), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, views.TaskSectionPath(task.CampaignID, task.ID, views.TaskSectionRecipients), http.StatusSeeOther)
}

// handlePreparationSelect records the completed run selected for review. It is
// preparation-only: no candidate, membership, schedule or approval record
// changes.
func (s *server) handlePreparationSelect(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.preparation == nil {
		http.Error(w, "AI data preparation is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "invalid preparation selection", http.StatusBadRequest)
		return
	}
	runID := strings.TrimSpace(r.FormValue("run"))
	if runID == "" {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "choose a completed preparation run", http.StatusBadRequest)
		return
	}
	if _, err := s.preparation.SelectRun(r.Context(), recordFromContext(r).Id, task.ID, runID); err != nil {
		s.app.Logger().Warn("preparation selection rejected", "task", task.ID, "error", err)
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, preparationMessage(err), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, views.TaskSectionPath(task.CampaignID, task.ID, views.TaskSectionRecipients), http.StatusSeeOther)
}

// handlePreparationInstructions saves the editable task-instruction draft.
func (s *server) handlePreparationInstructions(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	if s.preparation == nil {
		http.Error(w, "AI data preparation is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.renderTaskMessage(w, r, task, views.TaskSectionRecipients, "invalid instructions form", http.StatusBadRequest)
		return
	}
	text := r.FormValue("instructions")
	if err := s.preparation.SetInstructions(r.Context(), recordFromContext(r).Id, task.ID, text); err != nil {
		s.renderTaskMessageWith(w, r, task, views.TaskSectionRecipients, preparationMessage(err), http.StatusBadRequest, func(data *views.TaskPageData) {
			data.Preparation.Instructions = text
		})
		return
	}
	http.Redirect(w, r, views.TaskSectionPath(task.CampaignID, task.ID, views.TaskSectionRecipients), http.StatusSeeOther)
}

// preparationMessage maps one preparation refusal to fixed operator-facing
// text. Unexpected errors are logged and replaced.
func preparationMessage(err error) string {
	if validation, ok := errors.AsType[*intake.ValidationError](err); ok {
		return validation.Message
	}
	for _, sentinel := range []error{
		preparation.ErrNotFound,
		preparation.ErrNotReady,
		preparation.ErrStarted,
		preparation.ErrScheduled,
		preparation.ErrClosed,
		preparation.ErrInProgress,
		preparation.ErrNotFailed,
		preparation.ErrNotCompleted,
		preparation.ErrSetup,
		preparation.ErrStale,
		preparation.ErrEmpty,
	} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return "the preparation action could not be completed; reload this page and try again"
}
