// Package presentation derives the operator-facing status, blockers, next
// action, available controls, lifecycle evidence and validation summary from
// existing domain facts. Nothing here is stored or authoritative: a full page
// and an htmx fragment derive the same answer from the same facts, and the
// sending service still rechecks every action in its own transaction.
package presentation

import (
	"fmt"
	"strings"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Task workspace sections. The order is the navigation order.
const (
	SectionRecipients = "recipients"
	SectionTest       = "test"
	SectionSending    = "sending"
	SectionResults    = "results"
)

// Destinations outside the task workspace, used by the next-action link.
const (
	DestCampaignTasks    = "campaign-tasks"
	DestCampaignTemplate = "campaign-template"
	DestMailbox          = "mailbox"
)

// Execution statuses stored on a task record.
const (
	StatusStaging     = "staging"
	StatusReady       = "ready"
	StatusRunning     = "running"
	StatusWaiting     = "waiting"
	StatusStopping    = "stopping"
	StatusPaused      = "paused"
	StatusInterrupted = "interrupted"
	StatusStopped     = "stopped"
	StatusCompleted   = "completed"
)

// Artifact summary states from the exports package, repeated here so the
// presentation rules do not depend on the export implementation.
const (
	ArtifactPending    = "pending"
	ArtifactGenerating = "generating"
	ArtifactReady      = "ready"
	ArtifactFailed     = "failed"
)

// Facts is one task's current evidence. Every field comes from an existing
// record or service answer; the zero value is a staging task with no campaign
// revision.
type Facts struct {
	// Staging marks a workbook that has not produced a validation snapshot.
	Staging bool
	// ValidationError is the failure recorded on a staging task, if any.
	ValidationError string
	// Status is the stored execution status.
	Status string
	// HasRevision reports whether the campaign has a current revision.
	HasRevision bool
	// Stale reports that an unstarted snapshot differs from the current
	// revision.
	Stale bool
	// Approved reports applicable mailbox-scoped approval for the saved
	// content.
	Approved bool
	// MailboxUsable reports a connected sending mailbox.
	MailboxUsable bool
	// RestoreReview reports the server-wide restore-review restriction.
	RestoreReview bool
	// Execution/schedule evidence.
	ScheduleState string
	// Fresh and historical recipient counts.
	Eligible        int
	Remaining       int
	Submitted       int
	Failed          int
	RetryableFailed int
	Uncertain       int
	Held            int
	// ResultState is the task artifact state; empty means no artifact.
	ResultState string
	ResultReady bool
}

// TaskStatus is the main status label and its required explanation. The raw
// stored status stays available separately in details.
type TaskStatus struct {
	Label       string
	Explanation string
	// Success marks a neutral/finished state for badge styling.
	Success bool
	// Warning marks a state that needs the operator's attention.
	Warning bool
}

// NextAction is the single destination the task header or list row offers.
type NextLink struct {
	Label       string
	Section     string
	Destination string
}

// Blocker is one reason an otherwise relevant action cannot proceed. Every
// applicable blocker stays discoverable; the first is the most severe.
type Blocker struct {
	Label       string
	Remedy      string
	Section     string
	Destination string
}

// Controls is the set of candidate, recovery and result actions that apply to
// one task state. An omitted control is not rendered; a control that applies
// but is blocked is rendered with its reason.
type Controls struct {
	Validate       bool
	Revalidate     bool
	Start          bool
	Schedule       bool
	SendNowInstead bool
	EditSchedule   bool
	ClearSchedule  bool
	Stop           bool
	Resume         bool
	RetryAll       bool
	RetryRow       bool
	ResendRow      bool
	ResolveRow     bool
	GenerateResult bool
	RetryResult    bool
}

// Step is one lifecycle entry and the evidence it reflects. Completion is
// derived from saved records, never from a page visit.
type Step struct {
	Name     string
	State    string
	Complete bool
}

// Summary is the validation summary: a mutually exclusive partition whose
// categories sum to Total, plus Excluded at validation taken as Total minus
// Eligible.
type Summary struct {
	Total    int
	Eligible int
	Excluded int
	// ExcludedAtValidation equals Total-Eligible: every non-eligible row,
	// not only the file-only duplicate/invalid rows.
	ExcludedAtValidation int
	Categories           []Category
}

// Category is one mutually exclusive validation category.
type Category struct {
	Label string
	Count int
}

// started reports whether execution has ever moved past the validated state.
func (f Facts) started() bool {
	switch f.Status {
	case StatusRunning, StatusWaiting, StatusStopping, StatusPaused,
		StatusInterrupted, StatusStopped, StatusCompleted:
		return true
	default:
		return false
	}
}

// settled reports that no dispatch is active and recovery actions may be
// confirmed. A stopping task is still active.
func (f Facts) settled() bool {
	switch f.Status {
	case StatusPaused, StatusInterrupted, StatusStopped, StatusCompleted:
		return true
	default:
		return false
	}
}

// candidateWorkRemains reports a positive applicable send/resume, definite
// retry or uncertain resend scope.
func (f Facts) candidateWorkRemains() bool {
	return f.Remaining > 0 || f.RetryableFailed > 0 || f.Uncertain > 0
}

// MainStatus returns the task's main status using the spec's precedence. The
// schedule, approval, execution and artifact facts stay separately visible.
func MainStatus(f Facts) TaskStatus {
	if f.Staging {
		if f.ValidationError != "" {
			return TaskStatus{Label: "Validation failed", Explanation: f.ValidationError, Warning: true}
		}
		return TaskStatus{Label: "Needs worksheet selection", Explanation: "Choose the worksheet and header row to validate this workbook.", Warning: true}
	}
	switch f.Status {
	case StatusStopping:
		return TaskStatus{Label: "Stopping", Explanation: "No new sends; waiting for active requests to settle.", Warning: true}
	case StatusRunning:
		return TaskStatus{Label: "Sending", Explanation: "Durable progress below; the counts refresh before every action."}
	case StatusWaiting:
		return TaskStatus{Label: "Waiting", Explanation: "Pacing or a provider cooldown is holding the next start; the wait reason names when known.", Warning: true}
	}
	if f.ScheduleState == intake.ScheduleMissed {
		return TaskStatus{Label: "Missed scheduled start", Explanation: "The stored start time passed without starting. Review the miss and choose the recorded recovery path.", Warning: true}
	}
	if f.Status == StatusReady && f.Stale {
		return TaskStatus{Label: "Needs revalidation", Explanation: "The campaign content changed after this workbook was validated.", Warning: true}
	}
	if f.ScheduleState == intake.ScheduleScheduled {
		return TaskStatus{Label: "Scheduled", Explanation: "A future start is authorized; any current blocker is listed beside this status."}
	}
	switch f.Status {
	case StatusPaused:
		return TaskStatus{Label: "Paused", Explanation: "Sending stopped with a recorded reason; reconnecting a mailbox alone does not resume it.", Warning: true}
	case StatusInterrupted:
		return TaskStatus{Label: "Interrupted", Explanation: "A restart interrupted sending. The safe remainder and uncertain rows are distinct.", Warning: true}
	case StatusStopped:
		return TaskStatus{Label: "Stopped", Explanation: "Sending settled; the outcome summary and any safe remainder remain available.", Warning: true}
	case StatusCompleted:
		return TaskStatus{Label: "Finished", Explanation: "Sending finished. Submitted, failed, held and remaining rows are reported separately.", Success: true}
	}
	if f.Status == StatusReady && f.Eligible == 0 && f.Remaining == 0 {
		return TaskStatus{Label: "No eligible recipients", Explanation: "Every input row was excluded or already handled; the row reasons and exclusion report explain why.", Warning: true}
	}
	if f.Status == StatusReady && !f.Approved {
		return TaskStatus{Label: "Needs formatting approval", Explanation: "The saved content is not approved for the current mailbox yet.", Warning: true}
	}
	return TaskStatus{Label: "Ready to send", Explanation: "Validated and approved; sending still requires an explicit start or schedule authorization."}
}

// Blockers lists every reason an otherwise relevant action cannot proceed, in
// the spec's severity order: restore review, mailbox authorization, then
// content/approval problems.
func Blockers(f Facts) []Blocker {
	var blockers []Blocker
	if f.RestoreReview && f.candidateWorkRemains() {
		blockers = append(blockers, Blocker{
			Label:   "Restore review is active",
			Remedy:  "Reconcile submissions made after the restored backup and restart the server without APP_RESTORE_REVIEW. Stop and manual outcome resolution stay available.",
			Section: SectionSending,
		})
	}
	if !f.MailboxUsable && f.candidateWorkRemains() {
		blockers = append(blockers, Blocker{
			Label:       "Sending mailbox is not usable",
			Remedy:      "Reconnect the mailbox, then return to this task; reconnecting never resumes candidates by itself.",
			Destination: DestMailbox,
		})
	}
	if f.Status == StatusReady && f.Stale {
		blockers = append(blockers, Blocker{
			Label:   "Saved content is out of date",
			Remedy:  "Revalidate against the current template before starting or scheduling.",
			Section: SectionRecipients,
		})
	}
	if f.HasRevision && !f.Approved && f.candidateWorkRemains() {
		blockers = append(blockers, Blocker{
			Label:   "Formatting is not approved for this mailbox",
			Remedy:  "Send and verify a formatting test, then approve the received formatting for the saved revision.",
			Section: SectionTest,
		})
	}
	if f.ScheduleState == intake.ScheduleScheduled && f.Status == StatusReady && !f.Approved {
		blockers = append(blockers, Blocker{
			Label:   "The scheduled start has no applicable approval",
			Remedy:  "Approve the saved content for the current mailbox, or clear the schedule and enter a new one after approval.",
			Section: SectionTest,
		})
	}
	return blockers
}

// NextAction selects the one link shown in the task header or list row. The
// full action matrix still governs the section-local controls.
func NextAction(f Facts) NextLink {
	switch {
	case f.Status == StatusRunning || f.Status == StatusWaiting || f.Status == StatusStopping:
		return NextLink{Label: "View progress", Section: SectionSending}
	case f.Staging && !f.HasRevision:
		return NextLink{Label: "Add template", Destination: DestCampaignTemplate}
	case f.Staging:
		return NextLink{Label: "Validate workbook", Section: SectionRecipients}
	case f.Status == StatusReady && f.Stale && f.ScheduleState == intake.ScheduleScheduled:
		return NextLink{Label: "Review schedule", Section: SectionSending}
	case f.Status == StatusReady && f.Stale:
		return NextLink{Label: "Revalidate with current template", Section: SectionRecipients}
	case f.RestoreReview && f.candidateWorkRemains():
		return NextLink{Label: "Review sending restriction", Section: SectionSending}
	case !f.MailboxUsable && f.candidateWorkRemains():
		return NextLink{Label: "Reconnect mailbox", Destination: DestMailbox}
	case f.HasRevision && !f.Approved && f.candidateWorkRemains():
		return NextLink{Label: "Test & approve", Section: SectionTest}
	case f.ScheduleState == intake.ScheduleMissed:
		return NextLink{Label: "Review missed start", Section: SectionSending}
	case f.ScheduleState == intake.ScheduleScheduled && f.Status == StatusReady:
		return NextLink{Label: "View schedule", Section: SectionSending}
	case f.Status == StatusReady && f.Eligible > 0 && !f.Stale:
		return NextLink{Label: "Choose sending time", Section: SectionSending}
	case f.started() && f.Remaining > 0:
		return NextLink{Label: "Resume sending", Section: SectionSending}
	case f.settled() && f.RetryableFailed > 0:
		return NextLink{Label: "Retry failed recipients", Section: SectionSending}
	case f.settled() && f.Uncertain > 0:
		return NextLink{Label: "Review outcomes", Section: SectionSending}
	case f.Status == StatusReady && f.Eligible == 0 && f.Remaining == 0:
		return NextLink{Label: "Review exclusions", Section: SectionRecipients}
	case f.Status == StatusPaused && f.Remaining == 0 && f.RetryableFailed == 0 && f.Uncertain == 0 && !f.ResultEligible():
		return NextLink{Label: "Stop task", Section: SectionSending}
	case f.ResultEligible() && f.ResultState == ArtifactFailed:
		return NextLink{Label: "Retry result generation", Section: SectionResults}
	default:
		return NextLink{Label: "View results", Section: SectionResults}
	}
}

// ResultEligible reports whether terminal eligibility allows result
// generation: completed, stopped or a recovered interruption.
func (f Facts) ResultEligible() bool {
	switch f.Status {
	case StatusCompleted, StatusStopped, StatusInterrupted:
		return true
	default:
		return false
	}
}

// AvailableControls returns the controls the current state permits, omitting
// irrelevant operations. Each returned operation is still subject to the
// service's own approval, mailbox, restore-review and fresh-count checks.
func AvailableControls(f Facts) Controls {
	var controls Controls
	if f.Staging || (f.Status == StatusReady && f.Stale && f.ScheduleState != intake.ScheduleScheduled) {
		controls.Validate = f.Staging
		controls.Revalidate = !f.Staging
	}
	switch f.Status {
	case StatusStopping:
		// No new operations while active requests drain.
	case StatusRunning, StatusWaiting:
		controls.Stop = true
	case StatusPaused, StatusInterrupted:
		controls.Resume = f.Remaining > 0
		controls.Stop = true
		controls.ClearSchedule = f.ScheduleState == intake.ScheduleScheduled || f.ScheduleState == intake.ScheduleMissed
		if f.ScheduleState == intake.ScheduleMissed {
			// A downtime miss paused the task; clearing the schedule leaves it
			// paused, so a new first start is never offered here.
			controls.ClearSchedule = true
		}
	case StatusStopped:
		controls.Resume = f.Remaining > 0
	case StatusCompleted:
		controls.Resume = f.Remaining > 0
	case StatusReady:
		if f.Stale {
			controls.Revalidate = true
			controls.ClearSchedule = f.ScheduleState == intake.ScheduleScheduled
			break
		}
		switch f.ScheduleState {
		case intake.ScheduleScheduled:
			controls.EditSchedule = true
			controls.ClearSchedule = true
			controls.SendNowInstead = true
		case intake.ScheduleMissed:
			// A live-fire refusal leaves a still-ready task; sending now needs
			// a positive fresh scope, never a zero-recipient action.
			controls.Start = f.Eligible > 0 || f.Remaining > 0
			controls.ClearSchedule = true
		default:
			controls.Start = f.Eligible > 0 || f.Remaining > 0
			controls.Schedule = controls.Start
			controls.EditSchedule = false
		}
	}
	if f.settled() {
		// Restore review blocks every new dispatch, including a definite
		// retry; only non-dispatch manual resolution stays available.
		controls.RetryAll = f.RetryableFailed > 0 && !f.RestoreReview
		controls.RetryRow = f.RetryableFailed > 0 && !f.RestoreReview
		// Mark already submitted is non-dispatch evidence: it stays available
		// during active work and restore review. Sending again does not.
		controls.ResendRow = f.Uncertain > 0 && !f.RestoreReview
	}
	controls.ResolveRow = f.Uncertain > 0
	if f.ResultEligible() && !f.ResultReady {
		switch f.ResultState {
		case ArtifactFailed:
			controls.RetryResult = true
		case "", ArtifactReady:
			controls.GenerateResult = true
		}
	}
	return controls
}

// Lifecycle returns the ordered lifecycle strip with evidence-based states.
func Lifecycle(f Facts) []Step {
	recipients := Step{Name: "Recipients", State: "Validated", Complete: true}
	if f.Staging {
		if f.ValidationError != "" {
			recipients = Step{Name: "Recipients", State: "Validation failed"}
		} else {
			recipients = Step{Name: "Recipients", State: "Needs worksheet selection"}
		}
	}
	test := Step{Name: "Test & approve", State: "Approved", Complete: true}
	if !f.HasRevision {
		test = Step{Name: "Test & approve", State: "No template"}
	} else if !f.Approved {
		test = Step{Name: "Test & approve", State: "Not approved"}
	}
	sending := Step{Name: "Sending", State: "Not started"}
	switch {
	case f.ScheduleState == intake.ScheduleMissed:
		sending.State = "Missed scheduled start"
	case f.Status == StatusReady && f.ScheduleState == intake.ScheduleScheduled:
		sending.State = "Scheduled"
	case f.Status == StatusRunning:
		sending.State = "Sending"
	case f.Status == StatusWaiting:
		sending.State = "Waiting"
	case f.Status == StatusStopping:
		sending.State = "Stopping"
	case f.Status == StatusPaused:
		sending.State = "Paused"
	case f.Status == StatusInterrupted:
		sending.State = "Interrupted"
	case f.Status == StatusStopped:
		sending.State = "Stopped"
	case f.Status == StatusCompleted:
		sending.State = "Finished"
	}
	results := Step{Name: "Results", State: "Not available yet"}
	if f.ResultReady {
		results = Step{Name: "Results", State: "Available", Complete: true}
	} else if f.ResultState == ArtifactPending || f.ResultState == ArtifactGenerating {
		results = Step{Name: "Results", State: "Preparing results"}
	}
	return []Step{
		{Name: "Upload", State: "Stored", Complete: true},
		recipients,
		test,
		sending,
		results,
	}
}

// Summarize partitions one validation into exclusive categories. Total equals
// the sum of the categories, and ExcludedAtValidation is Total-Eligible, so it
// includes every non-eligible category rather than only invalid/duplicate
// rows.
func Summarize(counts intake.Counts) Summary {
	return Summary{
		Total:                counts.Total,
		Eligible:             counts.Eligible,
		Excluded:             counts.Total - counts.Eligible,
		ExcludedAtValidation: counts.Total - counts.Eligible,
		Categories: []Category{
			{Label: "Eligible", Count: counts.Eligible},
			{Label: "Ineligible or missing data", Count: counts.Ineligible},
			{Label: "Duplicate", Count: counts.Duplicate},
			{Label: "Invalid address", Count: counts.Invalid},
			{Label: "Already submitted", Count: counts.AlreadySubmitted},
			{Label: "Already scheduled", Count: counts.AlreadyScheduled},
			{Label: "Review required", Count: counts.ReviewRequired},
		},
	}
}

// NeedsAttention reports the campaign-list attention predicate. Each task
// counts at most once; ordinary worksheet selection and a future unblocked
// schedule alone do not count.
func NeedsAttention(f Facts) bool {
	switch {
	case f.ValidationError != "":
		return true
	case f.Status == StatusReady && f.Stale:
		return true
	case f.ScheduleState == intake.ScheduleMissed:
		return true
	case f.Status == StatusPaused, f.Status == StatusInterrupted:
		return true
	case f.Uncertain > 0:
		return true
	case f.RetryableFailed > 0:
		return true
	case f.ResultState == ArtifactFailed:
		return true
	default:
		return false
	}
}

// Timestamp renders a stored instant as an operator-facing date/time with the
// explicit +05:30 offset used for every task, attempt and artifact time.
func Timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(time.FixedZone("Asia/Kolkata", 19800)).Format("02 Jan 2006, 03:04 PM -07:00")
}

