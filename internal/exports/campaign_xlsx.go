package exports

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// campaignSnapshot is one consistent view of a campaign's candidate and
// attempt history, read inside a single database transaction.
type campaignSnapshot struct {
	Campaign    *core.Record
	AsOf        time.Time
	Candidates  []*core.Record
	Attempts    []*core.Record
	Tasks       []*core.Record
	Resolutions map[string]*core.Record
}

func (s *Service) campaignSnapshot(ctx context.Context, target *core.Record, asOf time.Time) (*campaignSnapshot, error) {
	snapshot := &campaignSnapshot{
		Campaign:    target,
		AsOf:        asOf,
		Resolutions: map[string]*core.Record{},
	}
	err := s.app.RunInTransaction(func(txApp core.App) error {
		var err error
		snapshot.Candidates, err = records(txApp, intake.CandidatesCollection, "campaign={:c}", "email_key", dbx.Params{"c": target.Id})
		if err != nil {
			return err
		}
		snapshot.Tasks, err = records(txApp, intake.TasksCollection, "campaign={:c}", "-created", dbx.Params{"c": target.Id})
		if err != nil {
			return err
		}
		snapshot.Attempts, err = records(txApp, sending.AttemptsCollection, "task.campaign={:c}", "-created,-id", dbx.Params{"c": target.Id})
		if err != nil {
			return err
		}
		resolutions, err := records(txApp, sending.ResolutionsCollection, "member.task.campaign={:c}", "-resolved_at,-id", dbx.Params{"c": target.Id})
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
	return snapshot, nil
}

// CampaignReport builds the campaign XLSX: an as-of summary, one row per
// campaign candidate and every attempt with its kind kept explicit, so tests
// never inflate candidate totals.
func CampaignReport(snapshot *campaignSnapshot) ([]byte, error) {
	book := excelize.NewFile()
	if err := book.SetSheetName("Sheet1", "Report"); err != nil {
		return nil, err
	}
	for _, sheet := range []string{"Candidates", "Attempts", "Resolutions"} {
		if _, err := book.NewSheet(sheet); err != nil {
			return nil, err
		}
	}

	latestByCandidate := map[string]*core.Record{}
	submittedByCandidate := map[string]*core.Record{}
	attemptCount := map[string]int{}
	tests, candidateAttempts := 0, 0
	for _, attempt := range snapshot.Attempts {
		if attempt.GetString("kind") == sending.KindTest {
			tests++
			continue
		}
		candidateAttempts++
		candidateID := attempt.GetString("candidate")
		attemptCount[candidateID]++
		if latestByCandidate[candidateID] == nil {
			latestByCandidate[candidateID] = attempt
		}
		if attempt.GetString("state") == sending.AttemptSubmitted && submittedByCandidate[candidateID] == nil {
			submittedByCandidate[candidateID] = attempt
		}
	}

	submitted, uncertain := 0, 0
	for _, candidate := range snapshot.Candidates {
		switch candidate.GetString("submission_state") {
		case intake.SubmissionSubmitted:
			submitted++
		case intake.SubmissionUncertain:
			uncertain++
		}
	}

	report := [][2]string{
		{"Campaign", snapshot.Campaign.GetString("name")},
		{"Campaign ID", snapshot.Campaign.Id},
		{"As of", sending.DisplayTime(snapshot.AsOf)},
		{"Timezone", "Asia/Kolkata (+05:30)"},
		{"Candidates", fmt.Sprintf("%d", len(snapshot.Candidates))},
		{"Submitted (provider accepted)", fmt.Sprintf("%d", submitted)},
		{"Uncertain - review required", fmt.Sprintf("%d", uncertain)},
		{"Candidate attempts", fmt.Sprintf("%d", candidateAttempts)},
		{"Test attempts (excluded from candidate totals)", fmt.Sprintf("%d", tests)},
		{"Email tasks", fmt.Sprintf("%d", len(snapshot.Tasks))},
	}
	for i, entry := range report {
		if err := setRow(book, "Report", 1, i+1, entry[:]); err != nil {
			return nil, err
		}
	}

	candidateHeaders := []string{"Candidate ID", "Recipient", "Current state", "Claimed by task", "Candidate attempts", "Submitted at", "Last outcome", "Last attempt at"}
	if err := setRow(book, "Candidates", 1, 1, candidateHeaders); err != nil {
		return nil, err
	}
	for i, candidate := range snapshot.Candidates {
		latest := latestByCandidate[candidate.Id]
		submittedAt := time.Time{}
		if attempt := submittedByCandidate[candidate.Id]; attempt != nil {
			submittedAt = attempt.GetDateTime("finished_at").Time().UTC()
		}
		if latest != nil {
			if resolution := snapshot.Resolutions[latest.GetString("member")]; resolution != nil && submittedAt.IsZero() {
				submittedAt = resolution.GetDateTime("resolved_at").Time().UTC()
			}
		}
		lastOutcome := "not attempted"
		lastAt := time.Time{}
		if latest != nil {
			lastOutcome = latest.GetString("state")
			lastAt = attemptTime(latest, true)
		}
		values := []string{
			candidate.Id,
			candidate.GetString("email_key"),
			campaignState(candidate),
			candidate.GetString("claim_task"),
			fmt.Sprintf("%d", attemptCount[candidate.Id]),
			sending.DisplayTime(submittedAt),
			lastOutcome,
			sending.DisplayTime(lastAt),
		}
		if err := setRow(book, "Candidates", 1, i+2, values); err != nil {
			return nil, err
		}
	}

	attemptHeaders := []string{
		"Attempt ID", "Kind", "Task ID", "Configuration ID", "Candidate ID",
		"Recipient", "Subject", "State", "Reason", "Started at", "Finished at",
		"Retry count", "Retry at", "Content hash", "Resolution basis", "Resolved at",
	}
	if err := setRow(book, "Attempts", 1, 1, attemptHeaders); err != nil {
		return nil, err
	}
	for i, attempt := range snapshot.Attempts {
		var basis, resolvedAt string
		if resolution := snapshot.Resolutions[attempt.GetString("member")]; resolution != nil {
			basis = resolution.GetString("basis")
			resolvedAt = sending.DisplayTime(resolution.GetDateTime("resolved_at").Time().UTC())
		}
		values := []string{
			attempt.Id,
			attempt.GetString("kind"),
			attempt.GetString("task"),
			attempt.GetString("configuration"),
			attempt.GetString("candidate"),
			attempt.GetString("recipient"),
			attempt.GetString("subject"),
			attempt.GetString("state"),
			attempt.GetString("reason"),
			sending.DisplayTime(attempt.GetDateTime("started_at").Time().UTC()),
			sending.DisplayTime(attempt.GetDateTime("finished_at").Time().UTC()),
			fmt.Sprintf("%d", attempt.GetInt("retry_count")),
			sending.DisplayTime(attempt.GetDateTime("retry_at").Time().UTC()),
			attempt.GetString("content_hash"),
			basis,
			resolvedAt,
		}
		if err := setRow(book, "Attempts", 1, i+2, values); err != nil {
			return nil, err
		}
	}

	resolutionHeaders := []string{"Member ID", "Actor", "Basis", "Resolved at", "Action ID"}
	if err := setRow(book, "Resolutions", 1, 1, resolutionHeaders); err != nil {
		return nil, err
	}
	row := 2
	for _, resolution := range snapshot.Resolutions {
		values := []string{
			resolution.GetString("member"),
			resolution.GetString("actor"),
			resolution.GetString("basis"),
			sending.DisplayTime(resolution.GetDateTime("resolved_at").Time().UTC()),
			resolution.GetString("action_id"),
		}
		if err := setRow(book, "Resolutions", 1, row, values); err != nil {
			return nil, err
		}
		row++
	}

	var out bytes.Buffer
	if err := book.Write(&out); err != nil {
		return nil, fmt.Errorf("write the campaign report: %w", err)
	}
	return out.Bytes(), nil
}

// setRow writes plain strings with the string API so a value that begins with
// a formula prefix stays inert text.
// campaignState maps a stored candidate to its current campaign state without
// creating any new records.
func campaignState(candidate *core.Record) string {
	switch candidate.GetString("submission_state") {
	case intake.SubmissionSubmitted:
		return "submitted"
	case intake.SubmissionUncertain:
		return "uncertain"
	default:
		if candidate.GetString("claim_task") != "" {
			return "claimed"
		}
		return "free"
	}
}

func setRow(book *excelize.File, sheet string, startColumn, row int, values []string) error {
	for offset, value := range values {
		cell, err := excelize.CoordinatesToCellName(startColumn+offset, row)
		if err != nil {
			return err
		}
		if err := book.SetCellStr(sheet, cell, value); err != nil {
			return err
		}
	}
	return nil
}
