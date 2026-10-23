// Package finalization turns reviewed prepared data or an operator-edited
// final sheet into one immutable sending snapshot.
//
// It owns the shared backend validator used by both finalization paths, the
// deterministic sending-sheet labels, the sealed snapshot evidence and the
// atomic replacement of the task's sending memberships. Nothing here calls AI:
// a prepared run is read as evidence, and an edited upload is validated
// deterministically. Sealing a snapshot never sends email or authorizes a test.
package finalization

import (
	"errors"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Collection names owned by this package.
const (
	// SnapshotsCollection holds immutable final sending snapshots.
	SnapshotsCollection = "final_snapshots"
)

// Snapshot sources.
const (
	// KindPrepared is the Approve as-is path: accepted prepared values.
	KindPrepared = "prepared"
	// KindUpload is an operator-edited final sheet. It is never AI-processed.
	KindUpload = "upload"
)

// Exclusion reason sources.
const (
	// SourceAI is an unresolved preparation finding.
	SourceAI = "ai"
	// SourceBackend is a deterministic application check.
	SourceBackend = "backend"
)

// Backend reason codes.
const (
	ReasonMissingRequired = "missing_required"
	ReasonFallback        = "fallback_empty"
	ReasonInvalidAddress  = "invalid_address"
	ReasonDuplicate       = "duplicate"
	ReasonAlreadySent     = "already_submitted"
	ReasonScheduled       = "already_scheduled"
	ReasonReviewRequired  = "review_required"
	ReasonSafety          = "rendering_safety"
)

// Structural failures of one final upload. They are fixed operator-facing text
// and never carry a reader diagnostic or a filesystem path.
var (
	// ErrNotFound reports a task or snapshot that does not exist or belongs to
	// another operator. Both refusals are identical so an id cannot be probed.
	ErrNotFound = intake.ErrNotFound
	// ErrNotReady reports a task without a published validation snapshot.
	ErrNotReady = errors.New("validate this task before finalizing its data")
	// ErrStarted reports a task whose candidate sending already started.
	ErrStarted = errors.New("finalization is only available before this task starts sending; upload a new task for further work")
	// ErrScheduled reports an enabled future schedule.
	ErrScheduled = errors.New("clear the scheduled start before changing final sending data")
	// ErrClosed reports a closed or closing task.
	ErrClosed = errors.New("this task is closed; upload a new task for future work")
	// ErrNoPrepared reports Approve as-is without a selected completed run.
	ErrNoPrepared = errors.New("select a completed preparation run before approving its data as-is")
	// ErrStale reports a selection or revision that moved after review.
	ErrStale = errors.New("the prepared run or campaign revision changed after review; review the current values and finalize again")
	// ErrTooManyEntries reports an expanded input above the support bound.
	ErrTooManyEntries = errors.New("the final data expands into more than 1,000 recipient entries; split the workbook and upload one task at a time")
	// ErrNoSnapshot reports a task without a selected final snapshot.
	ErrNoSnapshot = errors.New("approve the prepared data as-is or upload an edited final sheet before testing or sending")
)

// StructuralError reports a final upload that cannot be read as a sending
// sheet. The previous selected snapshot stays in place.
type StructuralError struct{ Message string }

// Error implements error.
func (e *StructuralError) Error() string { return e.Message }

// Counts summarizes one final snapshot.
type Counts struct {
	Entries    int `json:"entries"`
	SourceRows int `json:"source_rows"`
	Accepted   int `json:"accepted"`
	Excluded   int `json:"excluded"`
	// Reason category counts are not exclusive of one another; one entry may
	// carry several reasons. Accepted + Excluded equals Entries.
	AIHeld       int `json:"ai_held"`
	MissingValue int `json:"missing_value"`
	Invalid      int `json:"invalid"`
	Duplicate    int `json:"duplicate"`
	History      int `json:"history"`
	Safety       int `json:"safety"`
}

// Reason is one exclusion reason with its source and affected fields.
type Reason struct {
	Source string   `json:"source"`
	Code   string   `json:"code"`
	Text   string   `json:"text"`
	Fields []string `json:"fields,omitempty"`
}

// Entry is one accepted recipient entry of a final snapshot. Values contains
// the exact mapped values used for rendering, including a resolved optional
// fallback and a recipient value normalized only by trim/lowercase.
type Entry struct {
	FinalRow    int               `json:"final_row"`
	Position    int               `json:"position"`
	SourceRow   int               `json:"source_row,omitempty"`
	Recipient   string            `json:"recipient"`
	Values      map[string]string `json:"values"`
	CandidateID string            `json:"candidate_id,omitempty"`
}

// Exclusion is one rejected entry with the evidence needed to correct it.
type Exclusion struct {
	FinalRow  int               `json:"final_row"`
	Position  int               `json:"position"`
	SourceRow int               `json:"source_row,omitempty"`
	Recipient string            `json:"recipient"`
	Original  map[string]string `json:"original,omitempty"`
	Proposed  map[string]string `json:"proposed,omitempty"`
	Reasons   []Reason          `json:"reasons"`
}

// Snapshot is one immutable accepted dataset.
type Snapshot struct {
	ID              string
	TaskID          string
	CampaignID      string
	ConfigurationID string
	ContentHash     string
	Kind            string
	RunID           string
	SourceDigest    string
	Digest          string
	Actor           string
	Counts          Counts
	Entries         []Entry
	Exclusions      []Exclusion
	CreatedAt       time.Time
	Selected        bool
}
