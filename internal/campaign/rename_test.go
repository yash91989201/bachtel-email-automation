package campaign_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/campaign"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// dumpTable returns every row of one table ordered by id, so a before/after
// comparison covers the whole stored state, not a hand-picked column list.
func dumpTable(t *testing.T, app core.App, table string, params dbx.Params) []dbx.NullStringMap {
	t.Helper()
	query := app.DB().NewQuery("SELECT * FROM " + table + " ORDER BY id")
	if params != nil {
		query = query.Bind(params)
	}
	var rows []dbx.NullStringMap
	if err := query.All(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// insertCampaignTask stores one send_tasks row directly, mirroring the
// lifecycle shapes the intake tests exercise: staging, ready, scheduled,
// running, completed and closed.
func insertCampaignTask(t *testing.T, app core.App, id, campaignID, revisionID, status, scheduleState, name, closedAt string) {
	t.Helper()
	scheduleAt := ""
	if scheduleState != intake.ScheduleNone {
		scheduleAt = "2026-11-02 09:30:00.000Z"
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO send_tasks
			(id, campaign, configuration, status, source, source_name, schedule_at, schedule_state, name, closed_at, created, updated)
		VALUES
			({:id}, {:campaign}, {:configuration}, {:status}, 'source.xlsx', 'source.xlsx', {:schedule_at}, {:schedule_state}, {:name}, {:closed_at}, '2026-10-20 08:00:00.000Z', '2026-10-20 08:00:00.000Z')
	`).Bind(dbx.Params{
		"id":             id,
		"campaign":       campaignID,
		"configuration":  revisionID,
		"status":         status,
		"schedule_at":    scheduleAt,
		"schedule_state": scheduleState,
		"name":           name,
		"closed_at":      closedAt,
	}).Execute()
	if err != nil {
		t.Fatal(err)
	}
}

// TestRenameCampaignAppliesNameRules covers the name rules end to end: blank
// input is refused with the required-name error, whitespace is trimmed on
// save, duplicates are legal because identity is the record id, and the
// length bound counts Unicode code points so a name the browser measured in
// UTF-16 units still fits.
func TestRenameCampaignAppliesNameRules(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()
	owner := newOperator(t, app, "rename-rules@bachtelcorporation.com")

	created, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Original Name",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, blank := range []string{"", "   ", "\t\n"} {
		if err := service.RenameCampaign(ctx, owner, created.ID, blank); !errors.Is(err, campaign.ErrCampaignNameRequired) {
			t.Fatalf("RenameCampaign(%q) = %v, want ErrCampaignNameRequired", blank, err)
		}
		loaded, err := service.LoadCampaign(ctx, owner, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Name != "Original Name" {
			t.Fatalf("refused rename replaced the name with %q", loaded.Name)
		}
	}

	if err := service.RenameCampaign(ctx, owner, created.ID, "  October outreach  "); err != nil {
		t.Fatal(err)
	}
	trimmed, err := service.LoadCampaign(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.Name != "October outreach" {
		t.Fatalf("renamed to %q, want the trimmed name", trimmed.Name)
	}

	// A second campaign may share the name: identity is the record id.
	second, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Twin",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RenameCampaign(ctx, owner, second.ID, "October outreach"); err != nil {
		t.Fatalf("duplicate name must be allowed: %v", err)
	}

	// A supplementary rune counts as one code point but two UTF-16 units and
	// four UTF-8 bytes: the bound must accept exactly 200 code points.
	atLimit := strings.Repeat("😀", campaign.MaxCampaignNameLength)
	if err := service.RenameCampaign(ctx, owner, created.ID, atLimit); err != nil {
		t.Fatalf("a %d-code-point name was refused: %v", campaign.MaxCampaignNameLength, err)
	}
	loaded, err := service.LoadCampaign(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != atLimit {
		t.Fatalf("stored name has %d runes, want exactly %d", len([]rune(loaded.Name)), campaign.MaxCampaignNameLength)
	}

	tooLong := atLimit + "😀"
	if err := service.RenameCampaign(ctx, owner, created.ID, tooLong); !errors.Is(err, campaign.ErrCampaignNameTooLong) {
		t.Fatalf("RenameCampaign(201 code points) = %v, want ErrCampaignNameTooLong", err)
	}
	loaded, err = service.LoadCampaign(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != atLimit {
		t.Fatalf("over-long rename replaced the name with %q", loaded.Name)
	}
}

// TestRenameCampaignRequiresOwnership proves both refusals are the same
// ErrNotFound and neither writes anything, so an id can never be probed.
func TestRenameCampaignRequiresOwnership(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()
	owner := newOperator(t, app, "rename-owner@bachtelcorporation.com")
	foreign := newOperator(t, app, "rename-stranger@bachtelcorporation.com")

	created, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Private Campaign",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})

	if err := service.RenameCampaign(ctx, owner, "missing000000000", "Renamed"); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("renaming a missing campaign = %v, want ErrNotFound", err)
	}
	if err := service.RenameCampaign(ctx, foreign, created.ID, "Renamed"); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("a foreign owner renaming = %v, want ErrNotFound", err)
	}
	if err := service.RenameCampaign(ctx, "", created.ID, "Renamed"); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("a missing owner renaming = %v, want ErrNotFound", err)
	}

	after := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a refused rename changed the campaign row:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestRenameCampaignStorageFailureKeepsPriorName installs a SQLite trigger
// that aborts the column-scoped UPDATE, so the transaction rolls back and the
// prior name stays authoritative.
func TestRenameCampaignStorageFailureKeepsPriorName(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()
	owner := newOperator(t, app, "rename-failure@bachtelcorporation.com")

	created, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Kept Name",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().NewQuery(`
		CREATE TRIGGER fail_campaign_name_update BEFORE UPDATE OF name ON campaigns
		BEGIN SELECT RAISE(ABORT, 'storage failure'); END
	`).Execute(); err != nil {
		t.Skipf("this SQLite build cannot install a failure trigger: %v", err)
	}
	if err := service.RenameCampaign(ctx, owner, created.ID, "Lost Name"); err == nil {
		t.Fatal("rename under a failing store reported success")
	}
	if _, err := app.DB().NewQuery("DROP TRIGGER fail_campaign_name_update").Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadCampaign(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Kept Name" {
		t.Fatalf("name after failed storage = %q, want the prior name", loaded.Name)
	}
}

// TestRenameCampaignCancelledContextWritesNothing checks the early context
// gate: a cancelled request errors before the transaction and leaves the
// stored row untouched.
func TestRenameCampaignCancelledContextWritesNothing(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	owner := newOperator(t, app, "rename-cancelled@bachtelcorporation.com")

	created, err := service.CreateCampaign(context.Background(), owner, campaign.CreateCampaignInput{
		Name:          "Untouched",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.RenameCampaign(ctx, owner, created.ID, "Renamed"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rename = %v, want context.Canceled", err)
	}
	after := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a cancelled rename changed the campaign row:\nbefore=%+v\nafter=%+v", before, after)
	}
}

// TestRenameCampaignUpdatesOnlyNameMetadata pins the update to two columns:
// name changes and updated moves, while every other stored column of the
// campaign row stays byte-identical.
func TestRenameCampaignUpdatesOnlyNameMetadata(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()
	owner := newOperator(t, app, "rename-metadata@bachtelcorporation.com")

	created, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Before",
		SourceHeaders: []string{"Email"},
		ResultLabels:  []string{"Status"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Force a stale updated value so the new timestamp definitely differs.
	if _, err := app.DB().NewQuery("UPDATE campaigns SET updated = {:stale} WHERE id = {:id}").
		Bind(dbx.Params{"stale": "2020-01-02 03:04:05.000Z", "id": created.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	before := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})

	if err := service.RenameCampaign(ctx, owner, created.ID, "After"); err != nil {
		t.Fatal(err)
	}
	after := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("expected one campaign row before and after, got %d and %d", len(before), len(after))
	}
	for column, beforeValue := range before[0] {
		afterValue := after[0][column]
		switch column {
		case "name", "updated":
			if reflect.DeepEqual(beforeValue, afterValue) {
				t.Errorf("%s did not change: %v", column, beforeValue)
			}
		default:
			if !reflect.DeepEqual(beforeValue, afterValue) {
				t.Errorf("%s changed from %+v to %+v", column, beforeValue, afterValue)
			}
		}
	}
}

// TestRenameCampaignPreservesChildRecords builds representative evidence
// around one campaign — a lifecycle spread of tasks, an approved content
// revision, a candidate with its duplicate-prevention history, a submission
// attempt and an export artifact — and proves a rename touches nothing but
// the campaign row's name and updated columns. The service's converter path
// is unusable in these tests, so any provider/converter call would fail.
func TestRenameCampaignPreservesChildRecords(t *testing.T) {
	t.Parallel()
	app := newTestApp(t)
	service := newService(t, app)
	ctx := context.Background()
	owner := newOperator(t, app, "rename-preserve@bachtelcorporation.com")

	created, err := service.CreateCampaign(ctx, owner, campaign.CreateCampaignInput{
		Name:          "Preservation Campaign",
		SourceHeaders: []string{"Email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	revisionID := storeRevision(t, app, created.ID, 1, "Subject line", "<p>body</p>", []campaign.Mapping{
		{Variable: "email", Header: "Email", Required: true, Recipient: true},
	}, "content-hash-1")
	if _, err := app.DB().NewQuery("UPDATE campaign_configurations SET approved_hash = content_hash, approved_at = '2026-10-20 09:00:00.000Z' WHERE id = {:id}").
		Bind(dbx.Params{"id": revisionID}).Execute(); err != nil {
		t.Fatal(err)
	}

	insertCampaignTask(t, app, "taskstaging0000", created.ID, revisionID, intake.TaskStaging, intake.ScheduleNone, "staging task", "")
	insertCampaignTask(t, app, "taskready00000", created.ID, revisionID, intake.TaskReady, intake.ScheduleNone, "ready task", "")
	insertCampaignTask(t, app, "tasksched00000", created.ID, revisionID, intake.TaskReady, intake.ScheduleScheduled, "scheduled task", "")
	insertCampaignTask(t, app, "taskrunning000", created.ID, revisionID, "running", intake.ScheduleFired, "running task", "")
	insertCampaignTask(t, app, "taskcomplet000", created.ID, revisionID, "completed", intake.ScheduleFired, "completed task", "")
	insertCampaignTask(t, app, "taskclosed0000", created.ID, revisionID, "completed", intake.ScheduleCancelled, "closed task", "2026-10-21 10:00:00.000Z")

	if _, err := app.DB().NewQuery(`
		INSERT INTO campaign_candidates (id, campaign, email_key, submission_state, created, updated)
		VALUES ('candidatex0000', {:campaign}, 'person@example.com', 'none', '2026-10-20 08:00:00.000Z', '2026-10-20 08:00:00.000Z')
	`).Bind(dbx.Params{"campaign": created.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO task_candidates (id, task, candidate, source_row, state, raw, normalized, dispatch_state, created)
		VALUES ('taskcandidat000', 'taskready00000', 'candidatex0000', 2, 'eligible', '{"Email":"person@example.com"}', '{"Email":"person@example.com"}', 'pending', '2026-10-20 08:00:00.000Z')
	`).Execute(); err != nil {
		t.Fatal(err)
	}

	if _, err := app.DB().NewQuery(`
		INSERT INTO mail_connections (id, owner, provider, sender, status, generation, created, updated)
		VALUES ('connection0000', {:owner}, 'google', 'sender@example.com', 'connected', 1, '2026-10-19 08:00:00.000Z', '2026-10-19 08:00:00.000Z')
	`).Bind(dbx.Params{"owner": owner}).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO submission_attempts (id, owner, connection, connection_generation, task, candidate, configuration, kind, state, content_hash, recipient, subject, created)
		VALUES ('attempt0000000', {:owner}, 'connection0000', 1, 'taskcomplet000', 'candidatex0000', {:configuration}, 'candidate', 'sent', 'content-hash-1', 'person@example.com', 'Subject line', '2026-10-20 09:00:00.000Z')
	`).Bind(dbx.Params{"owner": owner, "configuration": revisionID}).Execute(); err != nil {
		t.Fatal(err)
	}

	if _, err := app.DB().NewQuery(`
		INSERT INTO export_artifacts (id, owner, kind, campaign, task, state, as_of, digest, created, updated)
		VALUES ('export00000000', {:owner}, 'task_results', {:campaign}, 'taskcomplet000', 'ready', '2026-10-20 10:00:00.000Z', 'digest-1', '2026-10-20 10:00:00.000Z', '2026-10-20 10:00:00.000Z')
	`).Bind(dbx.Params{"owner": owner, "campaign": created.ID}).Execute(); err != nil {
		t.Fatal(err)
	}

	// Force a stale updated value so the new timestamp definitely differs,
	// then snapshot every child table plus the campaign row.
	if _, err := app.DB().NewQuery("UPDATE campaigns SET updated = {:stale} WHERE id = {:id}").
		Bind(dbx.Params{"stale": "2020-01-02 03:04:05.000Z", "id": created.ID}).Execute(); err != nil {
		t.Fatal(err)
	}
	childTables := []string{
		campaign.ConfigurationsCollection,
		intake.TasksCollection,
		intake.CandidatesCollection,
		intake.TaskCandidatesCollection,
		"submission_attempts",
		"export_artifacts",
	}
	before := map[string][]dbx.NullStringMap{}
	for _, table := range childTables {
		before[table] = dumpTable(t, app, table, nil)
		if len(before[table]) == 0 {
			t.Fatalf("fixture inserted no rows into %s", table)
		}
	}
	campaignBefore := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})

	if err := service.RenameCampaign(ctx, owner, created.ID, "Renamed Campaign"); err != nil {
		t.Fatal(err)
	}

	for _, table := range childTables {
		after := dumpTable(t, app, table, nil)
		if !reflect.DeepEqual(before[table], after) {
			t.Errorf("%s changed across the rename:\nbefore=%+v\nafter=%+v", table, before[table], after)
		}
	}
	campaignAfter := dumpTable(t, app, campaign.CampaignsCollection, dbx.Params{"id": created.ID})
	if len(campaignBefore) != 1 || len(campaignAfter) != 1 {
		t.Fatalf("expected one campaign row before and after, got %d and %d", len(campaignBefore), len(campaignAfter))
	}
	for column, beforeValue := range campaignBefore[0] {
		afterValue := campaignAfter[0][column]
		switch column {
		case "name", "updated":
			if reflect.DeepEqual(beforeValue, afterValue) {
				t.Errorf("%s did not change: %v", column, beforeValue)
			}
		default:
			if !reflect.DeepEqual(beforeValue, afterValue) {
				t.Errorf("%s changed from %+v to %+v", column, beforeValue, afterValue)
			}
		}
	}

	// A result-label change made after the rename is a separate column
	// update; a later rename must not clobber it or the revision pointer.
	if err := service.UpdateResultLabels(ctx, owner, created.ID, []string{"Outcome"}); err != nil {
		t.Fatal(err)
	}
	if err := service.RenameCampaign(ctx, owner, created.ID, "Renamed Again"); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.LoadCampaign(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ResultLabels) != 1 || loaded.ResultLabels[0] != "Outcome" {
		t.Errorf("result labels after a later rename = %v, want [Outcome]", loaded.ResultLabels)
	}
	if loaded.CurrentRevisionID != revisionID {
		t.Errorf("current configuration after a later rename = %q, want %q", loaded.CurrentRevisionID, revisionID)
	}
	configuration, err := service.CurrentConfiguration(ctx, owner, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.ApprovalCurrent() {
		t.Error("content approval did not survive the renames")
	}
}
