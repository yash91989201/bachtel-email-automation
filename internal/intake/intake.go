// Package intake owns the Excel task intake: it validates an uploaded workbook
// against a campaign's content revision before any row can be sent, keeps
// campaign-scoped candidate identity, stores immutable task membership
// snapshots, and provides the transactional eligibility/reservation service
// that later dispatch actions use to claim candidates.
//
// Nothing in this package sends email: a validated task is ready, not running,
// and no sending attempt exists here. Rows excluded inside an upload (duplicate
// or unusable recipient addresses) live only in the stored workbook and the
// private validation artifacts, never as candidate, membership or attempt
// records.
package intake

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Collection names owned by this package.
const (
	// TasksCollection holds uploaded tasks and their private artifacts.
	TasksCollection = "send_tasks"
	// CandidatesCollection holds campaign-scoped recipient identity.
	CandidatesCollection = "campaign_candidates"
	// TaskCandidatesCollection holds immutable task membership snapshots.
	TaskCandidatesCollection = "task_candidates"
)

const (
	// TaskStaging marks an upload that has not been validated. Nothing in a
	// staging task is sendable and no candidate row exists for it yet.
	TaskStaging = "staging"
	// TaskReady marks a validated task: its manifest is published and its
	// snapshot is complete. Starting it is a later change's responsibility.
	TaskReady = "ready"
)

// Stored values of a task's single-shot start schedule. A task has at most one
// schedule; schedule_state is the source of truth for whether a fire is still
// pending.
const (
	// ScheduleNone means no start is scheduled.
	ScheduleNone = "none"
	// ScheduleScheduled means a fire is pending at the task's schedule_at.
	ScheduleScheduled = "scheduled"
	// ScheduleFired records that the schedule started the task once.
	ScheduleFired = "fired"
	// ScheduleMissed records a schedule that could not start sending.
	ScheduleMissed = "missed"
	// ScheduleCancelled records a schedule that explicit closure cancelled.
	// The stored start time stays as the recorded authorization; a cancelled
	// schedule never fires.
	ScheduleCancelled = "cancelled"
	// TaskSendingTypeDirect and TaskSendingTypeScheduled are the saved first-start
	// preferences. They select a view only and never authorize dispatch.
	TaskSendingTypeDirect    = "direct"
	TaskSendingTypeScheduled = "scheduled"
)

// MaxRecipientEntries is the supported number of expanded recipient entries in
// one task, counted after comma splitting and before duplicate or invalid-address
// exclusions. It is a support bound, not a tunable: a larger input fails
// validation before any candidate is admitted.
const MaxRecipientEntries = 1000

// MaxUploadBytes is the hard ceiling for one uploaded workbook, in bytes. It is
// also the storage bound of the task's source file field, so a configured limit
// can only ever be lower.
const MaxUploadBytes = 10 << 20

// HeaderRowDefault is the header row a new task starts with: the first row of
// the selected worksheet.
const HeaderRowDefault = 1

// DefaultLimits are the documented default resource limits of one upload.
const (
	DefaultUnzipSizeLimit    = 64 << 20
	DefaultUnzipXMLSizeLimit = 16 << 20
)

// Structured bounds of the selected worksheet. They exist to bound memory and
// time before a workbook's rows are read: a worksheet outside them is rejected
// even when it holds few candidate rows.
const (
	MaxWorksheetRows    = 10000
	MaxWorksheetColumns = 256
)

// Limits bounds one upload before any workbook content is read.
type Limits struct {
	// MaxUploadBytes bounds the uploaded file. Non-positive means the default.
	MaxUploadBytes int64
	// UnzipSizeLimit bounds the total expanded size of the workbook archive.
	UnzipSizeLimit int64
	// UnzipXMLSizeLimit bounds one expanded XML part of the archive. It must
	// not exceed UnzipSizeLimit.
	UnzipXMLSizeLimit int64
}

