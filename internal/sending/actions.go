package sending

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"strconv"
	"strings"
)

// restoreBlocked reports the actions a restored backup must not accept until
// the operator reconciles the gap between the backup and the interruption.
func restoreBlocked(kind string) bool {
	switch kind {
	case "start", "resume", "retry", "retry_all", "resend":
		return true
	default:
		return false
	}
}

// ErrNotFound is returned for a task that is absent or owned by another
// operator. Both cases produce the same refusal.
var ErrNotFound = errors.New("task not found")

// approved reports whether this exact content was approved through the given
// mailbox connection generation. A replacement bumps the generation, so every
// earlier approval stops authorizing candidate sending.
func approved(app core.App, t *core.Record, connectionID string, generation int) (bool, error) {
	r, e := app.FindFirstRecordByFilter(
		ApprovalsCollection,
		"configuration={:c} && content_hash={:h} && connection={:conn} && connection_generation={:g}",
		dbx.Params{
			"c":    t.GetString("configuration"),
			"h":    t.GetString("content_hash"),
			"conn": connectionID,
			"g":    generation,
		},
	)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return r != nil && e == nil, e
}

// authorizeTask loads a task only when it belongs to the trusted operator.
func (s *Store) authorizeTask(app core.App, ownerID, taskID string) (*core.Record, error) {
	t, err := app.FindRecordById(intake.TasksCollection, taskID)
	if err != nil {
		return nil, ErrNotFound
	}
	owner, err := ownerOfTask(app, t)
	if err != nil {
		return nil, err
	}
	if owner != ownerID {
		return nil, ErrNotFound
	}
	blocked, err := activeReset(app, ownerID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, errors.New("a mailbox replacement is in progress; finish it before changing sending work")
	}
	return t, nil
}

