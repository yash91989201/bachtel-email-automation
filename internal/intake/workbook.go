package intake

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Workbook is one opened XLSX upload: the worksheet inventory, the selected
// worksheet and header row, and the data rows below it.
type Workbook struct {
	file   *excelize.File
	digest string
	limits Limits

	Sheets    []SheetInfo
	Sheet     string
	HeaderRow int
	Headers   []Header
	Rows      []Row
}

// WorkbookDigest returns the SHA-256 hex digest of the uploaded workbook bytes.
// The same digest computed over the stored source bytes proves the stored copy
// is unchanged.
func WorkbookDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// OpenWorkbook validates and opens one uploaded workbook. Every failure is
// mapped to a fixed intake sentinel, so neither a filesystem path nor a raw
// reader diagnostic can reach an operator; the original cause stays in the
// message for logs.
func OpenWorkbook(data []byte, limits Limits) (*Workbook, error) {
	normalized, err := NormalizeLimits(limits)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || int64(len(data)) > normalized.MaxUploadBytes {
		return nil, ErrUploadTooLarge
	}
	if err := checkExpandedSize(data, normalized.UnzipSizeLimit); err != nil {
		return nil, err
	}
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit:    normalized.UnzipSizeLimit,
		UnzipXMLSizeLimit: normalized.UnzipXMLSizeLimit,
	})
	if err != nil {
		switch {
		case errors.Is(err, excelize.ErrWorkbookFileFormat):
			// Legacy .xls and password-protected workbooks both fail here;
			// both need a re-export as an unprotected .xlsx, not a repair.
			return nil, fmt.Errorf("%w: %v", ErrUnsupportedWorkbook, err)
		case errors.Is(err, excelize.ErrOptionsUnzipSizeLimit):
			return nil, fmt.Errorf("%w: %v", ErrExpandedTooLarge, err)
		default:
			return nil, fmt.Errorf("%w: %v", ErrCorruptWorkbook, err)
		}
	}

	w := &Workbook{file: file, digest: WorkbookDigest(data), limits: normalized}
	names := file.GetSheetList()
	if len(names) == 0 {
		return nil, errors.Join(ErrCorruptWorkbook, file.Close())
	}
	w.Sheets = make([]SheetInfo, 0, len(names))
	for _, name := range names {
		visible, err := file.GetSheetVisible(name)
		if err != nil {
			// A sheet whose visibility cannot be read must not become the
			// default selection by accident.
			visible = false
		}
		w.Sheets = append(w.Sheets, SheetInfo{Name: name, Visible: visible})
	}
	defaultSheet := w.Sheets[0].Name
	for _, sheet := range w.Sheets {
		if sheet.Visible {
			defaultSheet = sheet.Name
			break
		}
	}
	// The default selection is a convenience, not a promise: a workbook whose
	// first visible worksheet cannot be parsed is still a usable upload,
	// because the operator picks the worksheet and the header row next. Only a
	// workbook that cannot be read at all fails here. The default is recorded
	// either way so the selection form starts from it.
	w.Sheet, w.HeaderRow = defaultSheet, HeaderRowDefault
	if err := w.Select(defaultSheet, HeaderRowDefault); err != nil && !selectionFailure(err) {
		return nil, errors.Join(err, w.Close())
	}
	return w, nil
}

// selectionFailure reports whether a failed selection is an unusable choice
// rather than a broken workbook. The same problem is reported again when the
// choice is made explicitly, which is where an operator can act on it.
func selectionFailure(err error) bool {
	for _, sentinel := range []error{ErrUnknownSheet, ErrInvalidHeaderRow, ErrTooManyRows, ErrWorksheetTooLarge} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// checkExpandedSize rejects an archive whose members declare more expanded
// bytes than the limit allows. The bound is enforced here with the stdlib zip
// reader because excelize reports its own expansion failure as a plain error,
// which could not be told apart from a corrupt file.
func checkExpandedSize(data []byte, limit int64) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		// An archive whose central directory cannot even be read is corrupt,
		// not oversized: the workbook reader classifies it.
		return nil
	}
	max, total := uint64(limit), uint64(0)
	for _, entry := range reader.File {
		size := entry.UncompressedSize64
		if size > max || total > max-size {
			return ErrExpandedTooLarge
		}
		total += size
	}
	return nil
}

// SheetNames lists the worksheet names in workbook order.
func (w *Workbook) SheetNames() []string {
	names := make([]string, 0, len(w.Sheets))
	for _, sheet := range w.Sheets {
		names = append(names, sheet.Name)
	}
	return names
}

// HeaderLabels returns the trimmed header labels of the current selection in
// column order.
func (w *Workbook) HeaderLabels() []string {
	labels := make([]string, 0, len(w.Headers))
	for _, header := range w.Headers {
		labels = append(labels, header.Label)
	}
	return labels
}

