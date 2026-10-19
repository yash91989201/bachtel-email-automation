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

var ErrReleaseGate = errors.New("candidate sending remains disabled until change 5 release verification")
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
	AllowCandidateSending bool
	Now                   func() time.Time
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
	ID, Kind, TaskID, MemberID, CandidateID, ConfigurationID, ContentHash, Recipient, Subject, Body string
	RetryCount                                                                                      int
}
type Attempt struct {
	Work
	ID, State, Reason     string
	StartedAt, FinishedAt time.Time
}
type Outcome struct {
	RequiresCorrection bool
	State, Reason      string
	RetryAt            time.Time
	PauseMailbox       bool
}
type Settings struct {
	Rate                        int
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
	LatestAttempt                    *Attempt
}
type Progress struct {
	RetryableFailed                                                                                  int
	Members                                                                                          []MemberProgress
	TaskID, Status, Reason, ContentHash, LatestSuccessfulTestID                                      string
	Approved, ProductionEnabled, ArtifactNeeded                                                      bool
	Eligible, Remaining, Submitted, Failed, Held, InFlight, Reserved, Invalid, Duplicate, Ineligible int
	Recent                                                                                           []Attempt
}
type ActionRequest struct {
	ActionID, TaskID, Kind, MemberID, Actor, Basis string
	Confirmed, DuplicateRisk                       bool
	ExpectedCount                                  int
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
