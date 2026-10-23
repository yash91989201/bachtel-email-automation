package finalization

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// inputRow is one worksheet row of a finalization candidate set: its final and
// original coordinates, the values the operator is about to accept and the
// unresolved AI findings that apply to it.
type inputRow struct {
	FinalRow  int
	SourceRow int
	Values    map[string]string
	Original  map[string]string
	Shared    []Reason
	Recipient map[int][]Reason
}

// accepted is one classified entry before candidate identity is resolved.
type accepted struct {
	entry Entry
}

// classification is the complete deterministic outcome of one input set. It
// contains no database mutation.
type classification struct {
	accepted   []accepted
	exclusions []Exclusion
	counts     Counts
}

// classify expands the complete recipient input before any candidate is
// touched, then applies every applicable check to every entry. Duplicate
// detection runs over the whole expanded input, so no preferred occurrence can
// be admitted while its sibling is held. AI findings are combined with
// deterministic backend reasons; every reason is retained.
func (s *Service) classify(ctx context.Context, ownerID string, configuration *campaign.Configuration, taskID string, rows []inputRow) (classification, error) {
	result := classification{accepted: []accepted{}, exclusions: []Exclusion{}}
	if configuration == nil {
		return result, ErrNotReady
	}
	recipientVariable := ""
	for _, mapping := range configuration.Mappings {
		if mapping.Recipient {
			recipientVariable = mapping.Variable
		}
	}
	if recipientVariable == "" {
		return result, &StructuralError{Message: "the pinned campaign revision has no recipient mapping; create a corrected revision and revalidate the task"}
	}
	candidates, err := s.campaignCandidates(ctx, ownerID, configuration.CampaignID)
	if err != nil {
		return result, err
	}

	type planned struct {
		finalRow  int
		sourceRow int
		position  int
		token     string
		key       string
		values    map[string]string
		original  map[string]string
		shared    []Reason
		perEntry  []Reason
	}
	plannedEntries := make([]planned, 0, len(rows))
	sourceRows := make(map[int]struct{}, len(rows))
	duplicates := make(map[string]int, len(rows))
	for _, row := range rows {
		sourceRow := row.SourceRow
		sourceRows[row.FinalRow] = struct{}{}
		cell := row.Values[recipientVariable]
		for index, token := range strings.Split(cell, ",") {
			if len(plannedEntries) >= intake.MaxRecipientEntries {
				return result, ErrTooManyEntries
			}
			position := index + 1
			key := intake.RecipientKey(token)
			if key != "" {
				duplicates[key]++
			}
			values := maps.Clone(row.Values)
			if values == nil {
				values = map[string]string{}
			}
			values[recipientVariable] = strings.TrimSpace(token)
			plannedEntries = append(plannedEntries, planned{
				finalRow:  row.FinalRow,
				sourceRow: sourceRow,
				position:  position,
				token:     token,
				key:       key,
				values:    values,
				original:  row.Original,
				shared:    row.Shared,
				perEntry:  row.Recipient[position],
			})
		}
	}
	result.counts.Entries = len(plannedEntries)
	result.counts.SourceRows = len(sourceRows)

	for _, item := range plannedEntries {
		reasons := append([]Reason(nil), item.shared...)
		reasons = append(reasons, item.perEntry...)

		address, addressErr := campaign.NormalizeRecipient(item.token)
		switch {
		case item.key != "" && duplicates[item.key] > 1:
			reasons = append(reasons, Reason{
				Source: SourceBackend,
				Code:   ReasonDuplicate,
				Text:   "duplicate recipient address in this dataset; every occurrence of the group is held",
			})
			result.counts.Duplicate++
		case addressErr != nil:
			reasons = append(reasons, Reason{
				Source: SourceBackend,
				Code:   ReasonInvalidAddress,
				Text:   "the recipient entry is empty or is not a single usable email address",
			})
			result.counts.Invalid++
		}

		resolved, missing := campaign.ResolveValues(configuration.MappingVersion, configuration.Mappings, item.values)
		if addressErr == nil {
			resolved[recipientVariable] = address
		}
		if len(missing) > 0 {
			reasons = append(reasons, Reason{
				Source: SourceBackend,
				Code:   ReasonMissingRequired,
				Text:   (&campaign.MissingRequiredError{Variables: missing}).Error(),
				Fields: missing,
			})
			result.counts.MissingValue++
		}
		if addressErr == nil && len(missing) == 0 {
			if _, renderErr := configuration.Compiled.Render(resolved); renderErr != nil {
				reasons = append(reasons, Reason{
					Source: SourceBackend,
					Code:   ReasonSafety,
					Text:   safetyText(renderErr),
				})
				result.counts.Safety++
			}
		}
		if addressErr == nil {
			if reason, held := candidateHistory(candidates[item.key], taskID); held {
				reasons = append(reasons, reason)
				result.counts.History++
			}
		}
		for _, reason := range reasons {
			if reason.Source == SourceAI {
				result.counts.AIHeld++
				break
			}
		}

		if len(reasons) == 0 {
			result.accepted = append(result.accepted, accepted{entry: Entry{
				FinalRow:  item.finalRow,
				Position:  item.position,
				SourceRow: item.sourceRow,
				Recipient: address,
				Values:    resolved,
			}})
			continue
		}
		original := maps.Clone(item.original)
		if original == nil {
			original = maps.Clone(item.values)
		}
		result.exclusions = append(result.exclusions, Exclusion{
			FinalRow:  item.finalRow,
			Position:  item.position,
			SourceRow: item.sourceRow,
			Recipient: strings.TrimSpace(item.token),
			Original:  original,
			Proposed:  resolved,
			Reasons:   reasons,
		})
	}
	result.counts.Accepted = len(result.accepted)
	result.counts.Excluded = len(result.exclusions)
	return result, nil
}

