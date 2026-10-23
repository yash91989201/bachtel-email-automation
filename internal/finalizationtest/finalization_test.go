package finalizationtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/xuri/excelize/v2"

	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/finalization"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/preparation"
)

type fixture struct {
	app       core.App
	owner     string
	campaign  string
	config    string
	task      string
	service   *finalization.Service
	campaigns *campaign.Service
}

func newFixture(t *testing.T) *fixture {
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
	f := &fixture{app: app}
	f.owner = f.createOperator(t, "snapshot@example.test")
	f.campaign, f.config = f.createCampaign(t)
	f.task = f.createTask(t)
	campaigns := campaign.NewService(app, "")
	tasks := intake.NewService(app, campaigns, intake.DefaultLimits())
	f.campaigns = campaigns
	f.service = finalization.NewService(app, tasks, campaigns, preparation.NewService(app, tasks, campaigns, nil))
	return f
}

func (f *fixture) createOperator(t *testing.T, email string) string {
	t.Helper()
	collection, err := f.app.FindCollectionByNameOrId(operator.CollectionName)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.SetEmail(email)
	record.SetPassword("correct-horse-battery")
	record.SetVerified(true)
	if err := f.app.Save(record); err != nil {
		t.Fatalf("save operator: %v", err)
	}
	return record.Id
}

func (f *fixture) createCampaign(t *testing.T) (campaignID, configurationID string) {
	t.Helper()
	campaigns, err := f.app.FindCollectionByNameOrId(campaign.CampaignsCollection)
	if err != nil {
		t.Fatal(err)
	}
	campaignRecord := core.NewRecord(campaigns)
	campaignRecord.Set("owner", f.owner)
	campaignRecord.Set("name", "Snapshot campaign")
	if err := f.app.Save(campaignRecord); err != nil {
		t.Fatalf("save campaign: %v", err)
	}
	configurations, err := f.app.FindCollectionByNameOrId(campaign.ConfigurationsCollection)
	if err != nil {
		t.Fatal(err)
	}
	document, err := filesystem.NewFileFromBytes([]byte("document"), "body.docx")
	if err != nil {
		t.Fatal(err)
	}
	configuration := core.NewRecord(configurations)
	configuration.Set("campaign", campaignRecord.Id)
	configuration.Set("docx", document)
	configuration.Set("revision", 1)
	configuration.Set("subject", "Hello {{candidate_name}}")
	configuration.Set("body_html", "<p>Dear {{candidate_name}}, your role is {{role}}.</p>")
	configuration.Set("content_hash", "content-hash-1")
	configuration.Set("converter", "test")
	configuration.Set("mapping_version", campaign.CurrentMappingVersion)
	configuration.Set("mappings", []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "candidate_name", Header: "Name", Required: true},
		{Variable: "role", Header: "Role", Fallback: "Team member"},
	})
	if err := f.app.Save(configuration); err != nil {
		t.Fatalf("save configuration: %v", err)
	}
	campaignRecord.Set("current_configuration", configuration.Id)
	if err := f.app.Save(campaignRecord); err != nil {
		t.Fatalf("current configuration: %v", err)
	}
	return campaignRecord.Id, configuration.Id
}

