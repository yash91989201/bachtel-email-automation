package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Dispatcher wakes the application-owned worker; requests never own its lifetime.
type Dispatcher interface {
	Wake()
	Synchronize(func() error) error
}

func (s *server) registerSendingRoutes(mux *http.ServeMux) {
	base := "/campaigns/{id}/tasks/{task}/sending"
	mux.HandleFunc("GET "+base, s.requireOperator(s.handleSending))
	mux.HandleFunc("GET "+base+"/progress", s.requireOperator(s.handleSendingProgress))
	for _, action := range []string{"test", "approve", "start", "resume", "stop", "retry", "retry_all", "resend", "resolve", "rate", "artifacts"} {
		mux.HandleFunc("POST "+base+"/"+action, s.requireOperator(s.handleSendingMutation(action)))
	}
}

func actionID() string {
	var id [24]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id[:])
}

func (s *server) sendingData(r *http.Request) (views.SendingData, error) {
	task, err := s.tasks.LoadTask(r.Context(), r.PathValue("task"))
	if err != nil {
		return views.SendingData{}, err
	}
	progress, err := s.sending.Preview(r.Context(), task.ID)
	if err != nil {
		return views.SendingData{}, err
	}
	data := views.SendingData{ArtifactNeeded: progress.ArtifactNeeded, Base: "/campaigns/" + task.CampaignID + "/tasks/" + task.ID + "/sending", TaskURL: "/campaigns/" + task.CampaignID + "/tasks/" + task.ID, Sender: s.cfg.Microsoft.SenderAddress, Hash: progress.ContentHash, Approved: progress.Approved, Enabled: progress.ProductionEnabled, Status: progress.Status, Reason: progress.Reason, Eligible: progress.Eligible, Remaining: progress.Remaining, Submitted: progress.Submitted, Failed: progress.Failed, RetryableFailed: progress.RetryableFailed, Held: progress.Held, InFlight: progress.InFlight, Reserved: progress.Reserved, Invalid: progress.Invalid, Duplicate: progress.Duplicate, Ineligible: progress.Ineligible, ActionID: actionID()}
	settings, err := s.sending.DispatcherState(r.Context())
	if err != nil {
		return data, err
	}
	data.Rate = settings.Rate
	data.Cooldown = sending.DisplayTime(settings.CooldownUntil)
	for _, row := range progress.Members {
		viewRow := views.SendingRow{ResolutionActor: row.ResolutionActor, ResolutionBasis: row.ResolutionBasis, ResolvedAt: sending.DisplayTime(row.ResolvedAt), SampleEligible: row.SampleEligible, ID: row.ID, Number: row.SourceRow, State: row.State, Reason: row.Reason, ActionID: actionID()}
		if row.LatestAttempt != nil {
			a := row.LatestAttempt
			when := a.FinishedAt
			if when.IsZero() {
				when = a.StartedAt
			}
			viewRow.Attempt = views.SendingOutcome{ID: a.ID, Kind: a.Kind, Recipient: a.Recipient, State: a.State, Reason: a.Reason, Time: sending.DisplayTime(when)}
		}
		data.Rows = append(data.Rows, viewRow)
	}
	if progress.LatestSuccessfulTestID != "" {
		data.LatestTest = progress.LatestSuccessfulTestID
	}
	for _, attempt := range progress.Recent {
		when := attempt.FinishedAt
		if when.IsZero() {
			when = attempt.StartedAt
		}
		data.Recent = append(data.Recent, views.SendingOutcome{ID: attempt.ID, Kind: attempt.Kind, Recipient: attempt.Recipient, State: attempt.State, Reason: attempt.Reason, Time: sending.DisplayTime(when)})
	}
	return data, nil
}

func (s *server) handleSending(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.loadCampaignTask(w, r); !ok {
		return
	}
	data, err := s.sendingData(r)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.SendingPage(data))
}
func (s *server) handleSendingProgress(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.loadCampaignTask(w, r); !ok {
		return
	}
	data, err := s.sendingData(r)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, views.SendingProgress(data))
}

func (s *server) handleSendingMutation(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		task, ok := s.loadCampaignTask(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid sending form", http.StatusBadRequest)
			return
		}
		id := strings.TrimSpace(r.FormValue("action_id"))
		if len(id) < 16 || len(id) > 128 {
			http.Error(w, "A valid action ID is required; reload this page", http.StatusBadRequest)
			return
		}
		actor := recordFromContext(r).Id
		var err error
		switch kind {
		case "test":
			if s.connections == nil {
				err = errors.New("Microsoft connection is unavailable")
				break
			}
			if _, err = s.connections.AccessToken(r.Context()); err != nil {
				break
			}
			_, err = s.sending.QueueTest(r.Context(), sending.TestRequest{ActionID: id, TaskID: task.ID, MemberID: r.FormValue("member_id"), Recipient: r.FormValue("test_address"), Actor: actor})
		case "approve":
			progress, e := s.sending.Preview(r.Context(), task.ID)
			if e != nil {
				err = e
				break
			}
			if r.FormValue("received_checked") != "yes" || r.FormValue("content_hash") != progress.ContentHash {
				http.Error(w, "Confirm received formatting for this exact content hash", http.StatusBadRequest)
				return
			}
			err = s.sending.Approve(r.Context(), task.ID, r.FormValue("attempt_id"), actor, id)
		case "rate":
			rate, e := strconv.Atoi(r.FormValue("rate"))
			if e != nil || rate < 1 || rate > 30 {
				http.Error(w, "Rate must be a whole number from 1 to 30", http.StatusBadRequest)
				return
			}
			setRate := func() error { return s.sending.SetRate(r.Context(), rate) }
			if s.dispatcher != nil {
				err = s.dispatcher.Synchronize(setRate)
			} else {
				err = setRate()
			}
		default:
			count, e := strconv.Atoi(r.FormValue("expected_count"))
			if e != nil || count < 0 {
				http.Error(w, "Reload the fresh count before confirming", http.StatusBadRequest)
				return
			}
			act := func() error {
				_, err = s.sending.Act(r.Context(), sending.ActionRequest{ActionID: id, TaskID: task.ID, Kind: kind, MemberID: r.FormValue("member_id"), Actor: actor, Basis: r.FormValue("basis"), Confirmed: r.FormValue("confirmed") == "yes", DuplicateRisk: r.FormValue("duplicate_risk") == "yes", ExpectedCount: count})
				return err
			}
			if kind == "stop" && s.dispatcher != nil {
				err = s.dispatcher.Synchronize(act)
			} else {
				err = act()
			}
		}
		if err != nil {
			s.app.Logger().Warn("sending action rejected", "kind", kind, "task", task.ID, "error", err)
			data, e := s.sendingData(r)
			if e != nil {
				s.taskLoadFailure(w, r, e)
				return
			}
			if kind == "test" {
				data.Error = "Diagnostic test was not queued. Check the test recipient and sample row, and reconnect Microsoft if the connection needs attention."
			} else {
				data.Error = "Action was not accepted. Refresh the counts and check approval, selected row, confirmations, and release readiness."
			}
			s.renderWithStatus(w, r, http.StatusBadRequest, views.SendingPage(data))
			return
		}
		if s.dispatcher != nil && kind != "artifacts" {
			s.dispatcher.Wake()
		}
		http.Redirect(w, r, "/campaigns/"+task.CampaignID+"/tasks/"+task.ID+"/sending", http.StatusSeeOther)
	}
}