// DefaultLimits returns the documented default resource limits.
func DefaultLimits() Limits {
	return Limits{
		MaxUploadBytes:    MaxUploadBytes,
		UnzipSizeLimit:    DefaultUnzipSizeLimit,
		UnzipXMLSizeLimit: DefaultUnzipXMLSizeLimit,
	}
}

// NormalizeLimits substitutes the defaults for non-positive values and rejects
// a combination the XLSX reader cannot honor. Every returned message is fixed
// text safe to show to an operator.
func NormalizeLimits(limits Limits) (Limits, error) {
	defaults := DefaultLimits()
	if limits.MaxUploadBytes <= 0 {
		limits.MaxUploadBytes = defaults.MaxUploadBytes
	}
	if limits.UnzipSizeLimit <= 0 {
		limits.UnzipSizeLimit = defaults.UnzipSizeLimit
	}
	if limits.UnzipXMLSizeLimit <= 0 {
		limits.UnzipXMLSizeLimit = defaults.UnzipXMLSizeLimit
	}
	if limits.MaxUploadBytes > MaxUploadBytes {
		return Limits{}, fmt.Errorf("the upload size limit must not exceed %d bytes", MaxUploadBytes)
	}
	if limits.UnzipXMLSizeLimit > limits.UnzipSizeLimit {
		return Limits{}, errors.New("the expanded XML part limit must not exceed the expanded archive limit")
	}
	return limits, nil
}

// SheetInfo describes one worksheet of an uploaded workbook.
type SheetInfo struct {
	Name    string `json:"name"`
	Visible bool   `json:"visible"`
}

// Header is one column of the selected header row. Index is the 0-based column
// offset used to look up column values in Row.Values.
type Header struct {
	Index int
	Label string
}

// Row is one data row below the header row. Number is the original 1-based
// worksheet row, so every operator-facing reason and the stored manifest keep
// pointing at the workbook the operator uploaded.
type Row struct {
	Number int
	Values []string
}

// Column returns the value at header column offset offset, or "" when the row
// has no cell there.
func (r Row) Column(offset int) string {
	if offset < 0 || offset >= len(r.Values) {
		return ""
	}
	return r.Values[offset]
}

// Row states of one admitted membership snapshot. Only RowEligible can ever be
// reserved and sent.
const (
	// RowEligible is a unique, valid-address row with every required value.
	RowEligible = "eligible"
	// RowIneligible is a unique, valid-address row missing a required value.
	RowIneligible = "ineligible"
	// RowAlreadySubmitted is a candidate already submitted in this campaign.
	RowAlreadySubmitted = "already_submitted"
	// RowAlreadyScheduled is a candidate reserved by another task.
	RowAlreadyScheduled = "already_scheduled"
	// RowReviewRequired is a candidate with an unresolved uncertain outcome.
	RowReviewRequired = "review_required"
)

// Exclusion reasons recorded in the validation manifest. An excluded row is
// file-only: it never becomes a candidate, membership or attempt record.
const (
	// ReasonDuplicate is a row whose recipient key appears more than once in
	// the upload. Every member of such a group is excluded.
	ReasonDuplicate = "duplicate"
	// ReasonInvalidAddress is a row whose recipient cell is not a single
	// usable email address.
	ReasonInvalidAddress = "invalid_address"
)

// Candidate states reported by the reservation service.
const (
	// CandidateFree is unclaimed with no submission history.
	CandidateFree = "free"
	// CandidateClaimed is reserved by a task.
	CandidateClaimed = "claimed"
	// CandidateSubmitted was accepted by the provider in this campaign.
	CandidateSubmitted = "submitted"
	// CandidateUncertain has an unresolved attempt outcome.
	CandidateUncertain = "uncertain"
)

// Stored values of a candidate's submission_state field.
const (
	// SubmissionNone means no submission history exists in this campaign.
	SubmissionNone = "none"
	// SubmissionSubmitted means the provider accepted a send.
	SubmissionSubmitted = "submitted"
	// SubmissionUncertain means an attempt outcome is unresolved.
	SubmissionUncertain = "uncertain"
)

