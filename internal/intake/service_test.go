// Package intake_test exercises the intake service against a real PocketBase
// app on a temporary data directory, a real XLSX workbook built with Excelize
// and a stub DOCX converter: nothing here sends email and nothing needs Pandoc.
package intake_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

const testCampaign = "Software Engineers"

func newTestApp(t *testing.T) core.App {
	t.Helper()
	app := testutil.NewApp(t)
	return app
}

// newOperator provisions one operator account and returns its record id, which
// is the owner id every service call is scoped by.
func newOperator(t *testing.T, app core.App, email string) string {
	t.Helper()
	// Provision stores the login identity lowercased, so the lookup has to
	// normalize the same way.
	email = strings.ToLower(strings.TrimSpace(email))
	if err := operator.Provision(app, email, "correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// stubPandoc writes a deterministic converter stand-in: it reports a version
// and emits one fixed body containing the placeholder under test, so intake
// tests never depend on a Pandoc install.
func stubPandoc(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pandoc")
	script := `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = "--version" ]; then
		echo "pandoc stub 1.0"
		exit 0
	fi
done
printf '%s' '<p>Dear {{candidate_name}}, we received your application.</p>'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// docxFixture builds a minimal readable DOCX package.
func docxFixture(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	part, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	document := `<?xml version="1.0"?><w:document xmlns:w="w"><w:body><w:p/></w:body></w:document>`
	if _, err := io.WriteString(part, document); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// env bundles one app, its intake service and the campaign the tasks belong to.
type env struct {
	t         *testing.T
	app       core.App
	campaigns *campaign.Service
	service   *intake.Service
	owner     string
	campaign  string
	revision  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	app := newTestApp(t)
	campaigns := campaign.NewServiceWithConverter(app, convert.Converter{Path: stubPandoc(t)})
	ctx := context.Background()
	owner := newOperator(t, app, "operator@bachtelcorporation.com")

	created, err := campaigns.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          testCampaign,
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		Subject:      "Application for {{candidate_name}}",
		Document:     docxFixture(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail}, {
			Variable: "candidate_name",
			Header:   "Applicant Full Name",
			Required: true,
			Format:   campaign.FormatName,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{
		t:         t,
		app:       app,
		campaigns: campaigns,
		service:   intake.NewService(app, campaigns, intake.Limits{}),
		owner:     owner,
		campaign:  created.ID,
		revision:  revision.ID,
	}
}

// approve marks the campaign's current revision as approved content. Change 4
// owns the approval action; intake only rechecks the recorded approval.
func (e *env) approve() {
	e.t.Helper()
	record, err := e.app.FindRecordById(campaign.ConfigurationsCollection, e.revision)
	if err != nil {
		e.t.Fatal(err)
	}
	record.Set("approved_hash", record.GetString("content_hash"))
	if err := e.app.Save(record); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) stage(data []byte, name string) *intake.Task {
	e.t.Helper()
	task, err := e.service.Stage(context.Background(), e.owner, e.campaign, name, data, "")
	if err != nil {
		e.t.Fatalf("stage %s: %v", name, err)
	}
	return task
}

// validate stages and validates one workbook in a single step.
func (e *env) validate(data []byte) *intake.Task {
	e.t.Helper()
	task := e.stage(data, "book.xlsx")
	if err := e.service.Validate(context.Background(), e.owner, task.ID, "Candidates", 1); err != nil {
		e.t.Fatalf("validate: %v", err)
	}
	return e.mustLoad(task.ID)
}

func (e *env) mustLoad(id string) *intake.Task {
	e.t.Helper()
	task, err := e.service.LoadTask(context.Background(), e.owner, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return task
}

func (e *env) rows(taskID string) []intake.RowSnapshot {
	e.t.Helper()
	rows, err := e.service.Rows(context.Background(), e.owner, taskID)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

func (e *env) candidateCount() int {
	e.t.Helper()
	records, err := e.app.FindRecordsByFilter(intake.CandidatesCollection, "", "", 0, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	return len(records)
}

// sheetSpec is one worksheet of a test workbook.
type sheetSpec struct {
	name   string
	rows   [][]string
	hidden bool
}

// buildWorkbook writes an ordered XLSX workbook in process. Row indices in the
// fixture are original worksheet rows: rows[0] is worksheet row 1.
func buildWorkbook(t *testing.T, sheets ...sheetSpec) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	for i, sheet := range sheets {
		if i == 0 {
			if err := file.SetSheetName("Sheet1", sheet.name); err != nil {
				t.Fatal(err)
			}
		} else if _, err := file.NewSheet(sheet.name); err != nil {
			t.Fatal(err)
		}
		for r, row := range sheet.rows {
			cell, err := excelize.CoordinatesToCellName(1, r+1)
			if err != nil {
				t.Fatal(err)
			}
			values := row
			if err := file.SetSheetRow(sheet.name, cell, &values); err != nil {
				t.Fatal(err)
			}
		}
		if sheet.hidden {
			if err := file.SetSheetVisible(sheet.name, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// mixedWorkbook is the fixture used by most tests. It exercises a mixed-case
// padded duplicate group, a duplicate whose member is missing a required value,
// an unusable address, a missing required value, an imported status column that
// must be ignored, an empty row, and one clean eligible row.
func mixedWorkbook(t *testing.T) []byte {
	t.Helper()
	return buildWorkbook(t, sheetSpec{
		name: "Candidates",
		rows: [][]string{
			{"Email", "Applicant Full Name", "Location", "Status"},
			{"alice@example.com", "Ada Lovelace", "Remote", "SUBMITTED"},
			{" ALICE@example.com ", "Grace Hopper", "", "SUBMITTED"},
			{" ALICE@EXAMPLE.COM", "", "London", ""},
			{"bob@example.com", "", "London", ""},
			{"not-an-address", "Zoe Quinn", "", ""},
			{"", "", "", ""},
			{"carol@example.com", "Carol Danvers", "", "SUBMITTED"},
			{"dana@example.com", "  Dana   Scully  ", "", ""},
			{"dana@example.com", "", "", ""},
		},
	})
}

func TestValidatePublishesSnapshotAndCounts(t *testing.T) {
	env := newEnv(t)
	task := env.validate(mixedWorkbook(t))

	if task.Status != intake.TaskReady {
		t.Fatalf("status = %q, want %q", task.Status, intake.TaskReady)
	}
	want := intake.Counts{Total: 8, SourceRows: 8, Eligible: 1, Ineligible: 1, Duplicate: 5, Invalid: 1}
	if task.Counts != want {
		t.Fatalf("counts = %+v, want %+v", task.Counts, want)
	}
	if got := task.Counts.Excluded(); got != 6 {
		t.Fatalf("Excluded() = %d, want 6", got)
	}
	// Only unique valid-address rows become candidates: the duplicate group
	// members and the unusable address never touch candidate records.
	if got := env.candidateCount(); got != 2 {
		t.Fatalf("candidate records = %d, want 2 (bob and carol only)", got)
	}

	rows := env.rows(task.ID)
	if len(rows) != 2 {
		t.Fatalf("membership rows = %d, want 2", len(rows))
	}
	byState := map[string][]int{}
	for _, row := range rows {
		byState[row.State] = append(byState[row.State], row.SourceRow)
	}
	if got := byState[intake.RowEligible]; len(got) != 1 || got[0] != 8 {
		t.Fatalf("eligible rows = %v, want [8]", got)
	}
	if got := byState[intake.RowIneligible]; len(got) != 1 || got[0] != 5 {
		t.Fatalf("ineligible rows = %v, want [5]", got)
	}
	// The imported SUBMITTED status cell is just source data: campaign history
	// decides eligibility, and this campaign has none.
	if reason := rows[bySourceRow(rows, 5)].Reason; !strings.Contains(reason, "candidate_name") {
		t.Fatalf("ineligible reason = %q, want the missing variable named", reason)
	}

	manifest, err := env.service.Manifest(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Sheet != "Candidates" || manifest.HeaderRow != 1 {
		t.Fatalf("manifest selection = %q row %d", manifest.Sheet, manifest.HeaderRow)
	}
	if len(manifest.Rows) != 8 {
		t.Fatalf("manifest rows = %d, want 8", len(manifest.Rows))
	}
	var duplicateRows []int
	for _, row := range manifest.Rows {
		if len(row.Reasons) > 0 && row.Reasons[0] == intake.ReasonDuplicate {
			duplicateRows = append(duplicateRows, row.Row)
		}
	}
	if fmt.Sprint(duplicateRows) != "[2 3 4 9 10]" {
		t.Fatalf("duplicate rows = %v, want every group member with its original row", duplicateRows)
	}
	if len(manifest.Groups) != 2 {
		t.Fatalf("duplicate groups = %d, want 2", len(manifest.Groups))
	}
	for _, group := range manifest.Groups {
		if len(group.Occurrences) < 2 {
			t.Fatalf("group %d has %d entries, want every member held", group.Group, len(group.Occurrences))
		}
		// No member is picked as the preferred copy: every entry of the group is
		// excluded, so the group can never send one of them.
		for _, occurrence := range group.Occurrences {
			if state := manifestState(manifest, occurrence.Row); state != "" {
				t.Fatalf("duplicate entry %+v carries state %q, want excluded", occurrence, state)
			}
		}
	}
}

func manifestState(manifest intake.Manifest, row int) string {
	for _, entry := range manifest.Rows {
		if entry.Row == row {
			return entry.State
		}
	}
	return ""
}

func TestPreviewUsesPinnedRevisionAndRawSnapshot(t *testing.T) {
	env := newEnv(t)
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
		{"dana@example.com", "  Dana   Scully  "},
		{"erin@example.com", ""},
	}}))
	rows := env.rows(task.ID)
	if len(rows) != 3 {
		t.Fatalf("membership rows = %d, want 3", len(rows))
	}

	preview, err := env.service.Preview(context.Background(), env.owner, task.ID, rows[bySourceRow(rows, 2)].ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rendered.Subject != "Application for Carol Danvers" {
		t.Fatalf("subject = %q", preview.Rendered.Subject)
	}
	if !strings.Contains(preview.Rendered.Body, "Carol Danvers") {
		t.Fatalf("body = %q", preview.Rendered.Body)
	}
	if preview.SourceRow != 2 {
		t.Fatalf("source row = %d, want 2", preview.SourceRow)
	}
	if preview.ContentHash != task.ContentHash {
		t.Fatalf("preview content hash = %q, task = %q", preview.ContentHash, task.ContentHash)
	}

	// Whitespace-only differences are normalized for the replacement value
	// while the raw cell stays available next to it.
	rendered, err := env.service.Preview(context.Background(), env.owner, task.ID, rows[bySourceRow(rows, 3)].ID)
	if err != nil {
		t.Fatal(err)
	}
	value := rendered.Rendered.Values[1]
	if value.Raw != "  Dana   Scully  " || value.Normalized != "Dana Scully" {
		t.Fatalf("value = %+v, want the raw cell beside the normalized replacement", value)
	}
	if rendered.Rendered.Subject != "Application for Dana Scully" {
		t.Fatalf("subject = %q, want the normalized replacement value", rendered.Rendered.Subject)
	}

	// A row of another task is never previewed through this task.
	other := env.validate(buildWorkbook(t, sheetSpec{
		name: "Candidates",
		rows: [][]string{{"Email", "Applicant Full Name"}, {"frank@example.com", "Frank Reads"}},
	}))
	foreign := env.rows(other.ID)
	if _, err := env.service.Preview(context.Background(), env.owner, task.ID, foreign[0].ID); err == nil {
		t.Fatal("a row of another task was previewed")
	}

	// A row admitted without its required value has no preview: the render
	// reports the missing variable instead.
	if _, err := env.service.Preview(context.Background(), env.owner, task.ID, rows[bySourceRow(rows, 4)].ID); err == nil {
		t.Fatal("preview of a row missing a required value succeeded")
	}
}

func bySourceRow(rows []intake.RowSnapshot, number int) int {
	for i, row := range rows {
		if row.SourceRow == number {
			return i
		}
	}
	return -1
}

func TestExclusionReportDownloadsEveryHeldRow(t *testing.T) {
	env := newEnv(t)
	task := env.validate(mixedWorkbook(t))

	data, name, err := env.service.ExclusionReport(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".xlsx") {
		t.Fatalf("download name = %q", name)
	}
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("report is not a readable workbook: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	sheets := file.GetSheetList()
	if len(sheets) != 1 || sheets[0] != "Excluded" {
		t.Fatalf("report worksheets = %v, want one Excluded sheet", sheets)
	}
	rows, err := file.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	// Header plus every held entry: five duplicates, one unusable address and
	// one missing value, in original row order.
	if len(rows) != 8 {
		t.Fatalf("report rows = %d, want header plus 7 held entries", len(rows))
	}
	if got := strings.Join(rows[0], "|"); got != "Email|Applicant Full Name|Location|Status|Source worksheet row|Recipient position|Recipient|Reason|Affected fields|Reason source|Original mapped values|Proposed mapped values" {
		t.Fatalf("report header = %q", got)
	}
	held := map[string][]string{}
	var order []string
	for _, row := range rows[1:] {
		held[row[4]] = row
		order = append(order, row[4])
	}
	if fmt.Sprint(order) != "[2 3 4 5 6 9 10]" {
		t.Fatalf("held rows = %v, want every held entry in original row order", order)
	}
	if !strings.Contains(held["2"][7], "duplicate") {
		t.Fatalf("duplicate row = %v", held["2"])
	}
	if held["2"][0] != "alice@example.com" || held["2"][1] != "Ada Lovelace" || held["2"][5] != "1" || held["2"][6] != "alice@example.com" {
		t.Fatalf("duplicate row lost its original columns or entry identity: %v", held["2"])
	}
	// The padded, mixed-case member of the same group is held as well.
	if !strings.Contains(held["3"][7], "duplicate") || held["3"][0] != " ALICE@example.com " {
		t.Fatalf("padded duplicate row = %v", held["3"])
	}
	if !strings.Contains(held["6"][7], "recipient cell") {
		t.Fatalf("unusable address row = %v", held["6"])
	}
	if !strings.Contains(held["5"][7], "required") {
		t.Fatalf("missing value row = %v: %v", held["5"], held)
	}
}

func TestValidationFailuresLeaveTheTaskUnscheduled(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	cases := map[string]struct {
		workbook []byte
		want     string
	}{
		"missing mapped header": {
			workbook: buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
				{"Email", "Name"}, {"carol@example.com", "Carol Danvers"},
			}}),
			want: `no "Applicant Full Name" column`,
		},
		"ambiguous mapped header": {
			workbook: buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
				{"Email", "Applicant Full Name", "Applicant Full Name "},
				{"carol@example.com", "Carol Danvers", "Carol Danvers"},
			}}),
			want: `more than one "Applicant Full Name" column`,
		},
		"missing recipient header": {
			workbook: buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
				{"Applicant Full Name", "Email Address"}, {"Carol Danvers", "carol@example.com"},
			}}),
			want: `no "Email" column`,
		},
		"too many recipient entries": {
			workbook: manyRows(t, intake.MaxRecipientEntries+1),
			want:     "more than 1000 recipient entries",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			task := env.stage(testCase.workbook, name+".xlsx")
			err := env.service.Validate(ctx, env.owner, task.ID, "Candidates", 1)
			var failure *intake.ValidationError
			if !errors.As(err, &failure) || !strings.Contains(failure.Message, testCase.want) {
				t.Fatalf("validate error = %v, want a validation failure naming %q", err, testCase.want)
			}
			loaded := env.mustLoad(task.ID)
			if loaded.Status != intake.TaskStaging {
				t.Fatalf("status = %q, want staging after a failed validation", loaded.Status)
			}
			if !strings.Contains(loaded.ValidationError, testCase.want) {
				t.Fatalf("recorded error = %q", loaded.ValidationError)
			}
			// Nothing was published: no snapshot, no candidate, no ready task.
			if rows := env.rows(task.ID); len(rows) != 0 {
				t.Fatalf("membership rows = %d after a failed validation", len(rows))
			}
			if _, err := env.service.Manifest(ctx, env.owner, task.ID); err == nil {
				t.Fatal("a failed validation published a manifest")
			}
			if _, _, err := env.service.ExclusionReport(ctx, env.owner, task.ID); err == nil {
				t.Fatal("a failed validation published an exclusion report")
			}
			preflight, err := env.service.Preflight(ctx, env.owner, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if preflight.Ready || preflight.Startable() {
				t.Fatalf("unvalidated task is startable: %+v", preflight)
			}
		})
	}
	if got := env.candidateCount(); got != 0 {
		t.Fatalf("candidate records = %d, want none for a failed validation", got)
	}
}

// manyRows builds one worksheet with count data rows.
func manyRows(t *testing.T, count int) []byte {
	t.Helper()
	rows := make([][]string, 0, count+1)
	rows = append(rows, []string{"Email", "Applicant Full Name"})
	for i := range count {
		rows = append(rows, []string{fmt.Sprintf("person%d@example.com", i), fmt.Sprintf("Person %d", i)})
	}
	return buildWorkbook(t, sheetSpec{name: "Candidates", rows: rows})
}

func TestCorruptAndUnsupportedUploadsAreRejectedBeforeStorage(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	oleBytes := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, bytes.Repeat([]byte{0}, 512)...)

	for name, data := range map[string][]byte{
		"corrupt":    []byte("this is not a workbook at all"),
		"legacy xls": oleBytes,
		"empty":      {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := env.service.Stage(ctx, env.owner, env.campaign, name+".xls", data, ""); err == nil {
				t.Fatal("a non-XLSX upload was staged")
			}
		})
	}
	// An oversized upload is refused before storage even when it is a workbook.
	workbook := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{{"Email", "Applicant Full Name"}, {"carol@example.com", "Carol"}}})
	tiny := intake.NewService(env.app, env.campaigns, intake.Limits{MaxUploadBytes: 128})
	if _, err := tiny.Stage(ctx, env.owner, env.campaign, "big.xlsx", workbook, ""); !errors.Is(err, intake.ErrUploadTooLarge) {
		t.Fatalf("oversized stage error = %v, want ErrUploadTooLarge", err)
	}
	tasks, err := env.service.ListTasks(ctx, env.owner, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("stored tasks = %d, want none for rejected uploads", len(tasks))
	}
}

func TestWorksheetSelection(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	data := buildWorkbook(t,
		// The first sheet of a generated workbook is tab-selected, and Excelize
		// only hides a worksheet that is not the selected tab.
		sheetSpec{name: "Candidates", rows: [][]string{
			{"Contact", "Applicant Full Name"},
			{"carol@example.com", "Carol Danvers"},
		}},
		sheetSpec{name: "Notes", rows: [][]string{{"Something else"}}, hidden: true},
		sheetSpec{name: "Old", rows: [][]string{{"Email", "Applicant Full Name"}, {"old@example.com", "Old Row"}}},
	)
	task := env.stage(data, "book.xlsx")
	if len(task.Sheets) != 3 || task.Sheets[1].Name != "Notes" || task.Sheets[1].Visible {
		t.Fatalf("sheet inventory = %+v", task.Sheets)
	}
	// The default selection is the first visible worksheet; when its headers do
	// not satisfy the campaign, the operator has to choose another one.
	if task.Sheet != "Candidates" {
		t.Fatalf("default selection = %q, want the first visible worksheet", task.Sheet)
	}
	err := env.service.Validate(ctx, env.owner, task.ID, task.Sheet, task.HeaderRow)
	if err == nil || !strings.Contains(err.Error(), `no "Email" column`) {
		t.Fatalf("validate with the default sheet = %v", err)
	}
	if err := env.service.Validate(ctx, env.owner, task.ID, "Missing", 1); !errors.Is(err, intake.ErrUnknownSheet) {
		t.Fatalf("unknown worksheet = %v, want ErrUnknownSheet", err)
	}
	if err := env.service.Validate(ctx, env.owner, task.ID, "Old", 1); err != nil {
		t.Fatalf("validate chosen sheet: %v", err)
	}
	loaded := env.mustLoad(task.ID)
	if loaded.Sheet != "Old" || loaded.HeaderRow != 1 {
		t.Fatalf("selection = %q row %d", loaded.Sheet, loaded.HeaderRow)
	}
	// Only the chosen worksheet is considered, at the chosen header row.
	if loaded.Counts.Total != 1 {
		t.Fatalf("counts = %+v, want only the selected worksheet", loaded.Counts)
	}
	// A second validation of the same task is refused: it already holds a
	// snapshot, and replacing it silently would be a new task's job.
	if err := env.service.Validate(ctx, env.owner, task.ID, "Candidates", 1); err == nil {
		t.Fatal("a validated task was validated again")
	}
	// A different header row inside the table is refused as a header choice.
	other := env.stage(data, "book2.xlsx")
	if err := env.service.Validate(ctx, env.owner, other.ID, "Old", 9); !errors.Is(err, intake.ErrInvalidHeaderRow) {
		t.Fatalf("header row below the table = %v, want ErrInvalidHeaderRow", err)
	}
}

func TestCorrectedUploadCreatesANewTaskAndKeepsTheOldSnapshot(t *testing.T) {
	env := newEnv(t)
	first := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"bob@example.com", ""},
		{"carol@example.com", "Carol Danvers"},
	}}))
	if first.Counts.Ineligible != 1 || first.Counts.Eligible != 1 {
		t.Fatalf("first counts = %+v", first.Counts)
	}
	firstRows := env.rows(first.ID)
	ineligibleID := firstRows[bySourceRow(firstRows, 2)].CandidateID

	// The corrected upload is a new task, not an edit: the earlier task keeps
	// its snapshot and both reference one campaign-scoped identity.
	second := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"bob@example.com", "Bob Barker"},
	}}))
	secondRows := env.rows(second.ID)
	if len(secondRows) != 1 || secondRows[0].State != intake.RowEligible {
		t.Fatalf("corrected rows = %+v", secondRows)
	}
	if secondRows[0].CandidateID != ineligibleID {
		t.Fatalf("corrected row candidate = %q, want the same identity %q", secondRows[0].CandidateID, ineligibleID)
	}
	if got := env.candidateCount(); got != 2 {
		t.Fatalf("candidate records = %d, want 2 identities for 2 addresses", got)
	}
	after := env.rows(first.ID)
	if len(after) != 2 || after[bySourceRow(after, 2)].State != intake.RowIneligible {
		t.Fatalf("earlier snapshot changed: %+v", after)
	}
	if raw := after[bySourceRow(after, 2)].Raw; raw["candidate_name"] != "" {
		t.Fatalf("earlier raw snapshot = %+v", raw)
	}
}

func TestSubmissionHistoryHoldsRowsAndIsNeverCleared(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	first := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
	}}))
	rows := env.rows(first.ID)
	if err := env.service.MarkSubmitted(ctx, env.owner, rows[0].CandidateID); err != nil {
		t.Fatal(err)
	}

	second := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol With New Details"},
	}}))
	secondRows := env.rows(second.ID)
	if len(secondRows) != 1 || secondRows[0].State != intake.RowAlreadySubmitted {
		t.Fatalf("re-uploaded row = %+v, want already submitted", secondRows)
	}
	if secondRows[0].CandidateID != rows[0].CandidateID {
		t.Fatal("the re-upload created a second identity for the same address")
	}
	if got := env.candidateCount(); got != 1 {
		t.Fatalf("candidate records = %d, want 1", got)
	}
	candidate, err := env.app.FindRecordById(intake.CandidatesCollection, rows[0].CandidateID)
	if err != nil {
		t.Fatal(err)
	}
	if state := candidate.GetString("submission_state"); state != intake.SubmissionSubmitted {
		t.Fatalf("submission state = %q, want %q: a corrected upload must not clear it", state, intake.SubmissionSubmitted)
	}
	if second.Counts.AlreadySubmitted != 1 || second.Counts.Eligible != 0 {
		t.Fatalf("second counts = %+v", second.Counts)
	}
}

func TestPreflightRevalidationAndClaimOwnership(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
		{"dana@example.com", "Dana Scully"},
	}}))
	candidateID := env.rows(task.ID)[0].CandidateID

	// Content approval is recorded by a later change; until it exists a start
	// must be refused even though the snapshot is fresh.
	preflight, err := env.service.Preflight(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Ready || preflight.Stale || preflight.Approved {
		t.Fatalf("preflight = %+v", preflight)
	}
	if preflight.Startable() || len(preflight.Claimable) != 2 {
		t.Fatalf("preflight = %+v, want two claimable rows and a refusal without approval", preflight)
	}
	env.approve()
	preflight, err = env.service.Preflight(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Startable() || len(preflight.Claimable) != 2 {
		t.Fatalf("preflight after approval = %+v", preflight)
	}

	// Exactly one owner reserves a candidate.
	outcomes, err := env.service.Claim(ctx, env.owner, task.ID, preflight.Claimable)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range preflight.Claimable {
		if !outcomes[id].Claimed {
			t.Fatalf("claim outcome for %q = %+v", id, outcomes[id])
		}
	}
	second, err := env.service.Claim(ctx, env.owner, task.ID, []string{candidateID})
	if err != nil {
		t.Fatal(err)
	}
	if second[candidateID].Claimed || second[candidateID].State != intake.CandidateClaimed {
		t.Fatalf("repeat claim = %+v, want an already claimed report", second[candidateID])
	}
	preflight, err = env.service.Preflight(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preflight.Startable() {
		t.Fatalf("a fully claimed task is still startable: %+v", preflight)
	}

	// A second task cannot schedule a candidate the first task reserved.
	other := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
		{"erin@example.com", "Erin Reads"},
	}}))
	otherRows := env.rows(other.ID)
	if otherRows[bySourceRow(otherRows, 2)].State != intake.RowAlreadyScheduled {
		t.Fatalf("overlapping row = %+v, want already scheduled", otherRows[0])
	}
	if otherRows[bySourceRow(otherRows, 3)].State != intake.RowEligible {
		t.Fatalf("unscheduled row = %+v, want eligible", otherRows[1])
	}
	otherPreflight, err := env.service.Preflight(ctx, env.owner, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPreflight.Claimable) != 1 {
		t.Fatalf("claimable rows of the overlapping task = %v", otherPreflight.Claimable)
	}

	// Safe release frees only the claims the task owns, and a later action
	// rechecks instead of reclaiming blindly.
	released, err := env.service.Release(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released != 2 {
		t.Fatalf("released = %d, want 2", released)
	}
	if err := env.service.Revalidate(ctx, env.owner, other.ID); err != nil {
		t.Fatal(err)
	}
	if state := env.rows(other.ID)[0].State; state != intake.RowEligible {
		t.Fatalf("released row = %q, want eligible after release", state)
	}
}

func TestConcurrentClaimsAdmitOneOwner(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
	}}))
	candidateID := env.rows(task.ID)[0].CandidateID

	// Eight simultaneous actions race for one candidate: exactly one may win.
	const racers = 8
	var wait sync.WaitGroup
	results := make([]bool, racers)
	errs := make([]error, racers)
	for i := range racers {
		wait.Go(func() {
			outcomes, err := env.service.Claim(ctx, env.owner, task.ID, []string{candidateID})
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = outcomes[candidateID].Claimed
		})
	}
	wait.Wait()
	winners := 0
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("concurrent claim %d: %v", i, errs[i])
		}
		if results[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claims = %d winners, want exactly 1", winners)
	}
}

func TestRevalidatePinsTheCurrentRevisionAndLeavesStartedTasksAlone(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
	}}))
	env.approve()

	// A new revision makes the snapshot stale: the preview must be rebuilt
	// against the current revision before anything may start.
	revision, _, err := env.campaigns.CreateRevision(ctx, env.owner, env.campaign, campaign.RevisionInput{
		Subject:      "Interview for {{candidate_name}}",
		Document:     docxFixture(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail}, {
			Variable: "candidate_name",
			Header:   "Applicant Full Name",
			Required: true,
			Format:   campaign.FormatName,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := env.service.Preflight(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !preflight.Stale || preflight.Startable() {
		t.Fatalf("preflight = %+v, want a stale refusal", preflight)
	}
	if err := env.service.Revalidate(ctx, env.owner, task.ID); err != nil {
		t.Fatal(err)
	}
	rebalanced := env.mustLoad(task.ID)
	if rebalanced.ContentHash != revision.ContentHash {
		t.Fatalf("pinned hash = %q, want the new revision", rebalanced.ContentHash)
	}
	preview, err := env.service.Preview(ctx, env.owner, task.ID, env.rows(task.ID)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rendered.Subject != "Interview for Carol Danvers" {
		t.Fatalf("preview subject = %q", preview.Rendered.Subject)
	}

	// Once a task has started, its snapshot is history: revalidation refuses.
	record, err := env.app.FindRecordById(intake.TasksCollection, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("status", "running")
	if err := env.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := env.service.Revalidate(ctx, env.owner, task.ID); err == nil {
		t.Fatal("a started task was revalidated")
	}
}

func TestResultLabelCollisionsResolveWithoutOverwritingSourceColumns(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	if err := env.campaigns.UpdateResultLabels(ctx, env.owner, env.campaign, []string{"Status", "Applicant Full Name"}); err != nil {
		t.Fatal(err)
	}
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name", "Status"},
		{"carol@example.com", "Carol Danvers", "SUBMITTED"},
	}}))
	if got := fmt.Sprint(task.ResultLabels); got != "[Status (2) Applicant Full Name (2)]" {
		t.Fatalf("resolved labels = %v", task.ResultLabels)
	}
	// The source columns are untouched: the label snapshot lives on the task.
	if got := fmt.Sprint(task.Headers); got != "[Email Applicant Full Name Status]" {
		t.Fatalf("source headers = %v", task.Headers)
	}
	preflight, err := env.service.Preflight(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(preflight.ResultLabels); got != "[Status (2) Applicant Full Name (2)]" {
		t.Fatalf("preflight labels = %v", preflight.ResultLabels)
	}
}

func TestSourceBytesAndRowCoordinatesArePreserved(t *testing.T) {
	env := newEnv(t)
	data := mixedWorkbook(t)
	task := env.validate(data)

	// The stored original is byte-identical to the upload and still carries the
	// recorded digest: results can reproduce the source cells and row numbers
	// exactly, and the stored copy provably was not rewritten.
	record, err := env.app.FindRecordById(intake.TasksCollection, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := env.app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fsys.Close(); err != nil {
			t.Error(err)
		}
	}()
	reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + record.GetString("source"))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatal("the stored workbook differs from the upload")
	}
	if got := intake.WorkbookDigest(stored); got != task.SourceDigest {
		t.Fatalf("digest = %q, want the recorded %q", got, task.SourceDigest)
	}

	file, err := excelize.OpenReader(bytes.NewReader(stored))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := file.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 10 || rows[1][0] != "alice@example.com" || rows[3][1] != "" || rows[4][2] != "London" {
		t.Fatalf("stored cells changed: %v", rows)
	}

	// Row coordinates in the manifest point at the worksheet the operator
	// uploaded: row 1 is the header and the held rows keep their own numbers.
	manifest, err := env.service.Manifest(context.Background(), env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.HeaderRow != 1 || manifest.Sheet != "Candidates" {
		t.Fatalf("manifest selection = %q row %d", manifest.Sheet, manifest.HeaderRow)
	}
	for _, entry := range manifest.Rows {
		if entry.Row < 2 || entry.Row > 10 {
			t.Fatalf("manifest row %d is outside the uploaded table", entry.Row)
		}
	}
}

func TestIntakeCollectionsArePrivate(t *testing.T) {
	env := newEnv(t)
	for _, name := range []string{intake.TasksCollection, intake.CandidatesCollection, intake.TaskCandidatesCollection} {
		collection, err := env.app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatal(err)
		}
		if collection.ListRule != nil || collection.ViewRule != nil {
			t.Fatalf("%s is reachable through the public API: list=%v view=%v",
				name, collection.ListRule, collection.ViewRule)
		}
	}
}

func TestThousandRowFixture(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	// A 1,000-row mixed workbook: unique recipients, one duplicate pair, one
	// unusable address and one missing required value.
	rows := make([][]string, 0, intake.MaxRecipientEntries+1)
	rows = append(rows, []string{"Email", "Applicant Full Name", "Status"})
	rows = append(rows, []string{"duplicate@example.com", "Duplicate One", "SUBMITTED"})
	rows = append(rows, []string{" DUPLICATE@example.com ", "Duplicate Two", ""})
	rows = append(rows, []string{"not-an-address", "Zoe Quinn", ""})
	rows = append(rows, []string{"missing@example.com", "", ""})
	for i := 4; i < intake.MaxRecipientEntries; i++ {
		rows = append(rows, []string{fmt.Sprintf("person%d@example.com", i), fmt.Sprintf("Person %d", i), ""})
	}
	if len(rows)-1 != intake.MaxRecipientEntries {
		t.Fatalf("fixture rows = %d, want %d candidate rows", len(rows)-1, intake.MaxRecipientEntries)
	}
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: rows}))

	counts := task.Counts
	if counts.Total != intake.MaxRecipientEntries {
		t.Fatalf("total = %d, want %d", counts.Total, intake.MaxRecipientEntries)
	}
	if counts.Duplicate != 2 || counts.Invalid != 1 || counts.Ineligible != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	wantEligible := intake.MaxRecipientEntries - 4
	if counts.Eligible != wantEligible {
		t.Fatalf("eligible = %d, want %d", counts.Eligible, wantEligible)
	}
	if got := env.candidateCount(); got != wantEligible+1 {
		t.Fatalf("candidate records = %d, want %d", got, wantEligible+1)
	}
	if rows := env.rows(task.ID); len(rows) != wantEligible+1 {
		t.Fatalf("membership rows = %d, want %d", len(rows), wantEligible+1)
	}
	data, _, err := env.service.ExclusionReport(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()
	report, err := file.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 5 {
		t.Fatalf("report rows = %d, want header plus 4 held rows", len(report))
	}

	// A sequential upload of the same data stays a new task over the same
	// identities: the counts are equal and no identity is duplicated.
	second := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: rows}))
	if second.Counts != counts {
		t.Fatalf("second counts = %+v, want %+v", second.Counts, counts)
	}
	if got := env.candidateCount(); got != wantEligible+1 {
		t.Fatalf("candidate records after a second upload = %d, want %d", got, wantEligible+1)
	}
}

func TestRecipientMappingDrivesIntakeAndIdentity(t *testing.T) {
	t.Parallel()
	env := newEnv(t)
	ctx := context.Background()
	// Draft hints are deliberately unrelated to the selected recipient column.
	record, err := env.app.FindRecordById(campaign.CampaignsCollection, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("source_headers", []string{"Unused draft column"})
	if err := env.app.Save(record); err != nil {
		t.Fatal(err)
	}
	_, _, err = env.campaigns.CreateRevision(ctx, env.owner, env.campaign, campaign.RevisionInput{
		Subject: "Welcome {{candidate_name}}", Document: docxFixture(t), DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Contact", Recipient: true},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Contact", "Applicant Full Name", "Email"},
		{" ADA@EXAMPLE.COM ", "Ada", "ignored@example.com"},
		{"duplicate@example.com", "One", "different1@example.com"},
		{" DUPLICATE@example.com ", "Two", "different2@example.com"},
		{"not-email", "Invalid", "valid@example.com"},
	}}))
	rows := env.rows(task.ID)
	if len(rows) != 1 || rows[0].Normalized["candidate_email"] != "ada@example.com" {
		t.Fatalf("rows = %+v, want only the selected recipient admitted", rows)
	}
	candidate, err := env.app.FindRecordById(intake.CandidatesCollection, rows[0].CandidateID)
	if err != nil || candidate.GetString("email_key") != "ada@example.com" {
		t.Fatalf("candidate did not use the mapped recipient: %v", err)
	}
	preview, err := env.service.Preview(ctx, env.owner, task.ID, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rendered.Recipient != "ada@example.com" || strings.Contains(preview.Rendered.Body, "ada@example.com") {
		t.Fatalf("preview = %+v", preview.Rendered)
	}
	manifest, err := env.service.Manifest(ctx, env.owner, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Groups) != 1 || len(manifest.Groups[0].Occurrences) != 2 {
		t.Fatalf("duplicate groups = %+v", manifest.Groups)
	}
}

// TestTaskLoadsUnscheduledBeforeAndAfterValidation covers the schedule fields
// on both a staging task created after the migration and a validated one, plus
// the campaign listing.
func TestTaskLoadsUnscheduledBeforeAndAfterValidation(t *testing.T) {
	env := newEnv(t)
	staged := env.stage(mixedWorkbook(t), "book.xlsx")
	if staged.ScheduleState != intake.ScheduleNone || !staged.ScheduleAt.IsZero() {
		t.Fatalf("staged schedule = %q %s", staged.ScheduleState, staged.ScheduleAt)
	}
	ready := env.validate(mixedWorkbook(t))
	if ready.ScheduleState != intake.ScheduleNone || !ready.ScheduleAt.IsZero() {
		t.Fatalf("validated schedule = %q %s", ready.ScheduleState, ready.ScheduleAt)
	}
	tasks, err := env.service.ListTasks(context.Background(), env.owner, env.campaign)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.ScheduleState != intake.ScheduleNone {
			t.Fatalf("listed task %s schedule = %q", task.ID, task.ScheduleState)
		}
	}
}

// secondOperator provisions another operator with their own campaign and
// revision, so two-operator tests can prove the ownership boundary end to end.
type secondOperator struct {
	owner    string
	campaign string
	service  *intake.Service
}

func newSecondOperator(t *testing.T, env *env, email string) *secondOperator {
	t.Helper()
	ctx := context.Background()
	owner := newOperator(t, env.app, email)
	created, err := env.campaigns.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Other Engineers",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.campaigns.CreateRevision(ctx, owner, created.ID, campaign.RevisionInput{
		Subject:      "Application for {{candidate_name}}",
		Document:     docxFixture(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail}, {
			Variable: "candidate_name",
			Header:   "Applicant Full Name",
			Required: true,
			Format:   campaign.FormatName,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	return &secondOperator{
		owner:    owner,
		campaign: created.ID,
		service:  env.service,
	}
}

func (o *secondOperator) workbook(t *testing.T) []byte {
	t.Helper()
	return buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Other"},
	}})
}

// TestTwoOperatorsAreIsolated proves the owner boundary across the whole
// intake lifecycle: foreign ids are refused with the same ErrNotFound as
// missing ones and produce no writes, each operator manages their own tasks,
// and the same recipient is an independent identity per owner.
func TestTwoOperatorsAreIsolated(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	bob := newSecondOperator(t, env, "bob@bachtelcorporation.com")

	// The same recipient staged and validated by both owners creates two
	// independent candidate identities: one per campaign.
	aliceTask := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Alice"},
	}}))
	bobTask := env.validateFor(bob, bob.workbook(t))
	aliceRows := env.rows(aliceTask.ID)
	bobRows := env.rowsFor(bob, bobTask.ID)
	if len(aliceRows) != 1 || len(bobRows) != 1 {
		t.Fatalf("rows = %d and %d, want one each", len(aliceRows), len(bobRows))
	}
	if aliceRows[0].CandidateID == bobRows[0].CandidateID {
		t.Fatal("the same recipient shared one identity across owners")
	}

	// Bob cannot reach Alice's task through any read path; every refusal is
	// the same sentinel a missing id gets.
	if _, err := bob.service.LoadTask(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign LoadTask = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.LoadTask(ctx, bob.owner, "missing-id"); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("missing LoadTask = %v, want the same ErrNotFound", err)
	}
	if _, err := bob.service.Rows(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign Rows = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.Manifest(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign Manifest = %v, want ErrNotFound", err)
	}
	if _, _, err := bob.service.ExclusionReport(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign ExclusionReport = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.Preview(ctx, bob.owner, aliceTask.ID, aliceRows[0].ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign Preview = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.Preflight(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign Preflight = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.ListTasks(ctx, bob.owner, env.campaign); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("foreign ListTasks = %v, want ErrNotFound", err)
	}
	if _, err := bob.service.Stage(ctx, bob.owner, env.campaign, "evil.xlsx", bob.workbook(t), ""); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("foreign Stage = %v, want ErrNotFound", err)
	}

	// No foreign write landed: Alice's task, rows and history are untouched.
	if loaded := env.mustLoad(aliceTask.ID); loaded.Status != intake.TaskReady {
		t.Fatalf("alice's task status = %q, want ready", loaded.Status)
	}
	if rows := env.rows(aliceTask.ID); len(rows) != 1 || rows[0].State != intake.RowEligible {
		t.Fatalf("alice's rows = %+v, want untouched eligibility", rows)
	}

	// Bob's writes stay inside his own scope. He claims his own candidate
	// while Alice's id is mixed into the same request: it is refused, never
	// reserved.
	aliceCandidate := aliceRows[0].CandidateID
	bobCandidate := bobRows[0].CandidateID
	outcomes, err := bob.service.Claim(ctx, bob.owner, bobTask.ID, []string{bobCandidate, aliceCandidate})
	if err != nil {
		t.Fatal(err)
	}
	if !outcomes[bobCandidate].Claimed {
		t.Fatalf("own claim = %+v", outcomes)
	}
	if outcomes[aliceCandidate].Claimed {
		t.Fatalf("a foreign candidate id was reserved: %+v", outcomes[aliceCandidate])
	}
	// Marking history follows the same boundary: Bob may mark his own
	// candidate and never Alice's.
	if err := bob.service.MarkSubmitted(ctx, bob.owner, bobCandidate); err != nil {
		t.Fatalf("bob marked his own candidate: %v", err)
	}
	if err := bob.service.MarkSubmitted(ctx, bob.owner, aliceCandidate); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign MarkSubmitted = %v, want ErrNotFound and no write", err)
	}
	aliceRecord, err := env.app.FindRecordById(intake.CandidatesCollection, aliceCandidate)
	if err != nil {
		t.Fatal(err)
	}
	if state := aliceRecord.GetString("submission_state"); state != intake.SubmissionNone {
		t.Fatalf("alice's candidate state = %q, want untouched", state)
	}
	if released, err := bob.service.Release(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) || released != 0 {
		t.Fatalf("foreign Release = %d, %v, want 0 and ErrNotFound", released, err)
	}
	if outcomes, err := bob.service.Claim(ctx, bob.owner, aliceTask.ID, []string{aliceCandidate}); !errors.Is(err, intake.ErrNotFound) || outcomes != nil {
		t.Fatalf("foreign Claim = %v, %v, want nil and ErrNotFound", outcomes, err)
	}
	if err := bob.service.Validate(ctx, bob.owner, bobTask.ID, "Candidates", 1); err == nil {
		t.Fatal("a validated task was validated again")
	}
	if err := bob.service.Revalidate(ctx, bob.owner, aliceTask.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign Revalidate = %v, want ErrNotFound", err)
	}
	// Bob still lists and manages his own tasks.
	if tasks, err := bob.service.ListTasks(ctx, bob.owner, bob.campaign); err != nil || len(tasks) != 1 {
		t.Fatalf("bob's tasks = %v, %v, want his single task", tasks, err)
	}
}

// validateFor stages and validates one workbook under another operator.
func (e *env) validateFor(o *secondOperator, data []byte) *intake.Task {
	e.t.Helper()
	ctx := context.Background()
	task, err := o.service.Stage(ctx, o.owner, o.campaign, "book.xlsx", data, "")
	if err != nil {
		e.t.Fatalf("stage for %s: %v", o.owner, err)
	}
	if err := o.service.Validate(ctx, o.owner, task.ID, "Candidates", 1); err != nil {
		e.t.Fatalf("validate for %s: %v", o.owner, err)
	}
	loaded, err := o.service.LoadTask(ctx, o.owner, task.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return loaded
}

func (e *env) rowsFor(o *secondOperator, taskID string) []intake.RowSnapshot {
	e.t.Helper()
	rows, err := o.service.Rows(context.Background(), o.owner, taskID)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// TestWriteGuardBlocksNewSendingDataWrites proves the guard runs at the start
// of Stage, Validate, Revalidate and Claim and its error is returned unchanged,
// while reads stay open.
func TestWriteGuardBlocksNewSendingDataWrites(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	task := env.stage(mixedWorkbook(t), "book.xlsx")

	guardErr := errors.New("a mailbox replacement is in progress")
	blocked := 0
	env.service.SetWriteGuard(func(ctx context.Context, ownerID string) error {
		if ownerID != env.owner {
			t.Errorf("guard owner = %q, want %q", ownerID, env.owner)
		}
		blocked++
		return guardErr
	})

	if _, err := env.service.Stage(ctx, env.owner, env.campaign, "blocked.xlsx", mixedWorkbook(t), ""); !errors.Is(err, guardErr) {
		t.Fatalf("stage under guard = %v, want the guard error unchanged", err)
	}
	if err := env.service.Validate(ctx, env.owner, task.ID, "Candidates", 1); !errors.Is(err, guardErr) {
		t.Fatalf("validate under guard = %v, want the guard error unchanged", err)
	}
	if _, err := env.service.Claim(ctx, env.owner, task.ID, []string{"any"}); !errors.Is(err, guardErr) {
		t.Fatalf("claim under guard = %v, want the guard error unchanged", err)
	}
	if err := env.service.Revalidate(ctx, env.owner, task.ID); !errors.Is(err, guardErr) {
		t.Fatalf("revalidate under guard = %v, want the guard error unchanged", err)
	}
	if blocked != 4 {
		t.Fatalf("guard calls = %d, want 4", blocked)
	}
	// Nothing was written by a guarded call.
	if loaded := env.mustLoad(task.ID); loaded.Status != intake.TaskStaging {
		t.Fatalf("status = %q, want staging after guarded writes", loaded.Status)
	}
	if got := env.candidateCount(); got != 0 {
		t.Fatalf("candidate records = %d, want none", got)
	}
	// Reads are not guarded: the staged task still lists.
	if tasks, err := env.service.ListTasks(ctx, env.owner, env.campaign); err != nil || len(tasks) != 1 {
		t.Fatalf("listed tasks = %v, %v, want the staged task", tasks, err)
	}
	// Once the reset finishes, writes flow again.
	env.service.SetWriteGuard(nil)
	if err := env.service.Validate(ctx, env.owner, task.ID, "Candidates", 1); err != nil {
		t.Fatalf("validate after the guard lifted: %v", err)
	}
}

// A task read model exposes the original filename, an immutable upload time
// and a stable id reference, and never another operator's task.
func TestTaskReadModelKeepsUploadIdentity(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	workbook := buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"ada@example.com", "Ada Lovelace"},
	}})
	first := env.stage(workbook, "recipients.xlsx")
	second := env.stage(workbook, "recipients.xlsx")
	if first.ID == second.ID || first.SourceName != "recipients.xlsx" || second.SourceName != "recipients.xlsx" {
		t.Fatalf("two same-name uploads are not distinguishable: %+v / %+v", first, second)
	}
	if first.UploadedAt.IsZero() || second.UploadedAt.IsZero() {
		t.Fatal("upload time was not exposed on the task read model")
	}
	if err := env.service.Validate(ctx, env.owner, first.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	validated := env.mustLoad(first.ID)
	if !validated.UploadedAt.Equal(first.UploadedAt) {
		t.Fatalf("upload time changed on validation: %v then %v", first.UploadedAt, validated.UploadedAt)
	}
	if validated.SourceName != first.SourceName || validated.ContentHash == "" {
		t.Fatalf("validated read model lost identity: %+v", validated)
	}

	other := newOperator(t, env.app, "other-intake@bachtelcorporation.com")
	if _, err := env.service.LoadTask(ctx, other, first.ID); !errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("foreign task load error = %v, want ErrNotFound", err)
	}
}

// An enabled future schedule authorizes the current snapshot only. Revalidation
// is refused inside the publishing transaction and leaves the snapshot intact.
func TestRevalidateRefusedWhileScheduleEnabled(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	task := env.validate(buildWorkbook(t, sheetSpec{name: "Candidates", rows: [][]string{
		{"Email", "Applicant Full Name"},
		{"carol@example.com", "Carol Danvers"},
	}}))
	original := env.mustLoad(task.ID)

	// A newer revision would change the snapshot on revalidation.
	revision, _, err := env.campaigns.CreateRevision(ctx, env.owner, env.campaign, campaign.RevisionInput{
		Subject:      "Interview for {{candidate_name}}",
		Document:     docxFixture(t),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
			{Variable: "candidate_name", Header: "Applicant Full Name", Required: true, Format: campaign.FormatName},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	record, err := env.app.FindRecordById(intake.TasksCollection, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("schedule_state", intake.ScheduleScheduled)
	record.Set("schedule_at", original.UploadedAt.Add(24*time.Hour))
	if err := env.app.Save(record); err != nil {
		t.Fatal(err)
	}

	err = env.service.Revalidate(ctx, env.owner, task.ID)
	var validation *intake.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "clear the scheduled start") {
		t.Fatalf("revalidate error = %v, want the clear-schedule refusal", err)
	}
	unchanged := env.mustLoad(task.ID)
	if unchanged.ContentHash != original.ContentHash || unchanged.Counts.Total != original.Counts.Total {
		t.Fatalf("refused revalidation changed the snapshot: %+v -> %+v", original, unchanged)
	}
	if rows := env.rows(task.ID); len(rows) != len(env.rows(task.ID)) || len(rows) == 0 {
		t.Fatal("refused revalidation dropped the membership snapshot")
	}

	record, err = env.app.FindRecordById(intake.TasksCollection, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("schedule_state", intake.ScheduleNone)
	record.Set("schedule_at", "")
	if err := env.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := env.service.Revalidate(ctx, env.owner, task.ID); err != nil {
		t.Fatal(err)
	}
	if got := env.mustLoad(task.ID).ContentHash; got != revision.ContentHash {
		t.Fatalf("revalidated hash = %q, want the current revision", got)
	}
}
