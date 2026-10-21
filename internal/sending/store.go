package sending

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

func records(app core.App, collection, filter, sort string, params dbx.Params) ([]*core.Record, error) {
	return app.FindRecordsByFilter(collection, filter, sort, 0, 0, params)
}
func stamp(r *core.Record, field string) time.Time       { return r.GetDateTime(field).Time().UTC() }
func nowField(r *core.Record, field string, t time.Time) { r.Set(field, t.UTC()) }

// stateRecord finds the pacing row of one connection, creating it at the
// documented default rate the first time that mailbox sends. Pacing history is
// never shared across connections.
func stateRecord(app core.App, connectionID, ownerID string) (*core.Record, error) {
	record, err := app.FindFirstRecordByFilter(
		DispatcherCollection,
		"connection = {:connection}",
		dbx.Params{"connection": connectionID},
	)
	if err == nil {
		return record, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	collection, err := app.FindCollectionByNameOrId(DispatcherCollection)
	if err != nil {
		return nil, err
	}
	record = core.NewRecord(collection)
	record.Set("connection", connectionID)
	record.Set("owner", ownerID)
	record.Set("rate", DefaultRate)
	if err := app.Save(record); err != nil {
		// A concurrent create lost the unique constraint race; read the winner.
		if existing, findErr := app.FindFirstRecordByFilter(DispatcherCollection, "connection = {:connection}", dbx.Params{"connection": connectionID}); findErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return record, nil
}

func settings(r *core.Record) (Settings, error) {
	v := Settings{Rate: r.GetInt("rate"), NextEligible: stamp(r, "next_eligible"), CooldownUntil: stamp(r, "cooldown_until"), Reason: r.GetString("reason")}
	if err := r.UnmarshalJSONField("starts", &v.Starts); err != nil {
		return Settings{}, fmt.Errorf("decode sending pacing history: %w", err)
	}
	return v, nil
}

// DispatcherState returns the pacing and cooldown state of one mailbox
// connection.
func (s *Store) DispatcherState(ctx context.Context, connectionID, ownerID string) (Settings, error) {
	r, e := stateRecord(s.app, connectionID, ownerID)
	if e != nil {
		return Settings{}, e
	}
	v, e := settings(r)
	if e != nil {
		return Settings{}, e
	}
	if v.Rate < 1 || v.Rate > 30 || r.GetFloat("rate") != float64(v.Rate) {
		return v, errors.New("stored sending rate must be an integer from 1 through 30")
	}
	return v, nil
}

// SetRate changes the sending rate of the operator's current mailbox.
func (s *Store) SetRate(ctx context.Context, ownerID string, rate int) error {
	if rate < 1 || rate > 30 {
		return errors.New("sending rate must be an integer from 1 through 30")
	}
	connection, err := s.currentConnection(ownerID)
	if err != nil {
		return err
	}
	return s.app.RunInTransaction(func(app core.App) error {
		r, e := stateRecord(app, connection.Id, ownerID)
		if e != nil {
			return e
		}
		r.Set("rate", rate)
		return app.Save(r)
	})
}

// SetCooldown records a provider-indicated or backoff wait for one connection.
func (s *Store) SetCooldown(ctx context.Context, connectionID, ownerID string, until time.Time, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		r, e := stateRecord(app, connectionID, ownerID)
		if e != nil {
			return e
		}
		if until.After(stamp(r, "cooldown_until")) {
			nowField(r, "cooldown_until", until)
		}
		r.Set("reason", reason)
		return app.Save(r)
	})
}

// currentConnection returns the operator's current connection record. A
// connection that requires reconnection is still returned; callers decide
// whether it may send.
func (s *Store) currentConnection(ownerID string) (*core.Record, error) {
	record, err := s.app.FindFirstRecordByFilter(
		mailbox.ConnectionsCollection,
		"owner = {:owner} && current = true",
		dbx.Params{"owner": ownerID},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, mailbox.ErrNotConnected
	}
	if err != nil {
		return nil, fmt.Errorf("read the mailbox connection: %w", err)
	}
	return record, nil
}

