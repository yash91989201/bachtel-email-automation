package finalization

import (
	"context"
	"fmt"
	"maps"

	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// Review is the cleaned dataset after AI findings and backend eligibility have
// been combined. It is read-only evidence; candidate memberships are published
// only when the operator finalizes the dataset.
type Review struct {
	Counts     Counts
	Entries    []Entry
	Exclusions []Exclusion
}

// ReviewPrepared validates a completed run against its pinned task revision and
// current campaign history, without invoking AI or admitting candidates.
func (s *Service) ReviewPrepared(ctx context.Context, ownerID, taskID, runID string) (*Review, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	run, err := s.prep.LoadRun(ctx, ownerID, taskID, runID)
	if err != nil {
		return nil, err
	}
	if run.State != preparation.RunCompleted || run.Output == nil {
		return nil, ErrNoPrepared
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, err
	}
	if run.ConfigurationID != task.ConfigurationID || run.ContentHash != task.ContentHash || run.SourceDigest != task.SourceDigest {
		return nil, ErrStale
	}
	rows, err := s.preparedRows(ctx, ownerID, taskID, run.Output)
	if err != nil {
		return nil, err
	}
	classified, err := s.classify(ctx, ownerID, configuration, taskID, rows)
	if err != nil {
		return nil, err
	}
	review := &Review{Counts: classified.counts, Entries: make([]Entry, 0, len(classified.accepted)), Exclusions: classified.exclusions}
	for _, accepted := range classified.accepted {
		review.Entries = append(review.Entries, accepted.entry)
	}
	return review, nil
}

// preparedRows retains the uploaded cell values for exclusion provenance even
// when the model corrected them. Raw values never determine eligibility here.
func (s *Service) preparedRows(ctx context.Context, ownerID, taskID string, output *preparation.Output) ([]inputRow, error) {
	original, err := s.tasks.MappedRows(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	byRow := make(map[int]map[string]string, len(original))
	for _, row := range original {
		byRow[row.Number] = row.Values
	}
	rows := rowsFromPrepared(output)
	for index := range rows {
		rows[index].Original = maps.Clone(byRow[rows[index].SourceRow])
	}
	return rows, nil
}

// PreparedExclusionSheet reports only exclusions from cleaned prepared data.
func (s *Service) PreparedExclusionSheet(ctx context.Context, ownerID, taskID, runID string) ([]byte, string, error) {
	review, err := s.ReviewPrepared(ctx, ownerID, taskID, runID)
	if err != nil {
		return nil, "", err
	}
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, "", err
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, "", err
	}
	data, err := BuildExclusionReport(configuration, review.Exclusions)
	if err != nil {
		return nil, "", fmt.Errorf("build the prepared exclusions: %w", err)
	}
	return data, "prepared-exclusions-" + shortID(taskID) + ".xlsx", nil
}
