package intake

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Store provides campaign-scoped candidate identity and the transactional
// reservation operations that decide candidate eligibility.
type Store struct {
	app core.App
}

// NewStore returns the eligibility store.
func NewStore(app core.App) *Store {
	return &Store{app: app}
}

// EnsureCandidate returns the campaign-scoped identity for a normalized
// address, creating it on first use, and never modifies an existing candidate.
//
// txApp may be a transaction app or the plain app: every read and write of
// this call goes through it, so a caller publishing inside a transaction
// resolves identity in that same transaction. The unique index on
// (campaign, email_key) is the real guard against duplicates: when another
// writer creates the same identity first, this save fails and the already
// stored record is returned instead. Submission state, claim owner and claim
// timestamp of an existing candidate are never touched here — they are
// campaign history, not identity.
func (s *Store) EnsureCandidate(txApp core.App, campaignID, emailKey string) (*Candidate, error) {
	if strings.TrimSpace(campaignID) == "" {
		return nil, errors.New("a campaign is required to identify a candidate")
	}
	if strings.TrimSpace(emailKey) == "" {
		return nil, errors.New("a recipient address is required to identify a candidate")
	}
	const identity = "campaign = {:campaign} && email_key = {:email}"
	params := dbx.Params{"campaign": campaignID, "email": emailKey}

	record, err := txApp.FindFirstRecordByFilter(CandidatesCollection, identity, params)
	if err == nil {
		return candidateFromRecord(record), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read the candidate identity: %w", err)
	}

	collection, err := txApp.FindCollectionByNameOrId(CandidatesCollection)
	if err != nil {
		return nil, fmt.Errorf("candidates collection is missing; run migrations first: %w", err)
	}
	record = core.NewRecord(collection)
	record.Set("campaign", campaignID)
	record.Set("email_key", emailKey)
	record.Set("submission_state", SubmissionNone)
	if err := txApp.Save(record); err != nil {
		// The unique index rejected a concurrent creation of the same
		// identity: re-read once and return that record. The save failure
		// is raised only when the re-read finds nothing to return.
		if existing, lookupErr := txApp.FindFirstRecordByFilter(CandidatesCollection, identity, params); lookupErr == nil {
			return candidateFromRecord(existing), nil
		}
		return nil, fmt.Errorf("store the candidate identity: %w", err)
	}
	return candidateFromRecord(record), nil
}

// LoadCandidate returns one candidate by id. The wrapped error keeps the
// driver's not-found cause for logs while staying fixed operator-facing text.
func (s *Store) LoadCandidate(txApp core.App, id string) (*Candidate, error) {
	record, err := txApp.FindRecordById(CandidatesCollection, id)
	if err != nil {
		return nil, fmt.Errorf("candidate not found: %w", err)
	}
	return candidateFromRecord(record), nil
}

