// Package reset performs the destructive mailbox replacement reset: it drains
// the owner's in-flight submissions, deletes exactly the sending data disclosed
// to the operator, removes the corresponding private files, and only then
// activates the replacement connection.
//
// The reset is durable: an interruption leaves the workspace blocked and the
// operation is resumed on the next start. It never deletes another operator's
// records, campaign definitions, configuration revisions or template documents.
package reset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/yash91989201/bachtel-email-automation/internal/exports"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/mailbox"
	"github.com/yash91989201/bachtel-email-automation/internal/sending"
)

// Drainer reports how many submissions the operator's mailbox still has in
// flight. A reset waits for them to settle before deleting their records.
type Drainer interface {
	InFlightFor(ctx context.Context, ownerID string) (int, error)
}

// Service owns replacement confirmation, deletion and recovery.
type Service struct {
	app    core.App
	conns  *mailbox.Store
	drain  Drainer
	poll   func(time.Duration) <-chan time.Time
	settle time.Duration
	// change refuses a replacement confirmation while the owning account has
	// pending tasks. The mailbox claim and reset records already exist; the
	// reset must not erase blockers to install a new mailbox.
	change func(ctx context.Context, ownerID string) error
}

// NewService builds the reset service.
func NewService(app core.App, connections *mailbox.Store) *Service {
	return &Service{
		app:    app,
		conns:  connections,
		poll:   time.After,
		settle: 30 * time.Second,
	}
}

// SetDrainer binds the dispatcher's per-owner in-flight query.
func (s *Service) SetDrainer(drainer Drainer) { s.drain = drainer }

// SetChangeGuard binds the account-wide pending-task check. A confirmed
// replacement must never run while any task remains pending.
func (s *Service) SetChangeGuard(fn func(ctx context.Context, ownerID string) error) { s.change = fn }

// ErrDraining reports submissions that did not settle within the bounded drain.
var ErrDraining = errors.New("active submissions are still settling; retry the replacement once they finish")

// Blocking reports whether the owner's workspace is blocked by an in-progress
// reset. A staged proposal does not block anything until it is confirmed.
func Blocking(app core.App, ownerID string) (bool, error) {
	_, err := app.FindFirstRecordByFilter(
		mailbox.ResetsCollection,
		"owner = {:owner} && status = {:status}",
		dbx.Params{"owner": ownerID, "status": mailbox.ResetResetting},
	)
	if err != nil && !isNoRows(err) {
		return false, err
	}
	return err == nil, nil
}

// Guard returns the write-guard closure used by intake and sending so no new
// sending data or launches are accepted while a reset runs.
func Guard(app core.App) func(ctx context.Context, ownerID string) error {
	return func(ctx context.Context, ownerID string) error {
		blocked, err := Blocking(app, ownerID)
		if err != nil {
			return err
		}
		if blocked {
			return errors.New("a mailbox replacement is in progress; finish it before changing sending data")
		}
		return nil
	}
}

// Confirm runs a staged replacement to completion.
func (s *Service) Confirm(ctx context.Context, ownerID, resetID string) error {
	if s.change != nil {
		if err := s.change(ctx, ownerID); err != nil {
			return err
		}
	}
	operation, err := s.conns.ConfirmReset(ctx, ownerID, resetID)
	if err != nil {
		return err
	}
	return s.run(ctx, operation)
}

// Recover resumes every unfinished reset. A staged proposal is left for the
// operator; a confirmed reset always completes.
func (s *Service) Recover(ctx context.Context) error {
	unfinished, err := s.conns.UnfinishedResets(ctx)
	if err != nil {
		return err
	}
	for _, operation := range unfinished {
		if operation.Status != mailbox.ResetResetting {
			continue
		}
		if err := s.run(ctx, operation); err != nil {
			// Remaining blocked is the safe outcome; report it without
			// pretending the replacement completed.
			return err
		}
	}
	return nil
}

