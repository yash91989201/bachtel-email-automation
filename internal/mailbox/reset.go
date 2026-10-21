package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Replacement/reset lifecycle. A replacement is staged before anything is
// deleted: the target connection is claimed (uniqueness constraints reject a
// duplicate), the operator confirms the exact scope, and only then does the
// destructive reset run. An interrupted reset stays visible as resetting until
// recovery finishes it.
const (
	// ResetStaging means the target is validated and claimed but not current.
	ResetStaging = "staging"
	// ResetResetting means the destructive reset is in progress or must be
	// recovered; the operator's workspace is blocked from sending.
	ResetResetting = "resetting"
	// ResetCompleted means the target is current and the old claim is released.
	ResetCompleted = "completed"
)

// Errors reported by the replacement lifecycle.
var (
	// ErrNoReset reports an operator without an unfinished replacement.
	ErrNoReset = errors.New("this workspace has no pending mailbox replacement")
	// ErrSameMailbox reports a proposed connection that is the current one; the
	// operator should reconnect instead.
	ErrSameMailbox = errors.New("this is the same mailbox; reconnect it instead of replacing it")
	// ErrResetInProgress reports an owner that must finish an earlier reset.
	ErrResetInProgress = errors.New("a mailbox replacement is already in progress for this workspace")
)

// Reset is one durable mailbox replacement operation.
type Reset struct {
	ID               string
	OwnerID          string
	FromConnectionID string
	ToConnectionID   string
	Status           string
	Error            string
	CreatedAt        time.Time
}

// ResetManifest is the exact retained/deleted scope disclosed to the operator
// and used by the idempotent cleanup.
type ResetManifest struct {
	// RecordsDeleted records that the record-deletion transaction committed.
	// Recovery resumes file cleanup from here instead of deleting twice.
	RecordsDeleted bool     `json:"records_deleted"`
	Files          []string `json:"files,omitempty"`
	Candidates     int      `json:"candidates"`
	Tasks          int      `json:"tasks"`
	Attempts       int      `json:"attempts"`
	Artifacts      int      `json:"artifacts"`
}

// StageReplacement validates a proposed different mailbox and reserves its
// claim, returning a staged reset. Nothing existing is changed.
func (s *Store) StageReplacement(ctx context.Context, candidate Candidate) (*Reset, error) {
	if err := validateCandidate(candidate); err != nil {
		return nil, err
	}
	var out *Reset
	err := s.app.RunInTransaction(func(app core.App) error {
		if _, err := app.FindFirstRecordByFilter(
			ResetsCollection,
			"owner = {:owner} && status != {:done}",
			dbx.Params{"owner": candidate.OwnerID, "done": ResetCompleted},
		); err == nil {
			return ErrResetInProgress
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		currentRecord, err := app.FindFirstRecordByFilter(
			ConnectionsCollection,
			"owner = {:owner} && current = true",
			dbx.Params{"owner": candidate.OwnerID},
		)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotConnected
		}
		if err != nil {
			return err
		}
		current := connectionFromRecord(currentRecord)
		if current.Sender == CanonicalSender(candidate.Sender) || (candidate.Identity != "" && current.Identity == candidate.Identity) {
			return ErrSameMailbox
		}
		if err := ClaimFree(app, candidate.Sender, candidate.Identity, current.ID); err != nil {
			return err
		}

		connections, err := app.FindCollectionByNameOrId(ConnectionsCollection)
		if err != nil {
			return err
		}
		target := core.NewRecord(connections)
		// Seal under the final id, exactly like Install.
		target.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
		target.Set("owner", candidate.OwnerID)
		target.Set("provider", string(candidate.Provider))
		target.Set("sender", CanonicalSender(candidate.Sender))
		target.Set("identity", candidate.Identity)
		target.Set("status", string(StatusConnected))
		target.Set("current", false)
		target.Set("generation", current.Generation+1)
		sealed, err := s.seal(candidate.OwnerID, candidate.Provider, target.Id, candidate.Credential)
		if err != nil {
			return err
		}
		target.Set("credential", sealed)
		target.Set("credential_expires_at", candidate.Credential.ExpiresAt.UTC())
		if err := app.Save(target); err != nil {
			if isUniqueViolation(err) {
				return ErrClaimed
			}
			return err
		}

		resets, err := app.FindCollectionByNameOrId(ResetsCollection)
		if err != nil {
			return err
		}
		record := core.NewRecord(resets)
		record.Set("owner", candidate.OwnerID)
		record.Set("from_connection", current.ID)
		record.Set("to_connection", target.Id)
		record.Set("status", ResetStaging)
		if err := app.Save(record); err != nil {
			return err
		}
		out = resetFromRecord(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ActiveReset returns the owner's unfinished replacement, or ErrNoReset.
func (s *Store) ActiveReset(ctx context.Context, ownerID string) (*Reset, error) {
	record, err := s.app.FindFirstRecordByFilter(
		ResetsCollection,
		"owner = {:owner} && status != {:done}",
		dbx.Params{"owner": ownerID, "done": ResetCompleted},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoReset
	}
	if err != nil {
		return nil, fmt.Errorf("read the mailbox replacement: %w", err)
	}
	return resetFromRecord(record), nil
}

// UnfinishedResets returns every reset that must be recovered or completed.
func (s *Store) UnfinishedResets(ctx context.Context) ([]*Reset, error) {
	records, err := s.app.FindRecordsByFilter(ResetsCollection, "status != {:done}", "created", 0, 0, dbx.Params{"done": ResetCompleted})
	if err != nil {
		return nil, fmt.Errorf("list mailbox replacements: %w", err)
	}
	resets := make([]*Reset, 0, len(records))
	for _, record := range records {
		resets = append(resets, resetFromRecord(record))
	}
	return resets, nil
}

// CancelReset abandons a staged replacement and releases the target claim.
// A reset that already began deleting is never cancelled: it must be recovered.
func (s *Store) CancelReset(ctx context.Context, ownerID, resetID string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ResetsCollection, resetID)
		if err != nil {
			return fmt.Errorf("read the mailbox replacement: %w", err)
		}
		if record.GetString("owner") != ownerID {
			return ErrNoReset
		}
		if record.GetString("status") != ResetStaging {
			return errors.New("this replacement already started; recovery must finish it")
		}
		if err := app.Delete(record); err != nil {
			return err
		}
		target, err := app.FindRecordById(ConnectionsCollection, record.GetString("to_connection"))
		if err != nil {
			return err
		}
		return app.Delete(target)
	})
}

// ConfirmReset moves a staged replacement into the destructive resetting state.
// The caller performs the deletion and activation through its own service.
func (s *Store) ConfirmReset(ctx context.Context, ownerID, resetID string) (*Reset, error) {
	var out *Reset
	err := s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ResetsCollection, resetID)
		if err != nil {
			return fmt.Errorf("read the mailbox replacement: %w", err)
		}
		if record.GetString("owner") != ownerID {
			return ErrNoReset
		}
		if record.GetString("status") != ResetStaging {
			return errors.New("this replacement was already confirmed")
		}
		record.Set("status", ResetResetting)
		if err := app.Save(record); err != nil {
			return err
		}
		out = resetFromRecord(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetResetManifest records progress so an interrupted reset resumes instead of
// repeating work or reporting success early.
func (s *Store) SetResetManifest(ctx context.Context, resetID string, manifest ResetManifest) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ResetsCollection, resetID)
		if err != nil {
			return fmt.Errorf("read the mailbox replacement: %w", err)
		}
		record.Set("manifest", manifest)
		return app.Save(record)
	})
}

