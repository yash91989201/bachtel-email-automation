package exports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Operator-facing row outcomes written into the generated workbook. They
// describe recorded state only; a provider acceptance is never called delivery.
const (
	statusSubmitted        = "Submitted"
	statusSubmittedManual  = "Submitted (manual resolution)"
	statusUncertain        = "Uncertain - review required"
	statusEligible         = "Eligible - not sent"
	statusScheduled        = "Scheduled"
	statusInFlight         = "In flight"
	statusQueued           = "Queued"
	statusFailed           = "Failed - definite failure"
	statusAuthFailed       = "Failed - authorization"
	statusRetryScheduled   = "Retry scheduled"
	statusReviewRequired   = "Review required"
	statusCorrection       = "Correction required"
	statusDuplicate        = "Excluded - duplicate"
	statusInvalidAddress   = "Excluded - invalid address"
	statusAlreadySubmitted = "Already submitted in this campaign"
	statusAlreadyScheduled = "Already scheduled in another task"
	statusIneligible       = "Ineligible"
	statusUnknown          = "Recorded state unavailable"
)

// taskSnapshot is one immutable view of a task's recorded results. Database
// records are read in one transaction; the retained workbook and manifest are
// immutable private files read outside it.
type taskSnapshot struct {
	Task        *core.Record
	Manifest    intake.Manifest
	Source      []byte
	Members     map[int]taskMember
	Attempts    map[string][]*core.Record
	Resolutions map[string]*core.Record
	AsOf        time.Time
}

type taskMember struct {
	Member    *core.Record
	Candidate *core.Record
}

