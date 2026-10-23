// Package intake_test exercises the workbook reader: the upload bounds, the
// worksheet inventory, header-row selection and the exclusion report, all
// against XLSX fixtures built in process.
package intake_test

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// makeXLSX runs fill over a fresh workbook and returns its serialized bytes.
func makeXLSX(t *testing.T, fill func(f *excelize.File)) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close fixture workbook: %v", err)
		}
	}()
	if fill != nil {
		fill(file)
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// writeRows writes rows starting at worksheet row startRow; an unwritten row
// number in between stays a blank worksheet row.
func writeRows(t *testing.T, file *excelize.File, sheet string, startRow int, rows [][]string) {
	t.Helper()
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, startRow+i)
		if err != nil {
			t.Fatal(err)
		}
		values := row
		if err := file.SetSheetRow(sheet, cell, &values); err != nil {
			t.Fatal(err)
		}
	}
}

// mustOpen opens a fixture and closes it when the test finishes.
func mustOpen(t *testing.T, data []byte, limits intake.Limits) *intake.Workbook {
	t.Helper()
	book, err := intake.OpenWorkbook(data, limits)
	if err != nil {
		t.Fatalf("OpenWorkbook: %v", err)
	}
	t.Cleanup(func() {
		if err := book.Close(); err != nil {
			t.Errorf("close workbook: %v", err)
		}
	})
	return book
}

// openInventory opens a fixture and returns the workbook that staging stores.
// An unusable default selection is deliberately not an upload failure: the
// operator confirms the worksheet and header row next.
func openInventory(t *testing.T, data []byte) *intake.Workbook {
	t.Helper()
	book := mustOpen(t, data, intake.DefaultLimits())
	if book.Sheet == "" {
		t.Fatal("the default selection was not recorded")
	}
	return book
}

// rowsEqual compares a read-back row against the wanted cells, treating
// missing trailing cells as empty: the reader drops blank cells from the tail
// of a row.
func rowsEqual(got, want []string) bool {
	for len(got) < len(want) {
		got = append(got, "")
	}
	return reflect.DeepEqual(got, want)
}

func TestWorkbookDigest(t *testing.T) {
	t.Parallel()

	// Known SHA-256 of "abc".
	const abc = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := intake.WorkbookDigest([]byte("abc")); got != abc {
		t.Fatalf("WorkbookDigest(abc) = %q, want %q", got, abc)
	}

	data := makeXLSX(t, func(f *excelize.File) {
		writeRows(t, f, "Sheet1", 1, [][]string{{"Email"}, {"a@example.com"}})
	})
	digest := intake.WorkbookDigest(data)
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		t.Fatalf("digest = %q, want 64 lowercase hex characters", digest)
	}
	// The same digest over a copy of the stored bytes proves the stored
	// workbook is unchanged.
	if copyDigest := intake.WorkbookDigest(append([]byte(nil), data...)); copyDigest != digest {
		t.Fatalf("digest of a copy = %q, want %q", copyDigest, digest)
	}
	if mutated := intake.WorkbookDigest(append(data, 'x')); mutated == digest {
		t.Fatal("mutated bytes keep the original digest")
	}
}

func TestOpenWorkbookInventoryAndDefaultSelection(t *testing.T) {
	t.Parallel()
	data := makeXLSX(t, func(f *excelize.File) {
		if _, err := f.NewSheet("Data"); err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetName("Sheet1", "Hidden"); err != nil {
			t.Fatal(err)
		}
		writeRows(t, f, "Hidden", 1, [][]string{{"Secret"}})
		writeRows(t, f, "Data", 1, [][]string{{" Email "}, {"a@example.com"}})
		// The first worksheet of a generated workbook is tab-selected, and
		// Excelize only hides a worksheet that is not the selected tab.
		f.SetActiveSheet(1)
		if err := f.SetSheetVisible("Hidden", false); err != nil {
			t.Fatal(err)
		}
	})

	book := mustOpen(t, data, intake.DefaultLimits())

	if names := book.SheetNames(); !reflect.DeepEqual(names, []string{"Hidden", "Data"}) {
		t.Fatalf("SheetNames() = %v, want workbook order", names)
	}
	if book.Sheets[0].Visible || !book.Sheets[1].Visible {
		t.Fatalf("visibility inventory = %+v, want Hidden hidden, Data visible", book.Sheets)
	}
	if book.Sheet != "Data" || book.HeaderRow != 1 {
		t.Fatalf("default selection = %q row %d, want the first visible worksheet at row 1", book.Sheet, book.HeaderRow)
	}
	if labels := book.HeaderLabels(); !reflect.DeepEqual(labels, []string{"Email"}) {
		t.Fatalf("HeaderLabels() = %v, want trimmed labels", labels)
	}
	if len(book.Rows) != 1 || book.Rows[0].Number != 2 {
		t.Fatalf("rows = %+v, want the single data row at worksheet row 2", book.Rows)
	}
	if got := book.Rows[0].Column(0); got != "a@example.com" {
		t.Fatalf("first row address = %q", got)
	}
}