// Select validates and parses one worksheet/header-row choice. The worksheet is
// streamed against the supported bounds before it is materialized, so a
// worksheet outside the bounds is rejected without ever being read into
// memory, and every call rebuilds the selection so a repeated choice can
// accumulate nothing from the previous one.
func (w *Workbook) Select(sheet string, headerRow int) error {
	if sheet == "" || !w.knownSheet(sheet) {
		return ErrUnknownSheet
	}
	if err := w.checkSheetBounds(sheet); err != nil {
		return err
	}
	if headerRow < 1 {
		return ErrInvalidHeaderRow
	}
	grid, err := w.file.GetRows(sheet)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptWorkbook, err)
	}
	if len(grid) > MaxWorksheetRows {
		return ErrWorksheetTooLarge
	}
	if headerRow > len(grid) {
		return ErrInvalidHeaderRow
	}
	cells := grid[headerRow-1]
	if len(cells) == 0 {
		return ErrInvalidHeaderRow
	}
	headers := make([]Header, len(cells))
	for i, cell := range cells {
		headers[i] = Header{Index: i, Label: strings.TrimSpace(cell)}
	}

	var rows []Row
	for i := headerRow; i < len(grid); i++ {
		if !hasHeaderValue(grid[i], headers) {
			// A row without any value in the table's columns is not a row of
			// the table: blank separator rows never become candidates, and a
			// stray value outside the table does not make a row part of it.
			continue
		}
		if len(rows) >= MaxCandidateRows {
			return ErrTooManyRows
		}
		// Values keep the original cell text verbatim: downstream validation
		// normalizes them, and duplicate detection trims for its own key.
		rows = append(rows, Row{Number: i + 1, Values: grid[i]})
	}
	w.Sheet, w.HeaderRow, w.Headers, w.Rows = sheet, headerRow, headers, rows
	return nil
}

// knownSheet reports whether the worksheet is part of the inventory. The
// comparison is exact and case-sensitive: worksheet names are.
func (w *Workbook) knownSheet(sheet string) bool {
	for _, known := range w.Sheets {
		if known.Name == sheet {
			return true
		}
	}
	return false
}

// checkSheetBounds streams one worksheet once and rejects it as soon as it
// exceeds a supported row or column bound. This pass is the memory guard: it
// must run before GetRows materializes the sheet.
func (w *Workbook) checkSheetBounds(sheet string) error {
	rows, err := w.file.Rows(sheet)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptWorkbook, err)
	}
	var (
		count     int
		streamErr error
		tooLarge  bool
	)
	for rows.Next() {
		count++
		cells, err := rows.Columns()
		if err != nil {
			streamErr = err
			break
		}
		if len(cells) > MaxWorksheetColumns || count > MaxWorksheetRows {
			tooLarge = true
			break
		}
	}
	if streamErr == nil {
		streamErr = rows.Error()
	}
	closeErr := rows.Close()
	switch {
	case tooLarge:
		return ErrWorksheetTooLarge
	case streamErr != nil:
		return fmt.Errorf("%w: %v", ErrCorruptWorkbook, streamErr)
	case closeErr != nil:
		return fmt.Errorf("%w: %v", ErrCorruptWorkbook, closeErr)
	}
	return nil
}

// hasHeaderValue reports whether the row carries any non-blank value in the
// table's header columns. Whitespace-only cells count as blank.
func hasHeaderValue(cells []string, headers []Header) bool {
	for _, header := range headers {
		if header.Index < len(cells) && strings.TrimSpace(cells[header.Index]) != "" {
			return true
		}
	}
	return false
}

// Close releases the opened workbook.
func (w *Workbook) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

// ExclusionReport builds the XLSX exclusion report: one worksheet named
// Excluded with a fixed two-column prefix (the original worksheet row and the
// joined reasons) followed by one column per selected header, so the report
// lines up with the workbook the operator uploaded. Rows with no recorded
// values are padded, and a report without excluded rows still carries its
// header.
func ExclusionReport(headers []string, rows []ReportRow) (data []byte, err error) {
	const sheet = "Excluded"
	file := excelize.NewFile()
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.SetSheetName("Sheet1", sheet); err != nil {
		return nil, fmt.Errorf("build exclusion report: %w", err)
	}

	header := make([]string, 0, len(headers)+2)
	header = append(header, "Row Number", "Reason")
	for i, label := range headers {
		label = strings.TrimSpace(label)
		if label == "" {
			// An unnamed column still needs a stable, unambiguous name.
			label = "Column " + strconv.Itoa(i+1)
		}
		header = append(header, label)
	}
	if err := file.SetSheetRow(sheet, "A1", &header); err != nil {
		return nil, fmt.Errorf("build exclusion report: %w", err)
	}

	for i, row := range rows {
		cells := make([]string, 0, len(headers)+2)
		cells = append(cells, strconv.Itoa(row.Number), strings.Join(row.Reasons, "; "))
		for col := range headers {
			value := ""
			if col < len(row.Values) {
				value = row.Values[col]
			}
			cells = append(cells, value)
		}
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return nil, fmt.Errorf("build exclusion report: %w", err)
		}
		if err := file.SetSheetRow(sheet, cell, &cells); err != nil {
			return nil, fmt.Errorf("build exclusion report: %w", err)
		}
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("build exclusion report: %w", err)
	}
	return buffer.Bytes(), nil
}