// usableConnection returns the operator's current connection when it may send.
func (s *Store) usableConnection(ownerID string) (*core.Record, error) {
	record, err := s.currentConnection(ownerID)
	if err != nil {
		return nil, err
	}
	if mailbox.Status(record.GetString("status")) != mailbox.StatusConnected {
		return nil, mailbox.ErrNotConnected
	}
	return record, nil
}

// ownerOfTask resolves a task's owning operator through its campaign.
func ownerOfTask(app core.App, task *core.Record) (string, error) {
	campaignRecord, err := app.FindRecordById(campaign.CampaignsCollection, task.GetString("campaign"))
	if err != nil {
		return "", fmt.Errorf("read the owning campaign: %w", err)
	}
	owner := campaignRecord.GetString("owner")
	if owner == "" {
		return "", errors.New("the campaign has no owner")
	}
	return owner, nil
}

// activeReset reports whether an owner's mailbox replacement is unfinished. No
// new work may launch for that owner until it completes.
func activeReset(app core.App, ownerID string) (bool, error) {
	_, err := app.FindFirstRecordByFilter(
		mailbox.ResetsCollection,
		"owner = {:owner} && status = {:status}",
		dbx.Params{"owner": ownerID, "status": mailbox.ResetResetting},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func attemptFrom(r *core.Record) *Attempt {
	return &Attempt{
		ID: r.Id,
		Work: Work{
			ID:              r.Id,
			Kind:            r.GetString("kind"),
			OwnerID:         r.GetString("owner"),
			ConnectionID:    r.GetString("connection"),
			Generation:      r.GetInt("connection_generation"),
			TaskID:          r.GetString("task"),
			MemberID:        r.GetString("member"),
			CandidateID:     r.GetString("candidate"),
			ConfigurationID: r.GetString("configuration"),
			ContentHash:     r.GetString("content_hash"),
			Recipient:       r.GetString("recipient"),
			Sender:          r.GetString("sender"),
			Subject:         r.GetString("subject"),
			Body:            r.GetString("body"),
			RetryCount:      r.GetInt("retry_count"),
		},
		State:      r.GetString("state"),
		Reason:     r.GetString("reason"),
		StartedAt:  stamp(r, "started_at"),
		FinishedAt: stamp(r, "finished_at"),
	}
}

func workFrom(app core.App, task, member, connection *core.Record) (*Work, error) {
	c, e := app.FindRecordById(intake.CandidatesCollection, member.GetString("candidate"))
	if e != nil {
		return nil, e
	}
	cfg, e := app.FindRecordById(campaign.ConfigurationsCollection, task.GetString("configuration"))
	if e != nil {
		return nil, e
	}
	var mappings []campaign.Mapping
	if e = cfg.UnmarshalJSONField("mappings", &mappings); e != nil {
		return nil, fmt.Errorf("decode campaign mappings: %w", e)
	}
	compiled, e := campaign.Compile(cfg.GetString("subject"), cfg.GetString("body_html"), mappings)
	if e != nil {
		return nil, e
	}
	raw := map[string]string{}
	if e = member.UnmarshalJSONField("raw", &raw); e != nil {
		return nil, fmt.Errorf("decode candidate snapshot: %w", e)
	}
	rendered, e := compiled.Render(raw)
	if e != nil {
		return nil, e
	}
	owner, e := ownerOfTask(app, task)
	if e != nil {
		return nil, e
	}
	return &Work{
		ID:              member.Id,
		Kind:            KindCandidate,
		OwnerID:         owner,
		ConnectionID:    connection.Id,
		Generation:      connection.GetInt("generation"),
		TaskID:          task.Id,
		MemberID:        member.Id,
		CandidateID:     c.Id,
		ConfigurationID: cfg.Id,
		ContentHash:     cfg.GetString("content_hash"),
		Recipient:       c.GetString("email_key"),
		Sender:          connection.GetString("sender"),
		Subject:         rendered.Subject,
		Body:            rendered.Body,
		RetryCount:      member.GetInt("retry_count"),
	}, nil
}

// NextWork selects one due unit of work for a usable mailbox. Tasks rotate so
// one busy campaign cannot monopolize opportunities, and work only exists for
// connections that are currently connected. Connection IDs passed as exhausted
// are skipped so a mailbox with no free slots cannot delay another mailbox.
func (s *Store) NextWork(ctx context.Context, exhausted ...string) (*Work, error) {
	if s.options.RestoreReview {
		return nil, nil
	}
	now := s.options.Now().UTC().Format(types.DefaultDateLayout)
	tests, e := s.app.FindRecordsByFilter(AttemptsCollection, "kind='test' && state='queued' && (retry_at='' || retry_at <= {:now})", "created", 1, 0, dbx.Params{"now": now})
	if e != nil {
		return nil, e
	}
	for _, r := range tests {
		if stamp(r, "retry_at").After(s.options.Now()) {
			continue
		}
		work := attemptFrom(r).Work
		if slices.Contains(exhausted, work.ConnectionID) {
			continue
		}
		if err := s.workConnectionUsable(work.OwnerID, work.ConnectionID); err != nil {
			continue
		}
		return &work, nil
	}
	tasks, e := records(s.app, intake.TasksCollection, "status='running'", "created", nil)
	if e != nil {
		return nil, e
	}
	s.cursorMu.Lock()
	lastTask := s.lastTask
	s.cursorMu.Unlock()
	start := 0
	for i, t := range tasks {
		if t.Id == lastTask {
			start = i + 1
			break
		}
	}
	for n := range tasks {
		task := tasks[(start+n)%len(tasks)]
		owner, e := ownerOfTask(s.app, task)
		if e != nil {
			return nil, e
		}
		blocked, e := activeReset(s.app, owner)
		if e != nil {
			return nil, e
		}
		if blocked {
			continue
		}
		connection, e := s.usableConnection(owner)
		if errors.Is(e, mailbox.ErrNotConnected) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if slices.Contains(exhausted, connection.Id) {
			continue
		}
		members, e := s.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:task} && dispatch_state='reserved' && (retry_at='' || retry_at <= {:now})", "source_row", 1, 0, dbx.Params{"task": task.Id, "now": now})
		if e != nil {
			return nil, e
		}
		for _, m := range members {
			if stamp(m, "retry_at").After(s.options.Now()) {
				continue
			}
			w, e := workFrom(s.app, task, m, connection)
			if e != nil {
				return nil, e
			}
			return w, nil
		}
	}
	return nil, nil
}

// workConnectionUsable reports whether the recorded connection may still send.
func (s *Store) workConnectionUsable(ownerID, connectionID string) error {
	connection, err := s.currentConnection(ownerID)
	if err != nil {
		return err
	}
	if connection.Id != connectionID || mailbox.Status(connection.GetString("status")) != mailbox.StatusConnected {
		return mailbox.ErrNotConnected
	}
	blocked, err := activeReset(s.app, ownerID)
	if err != nil {
		return err
	}
	if blocked {
		return errors.New("a mailbox replacement is in progress")
	}
	return nil
}

// NextWorkDeadline returns the earliest persisted retry time among currently
// eligible pending work. It is consulted only after NextWork finds nothing;
// historical retryable attempts and tasks outside the running state do not
// schedule dispatcher wakeups.
func (s *Store) NextWorkDeadline(ctx context.Context) (time.Time, error) {
	now := s.options.Now().UTC()
	var earliest time.Time
	tests, err := s.app.FindRecordsByFilter(AttemptsCollection, "kind='test' && state='queued'", "retry_at", 1, 0, nil)
	if err != nil {
		return time.Time{}, err
	}
	if len(tests) > 0 {
		earliest = stamp(tests[0], "retry_at")
		if earliest.IsZero() {
			return now, nil
		}
	}
	// A pending schedule is itself a wakeup: it must be observed even when no
	// candidate work is runnable, so the fire path can recheck approval and
	// eligibility before starting.
	scheduled, err := s.app.FindRecordsByFilter(intake.TasksCollection, "status={:ready} && schedule_state={:scheduled}", "schedule_at", 1, 0, dbx.Params{"ready": intake.TaskReady, "scheduled": intake.ScheduleScheduled})
	if err != nil {
		return time.Time{}, err
	}
	if len(scheduled) > 0 {
		deadline := stamp(scheduled[0], "schedule_at")
		if deadline.IsZero() {
			return now, nil
		}
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	tasks, err := records(s.app, intake.TasksCollection, "status='running'", "created", nil)
	if err != nil {
		return time.Time{}, err
	}
	for _, task := range tasks {
		members, err := s.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:task} && dispatch_state='reserved'", "retry_at", 1, 0, dbx.Params{"task": task.Id})
		if err != nil {
			return time.Time{}, err
		}
		if len(members) == 0 {
			continue
		}
		deadline := stamp(members[0], "retry_at")
		if deadline.IsZero() {
			return now, nil
		}
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	return earliest, nil
}

// BeginAttempt persists the intent before transmission and spends one paced
// start on the attempt's own mailbox.
func (s *Store) BeginAttempt(ctx context.Context, w *Work, startedAt, nextEligible time.Time) (*Attempt, error) {
	var out *Attempt
	err := s.app.RunInTransaction(func(app core.App) error {
		var r *core.Record
		if w.Kind == KindTest {
			var e error
			r, e = app.FindRecordById(AttemptsCollection, w.ID)
			if e != nil {
				return e
			}
			if r.GetString("state") != AttemptQueued && r.GetString("state") != AttemptRetryable {
				return ErrNoWork
			}
			// A queued test must not outlive a connection change: it sends only
			// through the connection that was current when it was queued.
			connection, e := app.FindRecordById(mailbox.ConnectionsCollection, w.ConnectionID)
			if e != nil {
				return e
			}
			if mailbox.Status(connection.GetString("status")) != mailbox.StatusConnected {
				return ErrNoWork
			}
		} else {
			task, e := app.FindRecordById(intake.TasksCollection, w.TaskID)
			if e != nil {
				return e
			}
			m, e := app.FindRecordById(intake.TaskCandidatesCollection, w.MemberID)
			if e != nil {
				return e
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, w.CandidateID)
			if e != nil {
				return e
			}
			if task.GetString("status") != "running" || m.GetString("dispatch_state") != "reserved" || c.GetString("claim_task") != task.Id || c.GetString("submission_state") != intake.SubmissionNone {
				return ErrNoWork
			}
			connection, e := app.FindRecordById(mailbox.ConnectionsCollection, w.ConnectionID)
			if e != nil {
				return e
			}
			if mailbox.Status(connection.GetString("status")) != mailbox.StatusConnected {
				return ErrNoWork
			}
			fresh, e := workFrom(app, task, m, connection)
			if e != nil {
				return e
			}
			w = fresh
			collection, e := app.FindCollectionByNameOrId(AttemptsCollection)
			if e != nil {
				return e
			}
			r = core.NewRecord(collection)
			putWork(r, w)
			m.Set("dispatch_state", AttemptInFlight)
			if e = app.Save(m); e != nil {
				return e
			}
		}
		r.Set("state", AttemptInFlight)
		nowField(r, "started_at", startedAt)
		if e := app.Save(r); e != nil {
			return e
		}
		p, e := stateRecord(app, w.ConnectionID, w.OwnerID)
		if e != nil {
			return e
		}
		v, e := settings(p)
		if e != nil {
			return e
		}
		var starts []time.Time
		for _, t := range v.Starts {
			if t.After(startedAt.Add(-time.Minute)) {
				starts = append(starts, t)
			}
		}
		starts = append(starts, startedAt.UTC())
		p.Set("starts", starts)
		nowField(p, "next_eligible", nextEligible)
		if e = app.Save(p); e != nil {
			return e
		}
		out = attemptFrom(r)
		return nil
	})
	if err == nil && out != nil && out.Kind == KindCandidate {
		s.cursorMu.Lock()
		s.lastTask = out.TaskID
		s.cursorMu.Unlock()
	}
	return out, err
}

func putWork(r *core.Record, w *Work) {
	r.Set("owner", w.OwnerID)
	r.Set("connection", w.ConnectionID)
	r.Set("connection_generation", w.Generation)
	r.Set("task", w.TaskID)
	r.Set("member", w.MemberID)
	r.Set("candidate", w.CandidateID)
	r.Set("configuration", w.ConfigurationID)
	r.Set("content_hash", w.ContentHash)
	r.Set("kind", w.Kind)
	r.Set("recipient", w.Recipient)
	r.Set("sender", w.Sender)
	r.Set("subject", w.Subject)
	r.Set("body", w.Body)
	r.Set("retry_count", w.RetryCount)
}

// RecordActualStart replaces the provisional paced start with the real launch
// instant in the same mailbox's rolling history.
func (s *Store) RecordActualStart(ctx context.Context, w *Work, id string, startedAt, nextEligible time.Time) error {
	return s.app.RunInTransaction(func(app core.App) error {
		r, e := app.FindRecordById(AttemptsCollection, id)
		if e != nil {
			return e
		}
		old := stamp(r, "started_at")
		p, e := stateRecord(app, w.ConnectionID, w.OwnerID)
		if e != nil {
			return e
		}
		v, e := settings(p)
		if e != nil {
			return e
		}
		for i, t := range v.Starts {
			if t.Equal(old) {
				v.Starts[i] = startedAt.UTC()
				break
			}
		}
		p.Set("starts", v.Starts)
		nowField(p, "next_eligible", nextEligible)
		if e = app.Save(p); e != nil {
			return e
		}
		nowField(r, "started_at", startedAt)
		return app.Save(r)
	})
}

func (s *Store) CompleteAttempt(ctx context.Context, id string, outcome Outcome) error {
	return s.app.RunInTransaction(func(app core.App) error {
		r, e := app.FindRecordById(AttemptsCollection, id)
		if e != nil {
			return e
		}
		if r.GetString("state") != AttemptInFlight {
			return ErrNoWork
		}
		state := outcome.State
		if state != AttemptSubmitted && state != AttemptFailed && state != AttemptReview && state != AttemptRetryable && state != AttemptAuth {
			return errors.New("unknown attempt outcome")
		}
		r.Set("state", state)
		r.Set("reason", outcome.Reason)
		nowField(r, "finished_at", s.options.Now())
		nowField(r, "retry_at", outcome.RetryAt)
		retry := r.GetInt("retry_count") + 1
		if state == AttemptRetryable {
			r.Set("retry_count", retry)
			if retry > 3 {
				state = AttemptFailed
				r.Set("state", state)
			}
		}
		if e = app.Save(r); e != nil {
			return e
		}
		if r.GetString("kind") == KindTest && state == AttemptRetryable {
			collection, e := app.FindCollectionByNameOrId(AttemptsCollection)
			if e != nil {
				return e
			}
			next := core.NewRecord(collection)
			w := attemptFrom(r).Work
			w.RetryCount = retry
			putWork(next, &w)
			next.Set("state", AttemptQueued)
			nowField(next, "retry_at", outcome.RetryAt)
			if e = app.Save(next); e != nil {
				return e
			}
		}
		if r.GetString("kind") == KindCandidate {
			m, e := app.FindRecordById(intake.TaskCandidatesCollection, r.GetString("member"))
			if e != nil {
				return e
			}
			c, e := app.FindRecordById(intake.CandidatesCollection, r.GetString("candidate"))
			if e != nil {
				return e
			}
			m.Set("dispatch_state", state)
			if outcome.RequiresCorrection && state == AttemptFailed {
				m.Set("dispatch_state", "correction_required")
			}
			switch state {
			case AttemptSubmitted:
				c.Set("submission_state", intake.SubmissionSubmitted)
				c.Set("claim_task", "")
			case AttemptReview:
				c.Set("submission_state", intake.SubmissionUncertain)
				c.Set("claim_task", "")
			case AttemptRetryable:
				m.Set("dispatch_state", "reserved")
				m.Set("retry_count", retry)
				nowField(m, "retry_at", outcome.RetryAt)
			default:
				c.Set("claim_task", "")
			}
			if e = app.Save(c); e != nil {
				return e
			}
			if e = app.Save(m); e != nil {
				return e
			}
			if outcome.State == AttemptRetryable && retry > 3 {
				if e = pauseTask(app, r.GetString("task"), "safe retry limit reached"); e != nil {
					return e
				}
			}
		}
		if outcome.PauseMailbox || state == AttemptAuth {
			// A provider failure pauses only the affected operator's work; other
			// mailboxes keep their paced opportunities.
			return pauseOwner(app, r.GetString("owner"), outcome.Reason)
		}
		return nil
	})
}

func pauseTask(app core.App, id, reason string) error {
	r, e := app.FindRecordById(intake.TasksCollection, id)
	if e != nil {
		return e
	}
	r.Set("status", "paused")
	r.Set("pause_reason", reason)
	if e = releaseReservations(app, id); e != nil {
		return e
	}
	return app.Save(r)
}

// pauseOwner pauses every sendable task of one operator, leaving other
// operators untouched.
func pauseOwner(app core.App, ownerID, reason string) error {
	tasks, e := records(app, intake.TasksCollection, "status='running' || status='waiting'", "", nil)
	if e != nil {
		return e
	}
	for _, r := range tasks {
		owner, e := ownerOfTask(app, r)
		if e != nil {
			return e
		}
		if owner != ownerID {
			continue
		}
		if e = pauseTask(app, r.Id, reason); e != nil {
			return e
		}
	}
	return nil
}

// pauseAll stops every launch. It is reserved for a shared inability to persist
// outcomes, which affects every mailbox.
func pauseAll(app core.App, reason string) error {
	tasks, e := records(app, intake.TasksCollection, "status='running' || status='waiting'", "", nil)
	if e != nil {
		return e
	}
	for _, r := range tasks {
		if e = pauseTask(app, r.Id, reason); e != nil {
			return e
		}
	}
	return nil
}

// PauseMailbox pauses the affected operator's work after a provider failure.
func (s *Store) PauseMailbox(ctx context.Context, ownerID, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error { return pauseOwner(app, ownerID, reason) })
}

// PauseAll stops every launch after a shared storage failure.
func (s *Store) PauseAll(ctx context.Context, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error { return pauseAll(app, reason) })
}