// Claim atomically reserves every still-free candidate for one task.
//
// The whole action runs in ONE transaction: the reservations it creates land
// as a unit, and this transaction is the only place a reservation is ever
// created — everything else in the app can only observe a claim. The single
// compare-and-swap statement is what makes concurrent claims safe: it matches
// only a candidate with no submission history and no reservation, so two
// simultaneous claims for one candidate admit exactly one owner, with no
// read-then-write check outside the statement to undermine it.
//
// A failed swap re-reads the candidate inside the same transaction and reports
// the state it holds. CandidateClaimed includes a claim this same task already
// owns — a repeat claim is deliberately not treated as a winner — so a caller
// that sees State == CandidateClaimed lost the race and should re-read fresh
// counts instead of assuming its claim holds.
func (s *Store) Claim(ctx context.Context, taskID string, candidateIDs []string) (map[string]ClaimOutcome, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("a task is required to reserve candidates")
	}
	outcomes := make(map[string]ClaimOutcome, len(candidateIDs))
	if len(candidateIDs) == 0 {
		return outcomes, nil
	}

	err := s.app.RunInTransaction(func(txApp core.App) error {
		// The timestamp is bound, never interpolated, so the statement stays
		// a fixed query. PocketBase stores date fields as text in its default
		// layout, so the instant is formatted to that layout before binding
		// and reads back through GetDateTime unchanged.
		claimedAt := time.Now().UTC().Format(types.DefaultDateLayout)
		seen := make(map[string]struct{}, len(candidateIDs))
		for _, id := range candidateIDs {
			if _, duplicate := seen[id]; duplicate {
				// Duplicate input collapses to one attempt and one map entry.
				continue
			}
			seen[id] = struct{}{}

			result, err := txApp.DB().NewQuery(
				"UPDATE " + CandidatesCollection +
					" SET claim_task = {:task}, claimed_at = {:now}" +
					" WHERE id = {:id} AND submission_state = {:none}" +
					" AND (claim_task IS NULL OR claim_task = '')",
			).WithContext(ctx).Bind(dbx.Params{
				"task": taskID,
				"now":  claimedAt,
				"id":   id,
				"none": SubmissionNone,
			}).Execute()
			if err != nil {
				// A statement error aborts the whole reservation unit.
				return fmt.Errorf("reserve the candidate: %w", err)
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return fmt.Errorf("reserve the candidate: %w", err)
			}
			if affected == 1 {
				outcomes[id] = ClaimOutcome{Claimed: true, State: CandidateFree}
				continue
			}
			// An id that cannot be re-read has no state to report, so it
			// aborts the transaction instead of inventing an outcome.
			candidate, err := s.LoadCandidate(txApp, id)
			if err != nil {
				return err
			}
			outcomes[id] = ClaimOutcome{Claimed: false, State: candidate.State()}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcomes, nil
}

// Release clears only the reservations this task owns.
//
// submission_state is never part of the statement: submitted and uncertain
// history is campaign-scoped audit trail that a stopped or recovered task must
// not clear. Both columns are stored as NOT NULL text whose empty string is
// the zero value — an empty claim_task means free — so clearing writes the
// empty string and a NULL would violate the column constraint.
func (s *Store) Release(ctx context.Context, taskID string) (int, error) {
	var released int64
	err := s.app.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(
			"UPDATE " + CandidatesCollection +
				" SET claim_task = '', claimed_at = ''" +
				" WHERE claim_task = {:task}",
		).WithContext(ctx).Bind(dbx.Params{"task": taskID}).Execute()
		if err != nil {
			return fmt.Errorf("release the task's reservations: %w", err)
		}
		released, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("release the task's reservations: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(released), nil
}

// MarkSubmitted records that the campaign accepted a send for this candidate
// (used by later dispatch work; the campaign history it writes is never
// cleared by a new upload).
//
// The bound submission_state comes from the exported Submission* constants,
// which are the only accepted stored values. A submit also clears the
// reservation: a submitted candidate never needs one again.
func (s *Store) MarkSubmitted(ctx context.Context, candidateID string) error {
	err := s.app.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(
			"UPDATE " + CandidatesCollection +
				" SET submission_state = {:submitted}, claim_task = '', claimed_at = ''" +
				" WHERE id = {:id}",
		).WithContext(ctx).Bind(dbx.Params{
			"submitted": SubmissionSubmitted,
			"id":        candidateID,
		}).Execute()
		if err != nil {
			return fmt.Errorf("record the submission: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("record the submission: %w", err)
		}
		if affected == 0 {
			return errors.New("candidate not found")
		}
		return nil
	})
	return err
}

// candidateFromRecord maps one campaign_candidates record to the identity the
// reservation service reports.
func candidateFromRecord(record *core.Record) *Candidate {
	return &Candidate{
		ID:              record.Id,
		CampaignID:      record.GetString("campaign"),
		EmailKey:        record.GetString("email_key"),
		SubmissionState: record.GetString("submission_state"),
		ClaimTaskID:     record.GetString("claim_task"),
	}
}