// approvalFor answers the approval question for the operator's current
// connection generation.
func (s *Store) approvalFor(app core.App, t *core.Record, ownerID string) (bool, error) {
	connection, err := s.currentConnection(ownerID)
	if errors.Is(err, mailbox.ErrNotConnected) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return approved(app, t, connection.Id, connection.GetInt("generation"))
}
func (s *Store) Preview(ctx context.Context, ownerID, id string) (*Progress, error) {
	t, e := s.authorizeTask(s.app, ownerID, id)
	if e != nil {
		return nil, e
	}
	isApproved, e := s.approvalFor(s.app, t, ownerID)
	if e != nil {
		return nil, e
	}
	p := &Progress{TaskID: id, Status: t.GetString("status"), Reason: t.GetString("pause_reason"), ContentHash: t.GetString("content_hash"), Approved: isApproved, ArtifactNeeded: t.GetBool("result_generation_needed"), RestoreReview: s.options.RestoreReview}
	var counts intake.Counts
	if e = t.UnmarshalJSONField("counts", &counts); e != nil {
		return nil, fmt.Errorf("decode task counts: %w", e)
	}
	p.Invalid = counts.Invalid
	p.Duplicate = counts.Duplicate
	p.Ineligible = counts.Ineligible
	rows, e := records(s.app, intake.TaskCandidatesCollection, "task={:t}", "source_row", dbx.Params{"t": id})
	if e != nil {
		return nil, e
	}
	attempts, e := records(s.app, AttemptsCollection, "task={:t} || candidate.campaign={:c}", "-created,-id", dbx.Params{"t": id, "c": t.GetString("campaign")})
	if e != nil {
		return nil, e
	}
	candidates, e := records(s.app, intake.CandidatesCollection, "campaign={:c}", "", dbx.Params{"c": t.GetString("campaign")})
	if e != nil {
		return nil, e
	}
	candidateByID := make(map[string]*core.Record, len(candidates))
	for _, c := range candidates {
		candidateByID[c.Id] = c
	}
	latest := map[string]*core.Record{}
	latestCandidate := map[string]*core.Record{}
	for _, a := range attempts {
		if a.GetString("candidate") != "" && latestCandidate[a.GetString("candidate")] == nil {
			latestCandidate[a.GetString("candidate")] = a
		}
		if a.GetString("member") != "" && latest[a.GetString("member")] == nil {
			latest[a.GetString("member")] = a
		}
	}
	resolutions, e := records(s.app, ResolutionsCollection, "member.task={:t}", "-resolved_at", dbx.Params{"t": id})
	if e != nil {
		return nil, e
	}
	resolved := map[string]*core.Record{}
	for _, r := range resolutions {
		if resolved[r.GetString("member")] == nil {
			resolved[r.GetString("member")] = r
		}
	}
	for _, m := range rows {
		if latest[m.Id] == nil {
			latest[m.Id] = latestCandidate[m.GetString("candidate")]
		}
		memberState := m.GetString("dispatch_state")
		if memberState == "" {
			memberState = m.GetString("state")
		}
		reason := m.GetString("reason")
		if a := latest[m.Id]; a != nil && a.GetString("reason") != "" {
			reason = a.GetString("reason")
		}
		var latestAttempt *Attempt
		if a := latest[m.Id]; a != nil {
			latestAttempt = attemptFrom(a)
		}
		member := MemberProgress{SampleEligible: m.GetString("state") == intake.RowEligible, LatestAttempt: latestAttempt, ID: m.Id, State: memberState, Reason: reason, SourceRow: m.GetInt("source_row")}
		if r := resolved[m.Id]; r != nil {
			member.ResolutionActor = r.GetString("actor")
			member.ResolutionBasis = r.GetString("basis")
			member.ResolvedAt = stamp(r, "resolved_at")
		}
		p.Members = append(p.Members, member)
		if m.GetString("state") != intake.RowEligible {
			if c := candidateByID[m.GetString("candidate")]; c != nil {
				switch c.GetString("submission_state") {
				case intake.SubmissionSubmitted:
					p.Submitted++
					p.Members[len(p.Members)-1].State = AttemptSubmitted
				case intake.SubmissionUncertain:
					p.Held++
					p.Members[len(p.Members)-1].State = AttemptReview
				default:
					if c.GetString("claim_task") != "" {
						p.Reserved++
						p.Members[len(p.Members)-1].State = intake.RowAlreadyScheduled
					}
				}
			}
			continue
		}
		c := candidateByID[m.GetString("candidate")]
		if c == nil {
			return nil, errors.New("task candidate is missing")
		}
		switch {
		case c.GetString("submission_state") == intake.SubmissionSubmitted:
			p.Submitted++
			if memberState != "manually_submitted" {
				p.Members[len(p.Members)-1].State = AttemptSubmitted
			}
		case c.GetString("submission_state") == intake.SubmissionUncertain:
			p.Held++
			p.Members[len(p.Members)-1].State = AttemptReview
		case m.GetString("dispatch_state") == AttemptInFlight:
			p.InFlight++
		case m.GetString("dispatch_state") == "reserved":
			p.Reserved++
			p.Remaining++
		case m.GetString("dispatch_state") == AttemptFailed || m.GetString("dispatch_state") == AttemptAuth || m.GetString("dispatch_state") == "correction_required":
			p.Failed++
			if m.GetString("dispatch_state") != "correction_required" {
				p.RetryableFailed++
			}
		case c.GetString("claim_task") == "" || c.GetString("claim_task") == id:
			p.Eligible++
			p.Remaining++
		default:
			p.Reserved++
			p.Members[len(p.Members)-1].State = intake.RowAlreadyScheduled
		}
	}
	for _, a := range attempts {
		if a.GetString("task") != id {
			continue
		}
		if p.LatestSuccessfulTestID == "" && a.GetString("kind") == KindTest && a.GetString("state") == AttemptSubmitted && a.GetString("content_hash") == p.ContentHash {
			p.LatestSuccessfulTestID = a.Id
		}
		if len(p.Recent) < 20 {
			p.Recent = append(p.Recent, *attemptFrom(a))
		}
	}
	return p, nil
}
func existingAction(app core.App, id, task, kind string) (*ActionResult, error) {
	r, e := app.FindFirstRecordByFilter(ActionsCollection, "action_id={:id}", dbx.Params{"id": id})
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if r.GetString("task") != task || r.GetString("kind") != kind {
		return nil, errors.New("action ID was already used for a different action")
	}
	v := &ActionResult{}
	if e = r.UnmarshalJSONField("result", v); e != nil {
		return nil, e
	}
	v.Repeated = true
	return v, nil
}
func saveAction(app core.App, ownerID string, in ActionRequest, out *ActionResult) error {
	c, e := app.FindCollectionByNameOrId(ActionsCollection)
	if e != nil {
		return e
	}
	r := core.NewRecord(c)
	r.Set("action_id", in.ActionID)
	r.Set("owner", ownerID)
	r.Set("task", in.TaskID)
	r.Set("kind", in.Kind)
	r.Set("actor", in.Actor)
	r.Set("result", out)
	return app.Save(r)
}