func (f *fixture) createTask(t *testing.T) string {
	t.Helper()
	tasks, err := f.app.FindCollectionByNameOrId(intake.TasksCollection)
	if err != nil {
		t.Fatal(err)
	}
	book := excelize.NewFile()
	if err := book.SetSheetName("Sheet1", "Candidates"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A1", &[]string{"Email", "Name", "Role"}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSheetRow("Candidates", "A2", &[]string{"ada@example.com", "Ada", "Engineer"}); err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := filesystem.NewFileFromBytes(sourceBytes.Bytes(), "book.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(tasks)
	record.Set("campaign", f.campaign)
	record.Set("status", intake.TaskReady)
	record.Set("schedule_state", intake.ScheduleNone)
	record.Set("configuration", f.config)
	record.Set("content_hash", "content-hash-1")
	record.Set("source", source)
	record.Set("source_digest", "source-digest")
	record.Set("sheet", "Candidates")
	record.Set("header_row", 1)
	if err := f.app.Save(record); err != nil {
		t.Fatalf("save task: %v", err)
	}
	return record.Id
}

// addPreparedRun inserts one completed run, selects it and returns its id.
func (f *fixture) addPreparedRun(t *testing.T, rows []preparation.RowResult) string {
	t.Helper()
	output := preparation.Output{Version: preparation.OutputVersion, Rows: rows, Entries: []preparation.Entry{}}
	for _, row := range rows {
		output.Entries = append(output.Entries, entriesForRow(row)...)
	}
	output.Counts = countsForOutput(output)
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	file, err := filesystem.NewFileFromBytes(data, "preparation-output.json")
	if err != nil {
		t.Fatal(err)
	}
	runs, err := f.app.FindCollectionByNameOrId(preparation.RunsCollection)
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(runs)
	record.Set("owner", f.owner)
	record.Set("task", f.task)
	record.Set("kind", preparation.KindInitial)
	record.Set("state", preparation.RunCompleted)
	record.Set("provider", "custom")
	record.Set("api_root", "https://example.test/v1")
	record.Set("api_format", "chat_completions")
	record.Set("model", "test-model")
	record.Set("configuration_generation", 1)
	record.Set("credential_generation", 1)
	record.Set("configuration", f.config)
	record.Set("content_hash", "content-hash-1")
	record.Set("source_digest", "source-digest")
	record.Set("base_prompt_version", preparation.BasePromptVersion)
	record.Set("base_prompt", "base prompt")
	record.Set("input_digest", "input-digest")
	record.Set("output_digest", "output-digest")
	record.Set("output", file)
	if err := f.app.Save(record); err != nil {
		t.Fatal(err)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("selected_preparation_run", record.Id)
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

func entriesForRow(row preparation.RowResult) []preparation.Entry {
	shared := 0
	perPosition := map[int]int{}
	for _, finding := range row.Findings {
		if finding.Scope == preparation.ScopeRecipient {
			perPosition[finding.Position]++
			continue
		}
		shared++
	}
	var entries []preparation.Entry
	for index, token := range strings.Split(row.Values["candidate_email"], ",") {
		entry := preparation.Entry{SourceRow: row.SourceRow, Position: index + 1, Recipient: strings.TrimSpace(token), Outcome: preparation.OutcomeAccepted}
		if shared > 0 || perPosition[index+1] > 0 {
			entry.Outcome = preparation.OutcomeHeld
			entry.Reasons = []string{"held"}
		}
		entries = append(entries, entry)
	}
	return entries
}

func countsForOutput(output preparation.Output) preparation.Counts {
	counts := preparation.Counts{SourceRows: len(output.Rows), Entries: len(output.Entries)}
	for _, entry := range output.Entries {
		if entry.Outcome == preparation.OutcomeHeld {
			counts.Held++
			continue
		}
		counts.Accepted++
	}
	for _, row := range output.Rows {
		counts.Corrections += len(row.Corrections)
		counts.Findings += len(row.Findings)
	}
	return counts
}

type sheetRow struct {
	Values map[string]string
}

func (f *fixture) upload(t *testing.T, actionID string, rows []sheetRow) (*finalization.Snapshot, error) {
	t.Helper()
	configuration, err := f.campaigns.LoadConfiguration(context.Background(), f.owner, f.config)
	if err != nil {
		t.Fatal(err)
	}
	data, err := sendingSheet(configuration, rows)
	if err != nil {
		t.Fatal(err)
	}
	return f.service.AcceptUpload(context.Background(), f.owner, f.task, actionID, f.owner, "final.xlsx", data)
}

// sendingSheet writes the deterministic mapped-value layout a final upload
// reads.
func sendingSheet(configuration *campaign.Configuration, rows []sheetRow) (data []byte, err error) {
	file := excelize.NewFile()
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := file.SetSheetName("Sheet1", finalization.SendingSheetName); err != nil {
		return nil, err
	}
	labels := campaign.MappingLabels(configuration.Mappings)
	for index, label := range labels {
		cell, err := excelize.CoordinatesToCellName(index+1, 1)
		if err != nil {
			return nil, err
		}
		if err := file.SetCellStr(finalization.SendingSheetName, cell, label); err != nil {
			return nil, err
		}
	}
	for rowIndex, row := range rows {
		for column, mapping := range configuration.Mappings {
			cell, err := excelize.CoordinatesToCellName(column+1, rowIndex+2)
			if err != nil {
				return nil, err
			}
			if err := file.SetCellStr(finalization.SendingSheetName, cell, row.Values[mapping.Variable]); err != nil {
				return nil, err
			}
		}
	}
	buffer, err := file.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func TestApproveAsIsCombinesAIAndBackendReasons(t *testing.T) {
	f := newFixture(t)
	f.addPreparedRun(t, []preparation.RowResult{
		{SourceRow: 2, Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "  Ada   Lovelace  ", "role": "Engineer"}},
		{SourceRow: 3, Values: map[string]string{"candidate_email": "grace@example.com", "candidate_name": ""}},
		{SourceRow: 4, Values: map[string]string{"candidate_email": "linus@example.com", "candidate_name": "Linus"}},
		{SourceRow: 5, Values: map[string]string{"candidate_email": "dup@example.com, dup@example.com", "candidate_name": "Dup"}},
		{SourceRow: 6, Values: map[string]string{"candidate_email": "ai@example.com", "candidate_name": "AI"}, Findings: []preparation.Finding{{Row: 6, Field: "candidate_name", Header: "Name", Scope: preparation.ScopeShared, Note: "mismatch"}}},
	})
	snapshot, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "approve-as-is-123456", f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Kind != finalization.KindPrepared || snapshot.Counts.Accepted != 2 || snapshot.Counts.Duplicate != 2 || snapshot.Counts.MissingValue != 1 || snapshot.Counts.AIHeld != 1 {
		t.Fatalf("snapshot counts = %+v", snapshot.Counts)
	}
	for _, entry := range snapshot.Entries {
		if entry.SourceRow == 2 && entry.Values["candidate_name"] != "  Ada   Lovelace  " {
			t.Fatalf("populated prepared value rewritten: %q", entry.Values["candidate_name"])
		}
		if entry.SourceRow == 4 && entry.Values["role"] != "Team member" {
			t.Fatalf("optional fallback = %q, want the literal fallback", entry.Values["role"])
		}
	}
	aiReason := false
	for _, exclusion := range snapshot.Exclusions {
		if exclusion.SourceRow != 6 {
			continue
		}
		for _, reason := range exclusion.Reasons {
			aiReason = aiReason || reason.Source == finalization.SourceAI
		}
	}
	if !aiReason {
		t.Fatal("AI finding was not combined into the exclusion evidence")
	}
}

func TestUploadRejectsStructuralFailureAndSlashSeparator(t *testing.T) {
	f := newFixture(t)
	configuration, err := f.campaigns.LoadConfiguration(context.Background(), f.owner, f.config)
	if err != nil {
		t.Fatal(err)
	}
	data, err := sendingSheet(configuration, []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}}})
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.SetCellStr(finalization.SendingSheetName, "A1", "Email address"); err != nil {
		t.Fatal(err)
	}
	var malformed bytes.Buffer
	if err := book.Write(&malformed); err != nil {
		t.Fatal(err)
	}
	if err := book.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.AcceptUpload(context.Background(), f.owner, f.task, "bad-sheet-123456", f.owner, "bad.xlsx", malformed.Bytes())
	if _, ok := errors.AsType[*finalization.StructuralError](err); !ok {
		t.Fatalf("renamed label = %T %v, want StructuralError", err, err)
	}
	// Slash-separated addresses are invalid in a final upload; no AI repairs.
	snapshot, err := f.upload(t, "slash-list-123456", []sheetRow{{Values: map[string]string{"candidate_email": "a@example.com/b@example.com", "candidate_name": "A"}}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.Accepted != 0 || snapshot.Counts.Invalid != 1 {
		t.Fatalf("slash list = %+v", snapshot.Counts)
	}
}

func TestReplacementCreatesFreshIdentityAndZeroAcceptedSupersedes(t *testing.T) {
	f := newFixture(t)
	rows := []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada Lovelace"}}}
	first, err := f.upload(t, "replace-first-123456", rows)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := f.upload(t, "replace-first-123456", rows)
	if err != nil || repeated.ID != first.ID {
		t.Fatalf("repeated action = %v %v, want the same snapshot", repeated, err)
	}
	second, err := f.upload(t, "replace-second-123456", rows)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("identical replacement bytes reused the old identity")
	}
	if second.Digest != first.Digest {
		t.Fatalf("identical values changed the digest: %q %q", first.Digest, second.Digest)
	}
	selected, err := f.service.Selected(context.Background(), f.owner, f.task)
	if err != nil || selected.ID != second.ID {
		t.Fatalf("selected = %v %v, want the replacement", selected, err)
	}
	empty, err := f.upload(t, "replace-empty-123456", []sheetRow{{Values: map[string]string{"candidate_email": "not-an-address", "candidate_name": "Bad"}}})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Counts.Accepted != 0 || empty.Counts.Excluded != 1 {
		t.Fatalf("zero accepted = %+v", empty.Counts)
	}
	selected, err = f.service.Selected(context.Background(), f.owner, f.task)
	if err != nil || selected.ID != empty.ID {
		t.Fatalf("zero-accepted selection = %v %v", selected, err)
	}
	if _, err := f.service.AcceptUpload(context.Background(), f.owner, f.task, "replace-structural-1", f.owner, "bad.xlsx", []byte("not a workbook")); err == nil {
		t.Fatal("corrupt workbook accepted")
	}
	selected, err = f.service.Selected(context.Background(), f.owner, f.task)
	if err != nil || selected.ID != empty.ID {
		t.Fatalf("structural failure changed selection = %v %v", selected, err)
	}
}

// The expanded-entry bound is enforced on the final upload path before any
// candidate mutation.
func TestFinalUploadEnforcesRecipientEntryBound(t *testing.T) {
	f := newFixture(t)
	rows := make([]sheetRow, 0, intake.MaxRecipientEntries+1)
	for index := 0; index <= intake.MaxRecipientEntries; index++ {
		rows = append(rows, sheetRow{Values: map[string]string{
			"candidate_email": "recipient" + strconv.Itoa(index) + "@example.com",
			"candidate_name":  "Recipient",
		}})
	}
	if _, err := f.upload(t, "too-many-123456", rows); !errors.Is(err, finalization.ErrTooManyEntries) {
		t.Fatalf("over-bound upload = %v, want ErrTooManyEntries", err)
	}
}

func TestApproveAsIsIsIdempotentAndOwnerScoped(t *testing.T) {
	f := newFixture(t)
	f.addPreparedRun(t, []preparation.RowResult{{SourceRow: 2, Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}}})
	snapshot, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "approve-as-is-123456", f.owner)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "approve-as-is-123456", f.owner)
	if err != nil || again.ID != snapshot.ID {
		t.Fatalf("repeat approve = %v %v", again, err)
	}
	other := f.createOperator(t, "other@example.test")
	if _, err := f.service.ApproveAsIs(context.Background(), other, f.task, "approve-foreign-123456", other); !errors.Is(err, finalization.ErrNotFound) {
		t.Fatalf("foreign approve = %v, want ErrNotFound", err)
	}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("selected_preparation_run", "")
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApproveAsIs(context.Background(), f.owner, f.task, "approve-stale-123456", f.owner); !errors.Is(err, finalization.ErrNoPrepared) {
		t.Fatalf("stale approve = %v", err)
	}
}