// Validation errors reported by the intake service. Each one is fixed,
// operator-facing text: it names the missing header, the ambiguous label or the
// exceeded bound, and never a filesystem path or a reader diagnostic.
var (
	// ErrUploadTooLarge reports an upload above the configured size limit.
	ErrUploadTooLarge = errors.New("the workbook is larger than the configured upload limit")
	// ErrExpandedTooLarge reports a workbook whose expanded content exceeds
	// the configured resource limits.
	ErrExpandedTooLarge = errors.New("the workbook expands beyond the configured resource limit")
	// ErrWorksheetTooLarge reports a selected worksheet outside the supported
	// row and column bounds.
	ErrWorksheetTooLarge = errors.New("the selected worksheet is larger than the supported row or column bound")
	// ErrUnsupportedWorkbook reports a file that is not an XLSX workbook, for
	// example a legacy .xls file or a password-protected workbook.
	ErrUnsupportedWorkbook = errors.New("the upload is not a supported Excel workbook; save it as .xlsx without a password and upload it again")
	// ErrCorruptWorkbook reports a file that cannot be read as a workbook.
	ErrCorruptWorkbook = errors.New("the workbook could not be read; the file looks corrupt, so export it again and upload the new file")
	// ErrUnknownSheet reports a worksheet that is not part of the workbook.
	ErrUnknownSheet = errors.New("that worksheet is not part of the uploaded workbook")
	// ErrInvalidHeaderRow reports a header row inside the table.
	ErrInvalidHeaderRow = errors.New("choose the row number of the header row that names the workbook columns")
	// ErrNoUpload reports a submission without a workbook.
	ErrNoUpload = errors.New("choose an XLSX workbook to upload")
	// ErrTooManyRows reports a selection that expands into more recipient
	// entries than one task supports, whatever their validation outcome.
	ErrTooManyRows = fmt.Errorf("the selected worksheet produces more than %d recipient entries after splitting recipient cells on commas; split the workbook and upload one task at a time", MaxRecipientEntries)
	// ErrNotFound reports a task, row or candidate that does not exist or
	// belongs to another operator. Both refusals are identical so an id can
	// never be probed across operators.
	ErrNotFound = errors.New("task not found")
)

// ValidationError reports a failed task validation. The message is fixed
// operator-facing text, safe to render inline, and the task stays unready.
type ValidationError struct {
	// Message is the fixed reason the task cannot be validated.
	Message string
	// Err is the rejection this failure comes from, when there is one, so a
	// caller can switch on a sentinel with errors.Is instead of matching text.
	Err error
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "the task could not be validated"
}

// Unwrap exposes the underlying rejection to errors.Is and errors.As.
func (e *ValidationError) Unwrap() error { return e.Err }

// Candidate is the campaign-scoped identity of one recipient.
type Candidate struct {
	ID string
	// CampaignID scopes the identity: the same address in another campaign is
	// a different candidate.
	CampaignID string
	// EmailKey is the trimmed lowercase address.
	EmailKey string
	// SubmissionState is SubmissionNone, SubmissionSubmitted or
	// SubmissionUncertain.
	SubmissionState string
	// ClaimTaskID is the task currently reserving this candidate, or "".
	ClaimTaskID string
}

// State maps a stored candidate to its admission state.
func (c *Candidate) State() string {
	switch {
	case c.SubmissionState == SubmissionSubmitted:
		return CandidateSubmitted
	case c.SubmissionState == SubmissionUncertain:
		return CandidateUncertain
	case c.ClaimTaskID != "":
		return CandidateClaimed
	default:
		return CandidateFree
	}
}

