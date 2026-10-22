package sending

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
)

// TaskSummary is the batched campaign-list view of one task's sending facts.
// It carries counts and approval only: no member, attempt or artifact payload,
// and computing it writes nothing.
type TaskSummary struct {
	TaskID          string
	Status          string
	Reason          string
	Approved        bool
	RestoreReview   bool
	Eligible        int
	Remaining       int
	Submitted       int
	Failed          int
	RetryableFailed int
	Uncertain       int
	InFlight        int
	Reserved        int
	Invalid         int
	Duplicate       int
	Ineligible      int
}

// ApprovedFor reports whether the operator's current mailbox generation has an
// approval for this exact configuration content. It is the campaign-revision
// equivalent of the task-scoped approval lookup, so a campaign badge never
// implies approval for a different mailbox generation.
func (s *Store) ApprovedFor(_ context.Context, ownerID, configurationID, contentHash string) (bool, error) {
	if configurationID == "" || contentHash == "" {
		return false, nil
	}
	connection, err := s.currentConnection(ownerID)
	if errors.Is(err, mailbox.ErrNotConnected) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	record, err := s.app.FindFirstRecordByFilter(
		ApprovalsCollection,
		"configuration={:c} && content_hash={:h} && connection={:conn} && connection_generation={:g}",
		dbx.Params{
			"c":    configurationID,
			"h":    contentHash,
			"conn": connection.Id,
			"g":    connection.GetInt("generation"),
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return record != nil, nil
}

// CampaignSummaries answers one owner-scoped campaign's task sending facts with
// a bounded number of queries: the tasks, the current mailbox's approvals, the
// campaign candidates and the tasks' membership rows. It never renders a
// preview, generates an artifact or creates an attempt.
func (s *Store) CampaignSummaries(ctx context.Context, ownerID, campaignID string) (map[string]*TaskSummary, error) {
	if _, err := s.ownedCampaign(campaignID, ownerID); err != nil {
		return nil, err
	}
	tasks, err := s.app.FindRecordsByFilter(
		intake.TasksCollection,
		"campaign = {:campaign}",
		"created",
		0,
		0,
		dbx.Params{"campaign": campaignID},
	)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return map[string]*TaskSummary{}, nil
	}
	approved := s.approvedScopes(ownerID)
	candidates, err := s.app.FindRecordsByFilter(
		intake.CandidatesCollection,
		"campaign = {:campaign}",
		"",
		0,
		0,
		dbx.Params{"campaign": campaignID},
	)
	if err != nil {
		return nil, err
	}
	candidateByID := make(map[string]*core.Record, len(candidates))
	for _, candidate := range candidates {
		candidateByID[candidate.Id] = candidate
	}
	summaryByTask := make(map[string]*TaskSummary, len(tasks))
	for _, task := range tasks {
		summary := &TaskSummary{
			TaskID:        task.Id,
			Status:        task.GetString("status"),
			Reason:        task.GetString("pause_reason"),
			RestoreReview: s.options.RestoreReview,
		}
		summary.Approved = approved[approvalScope(task.GetString("configuration"), task.GetString("content_hash"))]
		var counts intake.Counts
		if err := task.UnmarshalJSONField("counts", &counts); err == nil {
			summary.Invalid = counts.Invalid
			summary.Duplicate = counts.Duplicate
			summary.Ineligible = counts.Ineligible
		}
		summaryByTask[task.Id] = summary
	}
	for start := 0; start < len(tasks); start += 50 {
		end := min(start+50, len(tasks))
		params := dbx.Params{}
		clauses := make([]string, 0, end-start)
		for i, task := range tasks[start:end] {
			key := fmt.Sprintf("t%d", i)
			clauses = append(clauses, "task = {:"+key+"}")
			params[key] = task.Id
		}
		members, err := s.app.FindRecordsByFilter(
			intake.TaskCandidatesCollection,
			strings.Join(clauses, " || "),
			"",
			0,
			0,
			params,
		)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			s.countSummaryMember(summaryByTask[member.GetString("task")], member, candidateByID)
		}
	}
	return summaryByTask, nil
}

// countSummaryMember mirrors the Preview counters for one membership row
// without loading attempts, resolutions or rendered content.
func (s *Store) countSummaryMember(summary *TaskSummary, member *core.Record, candidateByID map[string]*core.Record) {
	if summary == nil {
		return
	}
	candidate := candidateByID[member.GetString("candidate")]
	taskID := summary.TaskID
	if member.GetString("state") != intake.RowEligible {
		if candidate == nil {
			return
		}
		switch candidate.GetString("submission_state") {
		case intake.SubmissionSubmitted:
			summary.Submitted++
		case intake.SubmissionUncertain:
			summary.Uncertain++
		default:
			if candidate.GetString("claim_task") != "" {
				summary.Reserved++
			}
		}
		return
	}
	if candidate == nil {
		return
	}
	switch {
	case candidate.GetString("submission_state") == intake.SubmissionSubmitted:
		summary.Submitted++
	case candidate.GetString("submission_state") == intake.SubmissionUncertain:
		summary.Uncertain++
	case member.GetString("dispatch_state") == AttemptInFlight:
		summary.InFlight++
	case member.GetString("dispatch_state") == "reserved":
		summary.Reserved++
		summary.Remaining++
	case member.GetString("dispatch_state") == AttemptFailed || member.GetString("dispatch_state") == AttemptAuth || member.GetString("dispatch_state") == "correction_required":
		summary.Failed++
		if member.GetString("dispatch_state") != "correction_required" {
			summary.RetryableFailed++
		}
	case candidate.GetString("claim_task") == "" || candidate.GetString("claim_task") == taskID:
		summary.Eligible++
		summary.Remaining++
	default:
		summary.Reserved++
	}
}

// ownedCampaign loads a campaign only when it belongs to the operator.
func (s *Store) ownedCampaign(campaignID, ownerID string) (*core.Record, error) {
	record, err := s.app.FindRecordById(campaign.CampaignsCollection, campaignID)
	if err != nil {
		return nil, ErrNotFound
	}
	if record.GetString("owner") != ownerID {
		return nil, ErrNotFound
	}
	return record, nil
}

// approvalScope keys an approval question by its configuration and content
// hash.
func approvalScope(configurationID, contentHash string) string {
	return configurationID + "\x00" + contentHash
}

// approvedScopes returns every approval the operator's current mailbox
// generation holds, in one query. No mailbox means no approved scope.
func (s *Store) approvedScopes(ownerID string) map[string]bool {
	scopes := map[string]bool{}
	connection, err := s.currentConnection(ownerID)
	if err != nil {
		return scopes
	}
	records, err := s.app.FindRecordsByFilter(
		ApprovalsCollection,
		"connection={:conn} && connection_generation={:g}",
		"",
		0,
		0,
		dbx.Params{"conn": connection.Id, "g": connection.GetInt("generation")},
	)
	if err != nil {
		return scopes
	}
	for _, record := range records {
		scopes[approvalScope(record.GetString("configuration"), record.GetString("content_hash"))] = true
	}
	return scopes
}
