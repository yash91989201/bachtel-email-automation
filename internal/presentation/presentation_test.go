package presentation_test

import (
	"testing"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/intake"
	"github.com/yash91989201/bachtel-email-automation/internal/presentation"
)

const (
	statusReady    = presentation.StatusReady
	statusRunning  = presentation.StatusRunning
	scheduleNone   = intake.ScheduleNone
	scheduleOn     = intake.ScheduleScheduled
	scheduleMissed = intake.ScheduleMissed
)

func readyFacts() presentation.Facts {
	return presentation.Facts{
		Status:        statusReady,
		HasRevision:   true,
		Approved:      true,
		MailboxUsable: true,
		Eligible:      3,
		Remaining:     3,
	}
}

func TestMainStatusPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts presentation.Facts
		want  string
	}{
		{"validation failed", presentation.Facts{Staging: true, ValidationError: "Missing Email header"}, "Validation failed"},
		{"needs worksheet selection", presentation.Facts{Staging: true, HasRevision: true}, "Needs worksheet selection"},
		{"stopping", presentation.Facts{Status: "stopping"}, "Stopping"},
		{"running", presentation.Facts{Status: statusRunning}, "Sending"},
		{"waiting", presentation.Facts{Status: "waiting"}, "Waiting"},
		{"ready and missed", presentation.Facts{Status: statusReady, ScheduleState: scheduleMissed, HasRevision: true, Approved: true}, "Missed scheduled start"},
		{"paused and missed", presentation.Facts{Status: "paused", ScheduleState: scheduleMissed}, "Missed scheduled start"},
		{"needs revalidation", presentation.Facts{Status: statusReady, Stale: true}, "Needs revalidation"},
		{"scheduled", presentation.Facts{Status: statusReady, ScheduleState: scheduleOn}, "Scheduled"},
		{"paused", presentation.Facts{Status: "paused"}, "Paused"},
		{"interrupted", presentation.Facts{Status: "interrupted"}, "Interrupted"},
		{"stopped", presentation.Facts{Status: "stopped"}, "Stopped"},
		{"completed", presentation.Facts{Status: "completed"}, "Finished"},
		{"no eligible", presentation.Facts{Status: statusReady, HasRevision: true, Approved: true}, "No eligible recipients"},
		{"needs approval", presentation.Facts{Status: statusReady, HasRevision: true, Eligible: 2, Remaining: 2}, "Needs formatting approval"},
		{"ready", readyFacts(), "Ready to send"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := presentation.MainStatus(tc.facts); got.Label != tc.want {
				t.Fatalf("status = %q, want %q", got.Label, tc.want)
			}
			if got := presentation.MainStatus(tc.facts); got.Explanation == "" {
				t.Fatal("a main status must carry its required explanation")
			}
		})
	}
}