func (s *Service) taskSnapshot(ctx context.Context, task *core.Record, artifact *core.Record) (*taskSnapshot, error) {
	snapshot := &taskSnapshot{
		Task:        task,
		Members:     map[int]taskMember{},
		Attempts:    map[string][]*core.Record{},
		Resolutions: map[string]*core.Record{},
		AsOf:        artifact.GetDateTime("as_of").Time().UTC(),
	}
	err := s.app.RunInTransaction(func(txApp core.App) error {
		members, err := records(txApp, intake.TaskCandidatesCollection, "task={:t}", "source_row", dbx.Params{"t": task.Id})
		if err != nil {
			return err
		}
		candidates, err := records(txApp, intake.CandidatesCollection, "campaign={:c}", "", dbx.Params{"c": task.GetString("campaign")})
		if err != nil {
			return err
		}
		candidateByID := make(map[string]*core.Record, len(candidates))
		for _, candidate := range candidates {
			candidateByID[candidate.Id] = candidate
		}
		for _, member := range members {
			snapshot.Members[member.GetInt("source_row")] = taskMember{
				Member:    member,
				Candidate: candidateByID[member.GetString("candidate")],
			}
		}
		attempts, err := records(txApp, sending.AttemptsCollection, "task={:t}", "-created,-id", dbx.Params{"t": task.Id})
		if err != nil {
			return err
		}
		for _, attempt := range attempts {
			member := attempt.GetString("member")
			if member == "" {
				continue
			}
			snapshot.Attempts[member] = append(snapshot.Attempts[member], attempt)
		}
		resolutions, err := records(txApp, sending.ResolutionsCollection, "member.task={:t}", "-resolved_at,-id", dbx.Params{"t": task.Id})
		if err != nil {
			return err
		}
		for _, resolution := range resolutions {
			member := resolution.GetString("member")
			if snapshot.Resolutions[member] == nil {
				snapshot.Resolutions[member] = resolution
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	manifestData, err := s.readField(ctx, task, "manifest")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(manifestData, &snapshot.Manifest); err != nil {
		return nil, fmt.Errorf("read the validation manifest: %w", err)
	}
	snapshot.Source, err = s.readField(ctx, task, "source")
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func records(app core.App, collection, filter, sort string, params dbx.Params) ([]*core.Record, error) {
	return app.FindRecordsByFilter(collection, filter, sort, 0, 0, params)
}

// TaskWorkbook appends the task's configured result columns to the retained
// original workbook. Existing cells, sheets and ordering are never rewritten;
// new values are written with the string API so formula-like candidate text
// stays text.
func TaskWorkbook(snapshot *taskSnapshot) (data []byte, err error) {
	limits := intake.DefaultLimits()
	book, err := excelize.OpenReader(bytes.NewReader(snapshot.Source), excelize.Options{
		UnzipSizeLimit:    limits.UnzipSizeLimit,
		UnzipXMLSizeLimit: limits.UnzipXMLSizeLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("open the retained workbook: %w", err)
	}
	defer func() { err = errors.Join(err, book.Close()) }()

	sheet := snapshot.Task.GetString("sheet")
	headerRow := snapshot.Task.GetInt("header_row")
	if sheet == "" || headerRow < 1 {
		return nil, fmt.Errorf("the retained workbook selection is missing")
	}
	grid, err := book.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("read the selected worksheet: %w", err)
	}
	maxColumns := 0
	for _, row := range grid {
		if len(row) > maxColumns {
			maxColumns = len(row)
		}
	}
	if maxColumns == 0 {
		return nil, fmt.Errorf("the selected worksheet is empty")
	}

	var labels []string
	if err := snapshot.Task.UnmarshalJSONField("result_labels", &labels); err != nil {
		labels = nil
	}
	if len(labels) == 0 {
		labels = []string{"Email Status"}
	}
	for offset, label := range labels {
		cell, err := excelize.CoordinatesToCellName(maxColumns+offset+1, headerRow)
		if err != nil {
			return nil, err
		}
		if err := book.SetCellStr(sheet, cell, label); err != nil {
			return nil, err
		}
	}

	for _, row := range snapshot.Manifest.Rows {
		if row.Row <= headerRow {
			continue
		}
		status, at, reason := snapshot.rowResult(row)
		values := []string{status, sending.DisplayTime(at), reason}
		for offset := range labels {
			value := ""
			if offset < len(values) {
				value = values[offset]
			}
			cell, err := excelize.CoordinatesToCellName(maxColumns+offset+1, row.Row)
			if err != nil {
				return nil, err
			}
			if err := book.SetCellStr(sheet, cell, value); err != nil {
				return nil, err
			}
		}
	}

	var out bytes.Buffer
	if err := book.Write(&out); err != nil {
		return nil, fmt.Errorf("write the results workbook: %w", err)
	}
	return out.Bytes(), nil
}

// rowResult maps one manifest row to its status, timestamp and reason. The
// persisted attempt and candidate state is authoritative; missing evidence is
// never rendered as proof that an email was not submitted.
func (s *taskSnapshot) rowResult(row intake.ManifestRow) (string, time.Time, string) {
	if row.Excluded() {
		reasons := make([]string, 0, len(row.Reasons))
		for _, code := range row.Reasons {
			reasons = append(reasons, intake.ReasonText(code))
		}
		switch {
		case hasReason(row.Reasons, intake.ReasonDuplicate):
			return statusDuplicate, time.Time{}, strings.Join(reasons, "; ")
		case hasReason(row.Reasons, intake.ReasonInvalidAddress):
			return statusInvalidAddress, time.Time{}, strings.Join(reasons, "; ")
		default:
			return statusUnknown, time.Time{}, strings.Join(reasons, "; ")
		}
	}

	entry, ok := s.Members[row.Row]
	if !ok {
		return statusUnknown, time.Time{}, "the task snapshot has no row record"
	}
	member, candidate := entry.Member, entry.Candidate
	attempts := s.Attempts[member.Id]
	resolution := s.Resolutions[member.Id]
	latest := latestAttempt(attempts)

	if candidate != nil {
		switch candidate.GetString("submission_state") {
		case intake.SubmissionSubmitted:
			if member.GetString("dispatch_state") == "manually_submitted" && resolution != nil {
				return statusSubmittedManual, resolution.GetDateTime("resolved_at").Time().UTC(), resolution.GetString("basis")
			}
			return statusSubmitted, attemptTime(latest, true), attemptReason(latest)
		case intake.SubmissionUncertain:
			return statusUncertain, attemptTime(latest, true), attemptReason(latest)
		}
	}

	switch member.GetString("dispatch_state") {
	case "reserved":
		return statusScheduled, time.Time{}, member.GetString("reason")
	case "in_flight":
		return statusInFlight, attemptTime(latest, false), attemptReason(latest)
	case "queued":
		return statusQueued, time.Time{}, member.GetString("reason")
	case "definite_failure":
		return statusFailed, attemptTime(latest, true), attemptReason(latest)
	case "auth_failure":
		return statusAuthFailed, attemptTime(latest, true), attemptReason(latest)
	case "retryable":
		return statusRetryScheduled, attemptTime(latest, true), attemptReason(latest)
	case "review_required":
		return statusReviewRequired, attemptTime(latest, true), attemptReason(latest)
	case "correction_required":
		return statusCorrection, attemptTime(latest, true), attemptReason(latest)
	case "manually_submitted":
		if resolution != nil {
			return statusSubmittedManual, resolution.GetDateTime("resolved_at").Time().UTC(), resolution.GetString("basis")
		}
		return statusSubmittedManual, time.Time{}, ""
	}

	switch member.GetString("state") {
	case intake.RowEligible:
		return statusEligible, time.Time{}, member.GetString("reason")
	case intake.RowAlreadySubmitted:
		return statusAlreadySubmitted, time.Time{}, member.GetString("reason")
	case intake.RowAlreadyScheduled:
		return statusAlreadyScheduled, time.Time{}, member.GetString("reason")
	case intake.RowReviewRequired:
		return statusReviewRequired, attemptTime(latest, true), member.GetString("reason")
	case intake.RowIneligible:
		return statusIneligible, time.Time{}, member.GetString("reason")
	default:
		return statusUnknown, time.Time{}, member.GetString("reason")
	}
}

func hasReason(reasons []string, target string) bool {
	return slices.Contains(reasons, target)
}

func latestAttempt(attempts []*core.Record) *core.Record {
	if len(attempts) == 0 {
		return nil
	}
	return attempts[0]
}

func attemptTime(attempt *core.Record, finished bool) time.Time {
	if attempt == nil {
		return time.Time{}
	}
	if finished {
		if at := attempt.GetDateTime("finished_at").Time(); !at.IsZero() {
			return at.UTC()
		}
	}
	return attempt.GetDateTime("started_at").Time().UTC()
}

func attemptReason(attempt *core.Record) string {
	if attempt == nil {
		return ""
	}
	return attempt.GetString("reason")
}