func TestSelectHeaderRowAndRows(t *testing.T) {
	t.Parallel()
	data := makeXLSX(t, func(f *excelize.File) {
		writeRows(t, f, "Sheet1", 1, [][]string{
			{"junk above"},
			{"more junk"},
			{" Email ", "", "City"},
			{"  a@example.com  ", "x", " Paris  "},
		})
		// Worksheet row 5 is left blank on purpose; rows 6 to 8 exercise
		// whitespace-only cells, a value outside the table and a short row.
		writeRows(t, f, "Sheet1", 6, [][]string{
			{"   ", "\t", "   "},
			{" ", "", "", "stray"},
			{"b@example.com"},
		})
	})

	book := mustOpen(t, data, intake.DefaultLimits())
	if err := book.Select("Sheet1", 3); err != nil {
		t.Fatalf("Select row 3: %v", err)
	}
	wantHeaders := []intake.Header{{Index: 0, Label: "Email"}, {Index: 1, Label: ""}, {Index: 2, Label: "City"}}
	if !reflect.DeepEqual(book.Headers, wantHeaders) {
		t.Fatalf("headers = %+v, want %+v", book.Headers, wantHeaders)
	}
	if labels := book.HeaderLabels(); !reflect.DeepEqual(labels, []string{"Email", "", "City"}) {
		t.Fatalf("HeaderLabels() = %v", labels)
	}
	if len(book.Rows) != 2 {
		t.Fatalf("rows = %+v, want only worksheet rows 4 and 8", book.Rows)
	}
	if book.Rows[0].Number != 4 {
		t.Fatalf("first row number = %d, want the original worksheet row 4", book.Rows[0].Number)
	}
	// Values keep the original cell text verbatim: validation normalizes
	// later, and the stored snapshot must match what the operator uploaded.
	wantValues := []string{"  a@example.com  ", "x", " Paris  "}
	if !reflect.DeepEqual(book.Rows[0].Values, wantValues) {
		t.Fatalf("row 4 values = %q, want verbatim cells %q", book.Rows[0].Values, wantValues)
	}
	if book.Rows[1].Number != 8 || !reflect.DeepEqual(book.Rows[1].Values, []string{"b@example.com"}) {
		t.Fatalf("row 8 = %+v", book.Rows[1])
	}

	// A repeated selection rebuilds the selection instead of accumulating it.
	if err := book.Select("Sheet1", 1); err != nil {
		t.Fatalf("re-select row 1: %v", err)
	}
	if book.HeaderRow != 1 || len(book.Rows) != 4 {
		t.Fatalf("re-selection = row %d with %d rows, want row 1 with 4 rebuilt rows", book.HeaderRow, len(book.Rows))
	}
	if err := book.Select("Sheet1", 3); err != nil {
		t.Fatalf("re-select row 3: %v", err)
	}
	if len(book.Rows) != 2 {
		t.Fatalf("second re-selection has %d rows, want 2", len(book.Rows))
	}
}