// safetyText maps one render rejection to fixed operator-facing text. The
// underlying error may name internal template detail, so only its category is
// shown.
func safetyText(err error) string {
	var missing *campaign.MissingRequiredError
	var control *campaign.SubjectControlError
	switch {
	case errors.As(err, &missing):
		return "rendering would leave a required value empty"
	case errors.As(err, &control):
		return "the rendered subject would contain control characters or line breaks"
	default:
		return "the accepted values cannot be rendered safely as this revision's email"
	}
}

// candidateHistory maps a recipient key to its campaign-scoped candidate. A
// candidate without history and without another task's claim is free.
func (s *Service) campaignCandidates(ctx context.Context, ownerID, campaignID string) (map[string]*intake.Candidate, error) {
	campaignRecord, err := s.app.FindFirstRecordByFilter(
		campaign.CampaignsCollection,
		"id = {:id} && owner = {:owner}",
		dbx.Params{"id": campaignID, "owner": ownerID},
	)
	if err != nil {
		return nil, ErrNotFound
	}
	_ = campaignRecord
	records, err := s.app.FindRecordsByFilter(
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
	candidates := make(map[string]*intake.Candidate, len(records))
	for _, record := range records {
		candidate := &intake.Candidate{
			ID:              record.Id,
			CampaignID:      record.GetString("campaign"),
			EmailKey:        record.GetString("email_key"),
			SubmissionState: record.GetString("submission_state"),
			ClaimTaskID:     record.GetString("claim_task"),
		}
		candidates[candidate.EmailKey] = candidate
	}
	return candidates, nil
}

// candidateHistory reports the campaign-history reason that holds one entry.
func candidateHistory(candidate *intake.Candidate, taskID string) (Reason, bool) {
	if candidate == nil {
		return Reason{}, false
	}
	switch {
	case candidate.SubmissionState == intake.SubmissionSubmitted:
		return Reason{Source: SourceBackend, Code: ReasonAlreadySent, Text: intake.ReasonText(intake.RowAlreadySubmitted)}, true
	case candidate.SubmissionState == intake.SubmissionUncertain:
		return Reason{Source: SourceBackend, Code: ReasonReviewRequired, Text: intake.ReasonText(intake.RowReviewRequired)}, true
	case candidate.ClaimTaskID != "" && candidate.ClaimTaskID != taskID:
		return Reason{Source: SourceBackend, Code: ReasonScheduled, Text: intake.ReasonText(intake.RowAlreadyScheduled)}, true
	default:
		return Reason{}, false
	}
}

// findingReasons splits one prepared row's findings into shared and
// per-recipient AI reasons.
func findingReasons(row preparation.RowResult) ([]Reason, map[int][]Reason) {
	shared := []Reason{}
	perRecipient := make(map[int][]Reason)
	for _, finding := range row.Findings {
		reason := Reason{
			Source: SourceAI,
			Code:   "preparation_finding",
			Text:   preparation.FindingReason(finding),
			Fields: []string{finding.Field},
		}
		if finding.Scope == preparation.ScopeRecipient {
			perRecipient[finding.Position] = append(perRecipient[finding.Position], reason)
			continue
		}
		shared = append(shared, reason)
	}
	return shared, perRecipient
}

// originalValues maps the task's pre-finalization membership values by source
// occurrence, so an exclusion can show the value this finalization replaced.
func (s *Service) originalValues(ctx context.Context, taskID string) (map[intake.Occurrence]map[string]string, error) {
	records, err := s.app.FindRecordsByFilter(
		intake.TaskCandidatesCollection,
		"task = {:task}",
		"source_row,recipient_index",
		0,
		0,
		dbx.Params{"task": taskID},
	)
	if err != nil {
		return nil, err
	}
	original := make(map[intake.Occurrence]map[string]string, len(records))
	for _, record := range records {
		values := map[string]string{}
		if err := record.UnmarshalJSONField("raw", &values); err != nil {
			continue
		}
		original[intake.Occurrence{Row: record.GetInt("source_row"), Position: max(record.GetInt("recipient_index"), 1)}] = values
	}
	return original, nil
}
