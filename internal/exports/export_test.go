package exports_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/xuri/excelize/v2"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"

	"github.com/yash91989201/bachtel-email-automation/internal/config"
	"github.com/yash91989201/bachtel-email-automation/internal/convert"
	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

var safeEmailEntry = regexp.MustCompile(`^emails/[A-Za-z0-9_-]+\.html$`)

type exportFixture struct {
	app        core.App
	service    *exports.Service
	store      *sending.Store
	intake     *intake.Service
	campaignID string
	now        time.Time
	cfg        config.Config
}

func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	return newExportFixtureWithLabels(t, nil)
}

func newExportFixtureWithLabels(t *testing.T, labels []string) *exportFixture {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.ClearBootstrap(); err != nil {
			t.Error(err)
		}
	})
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(t.TempDir(), "pandoc")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor arg in \"$@\"; do\nif [ \"$arg\" = \"--version\" ]; then echo 'pandoc export'; exit 0; fi\ndone\nprintf '%s' '<p>Hello {{candidate_name}}</p>'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	campaigns := campaign.NewServiceWithConverter(app, convert.Converter{Path: stub})
	created, err := campaigns.CreateCampaign(context.Background(), campaign.CreateCampaignInput{Name: "Export fixture", SourceHeaders: []string{"Email", "Name"}, ResultLabels: labels})
	if err != nil {
		t.Fatal(err)
	}
	var doc bytes.Buffer
	z := zip.NewWriter(&doc)
	part, err := z.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p/></w:body></w:document>`)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = campaigns.CreateRevision(context.Background(), created.ID, campaign.RevisionInput{
		Subject:      "Hello {{candidate_name}}",
		Document:     doc.Bytes(),
		DocumentName: "body.docx",
		Mappings: []campaign.Mapping{
			{Variable: "candidate_email", Header: "Email", Recipient: true, Format: campaign.FormatEmail},
			{Variable: "candidate_name", Header: "Name", Required: true, Format: campaign.FormatName},
		},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 2, 30, 0, 0, time.UTC)
	cfg := config.Config{PandocPath: stub, Microsoft: config.Microsoft{SenderAddress: "hr@example.invalid"}}
	fixture := &exportFixture{
		app:        app,
		service:    exports.NewService(app, cfg),
		store:      sending.NewStore(app, sending.Options{AllowCandidateSending: true, SenderAddress: "hr@example.invalid", Now: func() time.Time { return now }}),
		intake:     intake.NewService(app, campaigns, intake.Limits{}),
		campaignID: created.ID,
		now:        now,
		cfg:        cfg,
	}
	return fixture
}

// stage uploads a workbook with a Candidates sheet plus a second worksheet, so
// the retained original layout is exercised.
func (f *exportFixture) stage(t *testing.T, rows [][]string) string {
	t.Helper()
	book := excelize.NewFile()
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name"}); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if err := book.SetSheetRow("Candidates", fmt.Sprintf("A%d", i+2), &row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := book.NewSheet("Notes"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Notes", "A1", &[]string{"Original note", "Kept"}); err != nil {
		t.Fatal(err)
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.intake.Stage(context.Background(), f.campaignID, "export.xlsx", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.intake.Validate(context.Background(), task.ID, "Candidates", 1); err != nil {
		t.Fatal(err)
	}
	return task.ID
}

// mixedTask creates five eligible rows, one duplicate pair and one unusable
// address, then drives the real store to produce a mix of recorded outcomes.
func (f *exportFixture) mixedTask(t *testing.T) string {
	t.Helper()
	rows := [][]string{
		{"alice@example.invalid", "Alice"},
		{"bob@example.invalid", "Bob =SUM(A1:A2)"},
		{"carol@example.invalid", "Carol"},
		{"dave@example.invalid", "Dave"},
		{"erin@example.invalid", "Erin"},
		{"alice@example.invalid", "Alice duplicate"},
		{"not-an-address", "Broken"},
	}
	task := f.stage(t, rows)
	f.approve(t, task)
	act := f.act(t, task, "start", "mixed-start", 4)
	if act.Claimed != 4 {
		t.Fatalf("claimed %d, want 4", act.Claimed)
	}
	// Two submissions, one definite failure, one uncertain outcome.
	f.drain(t, map[string]sending.Outcome{
		"carol@example.invalid": {State: sending.AttemptFailed, Reason: "mailbox unavailable"},
		"dave@example.invalid":  {State: sending.AttemptReview, Reason: "connection lost"},
	})
	// Retries are refused while the task owns active work: stop and settle
	// first, exactly as the sending page requires.
	f.act(t, task, "stop", "stop-mixed", 0)
	if err := f.store.SettleTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Act(context.Background(), sending.ActionRequest{ActionID: "retry-carol", TaskID: task, Kind: "retry", MemberID: f.memberID(t, task, "carol@example.invalid"), Actor: "operator", Confirmed: true, ExpectedCount: 1}); err != nil {
		t.Fatal(err)
	}
	f.drain(t, map[string]sending.Outcome{})
	req := sending.ActionRequest{ActionID: "resolve-dave", TaskID: task, Kind: "resolve", MemberID: f.memberID(t, task, "dave@example.invalid"), Actor: "operator", Basis: "verified in the provider sent folder", Confirmed: true, ExpectedCount: 1}
	if _, err := f.store.Act(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SettleTasks(context.Background()); err != nil {
		t.Fatal(err)
	}
	return task
}

func (f *exportFixture) memberID(t *testing.T, task, email string) string {
	t.Helper()
	candidates, err := f.app.FindRecordsByFilter(intake.CandidatesCollection, "campaign={:c} && email_key={:e}", "", 1, 0, dbx.Params{"c": f.campaignID, "e": email})
	if err != nil || len(candidates) == 0 {
		t.Fatalf("candidate %s: %v", email, err)
	}
	members, err := f.app.FindRecordsByFilter(intake.TaskCandidatesCollection, "task={:t} && candidate={:c}", "", 1, 0, dbx.Params{"t": task, "c": candidates[0].Id})
	if err != nil || len(members) == 0 {
		t.Fatalf("member %s: %v", email, err)
	}
	return members[0].Id
}

func (f *exportFixture) approve(t *testing.T, task string) {
	t.Helper()
	ctx := context.Background()
	a, err := f.store.QueueTest(ctx, sending.TestRequest{ActionID: "test-" + task, TaskID: task, Recipient: "operator@example.invalid", Values: map[string]string{"candidate_email": "sample@example.invalid", "candidate_name": "Sample"}})
	if err != nil {
		t.Fatal(err)
	}
	attempt := f.begin(t, a.ID)
	if err = f.store.CompleteAttempt(ctx, attempt.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Approve(ctx, task, attempt.ID, "operator", "approve-"+task); err != nil {
		t.Fatal(err)
	}
}

func (f *exportFixture) begin(t *testing.T, wantID string) *sending.Attempt {
	t.Helper()
	ctx := context.Background()
	for {
		w, err := f.store.NextWork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if w == nil {
			t.Fatalf("no work for %s", wantID)
		}
		a, err := f.store.BeginAttempt(ctx, w, f.now, f.now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if a.ID == wantID || wantID == "" {
			return a
		}
		if err = f.store.CompleteAttempt(ctx, a.ID, sending.Outcome{State: sending.AttemptSubmitted}); err != nil {
			t.Fatal(err)
		}
	}
}

// drain completes every available piece of work, choosing the outcome by
// recipient so a test can force one failure and one uncertain result.
func (f *exportFixture) drain(t *testing.T, outcomes map[string]sending.Outcome) int {
	t.Helper()
	ctx := context.Background()
	done := 0
	for {
		w, err := f.store.NextWork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if w == nil {
			return done
		}
		a, err := f.store.BeginAttempt(ctx, w, f.now, f.now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		outcome := sending.Outcome{State: sending.AttemptSubmitted}
		if chosen, ok := outcomes[w.Recipient]; ok {
			outcome = chosen
		}
		if err = f.store.CompleteAttempt(ctx, a.ID, outcome); err != nil {
			t.Fatal(err)
		}
		done++
	}
}

func (f *exportFixture) act(t *testing.T, task, kind, id string, count int) *sending.ActionResult {
	t.Helper()
	result, err := f.store.Act(context.Background(), sending.ActionRequest{ActionID: id, TaskID: task, Kind: kind, Actor: "operator", Confirmed: true, ExpectedCount: count})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// readStoredFile reads one private PocketBase file field for a test that needs
// to restore it after simulating a storage failure.
func (f *exportFixture) readStoredFile(t *testing.T, record *core.Record, name string) []byte {
	t.Helper()
	fsys, err := f.app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fsys.Close(); err != nil {
			t.Error(err)
		}
	}()
	reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *exportFixture) removeStoredFile(t *testing.T, record *core.Record, name string) {
	t.Helper()
	fsys, err := f.app.NewFilesystem()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fsys.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = fsys.Delete(record.BaseFilesPath() + "/" + name); err != nil {
		t.Fatal(err)
	}
}

func (f *exportFixture) records(t *testing.T, collection, filter string) []*core.Record {
	t.Helper()
	rows, err := f.app.FindRecordsByFilter(collection, filter, "created", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (f *exportFixture) attemptsFingerprint(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, a := range f.records(t, sending.AttemptsCollection, "") {
		b.WriteString(a.Id)
		b.WriteByte('|')
		b.WriteString(a.GetString("kind"))
		b.WriteByte('|')
		b.WriteString(a.GetString("state"))
		b.WriteByte('|')
		b.WriteString(a.GetString("recipient"))
		b.WriteByte('|')
		b.WriteString(a.GetString("subject"))
		b.WriteByte('|')
		b.WriteString(a.GetString("body"))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestTaskResultsWorkbookPreservesSourceAndReportsOutcomes(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	before := f.attemptsFingerprint(t)

	if err := f.service.GenerateTask(context.Background(), task, false); err != nil {
		t.Fatal(err)
	}
	if after := f.attemptsFingerprint(t); after != before {
		t.Fatal("generating the workbook changed recorded attempts")
	}
	// Repeated generation is a no-op on the database snapshot and cannot
	// create a second attempt.
	if err := f.service.GenerateTask(context.Background(), task, false); err != nil {
		t.Fatal(err)
	}
	if after := f.attemptsFingerprint(t); after != before {
		t.Fatal("repeated generation changed recorded attempts")
	}

	data, name, err := f.service.DownloadTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".xlsx") {
		t.Fatalf("unexpected download name %q", name)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()

	sheets := book.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Candidates" || sheets[1] != "Notes" {
		t.Fatalf("original worksheets changed: %v", sheets)
	}
	note, err := book.GetCellValue("Notes", "A1")
	if err != nil || note != "Original note" {
		t.Fatalf("second worksheet value lost: %q %v", note, err)
	}
	headers, err := book.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) < 8 {
		t.Fatalf("expected every original row, got %d", len(headers))
	}
	if headers[0][0] != "Email" || headers[0][1] != "Name" {
		t.Fatalf("original header columns changed: %v", headers[0])
	}
	if headers[0][2] != "Email Status" || headers[0][3] != "Submitted At" || headers[0][4] != "Email Error" {
		t.Fatalf("result labels missing: %v", headers[0])
	}
	if headers[1][1] != "Alice" {
		t.Fatalf("original row value changed: %v", headers[1])
	}
	status := func(row int) string {
		value, err := book.GetCellValue("Candidates", fmt.Sprintf("C%d", row))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if status(3) != "Submitted" || status(4) != "Submitted" {
		t.Fatalf("submitted rows wrong: %q %q", status(3), status(4))
	}
	if status(5) != "Submitted (manual resolution)" {
		t.Fatalf("manual resolution not distinct: %q", status(5))
	}
	if status(6) != "Submitted" {
		t.Fatalf("fourth eligible row wrong: %q", status(6))
	}
	if status(2) != "Excluded - duplicate" || status(7) != "Excluded - duplicate" || status(8) != "Excluded - invalid address" {
		t.Fatalf("excluded rows wrong: %q %q %q", status(2), status(7), status(8))
	}
	at, err := book.GetCellValue("Candidates", "D3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(at, "+05:30") {
		t.Fatalf("timestamp missing India offset: %q", at)
	}
	manual, err := book.GetCellValue("Candidates", "E5")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manual, "provider sent folder") {
		t.Fatalf("manual resolution basis missing: %q", manual)
	}
}

func TestTaskResultsKeepSourceColumnsAndCollisionFreeLabels(t *testing.T) {
	f := newExportFixtureWithLabels(t, []string{"Email", "Name", "Status"})
	task := f.stage(t, [][]string{{"collision@example.invalid", "Collision"}})
	if _, err := f.app.DB().NewQuery("UPDATE send_tasks SET status='completed' WHERE id={:id}").Bind(map[string]any{"id": task}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateTask(context.Background(), task, false); err != nil {
		t.Fatal(err)
	}
	data, _, err := f.service.DownloadTask(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	got := rows[0]
	want := []string{"Email", "Name", "Email (2)", "Name (2)", "Status"}
	if len(got) < len(want) {
		t.Fatalf("header row = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("header row = %v, want %v", got, want)
		}
	}
	if email, err := book.GetCellValue("Candidates", "A2"); err != nil || email != "collision@example.invalid" {
		t.Fatalf("original column overwritten: %q %v", email, err)
	}
}

func TestAttemptHTMLUsesStoredContentAfterTemplateChange(t *testing.T) {
	f := newExportFixture(t)
	f.mixedTask(t)
	attempts := f.records(t, sending.AttemptsCollection, "kind='candidate'")
	if len(attempts) == 0 {
		t.Fatal("no candidate attempts")
	}
	attempt := attempts[0]
	storedSubject := attempt.GetString("subject")
	storedBody := attempt.GetString("body")
	configuration, err := f.app.FindRecordById(campaign.ConfigurationsCollection, attempt.GetString("configuration"))
	if err != nil {
		t.Fatal(err)
	}
	configuration.Set("subject", "Changed subject")
	configuration.Set("body_html", "<p>Changed body</p>")
	if err = f.app.Save(configuration); err != nil {
		t.Fatal(err)
	}
	data, _, err := f.service.AttemptHTML(context.Background(), attempt.Id)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	if !strings.Contains(html, storedSubject) || !strings.Contains(html, storedBody) {
		t.Fatal("historical content changed after a template edit")
	}
	if strings.Contains(html, "Changed subject") || strings.Contains(html, "Changed body") {
		t.Fatal("attempt HTML used the current template instead of stored content")
	}
}

func TestCampaignReportSeparatesTestsAndHistory(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	if err := f.service.GenerateCampaign(context.Background(), f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	report, _, err := f.service.DownloadCampaign(context.Background(), f.campaignID, exports.KindCampaignReport)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()

	reportRows, err := book.GetRows("Report")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, row := range reportRows {
		if len(row) >= 2 {
			values[row[0]] = row[1]
		}
	}
	if values["Candidates"] != "4" {
		t.Fatalf("candidate total inflated: %v", values)
	}
	if values["Test attempts (excluded from candidate totals)"] != "1" {
		t.Fatalf("test attempts not separated: %v", values)
	}
	if values["Email tasks"] != "1" {
		t.Fatalf("task total wrong: %v", values)
	}
	if !strings.Contains(values["As of"], "+05:30") {
		t.Fatalf("as-of missing India offset: %q", values["As of"])
	}

	candidates, err := book.GetRows("Candidates")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 5 {
		t.Fatalf("expected one row per candidate, got %d", len(candidates)-1)
	}
	submitted := 0
	for _, row := range candidates[1:] {
		if strings.Contains(strings.Join(row, " "), "alice@example.invalid") {
			t.Fatalf("candidate recipient missing: %v", row)
		}
		if row[2] == "submitted" {
			submitted++
		}
	}
	if submitted != 4 {
		t.Fatalf("submitted candidate count %d, want 4", submitted)
	}

	attempts, err := book.GetRows("Attempts")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 7 {
		t.Fatalf("attempt history rows %d, want 6 attempts", len(attempts)-1)
	}
	kinds := map[string]int{}
	for _, row := range attempts[1:] {
		kinds[row[1]]++
	}
	if kinds["test"] != 1 || kinds["candidate"] != 5 {
		t.Fatalf("attempt kinds wrong: %v", kinds)
	}
	// A formula-like subject stored on an attempt stays text in the report.
	if _, err := f.app.DB().NewQuery("UPDATE submission_attempts SET subject = '=SUM(A1:A2)' WHERE kind='test'").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateCampaign(context.Background(), f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	report, _, err = f.service.DownloadCampaign(context.Background(), f.campaignID, exports.KindCampaignReport)
	if err != nil {
		t.Fatal(err)
	}
	book2, err := excelize.OpenReader(bytes.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book2.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book2.GetRows("Attempts")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, row := range rows[1:] {
		if row[1] == "test" {
			cellType, err := book2.GetCellType("Attempts", fmt.Sprintf("G%d", i+2))
			if err != nil {
				t.Fatal(err)
			}
			if cellType == excelize.CellTypeFormula {
				t.Fatal("formula-like subject was promoted to a formula")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("test attempt row missing from the report")
	}
	_ = task
}

func TestCampaignPackageIsIndexedAndInert(t *testing.T) {
	f := newExportFixture(t)
	f.mixedTask(t)
	// Hostile metadata must stay inert in the generated HTML.
	if _, err := f.app.DB().NewQuery("UPDATE submission_attempts SET subject = '<script>alert(1)</script>'").Execute(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateCampaign(context.Background(), f.campaignID, true); err != nil {
		t.Fatal(err)
	}
	data, name, err := f.service.DownloadCampaign(context.Background(), f.campaignID, exports.KindCampaignPackage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".zip") {
		t.Fatalf("unexpected package name %q", name)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]*zip.File{}
	for _, file := range archive.File {
		if strings.Contains(file.Name, "..") || strings.HasPrefix(file.Name, "/") {
			t.Fatalf("unsafe archive entry %q", file.Name)
		}
		if strings.HasPrefix(file.Name, "emails/") && !safeEmailEntry.MatchString(file.Name) {
			t.Fatalf("email entry %q does not use a stable attempt id", file.Name)
		}
		entries[file.Name] = file
	}
	if _, ok := entries["report.xlsx"]; !ok {
		t.Fatal("package is missing the report")
	}
	index, ok := entries["index.csv"]
	if !ok {
		t.Fatal("package is missing the index")
	}
	indexReader, err := index.Open()
	if err != nil {
		t.Fatal(err)
	}
	indexBytes, err := io.ReadAll(indexReader)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(indexBytes)), "\n")
	if len(lines) != 1+6 {
		t.Fatalf("index rows %d, want 6 plus header", len(lines)-1)
	}
	attempts := f.records(t, sending.AttemptsCollection, "")
	if len(attempts) == 0 {
		t.Fatal("no attempts recorded")
	}
	for _, attempt := range attempts {
		entry, ok := entries["emails/"+attempt.Id+".html"]
		if !ok {
			t.Fatalf("attempt %s missing from the package", attempt.Id)
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		html, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(html, []byte("<script>alert(1)</script>")) {
			t.Fatal("hostile stored metadata was not escaped")
		}
		if !bytes.Contains(html, []byte("&lt;script&gt;alert(1)&lt;/script&gt;")) {
			t.Fatalf("expected escaped metadata in the downloaded HTML: %.400s", html)
		}
		if !bytes.Contains(html, []byte("Recorded email attempt")) {
			t.Fatal("attempt HTML missing its wrapper")
		}
	}
}

func TestExportFailureNeverTouchesAttemptsAndIsRetryable(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	before := f.attemptsFingerprint(t)

	// Remove the retained manifest file from storage so generation fails after
	// it claims the record, mirroring a write failure with no incomplete file
	// published.
	record, err := f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	manifestName := record.GetString("manifest")
	manifestBytes := f.readStoredFile(t, record, manifestName)
	f.removeStoredFile(t, record, manifestName)
	if err = f.service.GenerateTask(context.Background(), task, true); err == nil {
		t.Fatal("generation succeeded without the retained manifest")
	}
	if after := f.attemptsFingerprint(t); after != before {
		t.Fatal("failed generation changed recorded attempts")
	}
	if _, _, err = f.service.DownloadTask(context.Background(), task); !errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("failed generation published a download: %v", err)
	}
	artifact, err := f.service.TaskArtifact(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if artifact == nil || artifact.State != exports.StateFailed || artifact.Ready {
		t.Fatalf("artifact not marked failed: %+v", artifact)
	}

	// Restoring the retained artifact and retrying publishes the file.
	record, err = f.app.FindRecordById(intake.TasksCollection, task)
	if err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.NewFileFromBytes(manifestBytes, manifestName)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("manifest", file)
	if err = f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err = f.service.GenerateTask(context.Background(), task, true); err != nil {
		t.Fatalf("retry after the write failure did not publish: %v", err)
	}
	if _, _, err = f.service.DownloadTask(context.Background(), task); err != nil {
		t.Fatalf("retried generation published no download: %v", err)
	}
}

func TestInterruptedGenerationCanBeRetried(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	if _, err := f.app.DB().NewQuery("UPDATE send_tasks SET status='interrupted' WHERE id={:id}").Bind(map[string]any{"id": task}).Execute(); err != nil {
		t.Fatal(err)
	}
	if err := f.service.GenerateTask(context.Background(), task, false); err != nil {
		t.Fatal(err)
	}
	artifact, err := f.service.TaskArtifact(context.Background(), task)
	if err != nil || artifact == nil {
		t.Fatalf("artifact missing: %v", err)
	}
	record, err := f.app.FindRecordById(exports.ArtifactsCollection, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.Set("state", exports.StateGenerating)
	record.Set("generation_started", f.now.Add(-20*time.Minute))
	if err = f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	if err = f.service.GenerateTask(context.Background(), task, true); err != nil {
		t.Fatalf("stale generation could not be retried: %v", err)
	}
	if _, _, err = f.service.DownloadTask(context.Background(), task); err != nil {
		t.Fatalf("retried generation published no file: %v", err)
	}
}

func TestGeneratedFileDeletionKeepsHistoryAndRegenerates(t *testing.T) {
	f := newExportFixture(t)
	task := f.mixedTask(t)
	if err := f.service.GenerateTask(context.Background(), task, false); err != nil {
		t.Fatal(err)
	}
	before := f.attemptsFingerprint(t)
	candidates := len(f.records(t, intake.CandidatesCollection, ""))
	if err := f.service.DeleteTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.DownloadTask(context.Background(), task); !errors.Is(err, exports.ErrNotReady) {
		t.Fatalf("deleted export still downloadable: %v", err)
	}
	if after := f.attemptsFingerprint(t); after != before {
		t.Fatal("deleting the generated file changed attempts")
	}
	if len(f.records(t, intake.CandidatesCollection, "")) != candidates {
		t.Fatal("deleting the generated file changed candidate eligibility rows")
	}
	if err := f.service.GenerateTask(context.Background(), task, true); err != nil {
		t.Fatalf("deleted workbook could not be regenerated: %v", err)
	}
	if _, _, err := f.service.DownloadTask(context.Background(), task); err != nil {
		t.Fatalf("regenerated workbook unavailable: %v", err)
	}
}