// SetResetError records why a reset is blocked. The reset stays unfinished.
func (s *Store) SetResetError(ctx context.Context, resetID, reason string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ResetsCollection, resetID)
		if err != nil {
			return fmt.Errorf("read the mailbox replacement: %w", err)
		}
		record.Set("error", reason)
		return app.Save(record)
	})
}

// ActivateReset makes the target connection current, releases the old claim and
// completes the reset in one transaction.
func (s *Store) ActivateReset(ctx context.Context, resetID string) error {
	return s.app.RunInTransaction(func(app core.App) error {
		record, err := app.FindRecordById(ResetsCollection, resetID)
		if err != nil {
			return fmt.Errorf("read the mailbox replacement: %w", err)
		}
		if record.GetString("status") == ResetCompleted {
			return nil
		}
		target, err := app.FindRecordById(ConnectionsCollection, record.GetString("to_connection"))
		if err != nil {
			return err
		}
		owner := record.GetString("owner")
		previous, err := app.FindRecordsByFilter(ConnectionsCollection, "owner = {:owner} && current = true", "", 0, 0, dbx.Params{"owner": owner})
		if err != nil {
			return err
		}
		var replaced []*core.Record
		for _, old := range previous {
			if old.Id == target.Id {
				continue
			}
			old.Set("current", false)
			if err := app.Save(old); err != nil {
				return err
			}
			replaced = append(replaced, old)
		}
		target.Set("current", true)
		if err := app.Save(target); err != nil {
			return err
		}
		// Complete the reset while the replaced connection still exists: the
		// reset record references it, and PocketBase validates relations on
		// save. The claim is released immediately after.
		record.Set("status", ResetCompleted)
		record.Set("error", "")
		if err := app.Save(record); err != nil {
			return err
		}
		for _, old := range replaced {
			if err := app.Delete(old); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReadResetManifest decodes the recorded manifest.
func (s *Store) ReadResetManifest(ctx context.Context, resetID string) (ResetManifest, error) {
	record, err := s.app.FindRecordById(ResetsCollection, resetID)
	if err != nil {
		return ResetManifest{}, fmt.Errorf("read the mailbox replacement: %w", err)
	}
	var manifest ResetManifest
	if err := record.UnmarshalJSONField("manifest", &manifest); err != nil {
		return ResetManifest{}, fmt.Errorf("decode the replacement manifest: %w", err)
	}
	return manifest, nil
}

func resetFromRecord(record *core.Record) *Reset {
	return &Reset{
		ID:               record.Id,
		OwnerID:          record.GetString("owner"),
		FromConnectionID: record.GetString("from_connection"),
		ToConnectionID:   record.GetString("to_connection"),
		Status:           record.GetString("status"),
		Error:            record.GetString("error"),
		CreatedAt:        record.GetDateTime("created").Time().UTC(),
	}
}
