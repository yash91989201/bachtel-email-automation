package migrations

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Adds per-entry provenance to task memberships and replaces the one-row-per-
// source-row uniqueness with one-membership-per-candidate. A recipient cell may
// expand into several addresses that share a source row, while a candidate
// still has at most one membership in a task.
//
// Existing memberships are single occurrences: their position becomes 1 and
// their saved recipient value becomes the original-cell comparison value. No
// candidate, membership or attempt ID changes, and retained workbooks are never
// resplit.
func init() {
	m.Register(func(app core.App) error {
		members, err := app.FindCollectionByNameOrId(intake.TaskCandidatesCollection)
		if err != nil {
			return err
		}
		members.Fields.Add(
			// 1-based position of this entry inside the source recipient cell.
			&core.NumberField{Name: "recipient_index", Required: true},
			// Full original recipient cell, kept for comparison previews only.
			&core.TextField{Name: "original_recipient", Max: 4000},
		)
		members.RemoveIndex("idx_task_candidates_row")
		members.AddIndex("idx_task_candidates_membership", true, "task, candidate", "")
		if err := app.Save(members); err != nil {
			return err
		}
		return backfillOccurrences(app)
	}, func(app core.App) error {
		// Memberships are audit history once sibling recipients exist, and the
		// old unique (task, source_row) index could not be restored without
		// deleting them. Roll back through a database backup and the matching
		// binary instead.
		return nil
	})
}

// backfillOccurrences turns every pre-existing membership into position 1 of
// its own source row and records the saved recipient value as its original
// cell, so a later comparison preview has the same evidence as a new entry.
func backfillOccurrences(app core.App) error {
	members, err := app.FindRecordsByFilter(intake.TaskCandidatesCollection, "id != ''", "id", 0, 0, nil)
	if err != nil {
		return err
	}
	for _, member := range members {
		recipient := savedRecipient(app, member)
		member.Set("recipient_index", 1)
		member.Set("original_recipient", recipient)
		if err := app.Save(member); err != nil {
			return err
		}
	}
	return nil
}

// savedRecipient recovers the single address a pre-expansion membership stored.
// The candidate identity is the trimmed lowercase address, so the raw value
// that normalizes to it is the recipient cell the operator uploaded.
func savedRecipient(app core.App, member *core.Record) string {
	candidate, err := app.FindRecordById(intake.CandidatesCollection, member.GetString("candidate"))
	if err != nil {
		return ""
	}
	emailKey := candidate.GetString("email_key")
	raw := map[string]string{}
	if err := member.UnmarshalJSONField("raw", &raw); err == nil {
		for _, value := range raw {
			if strings.ToLower(strings.TrimSpace(value)) == emailKey {
				return value
			}
		}
	}
	return emailKey
}
