package finalization

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// SanitizationSheetName is the worksheet of the prepared sanitization report.
const SanitizationSheetName = "Sanitization"

// sanitizationMetadata are the provenance and outcome columns of a prepared
// sanitization report. They are resolved against the mapping labels so no
// column is ever duplicated.
var sanitizationMetadata = []string{
	"Source row",
	"Position",
	"Prepared recipient",
	"Outcome",
	"Exclusion reasons",
	"AI corrections",
	"AI findings",
}

// occurrence is one prepared recipient entry of the selected run with its
// backend outcome.
type occurrence struct {
	sourceRow int
	position  int
	recipient string
	outcome   string
	reasons   []Reason
	values    map[string]string
}

// BuildSanitizationReport writes the protected per-occurrence sanitization
// workbook. Every prepared recipient entry appears once with its source row,
// position, prepared recipient, backend outcome, every exclusion reason, its
// source row's corrections and applicable findings, and each mapped value as an
// original and prepared pair. Values are written as inert text.
func BuildSanitizationReport(configuration *campaign.Configuration, review *Review) (data []byte, err error) {
	file := excelize.NewFile()
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.SetSheetName("Sheet1", SanitizationSheetName); err != nil {
		return nil, fmt.Errorf("build the sanitization report: %w", err)
	}
	labels := campaign.MappingLabels(configuration.Mappings)
	mappingColumns := make([]string, 0, len(labels)*2)
	for _, label := range labels {
		mappingColumns = append(mappingColumns, label+" (original)", label+" (prepared)")
	}
	metadata := resolveColumns(sanitizationMetadata, mappingColumns)
	header := make([]string, 0, len(metadata)+len(mappingColumns))
	header = append(header, metadata...)
	header = append(header, mappingColumns...)
	if err := writeTextRow(file, SanitizationSheetName, 1, header); err != nil {
		return nil, err
	}
	rows := make(map[int]preparation.RowResult, len(review.Rows))
	for _, row := range review.Rows {
		rows[row.SourceRow] = row
	}
	for index, item := range sanitizationOccurrences(review) {
		row := rows[item.sourceRow]
		cells := []string{
			strconv.Itoa(item.sourceRow),
			strconv.Itoa(item.position),
			item.recipient,
			item.outcome,
			sanitizationReasons(item.reasons),
			sanitizationCorrections(row.Corrections),
			sanitizationFindings(row.Findings, item.position),
		}
		original := review.Original[item.sourceRow]
		for _, mapping := range configuration.Mappings {
			cells = append(cells, original[mapping.Variable], item.values[mapping.Variable])
		}
		if err := writeTextRow(file, SanitizationSheetName, index+2, cells); err != nil {
			return nil, err
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("build the sanitization report: %w", err)
	}
	return buffer.Bytes(), nil
}

// SanitizationReport builds the sanitization workbook of the task's selected
// completed run. It reads stored evidence only: no AI call, no value rewrite and
// no stored record.
func (s *Service) SanitizationReport(ctx context.Context, ownerID, taskID string) ([]byte, string, error) {
	task, err := s.tasks.LoadTask(ctx, ownerID, taskID)
	if err != nil {
		return nil, "", err
	}
	if task.SelectedPreparationRunID == "" {
		return nil, "", ErrNoPrepared
	}
	review, err := s.ReviewPrepared(ctx, ownerID, taskID, task.SelectedPreparationRunID)
	if err != nil {
		return nil, "", err
	}
	configuration, err := s.configuration(ctx, ownerID, task)
	if err != nil {
		return nil, "", err
	}
	data, err := BuildSanitizationReport(configuration, review)
	if err != nil {
		return nil, "", err
	}
	return data, "prepared-sanitization-" + shortID(taskID) + ".xlsx", nil
}

// sanitizationOccurrences lists every prepared recipient entry, accepted and
// held, in source-row and position order.
func sanitizationOccurrences(review *Review) []occurrence {
	items := make([]occurrence, 0, len(review.Entries)+len(review.Exclusions))
	for _, entry := range review.Entries {
		items = append(items, occurrence{
			sourceRow: entry.SourceRow,
			position:  entry.Position,
			recipient: entry.Recipient,
			outcome:   preparation.OutcomeAccepted,
			values:    entry.Values,
		})
	}
	for _, exclusion := range review.Exclusions {
		items = append(items, occurrence{
			sourceRow: exclusion.SourceRow,
			position:  exclusion.Position,
			recipient: exclusion.Recipient,
			outcome:   preparation.OutcomeHeld,
			reasons:   exclusion.Reasons,
			values:    exclusion.Proposed,
		})
	}
	slices.SortFunc(items, func(a, b occurrence) int {
		return cmp.Or(cmp.Compare(a.sourceRow, b.sourceRow), cmp.Compare(a.position, b.position))
	})
	return items
}

// sanitizationReasons renders every exclusion reason with its source.
func sanitizationReasons(reasons []Reason) string {
	texts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		text := reason.Source + ": " + reason.Text
		if len(reason.Fields) > 0 {
			text += " (" + strings.Join(reason.Fields, ", ") + ")"
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, "; ")
}

// sanitizationCorrections lists the source row's resolved corrections. A
// correction never holds an entry, so it stays separate from the findings.
func sanitizationCorrections(corrections []preparation.Correction) string {
	texts := make([]string, 0, len(corrections))
	for _, correction := range corrections {
		header := correction.Header
		if header == "" {
			header = correction.Field
		}
		texts = append(texts, header+": "+correction.Note)
	}
	return strings.Join(texts, "; ")
}

// sanitizationFindings lists the unresolved findings that apply to one
// occurrence: row-wide findings on every sibling and a recipient-scoped finding
// on its address only.
func sanitizationFindings(findings []preparation.Finding, position int) string {
	texts := make([]string, 0, len(findings))
	for _, finding := range findings {
		if finding.Scope == preparation.ScopeRecipient && finding.Position != position {
			continue
		}
		texts = append(texts, preparation.FindingReason(finding))
	}
	return strings.Join(texts, "; ")
}
