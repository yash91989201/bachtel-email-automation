package finalization

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
)

// exclusionMetadata are the provenance and reason columns of a final exclusion
// report. They are resolved against the mapping labels so no column is ever
// duplicated.
var exclusionMetadata = []string{
	"Final worksheet row",
	"Final recipient position",
	"Recipient",
	"Original source row",
	"Affected fields",
	"Reason source",
	"Reason",
}

// BuildExclusionReport writes the protected final-upload exclusion workbook.
// Every rejected entry names its final and original coordinates, the affected
// mapped fields, every reason with its source, and the original and proposed
// value of each mapping. Values are written as inert text.
func BuildExclusionReport(configuration *campaign.Configuration, exclusions []Exclusion) (data []byte, err error) {
	const sheet = "Excluded"
	file := excelize.NewFile()
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.SetSheetName("Sheet1", sheet); err != nil {
		return nil, fmt.Errorf("build the exclusion report: %w", err)
	}
	labels := campaign.MappingLabels(configuration.Mappings)
	mappingColumns := make([]string, 0, len(labels)*2)
	for _, label := range labels {
		mappingColumns = append(mappingColumns, label+" (original)", label+" (proposed)")
	}
	metadata := resolveColumns(exclusionMetadata, mappingColumns)
	header := make([]string, 0, len(metadata)+len(mappingColumns))
	header = append(header, metadata...)
	header = append(header, mappingColumns...)
	if err := writeTextRow(file, sheet, 1, header); err != nil {
		return nil, err
	}
	for index, exclusion := range exclusions {
		sources := make([]string, 0, len(exclusion.Reasons))
		reasons := make([]string, 0, len(exclusion.Reasons))
		fields := make([]string, 0, len(exclusion.Reasons))
		for _, reason := range exclusion.Reasons {
			sources = appendUnique(sources, reason.Source)
			reasons = append(reasons, reason.Source+": "+reason.Text)
			for _, field := range reason.Fields {
				fields = appendUnique(fields, field)
			}
		}
		cells := []string{
			strconv.Itoa(exclusion.FinalRow),
			strconv.Itoa(exclusion.Position),
			exclusion.Recipient,
			strconv.Itoa(exclusion.SourceRow),
			strings.Join(fields, ", "),
			strings.Join(sources, ", "),
			strings.Join(reasons, "; "),
		}
		for _, mapping := range configuration.Mappings {
			cells = append(cells,
				exclusion.Original[mapping.Variable],
				exclusion.Proposed[mapping.Variable],
			)
		}
		if err := writeTextRow(file, sheet, index+2, cells); err != nil {
			return nil, err
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("build the exclusion report: %w", err)
	}
	return buffer.Bytes(), nil
}

// resolveColumns returns collision-free column names in input order, using the
// documented numbered suffix.
func resolveColumns(labels, occupied []string) []string {
	taken := make(map[string]struct{}, len(labels)+len(occupied))
	for _, label := range occupied {
		taken[label] = struct{}{}
	}
	resolved := make([]string, 0, len(labels))
	for _, label := range labels {
		base := strings.TrimSpace(label)
		if base == "" {
			continue
		}
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, exists := taken[candidate]; !exists {
				break
			}
			candidate = fmt.Sprintf("%s (%d)", base, suffix)
		}
		taken[candidate] = struct{}{}
		resolved = append(resolved, candidate)
	}
	return resolved
}

func appendUnique(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// writeTextRow writes one report row with the string cell API so copied or
// proposed text that looks like a formula stays inert.
func writeTextRow(file *excelize.File, sheet string, row int, values []string) error {
	for index, value := range values {
		cell, err := excelize.CoordinatesToCellName(index+1, row)
		if err != nil {
			return err
		}
		if err := file.SetCellStr(sheet, cell, value); err != nil {
			return err
		}
	}
	return nil
}
