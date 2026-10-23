package finalization

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

// SendingSheetName is the dedicated worksheet of a prepared or final sending
// workbook. It is the sheet the final-upload importer reads.
const SendingSheetName = "Sending"

// sheetRow is one recipient entry written to a sending sheet.
type sheetRow struct {
	Values map[string]string
}

// buildSendingSheet writes the accepted mapped values only: one row per
// recipient entry, the deterministic mapping labels as the header, and no
// status, exclusion or original-source columns. Values are written through the
// string cell API, so formula-like text stays inert.
func buildSendingSheet(configuration *campaign.Configuration, rows []sheetRow) (data []byte, err error) {
	labels := campaign.MappingLabels(configuration.Mappings)
	if len(labels) != len(configuration.Mappings) {
		return nil, errors.New("the pinned revision's mapping labels are inconsistent")
	}
	file := excelize.NewFile()
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.SetSheetName("Sheet1", SendingSheetName); err != nil {
		return nil, fmt.Errorf("build the sending sheet: %w", err)
	}
	for index, label := range labels {
		cell, err := excelize.CoordinatesToCellName(index+1, 1)
		if err != nil {
			return nil, err
		}
		if err := file.SetCellStr(SendingSheetName, cell, label); err != nil {
			return nil, err
		}
	}
	for rowIndex, row := range rows {
		for column, mapping := range configuration.Mappings {
			cell, err := excelize.CoordinatesToCellName(column+1, rowIndex+2)
			if err != nil {
				return nil, err
			}
			if err := file.SetCellStr(SendingSheetName, cell, row.Values[mapping.Variable]); err != nil {
				return nil, err
			}
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("build the sending sheet: %w", err)
	}
	return buffer.Bytes(), nil
}

// parseSendingSheet reads one operator-edited final workbook. Header labels are
// resolved through the same deterministic table the export used; a missing,
// duplicated or unexpected required label is a structural failure, and an
// extra column is ignored so it can never influence mapped values. No AI is
// involved and the uploaded content is preserved verbatim apart from the
// documented recipient trim/lowercase and optional fallback.
func parseSendingSheet(data []byte, configuration *campaign.Configuration) ([]inputRow, error) {
	if len(data) == 0 {
		return nil, &StructuralError{Message: "choose an edited final XLSX workbook to upload"}
	}
	limits := intake.DefaultLimits()
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    limits.UnzipSizeLimit,
		UnzipXMLSizeLimit: limits.UnzipXMLSizeLimit,
	})
	if err != nil {
		return nil, &StructuralError{Message: intake.ErrCorruptWorkbook.Error()}
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	sheet := ""
	for _, name := range file.GetSheetList() {
		if name == SendingSheetName {
			sheet = name
			break
		}
	}
	if sheet == "" {
		sheets := file.GetSheetList()
		if len(sheets) != 1 {
			return nil, &StructuralError{Message: fmt.Sprintf("the final workbook needs a %q worksheet; download the prepared or accepted final sheet and edit that file", SendingSheetName)}
		}
		sheet = sheets[0]
	}
	grid, err := file.GetRows(sheet)
	if err != nil {
		return nil, &StructuralError{Message: intake.ErrCorruptWorkbook.Error()}
	}
	if len(grid) == 0 {
		return nil, &StructuralError{Message: "the final workbook is empty; download the prepared or accepted final sheet and add entries"}
	}
	if len(grid) > intake.MaxWorksheetRows {
		return nil, &StructuralError{Message: intake.ErrWorksheetTooLarge.Error()}
	}
	header := grid[0]
	if len(header) > intake.MaxWorksheetColumns {
		return nil, &StructuralError{Message: intake.ErrWorksheetTooLarge.Error()}
	}

	labels := campaign.MappingLabels(configuration.Mappings)
	columns := make([]int, len(labels))
	seen := make(map[string]struct{}, len(header))
	for index, label := range header {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if _, repeated := seen[label]; repeated {
			return nil, &StructuralError{Message: fmt.Sprintf("the final workbook has more than one %q column; keep one column per mapped value and upload it again", label)}
		}
		seen[label] = struct{}{}
		if position, ok := campaign.MappingIndex(label, labels); ok {
			columns[position] = index + 1
		}
	}
	var missing []string
	for index, label := range labels {
		if columns[index] == 0 {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		return nil, &StructuralError{Message: "the final workbook is missing required mapped column(s): " + strings.Join(missing, ", ") + "; download the prepared or accepted final sheet and edit that file"}
	}

	rows := make([]inputRow, 0, len(grid)-1)
	for offset, cells := range grid[1:] {
		values := make(map[string]string, len(configuration.Mappings))
		blank := true
		for index, mapping := range configuration.Mappings {
			value := ""
			column := columns[index] - 1
			if column < len(cells) {
				value = cells[column]
			}
			values[mapping.Variable] = value
			if strings.TrimSpace(value) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		rows = append(rows, inputRow{
			FinalRow: offset + 2,
			Values:   values,
			Original: values,
		})
	}
	if len(rows) == 0 {
		return nil, &StructuralError{Message: "the final workbook has no mapped values; add at least one recipient row and upload it again"}
	}
	return rows, nil
}

// rowsFromPrepared converts one completed run's source rows into finalization
// input. Every occurrence is expanded exactly once by the shared validator,
// which then applies the run's unresolved findings together with backend
// checks.
func rowsFromPrepared(output *preparation.Output) []inputRow {
	rows := make([]inputRow, 0, len(output.Rows))
	for _, row := range output.Rows {
		shared, perRecipient := findingReasons(row)
		rows = append(rows, inputRow{
			FinalRow:  row.SourceRow,
			SourceRow: row.SourceRow,
			Values:    row.Values,
			Original:  row.Values,
			Shared:    shared,
			Recipient: perRecipient,
		})
	}
	return rows
}