// startable reports whether one membership row can be reserved by a start-like
// action right now: an eligible snapshot row, no submission history or
// unresolved outcome, and no reservation held by another task or by an active
// attempt. Retries and uncertain resends have their own narrower rules.
func startable(taskID string, m, c *core.Record) bool {
	if m.GetString("state") != intake.RowEligible {
		return false
	}
	if c.GetString("submission_state") != intake.SubmissionNone {
		return false
	}
	if claim := c.GetString("claim_task"); claim != "" && claim != taskID {
		return false
	}
	switch m.GetString("dispatch_state") {
	case AttemptInFlight, "reserved", AttemptFailed, AttemptAuth, AttemptSubmitted, AttemptReview, "correction_required":
		return false
	}
	return true
}

// reserve records the reservation of one candidate for one task.
func (s *Store) reserve(app core.App, t, m, c *core.Record) error {
	c.Set("submission_state", intake.SubmissionNone)
	c.Set("claim_task", t.Id)
	nowField(c, "claimed_at", s.options.Now())
	m.Set("dispatch_state", "reserved")
	m.Set("retry_count", 0)
	m.Set("retry_at", "")
	if e := app.Save(c); e != nil {
		return e
	}
	return app.Save(m)
}

func (s *Store) Act(ctx context.Context, ownerID string, in ActionRequest) (*ActionResult, error) {
	var out *ActionResult
	if strings.TrimSpace(in.ActionID) == "" || len(in.ActionID) > 128 {
		return nil, errors.New("an action ID is required")
	}
	err := s.app.RunInTransaction(func(app core.App) error {
		old, e := existingAction(app, in.ActionID, in.TaskID, in.Kind)
		if e != nil {
			return e
		}
		if old != nil {
			out = old
			return nil
		}
		t, e := s.authorizeTask(app, ownerID, in.TaskID)
		if e != nil {
			return e
		}
		out = &ActionResult{ActionID: in.ActionID, TaskID: t.Id}
		if in.Kind == "artifacts" {
			switch t.GetString("status") {
			case "completed", "stopped", "interrupted":
			default:
				return errors.New("result generation requires a terminal or recovered task")
			}
			t.Set("result_generation_needed", true)
			if e = app.Save(t); e != nil {
				return e
			}
			out.Status = t.GetString("status")
			return saveAction(app, ownerID, in, out)
		}
		if in.Kind == "stop" {
			if t.GetString("status") == "running" || t.GetString("status") == "waiting" || t.GetString("status") == "paused" || t.GetString("status") == "interrupted" {
				t.Set("status", "stopping")
			}
			// Stopping also cancels a schedule that has not fired yet, so a
			// later stop press can never be followed by an unattended start.
			if t.GetString("schedule_state") == intake.ScheduleScheduled {
				t.Set("schedule_at", "")
				t.Set("schedule_state", intake.ScheduleNone)
			}
			t.Set("result_generation_needed", true)
			if e = app.Save(t); e != nil {
				return e
			}
			out.Status = t.GetString("status")
			return saveAction(app, ownerID, in, out)
		}
		if !in.Confirmed {
			return errors.New("explicit confirmation is required")
		}
		if s.options.RestoreReview && restoreBlocked(in.Kind) {
			return &ScheduleRefusal{Reason: "restore review is active: reconcile submissions made after the backup and restart without APP_RESTORE_REVIEW before sending"}
		}
		if t.GetString("configuration") == "" || t.GetString("status") == intake.TaskStaging {
			return errors.New("validate the task before sending")
		}
		if _, e := s.usableConnection(ownerID); e != nil {
			return errors.New("connect a usable sending mailbox before starting, retrying or resending")
		}
		isApproved, e := s.approvalFor(app, t, ownerID)
		if e != nil {
			return e
		}
		if in.Kind != "resolve" && !isApproved {
			return errors.New("approve a successfully submitted formatting test first")
		}
		if in.Kind == "start" {
			c, e := app.FindRecordById(campaign.CampaignsCollection, t.GetString("campaign"))
			if e != nil {
				return e
			}
			if c.GetString("current_configuration") != t.GetString("configuration") {
				return errors.New("the configuration changed; revalidate this task")
			}
			if t.GetString("status") != intake.TaskReady {
				return errors.New("use resume or retry for an existing task")
			}
		}
		if in.Kind == "resume" {
			switch t.GetString("status") {
			case "completed":
				_, e := app.FindFirstRecordByFilter(ActionsCollection, "task={:t} && kind='start'", dbx.Params{"t": t.Id})
				if e != nil {
					if errors.Is(e, sql.ErrNoRows) {
						return errors.New("resume requires a previously started task")
					}
					return e
				}
			case "paused", "interrupted", "stopped":
			default:
				return errors.New("resume requires a paused, interrupted, stopped or previously started completed task")
			}
		}
		if (in.Kind == "retry" || in.Kind == "retry_all" || in.Kind == "resend") && (t.GetString("status") == "running" || t.GetString("status") == "stopping") {
			return errors.New("stop or pause the task before changing its work")
		}
		switch in.Kind {
		case "start", "resume", "retry", "retry_all", "resend", "resolve":
		default:
			return errors.New("unknown sending action")
		}
		if (in.Kind == "resend" || in.Kind == "resolve" || in.Kind == "retry") && in.MemberID == "" {
			return errors.New("select one candidate")
		}
		if in.Kind == "resend" && !in.DuplicateRisk {
			return errors.New("confirm the possible duplicate-send risk")
		}
		if in.Kind == "resolve" && (strings.TrimSpace(in.Basis) == "" || strings.TrimSpace(in.Actor) == "") {
			return errors.New("record the actor and external verification basis")
		}
		rows, e := records(app, intake.TaskCandidatesCollection, "task={:t}", "source_row", dbx.Params{"t": t.Id})
		if e != nil {
			return e
		}
		for _, m := range rows {
			if in.MemberID != "" && m.Id != in.MemberID {
				continue
			}
			if m.GetString("state") != intake.RowEligible {
				out.Excluded++
				continue
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, m.GetString("candidate"))
			if e != nil {
				return e
			}
			submission := c.GetString("submission_state")
			dispatch := m.GetString("dispatch_state")
			claim := c.GetString("claim_task")
			eligible := submission == intake.SubmissionNone && (claim == "" || claim == t.Id) && dispatch != AttemptInFlight && dispatch != "reserved"
			switch in.Kind {
			case "retry", "retry_all":
				eligible = eligible && (dispatch == AttemptFailed || dispatch == AttemptAuth)
			case "resend", "resolve":
				eligible = submission == intake.SubmissionUncertain && dispatch == AttemptReview
			default:
				eligible = startable(t.Id, m, c)
			}
			if !eligible {
				out.Excluded++
				continue
			}
			if in.Kind == "resolve" {
				c.Set("submission_state", intake.SubmissionSubmitted)
				c.Set("claim_task", "")
				m.Set("dispatch_state", "manually_submitted")
				col, e := app.FindCollectionByNameOrId(ResolutionsCollection)
				if e != nil {
					return e
				}
				r := core.NewRecord(col)
				r.Set("member", m.Id)
				r.Set("action_id", in.ActionID)
				r.Set("actor", in.Actor)
				r.Set("basis", in.Basis)
				nowField(r, "resolved_at", s.options.Now())
				if e = app.Save(r); e != nil {
					return e
				}
				if e = app.Save(c); e != nil {
					return e
				}
				if e = app.Save(m); e != nil {
					return e
				}
			} else {
				if in.Kind == "resend" {
					col, e := app.FindCollectionByNameOrId(ResolutionsCollection)
					if e != nil {
						return e
					}
					r := core.NewRecord(col)
					r.Set("member", m.Id)
					r.Set("action_id", in.ActionID)
					r.Set("actor", in.Actor)
					r.Set("basis", "operator confirmed possible duplicate send")
					nowField(r, "resolved_at", s.options.Now())
					if e = app.Save(r); e != nil {
						return e
					}
				}
				if e = s.reserve(app, t, m, c); e != nil {
					return e
				}
			}
			out.Claimed++
		}
		if out.Claimed != in.ExpectedCount {
			return errors.New("eligible count changed; refresh and confirm the current count")
		}
		if out.Claimed == 0 {
			return errors.New("no eligible candidates remain; refresh the counts")
		}
		if in.Kind != "resolve" {
			t.Set("status", "running")
			t.Set("pause_reason", "")
			// An explicit start supersedes a pending schedule; the schedule
			// must never fire a second time.
			t.Set("schedule_at", "")
			t.Set("schedule_state", intake.ScheduleNone)
		}
		if e = app.Save(t); e != nil {
			return e
		}
		out.Status = t.GetString("status")
		return saveAction(app, ownerID, in, out)
	})
	return out, err
}
func (s *Store) QueueTest(ctx context.Context, ownerID string, in TestRequest) (*Attempt, error) {
	var out *Attempt
	if in.ActionID == "" {
		return nil, errors.New("an action ID is required")
	}
	recipient, e := campaign.NormalizeRecipient(in.Recipient)
	if e != nil {
		return nil, e
	}
	err := s.app.RunInTransaction(func(app core.App) error {
		old, e := existingAction(app, in.ActionID, in.TaskID, "test")
		if e != nil {
			return e
		}
		if old != nil {
			r, e := app.FindRecordById(AttemptsCollection, old.Status)
			if e != nil {
				return e
			}
			out = attemptFrom(r)
			return nil
		}
		t, e := s.authorizeTask(app, ownerID, in.TaskID)
		if e != nil {
			return e
		}
		connection, e := s.usableConnection(ownerID)
		if e != nil {
			return e
		}
		cfg, e := app.FindRecordById(campaign.ConfigurationsCollection, t.GetString("configuration"))
		if e != nil {
			return e
		}
		raw := in.Values
		if in.MemberID != "" {
			m, e := app.FindRecordById(intake.TaskCandidatesCollection, in.MemberID)
			if e != nil {
				return e
			}
			if m.GetString("task") != t.Id || m.GetString("state") != intake.RowEligible {
				return errors.New("choose a valid sample in this task")
			}
			raw = map[string]string{}
			if e = m.UnmarshalJSONField("raw", &raw); e != nil {
				return e
			}
		}
		var mappings []campaign.Mapping
		if e = cfg.UnmarshalJSONField("mappings", &mappings); e != nil {
			return fmt.Errorf("decode campaign mappings: %w", e)
		}
		compiled, e := campaign.Compile(cfg.GetString("subject"), cfg.GetString("body_html"), mappings)
		if e != nil {
			return e
		}
		rendered, e := compiled.Render(raw)
		if e != nil {
			return e
		}
		col, e := app.FindCollectionByNameOrId(AttemptsCollection)
		if e != nil {
			return e
		}
		r := core.NewRecord(col)
		putWork(r, &Work{
			Kind:            KindTest,
			OwnerID:         ownerID,
			ConnectionID:    connection.Id,
			Generation:      connection.GetInt("generation"),
			TaskID:          t.Id,
			ConfigurationID: cfg.Id,
			ContentHash:     cfg.GetString("content_hash"),
			Recipient:       recipient,
			Sender:          connection.GetString("sender"),
			Subject:         rendered.Subject,
			Body:            rendered.Body,
		})
		r.Set("state", AttemptQueued)
		if e = app.Save(r); e != nil {
			return e
		}
		out = attemptFrom(r)
		return saveAction(app, ownerID, ActionRequest{ActionID: in.ActionID, TaskID: t.Id, Kind: "test", Actor: in.Actor}, &ActionResult{ActionID: in.ActionID, TaskID: t.Id, Status: r.Id})
	})
	return out, err
}
func (s *Store) Approve(ctx context.Context, ownerID, taskID, attemptID, actor, actionID string) error {
	if actionID == "" || actor == "" {
		return errors.New("approval requires an actor and action ID")
	}
	return s.app.RunInTransaction(func(app core.App) error {
		old, e := existingAction(app, actionID, taskID, "approve")
		if e != nil || old != nil {
			return e
		}
		t, e := s.authorizeTask(app, ownerID, taskID)
		if e != nil {
			return e
		}
		connection, e := s.currentConnection(ownerID)
		if e != nil {
			return e
		}
		a, e := app.FindRecordById(AttemptsCollection, attemptID)
		if e != nil {
			return e
		}
		if a.GetString("owner") != ownerID || a.GetString("connection") != connection.Id || a.GetString("connection_generation") != strconv.Itoa(connection.GetInt("generation")) {
			return errors.New("choose a submitted test for the current mailbox connection")
		}
		if a.GetString("kind") != KindTest || a.GetString("state") != AttemptSubmitted || a.GetString("configuration") != t.GetString("configuration") || a.GetString("content_hash") != t.GetString("content_hash") {
			return errors.New("choose a submitted test for this exact configuration")
		}
		isApproved, e := s.approvalFor(app, t, ownerID)
		if e != nil {
			return e
		}
		if !isApproved {
			col, e := app.FindCollectionByNameOrId(ApprovalsCollection)
			if e != nil {
				return e
			}
			r := core.NewRecord(col)
			r.Set("owner", ownerID)
			r.Set("connection", connection.Id)
			r.Set("connection_generation", connection.GetInt("generation"))
			r.Set("configuration", t.GetString("configuration"))
			r.Set("content_hash", t.GetString("content_hash"))
			r.Set("attempt", attemptID)
			r.Set("actor", actor)
			nowField(r, "approved_at", s.options.Now())
			if e = app.Save(r); e != nil {
				return e
			}
		}
		cfg, e := app.FindRecordById(campaign.ConfigurationsCollection, t.GetString("configuration"))
		if e != nil {
			return e
		}
		cfg.Set("approved_hash", t.GetString("content_hash"))
		nowField(cfg, "approved_at", s.options.Now())
		if e = app.Save(cfg); e != nil {
			return e
		}
		return saveAction(app, ownerID, ActionRequest{ActionID: actionID, TaskID: taskID, Kind: "approve", Actor: actor}, &ActionResult{ActionID: actionID, TaskID: taskID, Status: "approved"})
	})
}