func TestGuardRefusesScheduleAndStartedTasks(t *testing.T) {
	f := newFixture(t)
	rows := []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "candidate_name": "Ada"}}}
	task, err := f.app.FindRecordById(intake.TasksCollection, f.task)
	if err != nil {
		t.Fatal(err)
	}
	task.Set("schedule_state", intake.ScheduleScheduled)
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.upload(t, "guard-schedule-123456", rows); !errors.Is(err, finalization.ErrScheduled) {
		t.Fatalf("scheduled finalization = %v", err)
	}
	task.Set("schedule_state", intake.ScheduleNone)
	task.Set("status", "running")
	if err := f.app.Save(task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.upload(t, "guard-started-123456", rows); !errors.Is(err, finalization.ErrStarted) {
		t.Fatalf("started finalization = %v", err)
	}
}

func TestSharedHeaderLabelsRoundTrip(t *testing.T) {
	f := newFixture(t)
	config, err := f.app.FindRecordById(campaign.ConfigurationsCollection, f.config)
	if err != nil {
		t.Fatal(err)
	}
	config.Set("subject", "Hello {{name}}")
	config.Set("body_html", "<p>Expected: {{expected_name}}</p>")
	config.Set("mappings", []campaign.Mapping{
		{Variable: "candidate_email", Header: "Email", Recipient: true},
		{Variable: "name", Header: "Contact", Required: true},
		{Variable: "expected_name", Header: "Contact"},
	})
	if err := f.app.Save(config); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.upload(t, "shared-label-123456", []sheetRow{{Values: map[string]string{"candidate_email": "ada@example.com", "name": "Ada", "expected_name": "Ada Lovelace"}}})
	if err != nil {
		t.Fatal(err)
	}
	sheet, _, err := f.service.FinalSheet(context.Background(), f.owner, f.task, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(sheet))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := book.Close(); err != nil {
			t.Error(err)
		}
	}()
	rows, err := book.GetRows(finalization.SendingSheetName)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][1] != "Contact (name)" || rows[0][2] != "Contact (expected_name)" {
		t.Fatalf("export labels = %v", rows[0])
	}
	if rows[1][0] != "ada@example.com" || rows[1][1] != "Ada" || rows[1][2] != "Ada Lovelace" {
		t.Fatalf("exported values = %v", rows[1])
	}
}

