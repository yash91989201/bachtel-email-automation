// Package accountlock answers the account-wide connection-change lock: a task
// is pending from durable upload or task creation until execution is completed
// or an explicit close is final, and outstanding queued or in-flight task
// operations keep a completed task pending until they settle.
//
// The predicate is derived from stored records on every check. A caller's
// cached page state is never authority; activation rechecks inside its own
// transaction.
package accountlock

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Message is the fixed warning shown beside both connection sections.
const Message = "Mailbox and AI connection changes are locked until all pending tasks are completed or explicitly closed."

// Blocker is one pending task preventing a connection change. It carries only
// the owning operator's own task metadata.
type Blocker struct {
	TaskID       string
	CampaignID   string
	CampaignName string
	Filename     string
	Status       string
	Reference    string
	// Closing reports a confirmed Close that still waits for in-flight work.
	Closing bool
}

// BlockedError reports refused connection changes and their named blockers.
type BlockedError struct {
	Blockers []Blocker
}

// Error is fixed operator-safe text; the named tasks render separately.
func (e *BlockedError) Error() string { return Message }

// Blockers returns the named tasks from err, if it is a lock refusal.
func Blockers(err error) []Blocker {
	if blocked, ok := errors.AsType[*BlockedError](err); ok {
		return blocked.Blockers
	}
	return nil
}

// Check refuses a connection change while any owning-account task is pending.
// It returns *BlockedError with the named blockers.
func Check(ctx context.Context, app core.App, ownerID string) error {
	blockers, err := Pending(ctx, app, ownerID)
	if err != nil {
		return err
	}
	if len(blockers) == 0 {
		return nil
	}
	return &BlockedError{Blockers: blockers}
}

// Guard adapts Check to services that store a context-and-app check callback.
func Guard(app core.App) func(ctx context.Context, ownerID string) error {
	return func(ctx context.Context, ownerID string) error {
		return Check(ctx, app, ownerID)
	}
}

// Pending lists the owning operator's pending tasks in creation order. Tasks
// never leak across operators.
func Pending(ctx context.Context, app core.App, ownerID string) ([]Blocker, error) {
	tasks, err := app.FindRecordsByFilter(
		intake.TasksCollection,
		"campaign.owner = {:owner}",
		"created,id",
		0,
		0,
		dbx.Params{"owner": ownerID},
	)
	if err != nil {
		return nil, err
	}
	outstanding, err := outstandingTaskIDs(app, ownerID)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	var blockers []Blocker
	for _, task := range tasks {
		closing := task.GetString("closure_requested_at") != ""
		closed := task.GetString("closed_at") != ""
		status := task.GetString("status")
		pending := !closed && (status != presentation.StatusCompleted || closing || outstanding[task.Id])
		if !pending {
			continue
		}
		campaignID := task.GetString("campaign")
		name, ok := names[campaignID]
		if !ok {
			if campaign, err := app.FindRecordById("campaigns", campaignID); err == nil {
				name = campaign.GetString("name")
			}
			names[campaignID] = name
		}
		blockers = append(blockers, Blocker{
			TaskID:       task.Id,
			CampaignID:   campaignID,
			CampaignName: name,
			Filename:     task.GetString("source_name"),
			Status:       status,
			Reference:    presentation.ShortRef(task.Id),
			Closing:      closing,
		})
	}
	return blockers, nil
}

// outstandingTaskIDs reports tasks with queued or in-flight attempts, or with
// a member reservation that still holds work. Export generation is not a
// connection lock.
func outstandingTaskIDs(app core.App, ownerID string) (map[string]bool, error) {
	out := map[string]bool{}
	attempts, err := app.FindRecordsByFilter(
		sending.AttemptsCollection,
		"owner = {:owner} && (state = {:queued} || state = {:in_flight})",
		"",
		0,
		0,
		dbx.Params{"owner": ownerID, "queued": sending.AttemptQueued, "in_flight": sending.AttemptInFlight},
	)
	if err != nil {
		return nil, err
	}
	for _, attempt := range attempts {
		out[attempt.GetString("task")] = true
	}
	members, err := app.FindRecordsByFilter(
		intake.TaskCandidatesCollection,
		"task.campaign.owner = {:owner} && (dispatch_state = 'reserved' || dispatch_state = 'in_flight')",
		"",
		0,
		0,
		dbx.Params{"owner": ownerID},
	)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		out[member.GetString("task")] = true
	}
	return out, nil
}
