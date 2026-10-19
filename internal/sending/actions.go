package sending

import (
	"context"
	"database/sql"
	"errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"strings"
)

func approved(app core.App, t *core.Record) (bool, error) {
	r, e := app.FindFirstRecordByFilter(ApprovalsCollection, "configuration={:c} && content_hash={:h}", dbx.Params{"c": t.GetString("configuration"), "h": t.GetString("content_hash")})
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return r != nil && e == nil, e
}
func (s *Store) Preview(ctx context.Context, id string) (*Progress, error) {
	t, e := s.app.FindRecordById(intake.TasksCollection, id)
	if e != nil {
		return nil, e
	}
	isApproved, e := approved(s.app, t)
	if e != nil {
		return nil, e
	}
	p := &Progress{TaskID: id, Status: t.GetString("status"), Reason: t.GetString("pause_reason"), ContentHash: t.GetString("content_hash"), Approved: isApproved, ProductionEnabled: s.options.AllowCandidateSending, ArtifactNeeded: t.GetBool("result_generation_needed")}
	var counts intake.Counts
	_ = t.UnmarshalJSONField("counts", &counts)
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
func saveAction(app core.App, in ActionRequest, out *ActionResult) error {
	c, e := app.FindCollectionByNameOrId(ActionsCollection)
	if e != nil {
		return e
	}
	r := core.NewRecord(c)
	r.Set("action_id", in.ActionID)
	r.Set("task", in.TaskID)
	r.Set("kind", in.Kind)
	r.Set("actor", in.Actor)
	r.Set("result", out)
	return app.Save(r)
}
func (s *Store) Act(ctx context.Context, in ActionRequest) (*ActionResult, error) {
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
		t, e := app.FindRecordById(intake.TasksCollection, in.TaskID)
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
			return saveAction(app, in, out)
		}
		if in.Kind == "stop" {
			if t.GetString("status") == "running" || t.GetString("status") == "waiting" || t.GetString("status") == "paused" || t.GetString("status") == "interrupted" {
				t.Set("status", "stopping")
			}
			t.Set("result_generation_needed", true)
			if e = app.Save(t); e != nil {
				return e
			}
			out.Status = t.GetString("status")
			return saveAction(app, in, out)
		}
		if !in.Confirmed {
			return errors.New("explicit confirmation is required")
		}
		if in.Kind != "resolve" && !s.options.AllowCandidateSending {
			return ErrReleaseGate
		}
		if t.GetString("configuration") == "" || t.GetString("status") == intake.TaskStaging {
			return errors.New("validate the task before sending")
		}
		isApproved, e := approved(app, t)
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
				eligible = eligible && dispatch != AttemptFailed && dispatch != AttemptAuth && dispatch != AttemptSubmitted && dispatch != AttemptReview && dispatch != "correction_required"
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
				c.Set("submission_state", intake.SubmissionNone)
				c.Set("claim_task", t.Id)
				nowField(c, "claimed_at", s.options.Now())
				m.Set("dispatch_state", "reserved")
				m.Set("retry_count", 0)
				m.Set("retry_at", "")
			}
			if e = app.Save(c); e != nil {
				return e
			}
			if e = app.Save(m); e != nil {
				return e
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
		}
		if e = app.Save(t); e != nil {
			return e
		}
		out.Status = t.GetString("status")
		return saveAction(app, in, out)
	})
	return out, err
}
func (s *Store) QueueTest(ctx context.Context, in TestRequest) (*Attempt, error) {
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
		t, e := app.FindRecordById(intake.TasksCollection, in.TaskID)
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
		_ = cfg.UnmarshalJSONField("mappings", &mappings)
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
		putWork(r, &Work{Kind: KindTest, TaskID: t.Id, ConfigurationID: cfg.Id, ContentHash: cfg.GetString("content_hash"), Recipient: recipient, Subject: rendered.Subject, Body: rendered.Body})
		r.Set("state", AttemptQueued)
		if e = app.Save(r); e != nil {
			return e
		}
		out = attemptFrom(r)
		return saveAction(app, ActionRequest{ActionID: in.ActionID, TaskID: t.Id, Kind: "test", Actor: in.Actor}, &ActionResult{ActionID: in.ActionID, TaskID: t.Id, Status: r.Id})
	})
	return out, err
}
func (s *Store) Approve(ctx context.Context, taskID, attemptID, actor, actionID string) error {
	if actionID == "" || actor == "" {
		return errors.New("approval requires an actor and action ID")
	}
	return s.app.RunInTransaction(func(app core.App) error {
		old, e := existingAction(app, actionID, taskID, "approve")
		if e != nil || old != nil {
			return e
		}
		t, e := app.FindRecordById(intake.TasksCollection, taskID)
		if e != nil {
			return e
		}
		a, e := app.FindRecordById(AttemptsCollection, attemptID)
		if e != nil {
			return e
		}
		if a.GetString("kind") != KindTest || a.GetString("state") != AttemptSubmitted || a.GetString("configuration") != t.GetString("configuration") || a.GetString("content_hash") != t.GetString("content_hash") {
			return errors.New("choose a submitted test for this exact configuration")
		}
		isApproved, e := approved(app, t)
		if e != nil {
			return e
		}
		if !isApproved {
			col, e := app.FindCollectionByNameOrId(ApprovalsCollection)
			if e != nil {
				return e
			}
			r := core.NewRecord(col)
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
		return saveAction(app, ActionRequest{ActionID: actionID, TaskID: taskID, Kind: "approve", Actor: actor}, &ActionResult{ActionID: actionID, TaskID: taskID, Status: "approved"})
	})
}