func TestNextActionPriority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		facts   presentation.Facts
		label   string
		section string
		dest    string
	}{
		{"active work", presentation.Facts{Status: statusRunning}, "View progress", presentation.SectionSending, ""},
		{"stopping work", presentation.Facts{Status: "stopping"}, "View progress", presentation.SectionSending, ""},
		{"staging without template", presentation.Facts{Staging: true}, "Add template", "", presentation.DestCampaignTemplate},
		{"staging with template", presentation.Facts{Staging: true, HasRevision: true}, "Validate workbook", presentation.SectionRecipients, ""},
		{"stale scheduled", presentation.Facts{Status: statusReady, Stale: true, ScheduleState: scheduleOn}, "Review schedule", presentation.SectionSending, ""},
		{"stale", presentation.Facts{Status: statusReady, Stale: true}, "Revalidate with current template", presentation.SectionRecipients, ""},
		{"restore review", presentation.Facts{Status: statusReady, RestoreReview: true, Remaining: 2}, "Review sending restriction", presentation.SectionSending, ""},
		{"mailbox", presentation.Facts{Status: statusReady, MailboxUsable: false, Remaining: 2}, "Reconnect mailbox", "", presentation.DestMailbox},
		{"approval", presentation.Facts{Status: statusReady, HasRevision: true, Approved: false, MailboxUsable: true, Remaining: 2}, "Test & approve", presentation.SectionTest, ""},
		{"missed schedule", presentation.Facts{Status: "paused", ScheduleState: scheduleMissed}, "Review missed start", presentation.SectionSending, ""},
		{"enabled schedule", presentation.Facts{Status: statusReady, ScheduleState: scheduleOn, Approved: true}, "View schedule", presentation.SectionSending, ""},
		{"choose time", readyFacts(), "Choose sending time", presentation.SectionSending, ""},
		{"resume", presentation.Facts{Status: "paused", Remaining: 4, HasRevision: true, Approved: true, MailboxUsable: true}, "Resume sending", presentation.SectionSending, ""},
		{"retry", presentation.Facts{Status: "stopped", RetryableFailed: 2, HasRevision: true, Approved: true, MailboxUsable: true}, "Retry failed recipients", presentation.SectionSending, ""},
		{"review outcomes", presentation.Facts{Status: "stopped", Uncertain: 1, HasRevision: true, Approved: true, MailboxUsable: true}, "Review outcomes", presentation.SectionSending, ""},
		{"exclusions", presentation.Facts{Status: statusReady, HasRevision: true, Approved: true}, "Review exclusions", presentation.SectionRecipients, ""},
		{"stop paused task", presentation.Facts{Status: "paused"}, "Stop task", presentation.SectionSending, ""},
		{"retry result", presentation.Facts{Status: "completed", ResultState: presentation.ArtifactFailed}, "Retry result generation", presentation.SectionResults, ""},
		{"settled", presentation.Facts{Status: "completed"}, "View results", presentation.SectionResults, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := presentation.NextAction(tc.facts)
			if got.Label != tc.label || got.Section != tc.section || got.Destination != tc.dest {
				t.Fatalf("next action = %+v, want label %q section %q dest %q", got, tc.label, tc.section, tc.dest)
			}
		})
	}
}

func TestAvailableControlsFollowTheStateMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facts    presentation.Facts
		want     presentation.Controls
		unwanted presentation.Controls
	}{
		{
			name:     "staging validates only",
			facts:    presentation.Facts{Staging: true, HasRevision: true},
			want:     presentation.Controls{Validate: true},
			unwanted: presentation.Controls{Start: true, Schedule: true, Resume: true, Stop: true, RetryAll: true},
		},
		{
			name:     "stale revalidates only",
			facts:    presentation.Facts{Status: statusReady, Stale: true},
			want:     presentation.Controls{Revalidate: true},
			unwanted: presentation.Controls{Start: true, Schedule: true},
		},
		{
			name:     "current unstarted offers start and schedule",
			facts:    readyFacts(),
			want:     presentation.Controls{Start: true, Schedule: true},
			unwanted: presentation.Controls{Resume: true, Stop: true, Revalidate: true},
		},
		{
			name:     "scheduled offers edit clear and send now instead",
			facts:    presentation.Facts{Status: statusReady, ScheduleState: scheduleOn},
			want:     presentation.Controls{EditSchedule: true, ClearSchedule: true, SendNowInstead: true},
			unwanted: presentation.Controls{Resume: true, Stop: true},
		},
		{
			name:     "running stops",
			facts:    presentation.Facts{Status: statusRunning},
			want:     presentation.Controls{Stop: true},
			unwanted: presentation.Controls{Start: true, Schedule: true, Resume: true, RetryAll: true},
		},
		{
			name:     "stopping offers nothing",
			facts:    presentation.Facts{Status: "stopping"},
			want:     presentation.Controls{},
			unwanted: presentation.Controls{Stop: true, Start: true, Schedule: true, Resume: true},
		},
		{
			name:     "paused keeps stop beside resume",
			facts:    presentation.Facts{Status: "paused", Remaining: 2},
			want:     presentation.Controls{Resume: true, Stop: true},
			unwanted: presentation.Controls{Start: true, Schedule: true},
		},
		{
			name:     "stopped resumes remaining work",
			facts:    presentation.Facts{Status: "stopped", Remaining: 3},
			want:     presentation.Controls{Resume: true, GenerateResult: true},
			unwanted: presentation.Controls{Start: true, Stop: true, Schedule: true},
		},
		{
			name:     "finished resumes safe remainder only",
			facts:    presentation.Facts{Status: "completed", Remaining: 1},
			want:     presentation.Controls{Resume: true, GenerateResult: true},
			unwanted: presentation.Controls{Start: true, Schedule: true},
		},
		{
			name:     "live-fire miss sends now or clears",
			facts:    presentation.Facts{Status: statusReady, ScheduleState: scheduleMissed, Eligible: 2},
			want:     presentation.Controls{Start: true, ClearSchedule: true},
			unwanted: presentation.Controls{Resume: true, Schedule: true},
		},
		{
			name:     "downtime miss resumes and clears",
			facts:    presentation.Facts{Status: "paused", ScheduleState: scheduleMissed, Remaining: 4},
			want:     presentation.Controls{Resume: true, ClearSchedule: true, Stop: true},
			unwanted: presentation.Controls{Start: true, Schedule: true},
		},
		{
			name:     "settled retry and uncertain scopes",
			facts:    presentation.Facts{Status: "stopped", RetryableFailed: 2, Uncertain: 1},
			want:     presentation.Controls{RetryAll: true, RetryRow: true, ResendRow: true, ResolveRow: true, GenerateResult: true},
			unwanted: presentation.Controls{},
		},
		{
			name:     "restore review keeps resolution, blocks resend",
			facts:    presentation.Facts{Status: "stopped", RestoreReview: true, Uncertain: 1, RetryableFailed: 1},
			want:     presentation.Controls{ResolveRow: true, GenerateResult: true},
			unwanted: presentation.Controls{ResendRow: true},
		},
		{
			name:     "result failure retries",
			facts:    presentation.Facts{Status: "completed", ResultState: presentation.ArtifactFailed},
			want:     presentation.Controls{RetryResult: true},
			unwanted: presentation.Controls{GenerateResult: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := presentation.AvailableControls(tc.facts)
			if got != tc.want {
				t.Fatalf("controls = %+v, want %+v", got, tc.want)
			}
			if tc.unwanted.Validate && got.Validate {
				t.Error("validate must be omitted")
			}
		})
	}
}

func TestBlockersKeepParallelProblemsVisible(t *testing.T) {
	facts := presentation.Facts{
		Status:        statusRunning,
		RestoreReview: true,
		MailboxUsable: false,
		Remaining:     2,
	}
	blockers := presentation.Blockers(facts)
	if len(blockers) != 2 {
		t.Fatalf("blockers = %+v, want restore review and mailbox", blockers)
	}
	if blockers[0].Label != "Restore review is active" {
		t.Fatalf("first blocker = %q, want restore review", blockers[0].Label)
	}
	if presentation.AvailableControls(facts).Stop != true {
		t.Fatal("a running task must keep Stop despite a separate blocker")
	}
	if got := presentation.NextAction(facts); got.Label != "View progress" {
		t.Fatalf("next action = %q, want active progress to win over blockers", got.Label)
	}
}

func TestLifecycleUsesSavedEvidence(t *testing.T) {
	facts := readyFacts()
	steps := presentation.Lifecycle(facts)
	want := map[string]string{
		"Upload":         "Stored",
		"Recipients":     "Validated",
		"Test & approve": "Approved",
		"Sending":        "Not started",
		"Results":        "Not available yet",
	}
	if len(steps) != 5 {
		t.Fatalf("steps = %+v, want the five ordered lifecycle entries", steps)
	}
	for _, step := range steps {
		if step.State != want[step.Name] {
			t.Errorf("%s state = %q, want %q", step.Name, step.State, want[step.Name])
		}
	}
	interrupted := presentation.Lifecycle(presentation.Facts{Status: "interrupted", ResultReady: true, HasRevision: true, Approved: true})
	if interrupted[3].State != "Interrupted" || interrupted[4].State != "Available" {
		t.Fatalf("interrupted lifecycle = %+v", interrupted)
	}
}

