package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
	"github.com/yash91989201/bachtel-email-automation/internal/web/views"
)

// Dispatcher wakes the application-owned worker; requests never own its
// lifetime. In-flight counts are per operator so a connection change drains only
// the affected mailbox.
type Dispatcher interface {
	Wake()
	Synchronize(func() error) error
	InFlightFor(ctx context.Context, ownerID string) (int, error)
}

func (s *server) registerSendingRoutes(mux *http.ServeMux) {
	base := "/campaigns/{id}/tasks/{task}/sending"
	mux.HandleFunc("GET "+base, s.requireOperator(s.handleSending))
	mux.HandleFunc("GET "+base+"/progress", s.requireOperator(s.handleSendingProgress))
	for _, action := range []string{"test", "approve", "start", "resume", "stop", "retry", "retry_all", "resend", "resolve", "rate", "schedule", "artifacts"} {
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

// mailboxSummary renders the owning operator's sending mailbox and its state.
// A workspace without a usable connection can review history but cannot send.
func (s *server) mailboxSummary(ctx context.Context, ownerID string) (sender, status string) {
	connection, err := s.mailboxes.Current(ctx, ownerID)
	if err != nil {
		return "No mailbox connected", string(mailbox.StatusDisconnected)
	}
	return connection.Sender, string(connection.Status)
}

// handleSending opens the one task workspace on its Sending section. The old
// sending route stays as a bookmark-compatible entry point.
func (s *server) handleSending(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	data, err := s.taskPageData(r, task, views.TaskSectionSending)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.renderWithStatus(w, r, http.StatusOK, views.TaskPage(data))
}

// handleSendingProgress returns only the Sending section's live evidence. The
// shell, identity, lifecycle and navigation stay outside this swap.
func (s *server) handleSendingProgress(w http.ResponseWriter, r *http.Request) {
	task, ok := s.loadCampaignTask(w, r)
	if !ok {
		return
	}
	data, err := s.taskPageData(r, task, views.TaskSectionSending)
	if err != nil {
		s.taskLoadFailure(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.renderWithStatus(w, r, http.StatusOK, views.TaskSendingLive(data))
}

// sendingSection names the section a mutation belongs to, so a refused action
// re-renders the workspace where its controls are.
func sendingSection(kind string) string {
	switch kind {
	case "test", "approve":
		return views.TaskSectionTest
	case "artifacts":
		return views.TaskSectionResults
	default:
		return views.TaskSectionSending
	}
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
			connection, connectionErr := s.mailboxes.Current(r.Context(), actor)
			if connectionErr != nil || connection.Status != mailbox.StatusConnected {
				err = errors.New("reconnect the mailbox before sending a test")
				break
			}
			_, err = s.sending.QueueTest(r.Context(), actor, sending.TestRequest{ActionID: id, TaskID: task.ID, MemberID: r.FormValue("member_id"), Recipient: r.FormValue("test_address"), Actor: actor})
		case "approve":
			progress, e := s.sending.Preview(r.Context(), actor, task.ID)
			if e != nil {
				err = e
				break
			}
			if r.FormValue("received_checked") != "yes" || r.FormValue("content_hash") != progress.ContentHash {
				http.Error(w, "Confirm received formatting for this exact content hash", http.StatusBadRequest)
				return
			}
			err = s.sending.Approve(r.Context(), actor, task.ID, r.FormValue("attempt_id"), actor, id)
		case "rate":
			rate, e := strconv.Atoi(r.FormValue("rate"))
			if e != nil || rate < 1 || rate > 30 {
				http.Error(w, "Rate must be a whole number from 1 to 30", http.StatusBadRequest)
				return
			}
			setRate := func() error { return s.sending.SetRate(r.Context(), actor, rate) }
			if s.dispatcher != nil {
				err = s.dispatcher.Synchronize(setRate)
			} else {
				err = setRate()
			}
		case "schedule":
			var request sending.ScheduleRequest
			switch action := r.FormValue("schedule_action"); action {
			case "clear":
				request = sending.ScheduleRequest{ActionID: id, TaskID: task.ID, Actor: actor}
			case "enable":
				at, e := sending.ParseSchedule(r.FormValue("schedule_date"), r.FormValue("schedule_time"))
				if e != nil {
					err = e
					break
				}
				count, e := strconv.Atoi(r.FormValue("expected_count"))
				if e != nil || count < 0 {
					http.Error(w, "Reload the fresh count before confirming", http.StatusBadRequest)
					return
				}
				request = sending.ScheduleRequest{ActionID: id, TaskID: task.ID, Actor: actor, At: at, Confirmed: r.FormValue("confirmed") == "yes", ExpectedCount: count}
			default:
				http.Error(w, "Choose whether to schedule a start or clear the current one", http.StatusBadRequest)
				return
			}
			if err != nil {
				break
			}
			schedule := func() error {
				_, err = s.sending.Schedule(r.Context(), actor, request)
				return err
			}
			if s.dispatcher != nil {
				err = s.dispatcher.Synchronize(schedule)
			} else {
				err = schedule()
			}
		default:
			count, e := strconv.Atoi(r.FormValue("expected_count"))
			if e != nil || count < 0 {
				http.Error(w, "Reload the fresh count before confirming", http.StatusBadRequest)
				return
			}
			act := func() error {
				_, err = s.sending.Act(r.Context(), actor, sending.ActionRequest{ActionID: id, TaskID: task.ID, Kind: kind, MemberID: r.FormValue("member_id"), Actor: actor, Basis: r.FormValue("basis"), Confirmed: r.FormValue("confirmed") == "yes", DuplicateRisk: r.FormValue("duplicate_risk") == "yes", ExpectedCount: count})
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
			message := s.sendingRefusalMessage(err)
			adjust := func(data *views.TaskPageData) {
				if kind != "schedule" {
					return
				}
				data.ScheduleDraftDate = r.FormValue("schedule_date")
				data.ScheduleDraftTime = r.FormValue("schedule_time")
			}
			s.renderTaskMessageWith(w, r, task, sendingSection(kind), message, http.StatusBadRequest, adjust)
			return
		}
		if kind == "artifacts" {
			// Generation is a file export, never a send: a failure leaves the
			// durable marker set for the background worker or a retry.
			if err = s.exports.GenerateTask(r.Context(), actor, task.ID, false); err != nil && !errors.Is(err, exports.ErrInProgress) {
				s.app.Logger().Error("generate task results after request", "task", task.ID, "error", err)
			}
			s.exports.Trigger()
			http.Redirect(w, r, taskPath(task)+"#results", http.StatusSeeOther)
			return
		}
		if s.dispatcher != nil {
			s.dispatcher.Wake()
		}
		http.Redirect(w, r, taskPath(task)+"#"+sendingSection(kind), http.StatusSeeOther)
	}
}

// knownSendingMessages are the sending service's fixed, operator-facing
// refusals. They name the actual prerequisite; everything else stays generic so
// a storage or provider diagnostic never reaches the page.
var knownSendingMessages = map[string]struct{}{
	"a mailbox replacement is in progress; finish it before changing sending work":        {},
	"explicit confirmation is required":                                                   {},
	"validate the task before sending":                                                    {},
	"connect a usable sending mailbox before starting, retrying or resending":             {},
	"approve a successfully submitted formatting test first":                              {},
	"the configuration changed; revalidate this task":                                     {},
	"use resume or retry for an existing task":                                            {},
	"resume requires a previously started task":                                           {},
	"resume requires a paused, interrupted, stopped or previously started completed task": {},
	"stop or pause the task before changing its work":                                     {},
	"select one candidate":                                                                {},
	"confirm the possible duplicate-send risk":                                            {},
	"record the actor and external verification basis":                                    {},
	"eligible count changed; refresh and confirm the current count":                       {},
	"no eligible candidates remain; refresh the counts":                                   {},
	"choose a valid sample in this task":                                                  {},
	"choose a submitted test for the current mailbox connection":                          {},
	"choose a submitted test for this exact configuration":                                {},
	"approval requires an actor and action ID":                                            {},
	"result generation requires a terminal or recovered task":                             {},
	"reconnect the mailbox before sending a test":                                         {},
	"an action ID is required":                                                            {},
	sending.ClosedTaskMessage:                                                             {},
}

// sendingRefusalMessage maps a refused action to a safe, specific reason and
// remedy. A schedule refusal carries its own fixed message; a known sending
// prerequisite is shown as-is; anything unexpected is logged and replaced.
func (s *server) sendingRefusalMessage(err error) string {
	if refusal, ok := errors.AsType[*sending.ScheduleRefusal](err); ok {
		return refusal.Reason
	}
	if _, ok := knownSendingMessages[err.Error()]; ok {
		return err.Error()
	}
	s.app.Logger().Error("unexpected sending action failure", "error", err)
	return "The action was not accepted. Reload this page, check the saved revision, approval, mailbox and fresh counts, and confirm again. No duplicate email was sent by this refused request."
}