// ShortRef is the first eight characters of a record ID.
func ShortRef(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// ShortRefs returns stable short references for a displayed list. A reference
// starts at eight characters and grows only while it collides with another
// displayed entry, so two rows are never identified by the same short text.
func ShortRefs(ids []string) map[string]string {
	refs := make(map[string]string, len(ids))
	for _, id := range ids {
		length := min(8, len(id))
		for length < len(id) {
			prefix := id[:length]
			conflict := false
			for _, other := range ids {
				if other != id && strings.HasPrefix(other, prefix) {
					conflict = true
					break
				}
			}
			if !conflict {
				break
			}
			length += 4
		}
		if length > len(id) {
			length = len(id)
		}
		refs[id] = id[:length]
	}
	return refs
}

// RecipientSummary is the compact list-row recipient evidence. Validated is
// false for a task without a validation snapshot, where zero would falsely
// imply that every row was excluded.
type RecipientSummary struct {
	Validated     bool
	Total         int
	Eligible      int
	Submitted     int
	NeedAttention int
}

// SummarizeRecipients builds the list-row recipient summary. NeedAttention
// counts distinct affected rows from the exclusive validation categories, so
// overlapping row reasons are never added together.
func SummarizeRecipients(counts intake.Counts, submitted, uncertain, retryable int) RecipientSummary {
	return RecipientSummary{
		Validated: counts.Total > 0,
		Total:     counts.Total,
		Eligible:  counts.Eligible,
		Submitted: submitted,
		NeedAttention: counts.Ineligible + counts.Duplicate + counts.Invalid +
			counts.AlreadySubmitted + counts.AlreadyScheduled + counts.ReviewRequired +
			uncertain + retryable,
	}
}

// Label is the compact campaign list-row cell: the eligible count, or the
// not-validated marker that keeps zero from implying a snapshot exists.
func (s RecipientSummary) Label() string {
	if !s.Validated {
		return "Not validated"
	}
	return fmt.Sprintf("%d eligible", s.Eligible)
}

// Details are the remaining list-row facts, disclosed on demand beside the
// label. It is empty when there is nothing beyond the eligible count.
func (s RecipientSummary) Details() []string {
	if !s.Validated {
		return nil
	}
	return []string{
		fmt.Sprintf("Input rows: %d", s.Total),
		fmt.Sprintf("Submitted: %d", s.Submitted),
		fmt.Sprintf("Need attention: %d", s.NeedAttention),
	}
}

// SectionLabel names a section for headings and status explanations.
func SectionLabel(section string) string {
	switch section {
	case SectionRecipients:
		return "Recipients"
	case SectionTest:
		return "Test & approve"
	case SectionSending:
		return "Sending"
	case SectionResults:
		return "Results"
	default:
		return strings.TrimSpace(section)
	}
}
