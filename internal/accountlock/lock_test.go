package accountlock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/yash91989201/bachtel-email-automation/internal/accountlock"
	_ "github.com/yash91989201/bachtel-email-automation/internal/migrations"
	"github.com/yash91989201/bachtel-email-automation/internal/operator"
	"github.com/yash91989201/bachtel-email-automation/internal/testutil"
)

func newApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := testutil.NewApp(t)
	return app
}

func provision(t *testing.T, app core.App, email string) string {
	t.Helper()
	if err := operator.Provision(app, email, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	record, err := app.FindAuthRecordByEmail(operator.CollectionName, email)
	if err != nil {
		t.Fatal(err)
	}
	return record.Id
}

func campaign(t *testing.T, app core.App, owner, name string) string {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId("campaigns")
	if err != nil {
		t.Fatal(err)
	}
	record := core.NewRecord(collection)
	record.Set("owner", owner)
	record.Set("name", name)
	record.Set("recipient_header", "Email")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}
	return record.Id
}

// task inserts a task row directly. The intake service requires a readable
// workbook, while the lock predicate reads only durable status and closure
// markers, so a direct row keeps this test focused.
func task(t *testing.T, app core.App, campaignID, status, closureRequestedAt, closedAt string) string {
	t.Helper()
	id := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	now := types.NowDateTime().String()
	_, err := app.DB().NewQuery("INSERT INTO send_tasks (id, campaign, status, source_name, schedule_state, closure_requested_at, closed_at, created, updated) VALUES ({:id}, {:campaign}, {:status}, 'book.xlsx', 'none', {:closure}, {:closed}, {:now}, {:now})").
		Bind(dbx.Params{"id": id, "campaign": campaignID, "status": status, "closure": closureRequestedAt, "closed": closedAt, "now": now}).
		Execute()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPendingTaskStates(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "owner@example.test")
	other := provision(t, app, "other@example.test")
	main := campaign(t, app, owner, "Campaign A")
	second := campaign(t, app, owner, "Campaign B")
	foreign := campaign(t, app, other, "Foreign")

	closedAt := types.NowDateTime().String()
	task(t, app, main, "staging", "", "")
	task(t, app, main, "ready", "", "")
	task(t, app, main, "paused", "", "")
	task(t, app, main, "stopped", "", "")
	task(t, app, second, "interrupted", "", "")
	closed := task(t, app, main, "completed", "", closedAt)
	completed := task(t, app, main, "completed", "", "")
	closing := task(t, app, main, "stopping", types.NowDateTime().String(), "")
	foreignTask := task(t, app, foreign, "ready", "", "")

	blockers, err := accountlock.Pending(context.Background(), app, owner)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]accountlock.Blocker{}
	for _, blocker := range blockers {
		seen[blocker.TaskID] = blocker
	}
	if len(blockers) != 6 {
		t.Fatalf("blockers = %d, want 6: %+v", len(blockers), blockers)
	}
	for _, id := range []string{closed} {
		if _, ok := seen[id]; ok {
			t.Fatalf("closed task %s still blocks", id)
		}
	}
	if _, ok := seen[completed]; ok {
		t.Fatalf("completed task without outstanding work blocks")
	}
	if _, ok := seen[closing]; !ok {
		t.Fatalf("closing task must block")
	}
	if _, ok := seen[foreignTask]; ok {
		t.Fatalf("another operator's task leaked into blockers")
	}
	names := map[string]bool{}
	for _, blocker := range blockers {
		if blocker.Reference == "" || blocker.Filename == "" {
			t.Fatalf("blocker lacks display identity: %+v", blocker)
		}
		names[blocker.CampaignName] = true
	}
	if !names["Campaign A"] || !names["Campaign B"] {
		t.Fatalf("campaign names missing: %v", names)
	}

	// The guard carries the same named blockers.
	err = accountlock.Check(context.Background(), app, owner)
	var blocked *accountlock.BlockedError
	if !errors.As(err, &blocked) || len(blocked.Blockers) != 6 {
		t.Fatalf("check err = %v", err)
	}
	if err := accountlock.Check(context.Background(), app, other); err == nil {
		// The foreign campaign task is pending for its own owner.
		t.Fatal("foreign owner should be blocked by its own task")
	}
}

func TestOutstandingOperationKeepsCompletedTaskPending(t *testing.T) {
	app := newApp(t)
	owner := provision(t, app, "owner@example.test")
	main := campaign(t, app, owner, "Campaign")
	done := task(t, app, main, "completed", "", "")
	now := types.NowDateTime().String()
	attemptID := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	_, err := app.DB().NewQuery("INSERT INTO submission_attempts (id, owner, task, kind, state, content_hash, recipient, subject, created) VALUES ({:id}, {:owner}, {:task}, 'test', 'queued', 'hash', 'a@example.test', 'subject', {:now})").
		Bind(dbx.Params{"id": attemptID, "owner": owner, "task": done, "now": now}).
		Execute()
	if err != nil {
		t.Fatal(err)
	}
	blockers, err := accountlock.Pending(context.Background(), app, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].TaskID != done {
		t.Fatalf("outstanding queued operation did not keep the task pending: %+v", blockers)
	}
	if _, err := app.DB().NewQuery("UPDATE submission_attempts SET state = 'submitted' WHERE id = {:id}").
		Bind(dbx.Params{"id": attemptID}).Execute(); err != nil {
		t.Fatal(err)
	}
	blockers, err = accountlock.Pending(context.Background(), app, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Fatalf("settled operation still blocks: %+v", blockers)
	}
}

var _ = time.Now
