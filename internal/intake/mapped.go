package intake

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// MappedRow is one worksheet row's campaign-mapped cell values, keyed by
// mapping variable.
type MappedRow struct {
	// Number is the original 1-based worksheet row.
	Number int
	Values map[string]string
}

// MappedRows returns the mapped values of every non-blank source row of a
// validated task. It is the input boundary for AI preparation: only columns
// the campaign maps are read, the stored workbook is checked against its
// recorded digest, and a row whose mapped cells are all empty is not a row of
// the table and is omitted.
func (s *Service) MappedRows(ctx context.Context, ownerID, taskID string) ([]MappedRow, error) {
	record, err := s.ownedTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, err
	}
	task := taskFromRecord(record, "")
	if !task.Admitted() {
		return nil, &ValidationError{Message: "validate this task before preparing its data"}
	}
	if task.Sheet == "" || task.ConfigurationID == "" {
		return nil, &ValidationError{Message: "this task has no worksheet selection or content revision to prepare"}
	}
	configuration, err := s.campaigns.LoadConfiguration(ctx, ownerID, task.ConfigurationID)
	if err != nil {
		return nil, err
	}
	data, err := s.readArtifact(ctx, record, "source")
	if err != nil {
		return nil, fmt.Errorf("read the stored workbook: %w", err)
	}
	if WorkbookDigest(data) != task.SourceDigest {
		return nil, errors.New("the stored workbook no longer matches the digest recorded at upload; upload the workbook again")
	}
	workbook, err := OpenWorkbook(data, s.limits)
	if err != nil {
		return nil, workbookFailure(err)
	}
	defer func() {
		if err := workbook.Close(); err != nil {
			s.app.Logger().Warn("close intake workbook", "error", err)
		}
	}()
	if err := workbook.Select(task.Sheet, task.HeaderRow); err != nil {
		return nil, workbookFailure(err)
	}
	offsets := make(map[string]int, len(configuration.Mappings))
	for _, mapping := range configuration.Mappings {
		offset, ok := columnOffset(workbook.Headers, mapping.Header)
		if !ok {
			return nil, &ValidationError{Message: fmt.Sprintf("the workbook no longer has a single %q column in the selected header row", mapping.Header)}
		}
		offsets[mapping.Variable] = offset
	}
	rows := make([]MappedRow, 0, len(workbook.Rows))
	for _, row := range workbook.Rows {
		values := make(map[string]string, len(configuration.Mappings))
		blank := true
		for _, mapping := range configuration.Mappings {
			value := row.Column(offsets[mapping.Variable])
			values[mapping.Variable] = value
			if strings.TrimSpace(value) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		rows = append(rows, MappedRow{Number: row.Number, Values: values})
	}
	return rows, nil
}