func TestFormulaCellUsesCachedValueWithoutEvaluation(t *testing.T) {
	t.Parallel()
	data := makeXLSX(t, func(f *excelize.File) {
		// Stream writer only: normal-mode and stream-mode writes cannot be
		// mixed on one worksheet, and it is the only public API that writes a
		// formula together with its cached value.
		writer, err := f.NewStreamWriter("Sheet1")
		if err != nil {
			t.Fatal(err)
		}
		if err := writer.SetRow("A1", []any{"Email", "Name"}); err != nil {
			t.Fatal(err)
		}
		if err := writer.SetRow("A2", []any{
			excelize.Cell{Formula: "1+1", Value: "cached value"},
			"n",
		}); err != nil {
			t.Fatal(err)
		}
		if err := writer.SetRow("A3", []any{
			excelize.Cell{Formula: "TRIM(B3)"},
			"keep",
		}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Flush(); err != nil {
			t.Fatal(err)
		}
	})

	book := mustOpen(t, data, intake.DefaultLimits())
	// Excelize never evaluates uploaded formulas: the reader returns the
	// value cached next to the formula, and a formula without a cached value
	// reads as empty. Neither cell may ever surface its formula text.
	if len(book.Rows) != 2 {
		t.Fatalf("rows = %+v, want rows 2 and 3", book.Rows)
	}
	if got := book.Rows[0].Column(0); got != "cached value" {
		t.Fatalf("cached formula cell = %q, want the cached value, not 2 or the formula", got)
	}
	if got := book.Rows[1].Column(0); got != "" {
		t.Fatalf("formula cell without cached value = %q, want empty", got)
	}
	if got := book.Rows[1].Column(1); got != "keep" {
		t.Fatalf("row 3 neighbor value = %q", got)
	}
}

func TestCandidateRowParsing(t *testing.T) {
	t.Parallel()
	addressRows := func(count int) [][]string {
		rows := make([][]string, 0, count+1)
		rows = append(rows, []string{"Email"})
		for i := range count {
			rows = append(rows, []string{fmt.Sprintf("person%d@example.com", i)})
		}
		return rows
	}

	// The expanded-entry bound is enforced after splitting, not by the
	// worksheet reader: both sizes parse here so rows holding only unmapped
	// values can never trip a physical row gate.
	for _, count := range []int{intake.MaxRecipientEntries, intake.MaxRecipientEntries + 1} {
		data := makeXLSX(t, func(f *excelize.File) {
			writeRows(t, f, "Sheet1", 1, addressRows(count))
		})
		book := mustOpen(t, data, intake.DefaultLimits())
		if len(book.Rows) != count {
			t.Fatalf("rows = %d, want %d", len(book.Rows), count)
		}
		if book.Rows[0].Number != 2 || book.Rows[len(book.Rows)-1].Number != count+1 {
			t.Fatalf("row numbers span %d..%d, want 2..%d",
				book.Rows[0].Number, book.Rows[len(book.Rows)-1].Number, count+1)
		}
	}
}

func TestOpenWorkbookRejectsBadFiles(t *testing.T) {
	t.Parallel()
	valid := makeXLSX(t, func(f *excelize.File) {
		writeRows(t, f, "Sheet1", 1, [][]string{{"Email"}, {"a@example.com"}})
	})
	legacyXLS := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...)

	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"garbage bytes": {[]byte("this is not a workbook at all"), intake.ErrCorruptWorkbook},
		"truncated zip": {valid[:len(valid)/2], intake.ErrCorruptWorkbook},
		"legacy xls":    {legacyXLS, intake.ErrUnsupportedWorkbook},
		"empty upload":  {data: nil, want: intake.ErrUploadTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			opened, err := intake.OpenWorkbook(tc.data, intake.DefaultLimits())
			if opened != nil {
				t.Fatal("an invalid upload was opened")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("over the upload limit", func(t *testing.T) {
		if _, err := intake.OpenWorkbook(valid, intake.Limits{MaxUploadBytes: 8}); !errors.Is(err, intake.ErrUploadTooLarge) {
			t.Fatalf("error = %v, want ErrUploadTooLarge", err)
		}
	})

	// Unusable limits are a configuration error and pass through untouched.
	t.Run("unusable limits", func(t *testing.T) {
		_, err := intake.OpenWorkbook(valid, intake.Limits{UnzipSizeLimit: 100, UnzipXMLSizeLimit: 101})
		const want = "the expanded XML part limit must not exceed the expanded archive limit"
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want the fixed configuration message %q", err, want)
		}
	})
}

func TestExpandedArchiveBound(t *testing.T) {
	t.Parallel()
	// Highly repeated cells: the upload compresses to a few kilobytes while
	// the archive itself expands far past the configured bound.
	var rows [][]string
	for i := range 100 {
		rows = append(rows, []string{
			fmt.Sprintf("person%d@example.com", i),
			"XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY",
		})
	}
	data := makeXLSX(t, func(f *excelize.File) {
		writeRows(t, f, "Sheet1", 1, rows)
	})

	limits := intake.Limits{UnzipSizeLimit: 2048, UnzipXMLSizeLimit: 2048}
	if _, err := intake.OpenWorkbook(data, limits); !errors.Is(err, intake.ErrExpandedTooLarge) {
		t.Fatalf("error = %v, want ErrExpandedTooLarge", err)
	}
}

func TestWorksheetBounds(t *testing.T) {
	t.Parallel()

	t.Run("too many columns", func(t *testing.T) {
		wide := make([]string, intake.MaxWorksheetColumns+1)
		for i := range wide {
			wide[i] = fmt.Sprintf("c%d", i)
		}
		data := makeXLSX(t, func(f *excelize.File) {
			writeRows(t, f, "Sheet1", 1, [][]string{wide})
		})
		if err := openInventory(t, data).Select("Sheet1", intake.HeaderRowDefault); !errors.Is(err, intake.ErrWorksheetTooLarge) {
			t.Fatalf("Select error = %v, want ErrWorksheetTooLarge", err)
		}
	})

	t.Run("too many rows", func(t *testing.T) {
		data := makeXLSX(t, func(f *excelize.File) {
			// The stream writer keeps a 10,001-row fixture cheap to build.
			writer, err := f.NewStreamWriter("Sheet1")
			if err != nil {
				t.Fatal(err)
			}
			for row := 1; row <= intake.MaxWorksheetRows+1; row++ {
				cell, err := excelize.CoordinatesToCellName(1, row)
				if err != nil {
					t.Fatal(err)
				}
				if err := writer.SetRow(cell, []any{fmt.Sprintf("row%d", row)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Flush(); err != nil {
				t.Fatal(err)
			}
		})
		if err := openInventory(t, data).Select("Sheet1", intake.HeaderRowDefault); !errors.Is(err, intake.ErrWorksheetTooLarge) {
			t.Fatalf("Select error = %v, want ErrWorksheetTooLarge", err)
		}
	})
}

func TestSelectSheetAndHeaderValidation(t *testing.T) {
	t.Parallel()
	data := makeXLSX(t, func(f *excelize.File) {
		writeRows(t, f, "Sheet1", 1, [][]string{{"Email"}, {"a@example.com"}})
	})
	book := mustOpen(t, data, intake.DefaultLimits())

	for name, tc := range map[string]struct {
		sheet     string
		headerRow int
		want      error
	}{
		"empty sheet name":           {"", 1, intake.ErrUnknownSheet},
		"missing worksheet":          {"Nope", 1, intake.ErrUnknownSheet},
		"wrong letter case":          {"sheet1", 1, intake.ErrUnknownSheet},
		"header row zero":            {"Sheet1", 0, intake.ErrInvalidHeaderRow},
		"header row negative":        {"Sheet1", -1, intake.ErrInvalidHeaderRow},
		"header row below the table": {"Sheet1", 99, intake.ErrInvalidHeaderRow},
	} {
		t.Run(name, func(t *testing.T) {
			if err := book.Select(tc.sheet, tc.headerRow); !errors.Is(err, tc.want) {
				t.Fatalf("Select(%q, %d) = %v, want %v", tc.sheet, tc.headerRow, err, tc.want)
			}
		})
	}
}

func TestOpenWorkbookEmptySheetNeedsHeaderRow(t *testing.T) {
	t.Parallel()
	// A workbook whose only worksheet holds no rows at all still inventories
	// fine; the unusable default selection is reported when it is confirmed.
	opened := openInventory(t, makeXLSX(t, nil))
	if len(opened.Headers) != 0 || len(opened.Rows) != 0 {
		t.Fatalf("default selection parsed %d headers from an empty sheet", len(opened.Headers))
	}
	if err := opened.Select(opened.Sheet, intake.HeaderRowDefault); !errors.Is(err, intake.ErrInvalidHeaderRow) {
		t.Fatalf("Select error = %v, want ErrInvalidHeaderRow", err)
	}
}

func TestExclusionReportLayout(t *testing.T) {
	t.Parallel()
	headers := []string{"Email", "", "City"}
	reportRows := []intake.ReportRow{
		{Row: 12, Position: 1, Recipient: "a@example.com", Reasons: []string{intake.ReasonDuplicate, intake.ReasonInvalidAddress}, Values: []string{"a@example.com", "x", "Paris"}},
		{Row: 15, Position: 2, Recipient: "b@example.com", Reasons: []string{intake.ReasonInvalidAddress}, Values: []string{"a@example.com, b@example.com"}},
	}
	data, err := intake.ExclusionReport(headers, reportRows)
	if err != nil {
		t.Fatal(err)
	}

	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close report: %v", err)
		}
	}()
	if names := file.GetSheetList(); !reflect.DeepEqual(names, []string{"Excluded"}) {
		t.Fatalf("report worksheets = %v, want exactly [Excluded]", names)
	}
	grid, err := file.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Email", "Column 2", "City", "Source worksheet row", "Recipient position", "Recipient", "Reason"},
		{"a@example.com", "x", "Paris", "12", "1", "a@example.com", "duplicate; invalid_address"},
		{"a@example.com, b@example.com", "", "", "15", "2", "b@example.com", "invalid_address"},
	}
	if len(grid) != len(want) {
		t.Fatalf("report rows = %d (%v), want %d", len(grid), grid, len(want))
	}
	for i, wantRow := range want {
		if !rowsEqual(grid[i], wantRow) {
			t.Fatalf("report row %d = %q, want %q", i+1, grid[i], wantRow)
		}
	}

	// No excluded rows still yields a valid report with its header.
	empty, err := intake.ExclusionReport(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyFile, err := excelize.OpenReader(bytes.NewReader(empty))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := emptyFile.Close(); err != nil {
			t.Errorf("close empty report: %v", err)
		}
	}()
	emptyGrid, err := emptyFile.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	wantEmpty := []string{"Source worksheet row", "Recipient position", "Recipient", "Reason"}
	if len(emptyGrid) != 1 || !rowsEqual(emptyGrid[0], wantEmpty) {
		t.Fatalf("empty report = %v, want the header row only", emptyGrid)
	}
}