// RowSnapshot is one admitted membership snapshot: an immutable copy of the
// row's raw cells and normalized values, tied to its original worksheet row.
//
// Raw and normalized values plus the task's pinned revision reproduce the
// personalized content exactly, so the snapshot stays small (a rendered body is
// never stored per row) while remaining immutable: later uploads and campaign
// edits cannot change what this snapshot renders.
type RowSnapshot struct {
	ID          string
	CandidateID string
	SourceRow   int
	// RecipientIndex is the 1-based position of this entry inside its source
	// recipient cell, so siblings from one worksheet row stay distinguishable.
	RecipientIndex int
	// OriginalRecipient is the full recipient cell the entry was expanded
	// from, kept for comparison display only.
	OriginalRecipient string
	// State is RowEligible, RowIneligible, RowAlreadySubmitted,
	// RowAlreadyScheduled or RowReviewRequired.
	State string
	// Reason is the fixed operator-facing explanation of State.
	Reason string
	// Raw holds the source cell value per mapped variable.
	Raw map[string]string
	// Normalized holds the replacement value per mapped variable.
	Normalized map[string]string
}

// Counts summarizes one task validation. Total counts every expanded recipient
// entry, so Total equals Eligible + Ineligible + Duplicate + Invalid +
// AlreadySubmitted + AlreadyScheduled + ReviewRequired. SourceRows counts the
// distinct worksheet rows that contributed at least one entry. Duplicate and
// Invalid entries are file-only.
type Counts struct {
	Total int `json:"total"`
	// SourceRows is the number of distinct source worksheet rows that
	// contributed entries. It is never summed with the category counts.
	SourceRows       int `json:"source_rows"`
	Eligible         int `json:"eligible"`
	Ineligible       int `json:"ineligible"`
	Duplicate        int `json:"duplicate"`
	Invalid          int `json:"invalid"`
	AlreadySubmitted int `json:"already_submitted"`
	AlreadyScheduled int `json:"already_scheduled"`
	ReviewRequired   int `json:"review_required"`
}

// Excluded reports how many candidate rows were held back as duplicates or
// unusable addresses: the rows that never touch candidate records.
func (c Counts) Excluded() int { return c.Duplicate + c.Invalid }

// Task is one uploaded workbook and its validation state.
type Task struct {
	ID              string
	CampaignID      string
	CampaignName    string
	Status          string
	Sheet           string
	Sheets          []SheetInfo
	HeaderRow       int
	Headers         []string
	ConfigurationID string
	ContentHash     string
	ResultLabels    []string
	Counts          Counts
	ValidationError string
	SourceName      string
	SourceDigest    string
	// UploadedAt is the stored creation time of the task record. It is
	// immutable: a later validation, revalidation or status change never
	// rewrites it, so two uploads of the same filename stay distinguishable.
	UploadedAt time.Time
	// ScheduleAt is the stored UTC instant of the task's single-shot start
	// schedule. It stays set after a fire as history and is zero when the
	// operator cleared it.
	ScheduleAt time.Time
	// ScheduleState is one of none, scheduled, fired or missed.
	ScheduleState string
	// SendingType is the shared first-start choice saved at upload or changed
	// before execution. Empty means the operator has not chosen a type.
	SendingType string
	// ClosureRequestedAt is when the owner confirmed Close. A non-zero value
	// keeps the task pending until outstanding work settles.
	ClosureRequestedAt time.Time
	// ClosedBy is the operator who confirmed closure.
	ClosedBy string
	// ClosedAt is the terminal closure marker. A closed task never reopens.
	ClosedAt time.Time
	// PreparationInstructions is the editable task-instruction draft. Editing
	// it never changes a completed preparation run.
	PreparationInstructions string
	// SelectedPreparationRunID is the completed run explicitly selected for
	// review. Selection is preparation evidence only.
	SelectedPreparationRunID string
}

// Closed reports a finalized terminal closure.
func (t *Task) Closed() bool { return !t.ClosedAt.IsZero() }

// Closing reports a confirmed closure still waiting for work to settle.
func (t *Task) Closing() bool { return !t.ClosureRequestedAt.IsZero() && t.ClosedAt.IsZero() }

// Admitted reports whether the task holds a published validation snapshot.
func (t *Task) Admitted() bool { return t.Status == TaskReady }

// Occurrence identifies one expanded recipient entry: its original 1-based
// worksheet row and its 1-based position inside that row's recipient cell.
type Occurrence struct {
	Row      int `json:"row"`
	Position int `json:"position"`
}