// PauseTask pauses one task after an explicit operator or authorization action.
func (s *Store) PauseTask(ctx context.Context, ownerID, id, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		task, err := app.FindRecordById(intake.TasksCollection, id)
		if err != nil {
			return err
		}
		owner, err := ownerOfTask(app, task)
		if err != nil {
			return err
		}
		if owner != ownerID {
			return ErrNoWork
		}
		return pauseTask(app, id, reason)
	})
}

// releaseReservations releases only work which has no in-flight intent.
func releaseReservations(app core.App, taskID string) error {
	members, e := records(app, intake.TaskCandidatesCollection, "task={:t} && dispatch_state='reserved'", "", dbx.Params{"t": taskID})
	if e != nil {
		return e
	}
	for _, m := range members {
		c, e := app.FindRecordById(intake.CandidatesCollection, m.GetString("candidate"))
		if e != nil {
			return e
		}
		if c.GetString("claim_task") != taskID || c.GetString("submission_state") != intake.SubmissionNone {
			continue
		}
		// A persisted intent always wins over an inconsistent reservation marker.
		intents, e := records(app, AttemptsCollection, "member={:m} && state='in_flight'", "", dbx.Params{"m": m.Id})
		if e != nil {
			return e
		}
		if len(intents) > 0 {
			continue
		}
		c.Set("claim_task", "")
		c.Set("claimed_at", "")
		m.Set("dispatch_state", "")
		if e = app.Save(c); e != nil {
			return e
		}
		if e = app.Save(m); e != nil {
			return e
		}
	}
	return nil
}
