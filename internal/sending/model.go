// Package sending owns durable submission intents. SUBMITTED means provider
// acceptance, never delivery. UTC is stored; DisplayTime uses explicit +05:30.
package sending

import (
	"errors"
	"github.com/pocketbase/pocketbase/core"
	"sync"
	"time"
)

const (
	AttemptsCollection    = "submission_attempts"
	ApprovalsCollection   = "test_approvals"
	ActionsCollection     = "sending_actions"
	ResolutionsCollection = "manual_resolutions"
	DispatcherCollection  = "dispatcher_state"
	KindCandidate         = "candidate"
	KindTest              = "test"
	AttemptQueued         = "queued"
	AttemptInFlight       = "in_flight"
	AttemptSubmitted      = "submitted"
	AttemptFailed         = "definite_failure"
	AttemptReview         = "review_required"
	AttemptRetryable      = "retryable"
	AttemptAuth           = "auth_failure"
)

var ErrNoWork = errors.New("work is no longer eligible")

// ScheduleLead is the minimum distance between the current time and a stored
// start time. A shorter lead would not leave the operator time to reconsider.
const ScheduleLead = 30 * time.Minute

// ScheduleZone is the fixed operator-facing offset used to interpret and show
// schedule times. A Docker server often runs in UTC, so a wall-clock entry must
// never be interpreted in the process's local zone.
var ScheduleZone = time.FixedZone("Asia/Kolkata", 19800)

// ScheduleRefusal is a fixed, operator-facing reason a schedule was refused. Its
// message is safe to show in the UI; every other error stays generic there.
type ScheduleRefusal struct{ Reason string }

func (r *ScheduleRefusal) Error() string { return r.Reason }

type StoreOptions struct {
	// RestoreReview disables every send-like action after a backup restore.
	// Stop, history resolution and export generation remain available.
	RestoreReview bool
	Now           func() time.Time
}
type Options = StoreOptions
type Store struct {
	app      core.App
	options  StoreOptions
	lastTask string
	cursorMu sync.Mutex
}

func NewStore(app core.App, options StoreOptions) *Store {
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Store{app: app, options: options}
}

type Work struct {
	// OwnerID, ConnectionID and Generation record the exact mailbox that
	// authorizes and transmits this work. They are rechecked at launch, so a
	// replaced or disconnected connection cannot send queued work.
	ID, Kind, OwnerID, ConnectionID                                                       string
	Generation                                                                            int
	TaskID, MemberID, CandidateID, ConfigurationID, ContentHash, Recipient, Subject, Body string
	// SnapshotID and SnapshotDigest identify the exact final dataset this
	// work renders. They are empty only for legacy pinned content.
	SnapshotID, SnapshotDigest string
	RetryCount                 int
	Sender                     string
}
type Attempt struct {
	Work
	ID, State, Reason     string
	StartedAt, FinishedAt time.Time
}
type Outcome struct {
	RequiresCorrection bool
	State, Reason      string
	// AcceptedAt is the instant the provider definitely accepted the message.
	// It becomes the attempt's completion time, so a later Sent-copy step can
	// never be mistaken for the send time. Zero for every other outcome.
	AcceptedAt time.Time
	RetryAt    time.Time
	// PauseMailbox pauses only the affected mailbox's work.
	PauseMailbox bool
}
type Settings struct {
	Rate int
	// Concurrency is the mailbox's saved submission capacity: how many of its
	// emails may be in flight at once.
	Concurrency                 int
	Starts                      []time.Time
	NextEligible, CooldownUntil time.Time
	Reason                      string
}
type MemberProgress struct {
	ResolutionActor, ResolutionBasis string
	ResolvedAt                       time.Time
	SampleEligible                   bool
	ID, State, Reason                string
	SourceRow                        int
	// RecipientIndex is the entry's position inside its source recipient cell;
	// Recipient is that entry's own address.
	RecipientIndex int
	Recipient      string
	LatestAttempt  *Attempt
}
type Progress struct {
	RetryableFailed                                                                                  int
	Members                                                                                          []MemberProgress
	TaskID, Status, Reason, ContentHash, LatestSuccessfulTestID                                      string
	SnapshotID, SnapshotDigest                                                                       string
	Finalized                                                                                        bool
	Approved, ArtifactNeeded, RestoreReview                                                          bool
	Eligible, Remaining, Submitted, Failed, Held, InFlight, Reserved, Invalid, Duplicate, Ineligible int
	Recent                                                                                           []Attempt
}
type ActionRequest struct {
	ActionID, TaskID, Kind, MemberID, Actor, Basis string
	// MemberIDs selects several candidates for one bulk recovery action. It
	// narrows the same atomic recheck the single MemberID uses, so a bulk
	// request is all-or-nothing against the confirmed count.
	MemberIDs                []string
	Confirmed, DuplicateRisk bool
	ExpectedCount            int
}
type ActionResult struct {
	ActionID, TaskID, Status string
	Claimed, Excluded        int
	// Eligible records the fresh eligible count the operator confirmed when
	// enabling a schedule.
	Eligible int
	Repeated bool
}
type ScheduleRequest struct {
	ActionID, TaskID, Actor string
	// At is the absolute instant to start. A zero value clears a pending or
	// missed schedule instead.
	At            time.Time
	Confirmed     bool
	ExpectedCount int
}
type TestRequest struct {
	ActionID, TaskID, MemberID, Recipient, Actor string
	Values                                       map[string]string
}

func DisplayTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.RFC3339)
}