// DuplicateGroup is one detected recipient-key group. Every occurrence is
// excluded from candidate storage and listed here with its source coordinates.
type DuplicateGroup struct {
	Group       int          `json:"group"`
	EmailKey    string       `json:"email_key"`
	Occurrences []Occurrence `json:"occurrences"`
}

// ManifestRow is one expanded entry's outcome, keyed by its source coordinates.
// Recipient is the raw token this entry came from; Reasons is empty exactly for
// an admitted entry, whose State names the membership snapshot recorded for it.
type ManifestRow struct {
	Row       int      `json:"row"`
	Position  int      `json:"position"`
	Recipient string   `json:"recipient,omitempty"`
	EmailKey  string   `json:"email_key,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
	State     string   `json:"state,omitempty"`
	Group     int      `json:"group,omitempty"`
}

// Occurrence returns the source coordinates of the entry.
func (r ManifestRow) Occurrence() Occurrence {
	return Occurrence{Row: r.Row, Position: r.Position}
}

// Excluded reports whether the entry was held back from candidate storage.
func (r ManifestRow) Excluded() bool { return len(r.Reasons) > 0 }

// Manifest is the private validation manifest: the original sheet, header row
// and column labels plus every expanded entry's outcome, so row coordinates and
// excluded-entry reasons survive outside the candidate records.
type Manifest struct {
	Version   int      `json:"version"`
	Sheet     string   `json:"sheet"`
	HeaderRow int      `json:"header_row"`
	Headers   []string `json:"headers"`
	// SourceRows counts the distinct worksheet rows that contributed entries.
	SourceRows int              `json:"source_rows"`
	Rows       []ManifestRow    `json:"rows"`
	Groups     []DuplicateGroup `json:"duplicate_groups,omitempty"`
}

// UnmarshalJSON reads both manifest layouts at this one boundary. A version 1
// manifest recorded single-address source rows without positions: each row is
// upgraded to position 1 and keeps its saved address, and its group members
// become occurrences. Nothing is resplit or mutated.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	type manifestAlias Manifest
	var decoded manifestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = Manifest(decoded)
	if m.Version >= ManifestVersion {
		return nil
	}
	var legacy struct {
		Groups []struct {
			Group    int    `json:"group"`
			EmailKey string `json:"email_key"`
			Rows     []int  `json:"rows"`
		} `json:"duplicate_groups"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	for i := range m.Rows {
		if m.Rows[i].Position == 0 {
			m.Rows[i].Position = 1
		}
		if m.Rows[i].Recipient == "" {
			m.Rows[i].Recipient = m.Rows[i].EmailKey
		}
	}
	m.Groups = make([]DuplicateGroup, 0, len(legacy.Groups))
	for _, group := range legacy.Groups {
		upgraded := DuplicateGroup{Group: group.Group, EmailKey: group.EmailKey}
		for _, row := range group.Rows {
			upgraded.Occurrences = append(upgraded.Occurrences, Occurrence{Row: row, Position: 1})
		}
		m.Groups = append(m.Groups, upgraded)
	}
	if m.SourceRows == 0 {
		m.SourceRows = distinctRows(m.Rows)
	}
	return nil
}

// distinctRows counts the distinct source rows of a set of manifest entries.
func distinctRows(rows []ManifestRow) int {
	seen := make(map[int]struct{}, len(rows))
	for _, row := range rows {
		seen[row.Row] = struct{}{}
	}
	return len(seen)
}

// ReportRow is one excluded entry of the exclusion report: its source
// coordinates, the fixed operator-facing reasons it was excluded, its raw
// recipient token and its original column values.
type ReportRow struct {
	Row       int
	Position  int
	Recipient string
	Reasons   []string
	Values    []string
}

// ManifestVersion is the current manifest layout version.
const ManifestVersion = 2

// ClaimOutcome reports the result of one reservation attempt for a candidate.
type ClaimOutcome struct {
	// Claimed reports whether this action reserved the candidate.
	Claimed bool
	// State is the candidate state observed: CandidateFree when the
	// reservation succeeded, otherwise why it did not.
	State string
}