func TestExclusionReportPreservesBeforeAndAfter(t *testing.T) {
	f := newFixture(t)
	snapshot, err := f.upload(t, "exclusion-report-123456", []sheetRow{
		{Values: map[string]string{"candidate_email": "dup@example.com", "candidate_name": "Ada"}},
		{Values: map[string]string{"candidate_email": "dup@example.com", "candidate_name": "Grace"}},
		{Values: map[string]string{"candidate_email": "bad-address", "candidate_name": "Linus"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, _, err := f.service.ExclusionSheet(context.Background(), f.owner, f.task, snapshot.ID)
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
	rows, err := book.GetRows("Excluded")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("report rows = %d, want header plus three: %v", len(rows), rows)
	}
	header := rows[0]
	index := func(label string) int {
		for i, value := range header {
			if value == label {
				return i
			}
		}
		t.Fatalf("report missing %q: %v", label, header)
		return -1
	}
	if rows[1][index("Reason source")] != "backend" || rows[1][index("Reason")] == "" {
		t.Fatalf("reason evidence = %v", rows[1])
	}
	if rows[1][index("Name (original)")] != "Ada" || rows[2][index("Name (original)")] != "Grace" {
		t.Fatalf("original values = %q / %q", rows[1][index("Name (original)")], rows[2][index("Name (original)")])
	}
	if rows[1][index("Name (proposed)")] != "Ada" {
		t.Fatalf("proposed value = %q", rows[1][index("Name (proposed)")])
	}
	other := f.createOperator(t, "other@example.test")
	if _, _, err := f.service.FinalSheet(context.Background(), other, f.task, snapshot.ID); !errors.Is(err, finalization.ErrNotFound) {
		t.Fatalf("foreign download = %v", err)
	}
}