func (s *Service) run(ctx context.Context, operation *mailbox.Reset) error {
	if err := s.drainOwner(ctx, operation.OwnerID); err != nil {
		s.recordError(ctx, operation.ID, err.Error())
		return err
	}

	manifest, err := s.conns.ReadResetManifest(ctx, operation.ID)
	if err != nil {
		return err
	}
	if !manifest.RecordsDeleted {
		files, err := s.deleteRecords(ctx, operation)
		if err != nil {
			s.recordError(ctx, operation.ID, "The sending data could not be deleted; the workspace stays blocked until this is retried.")
			return err
		}
		manifest.Files = files
		manifest.RecordsDeleted = true
		if err := s.conns.SetResetManifest(ctx, operation.ID, manifest); err != nil {
			s.recordError(ctx, operation.ID, "The deletion progress could not be recorded; the workspace stays blocked until this is retried.")
			return err
		}
	}

	if err := s.cleanupFiles(manifest); err != nil {
		s.recordError(ctx, operation.ID, "Deleted sending files could not be removed; the workspace stays blocked until this is retried.")
		return err
	}
	if err := s.conns.ActivateReset(ctx, operation.ID); err != nil {
		s.recordError(ctx, operation.ID, "The replacement mailbox could not be activated; the workspace stays blocked until this is retried.")
		return err
	}
	return nil
}

// drainOwner waits for the owner's submissions to settle. New launches are
// already refused because the reset is in the resetting state.
func (s *Service) drainOwner(ctx context.Context, ownerID string) error {
	if s.drain == nil {
		return nil
	}
	deadline := time.Now().Add(s.settle)
	for {
		n, err := s.drain.InFlightFor(ctx, ownerID)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (%d in flight)", ErrDraining, n)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.poll(200 * time.Millisecond):
		}
	}
}

// deleteRecords removes exactly the disclosed sending-data records in one short
// transaction and returns the private file directories derived before deletion.
func (s *Service) deleteRecords(ctx context.Context, operation *mailbox.Reset) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerID := operation.OwnerID
	var files []string
	err := s.app.RunInTransaction(func(app core.App) error {
		files = nil
		// Derive the file lists from owned records before deleting them so a
		// shared campaign or template directory is never touched.
		collections := []struct {
			name   string
			filter string
			params dbx.Params
			file   bool
		}{
			{intake.TasksCollection, "campaign.owner = {:owner}", dbx.Params{"owner": ownerID}, true},
			{exports.ArtifactsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}, true},
		}
		for _, spec := range collections {
			records, err := app.FindRecordsByFilter(spec.name, spec.filter, "", 0, 0, spec.params)
			if err != nil {
				return err
			}
			for _, record := range records {
				if spec.file {
					files = append(files, record.BaseFilesPath())
				}
			}
		}

		for _, spec := range []struct {
			name   string
			filter string
			params dbx.Params
		}{
			{sending.ResolutionsCollection, "member.task.campaign.owner = {:owner}", dbx.Params{"owner": ownerID}},
			{sending.ApprovalsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}},
			{sending.ActionsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}},
			{sending.AttemptsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}},
			{intake.TaskCandidatesCollection, "task.campaign.owner = {:owner}", dbx.Params{"owner": ownerID}},
			{intake.TasksCollection, "campaign.owner = {:owner}", dbx.Params{"owner": ownerID}},
			{intake.CandidatesCollection, "campaign.owner = {:owner}", dbx.Params{"owner": ownerID}},
			{exports.ArtifactsCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}},
			{sending.DispatcherCollection, "owner = {:owner}", dbx.Params{"owner": ownerID}},
		} {
			records, err := app.FindRecordsByFilter(spec.name, spec.filter, "", 0, 0, spec.params)
			if err != nil {
				return err
			}
			for _, record := range records {
				if err := app.Delete(record); err != nil {
					return fmt.Errorf("delete %s record %s: %w", spec.name, record.Id, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// cleanupFiles removes the recorded private files. It is idempotent: an already
// removed directory is not an error, so a retried reset finishes cleanly.
func (s *Service) cleanupFiles(manifest mailbox.ResetManifest) error {
	storage := filepath.Join(s.app.DataDir(), "storage")
	for _, base := range manifest.Files {
		if base == "" || filepath.IsAbs(base) {
			continue
		}
		clean := filepath.Clean(base)
		if clean == "." || clean == ".." || len(clean) >= 2 && clean[:2] == ".." {
			continue
		}
		target := filepath.Join(storage, clean)
		if err := os.RemoveAll(target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordError(ctx context.Context, resetID, reason string) {
	if err := s.conns.SetResetError(ctx, resetID, reason); err != nil {
		s.app.Logger().Error("record mailbox replacement failure", "reset", resetID, "error", err)
	}
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