// Preflight is the reusable final-start contract. A later change calls it
// immediately before accepting a start, resume or retry action; it never
// reserves anything by itself.
type Preflight struct {
	TaskID     string
	CampaignID string
	// Ready reports whether the task holds a published validation snapshot at
	// all. A stale task is still ready: it needs revalidation, not a new upload.
	Ready bool
	// Stale reports that the campaign's current content revision differs from
	// the revision this task was validated against.
	Stale bool
	// Approved reports whether the current revision's content is approved.
	Approved bool
	// ConfigurationID is the campaign's current revision.
	ConfigurationID string
	// ResultLabels are the result labels resolved against the source columns
	// for the current revision.
	ResultLabels []string
	// Counts are the counts recorded by the last validation.
	Counts Counts
	// Claimable holds the candidate ids that are free right now: the rows a
	// start could still reserve. Every id belongs to an eligible row.
	Claimable []string
	// Reasons lists the fixed reasons a start must be refused. It is empty
	// exactly when a start may proceed.
	Reasons []string
}

// Startable reports whether a start action may proceed. A stale or unapproved
// task, and one without a claimable candidate, cannot start.
func (p *Preflight) Startable() bool { return len(p.Reasons) == 0 }

// ReasonText returns the fixed operator-facing explanation of a row state or an
// exclusion reason code. Unknown codes fall back to a generic sentence so the
// UI can never render an empty reason.
func ReasonText(code string) string {
	switch code {
	case ReasonDuplicate:
		return "duplicate recipient address in this upload; every row of the group is held"
	case ReasonInvalidAddress:
		return "the recipient cell is empty or is not a single usable email address"
	case RowIneligible:
		return "a required mapped value is empty"
	case RowEligible:
		return "eligible"
	case RowAlreadySubmitted:
		return "already submitted in this campaign"
	case RowAlreadyScheduled:
		return "already scheduled in another task of this campaign"
	case RowReviewRequired:
		return "an earlier attempt has an uncertain outcome and needs review"
	default:
		return "held back by validation"
	}
}

// RecipientKey is the duplicate-detection key of one recipient cell: the value
// trimmed of surrounding whitespace and lowercased. It is deliberately not a
// validity check, so a row that is both duplicated and unusable is still
// reported as a duplicate instead of bypassing duplicate detection.
func RecipientKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ResultSheetLabels are the metadata columns the recipient-results worksheet
// adds after the copied source columns and before the configured result
// columns. They are occupied names for result-label collision resolution.
var ResultSheetLabels = []string{"Source worksheet", "Source worksheet row", "Recipient position", "Recipient"}

// TaskResultLabels resolves the configured result labels for one task against
// both the source columns and the recipient-results metadata columns, so a
// generated results worksheet can never duplicate or overwrite a label.
func TaskResultLabels(labels, sourceHeaders []string) []string {
	occupied := SourceColumnLabels(sourceHeaders)
	occupied = append(occupied, ResultSheetLabels...)
	return ResolveResultLabels(labels, occupied)
}

// ResolveResultLabels returns the result column labels to snapshot for one
// task. A label that already exists among the source columns (compared after
// trimming surrounding whitespace) is given a deterministic numbered suffix,
// so a later result export can never overwrite an original column. The
// returned labels are unique and unoccupied.
func ResolveResultLabels(labels []string, sourceHeaders []string) []string {
	occupied := make(map[string]struct{}, len(sourceHeaders)+len(labels))
	for _, header := range sourceHeaders {
		occupied[strings.TrimSpace(header)] = struct{}{}
	}
	resolved := make([]string, 0, len(labels))
	for _, label := range labels {
		base := strings.TrimSpace(label)
		if base == "" {
			continue
		}
		candidate := base
		for suffix := 2; ; suffix++ {
			if _, taken := occupied[candidate]; !taken {
				break
			}
			candidate = fmt.Sprintf("%s (%d)", base, suffix)
		}
		occupied[candidate] = struct{}{}
		resolved = append(resolved, candidate)
	}
	return resolved
}