func TestLifecycleSendingCompletion(t *testing.T) {
	for _, status := range []string{
		presentation.StatusStaging, presentation.StatusReady, presentation.StatusRunning,
		presentation.StatusWaiting, presentation.StatusStopping, presentation.StatusPaused,
		presentation.StatusInterrupted, presentation.StatusStopped, presentation.StatusCompleted,
	} {
		for _, resultReady := range []bool{false, true} {
			facts := presentation.Facts{Status: status, ResultReady: resultReady}
			steps := presentation.Lifecycle(facts)
			wantComplete := status == presentation.StatusCompleted
			if steps[3].Complete != wantComplete {
				t.Errorf("status=%s resultsReady=%t: sending completion = %t, want %t", status, resultReady, steps[3].Complete, wantComplete)
			}
			if wantComplete && steps[3].State != "Finished" {
				t.Errorf("completed sending state = %q, want Finished", steps[3].State)
			}
			if steps[4].Complete != resultReady {
				t.Errorf("status=%s: results completion = %t, want %t", status, steps[4].Complete, resultReady)
			}
		}
	}
}

func TestSummarizePartitionsValidation(t *testing.T) {
	counts := intake.Counts{Total: 10, Eligible: 3, Ineligible: 2, Duplicate: 2, Invalid: 1, AlreadySubmitted: 1, AlreadyScheduled: 1, ReviewRequired: 0}
	summary := presentation.Summarize(counts)
	if summary.ExcludedAtValidation != 7 || summary.Excluded != 7 {
		t.Fatalf("excluded = %d/%d, want 7", summary.Excluded, summary.ExcludedAtValidation)
	}
	sum := 0
	for _, category := range summary.Categories {
		sum += category.Count
	}
	if sum != summary.Total {
		t.Fatalf("category sum = %d, want total %d", sum, summary.Total)
	}
	// The historical helper only counts file-only rows; the summary must not.
	if summary.ExcludedAtValidation <= counts.Excluded() {
		t.Fatalf("excluded at validation must include every non-eligible category")
	}
}

func TestNeedsAttentionCountsEachTaskOnce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts presentation.Facts
		want  bool
	}{
		{"validation error", presentation.Facts{ValidationError: "bad header"}, true},
		{"stale", presentation.Facts{Status: statusReady, Stale: true}, true},
		{"missed", presentation.Facts{Status: statusReady, ScheduleState: scheduleMissed}, true},
		{"paused", presentation.Facts{Status: "paused"}, true},
		{"interrupted", presentation.Facts{Status: "interrupted"}, true},
		{"uncertain", presentation.Facts{Status: "stopped", Uncertain: 1}, true},
		{"retryable", presentation.Facts{Status: "stopped", RetryableFailed: 1}, true},
		{"failed export", presentation.Facts{Status: "completed", ResultState: presentation.ArtifactFailed}, true},
		{"future schedule", presentation.Facts{Status: statusReady, ScheduleState: scheduleOn}, false},
		{"worksheet choice", presentation.Facts{Staging: true}, false},
		{"settled", presentation.Facts{Status: "completed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := presentation.NeedsAttention(tc.facts); got != tc.want {
				t.Fatalf("attention = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestShortRefsStayUnique(t *testing.T) {
	ids := []string{"aaaaaaaa1111", "aaaaaaaa2222", "bbbbbbbb3333"}
	refs := presentation.ShortRefs(ids)
	if refs["aaaaaaaa1111"] == refs["aaaaaaaa2222"] {
		t.Fatalf("colliding references were not extended: %+v", refs)
	}
	if refs["bbbbbbbb3333"] != "bbbbbbbb" {
		t.Fatalf("non-colliding reference = %q, want the first eight characters", refs["bbbbbbbb3333"])
	}
}

func TestTimestampUsesExplicitOffset(t *testing.T) {
	instant := time.Date(2026, 10, 22, 5, 42, 0, 0, time.UTC)
	if got, want := presentation.Timestamp(instant), "22 Oct 2026, 11:12 AM +05:30"; got != want {
		t.Fatalf("timestamp = %q, want %q", got, want)
	}
}
