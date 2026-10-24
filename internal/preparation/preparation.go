// Package preparation owns the AI data-preparation stage of one task: it
// sends only campaign-mapped values to the operator's confirmed AI connection,
// validates the complete structured response, and stores immutable run
// evidence with resolved corrections and unresolved findings.
//
// Nothing here sends email, publishes candidates or authorizes a start. A
// prepared dataset is evidence: finalization, review downloads and sending
// integration belong to the later snapshot change. A run is executed by a
// server-owned durable job, so browser closure and restarts cannot lose
// completed evidence or turn partial provider output into a usable result.
package preparation

import (
	"errors"
	"time"

	"github.com/yash91989201/bachtel-email-automation/internal/ai"
	"github.com/yash91989201/bachtel-email-automation/internal/intake"
)

// Collection names owned by this package.
const (
	// RunsCollection holds immutable preparation-run evidence.
	RunsCollection = "preparation_runs"
	// JobsCollection holds the durable execution claim of one run.
	JobsCollection = "preparation_jobs"
)

// Run states.
const (
	// RunQueued is durable work that has not started.
	RunQueued = "queued"
	// RunRunning is claimed by one worker attempt.
	RunRunning = "running"
	// RunCompleted published its validated output.
	RunCompleted = "completed"
	// RunFailed recorded a provider or response failure; it is retryable.
	RunFailed = "failed"
	// RunIncomplete was interrupted by a restart; it is retryable.
	RunIncomplete = "incomplete"
	// RunCancelled was stopped by task closure; a closed task is not retried.
	RunCancelled = "cancelled"
)

// Job states.
const (
	// JobQueued is durable work awaiting its claim.
	JobQueued = "queued"
	// JobRunning is owned by the attempt token of one worker.
	JobRunning = "running"
	// JobDone published the run output.
	JobDone = "done"
	// JobFailed recorded a failure; the run carries the operator-facing text.
	JobFailed = "failed"
	// JobCancelled was cancelled by task closure before it could publish.
	JobCancelled = "cancelled"
)

// Run kinds.
const (
	// KindInitial is the first preparation of a task.
	KindInitial = "initial"
	// KindRetry retries failed or interrupted work without changing its
	// completed history.
	KindRetry = "retry"
	// KindRerun prepares again after a completed run, for example with edited
	// instructions.
	KindRerun = "rerun"
)

// Finding scopes.
const (
	// ScopeShared means the unresolved issue applies to the whole source row,
	// so every recipient occurrence of that row is held.
	ScopeShared = "shared"
	// ScopeRecipient means only one address occurrence is affected.
	ScopeRecipient = "recipient"
)

// Entry outcomes of the prepared dataset.
const (
	// OutcomeAccepted is an entry without an unresolved finding.
	OutcomeAccepted = "accepted"
	// OutcomeHeld is an entry with at least one unresolved finding.
	OutcomeHeld = "held"
)

// Bounds. Every value is a support limit, not a tunable.
const (
	// BasePromptVersion is the version of the visible application base prompt.
	BasePromptVersion = 1
	// MaxInstructions bounds the editable task-instruction draft.
	MaxInstructions = 16 << 10
	// MaxValueLength bounds one returned normalized value. It matches the
	// spreadsheet cell bound, so a legitimate long cell is never truncated.
	MaxValueLength = 32767
	// MaxNoteLength bounds one correction or finding note.
	MaxNoteLength = 500
	// MaxNotesPerRow bounds corrections and findings of one source row.
	MaxNotesPerRow = 64
	// DefaultBatchSize is the number of source rows in one provider request.
	DefaultBatchSize = 20
	// MaxBatchSize bounds one provider request.
	MaxBatchSize = 100
	// MaxArtifactBytes bounds one stored input or output artifact.
	MaxArtifactBytes = int64(64 << 20)
	// InputVersion and OutputVersion identify the stored evidence layouts.
	InputVersion  = 1
	OutputVersion = 1
)

// Errors reported by this package. Every message is fixed operator-facing
// text and never carries provider bodies, requests or credentials.
var (
	// ErrNotFound reports a task or run that does not exist or belongs to
	// another operator. Both refusals are identical so an id cannot be probed.
	ErrNotFound = intake.ErrNotFound
	// ErrNotReady reports a task without a validated snapshot.
	ErrNotReady = errors.New("validate this task before preparing its data")
	// ErrStarted reports a task that already started sending.
	ErrStarted = errors.New("preparation can only run before this task starts sending; upload a new task for further work")
	// ErrScheduled reports an enabled future schedule.
	ErrScheduled = errors.New("clear the scheduled start before changing prepared data")
	// ErrClosed reports a closed or closing task.
	ErrClosed = errors.New("this task is closed; upload a new task for future work")
	// ErrInProgress reports preparation already queued or running.
	ErrInProgress = errors.New("preparation is already running for this task")
	// ErrNotFailed reports a retry without failed work.
	ErrNotFailed = errors.New("only a failed or interrupted preparation can be retried")
	// ErrNotCompleted reports a rerun or selection without completed work.
	ErrNotCompleted = errors.New("only a completed preparation can be rerun or selected")
	// ErrSetup reports a missing or unreadable AI configuration.
	ErrSetup = errors.New("AI preparation needs a confirmed AI connection; set it up in Account settings and try again")
	// ErrStale reports a task whose pinned revision no longer matches its
	// validated snapshot.
	ErrStale = errors.New("this task's pinned content revision no longer matches its validated snapshot; revalidate it before preparing data")
	// ErrEmpty reports a validated task without any non-blank mapped row.
	ErrEmpty = errors.New("this task has no mapped values to prepare")
	// ErrConfigurationChanged reports a captured configuration that no longer
	// matches the confirmed connection, so no batch could use it safely.
	ErrConfigurationChanged = errors.New("the confirmed AI configuration changed after this run started; start preparation again")
	// ErrInvalidResponse reports an omitted, duplicated, unknown or mistyped
	// response. Nothing from an invalid response is published.
	ErrInvalidResponse = errors.New("the model returned an incomplete or invalid preparation response; nothing was published")
	// ErrInterrupted reports work interrupted by a server restart.
	ErrInterrupted = errors.New("preparation was interrupted by a server restart; retry preparation")
	// ErrCancelled reports work cancelled by task closure.
	ErrCancelled = errors.New("the task was closed before preparation finished; this run will not publish")
	// ErrNoOutput reports a run whose validated output is not published.
	ErrNoOutput = errors.New("this preparation run has no published output")
)

// Settled reports that one task's preparation work reached a terminal state.
// The closure lifecycle uses it to finalize a requested Close.
type Settled func(taskID string)

// Options bound one run's provider requests. They are recorded with the run.
type Options struct {
	BatchSize   int
	MaxTokens   int
	Temperature float64
	Now         func() time.Time
}

// DefaultOptions returns the production bounds. Whole-row batches need room for
// model reasoning as well as the normalized values, corrections and findings.
func DefaultOptions() Options {
	return Options{BatchSize: DefaultBatchSize, MaxTokens: ai.MaxTokens, Temperature: 0, Now: time.Now}
}

// normalize substitutes the defaults for unusable values and enforces the
// upper bounds.
func (o Options) normalize() Options {
	if o.BatchSize <= 0 {
		o.BatchSize = DefaultBatchSize
	}
	if o.BatchSize > MaxBatchSize {
		o.BatchSize = MaxBatchSize
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = ai.MaxTokens
	}
	if o.MaxTokens > ai.MaxTokens {
		o.MaxTokens = ai.MaxTokens
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// RunOptions is the recorded inference configuration of one run.
type RunOptions struct {
	BatchSize   int     `json:"batch_size"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

// Counts summarizes one prepared dataset. Entries equals Accepted + Held.
type Counts struct {
	SourceRows  int `json:"source_rows"`
	Entries     int `json:"entries"`
	Accepted    int `json:"accepted"`
	Held        int `json:"held"`
	Corrections int `json:"corrections"`
	Findings    int `json:"findings"`
}

// Field is one mapped variable sent to the provider. The id is the stable
// mapping variable; the header is the source-column label that guides meaning.
type Field struct {
	ID        string `json:"id"`
	Header    string `json:"header"`
	Recipient bool   `json:"recipient"`
}

// InputRow is one source row's mapped values. Only mapped columns are present.
type InputRow struct {
	ID     string            `json:"id"`
	Values map[string]string `json:"values"`
}

// Input is the captured, immutable request evidence of one run.
type Input struct {
	Version         int        `json:"version"`
	ConfigurationID string     `json:"configuration_id"`
	ContentHash     string     `json:"content_hash"`
	SourceDigest    string     `json:"source_digest"`
	Fields          []Field    `json:"fields"`
	Rows            []InputRow `json:"rows"`
}

// RowResult is one validated source-row response.
type RowResult struct {
	SourceRow   int               `json:"source_row"`
	Values      map[string]string `json:"values"`
	Corrections []Correction      `json:"corrections,omitempty"`
	Findings    []Finding         `json:"findings,omitempty"`
}

// Correction is a cleanup the model completed. A resolved correction is not a
// review finding.
type Correction struct {
	Row    int    `json:"row"`
	Field  string `json:"field"`
	Header string `json:"header"`
	Note   string `json:"note"`
}

// Finding is an unresolved issue. Its scope decides whether it holds the whole
// source row or one recipient occurrence.
type Finding struct {
	Row      int    `json:"row"`
	Field    string `json:"field"`
	Header   string `json:"header"`
	Scope    string `json:"scope"`
	Position int    `json:"position,omitempty"`
	Note     string `json:"note"`
}

// Entry is one prepared recipient occurrence. Held entries form the exclusion
// evidence; accepted entries form the prepared sending dataset.
type Entry struct {
	SourceRow int      `json:"source_row"`
	Position  int      `json:"position"`
	Recipient string   `json:"recipient"`
	Outcome   string   `json:"outcome"`
	Reasons   []string `json:"reasons,omitempty"`
}

// Output is the published, immutable result of one run.
type Output struct {
	Version int         `json:"version"`
	Rows    []RowResult `json:"rows"`
	Entries []Entry     `json:"entries"`
	Counts  Counts      `json:"counts"`
}

// Run is the operator-facing state of one preparation run.
type Run struct {
	ID        string
	OwnerID   string
	TaskID    string
	Kind      string
	State     string
	Error     string
	Provider  ai.Provider
	APIRoot   string
	APIFormat ai.APIFormat
	Model     string

	ConfigurationGeneration int
	CredentialGeneration    int
	ConfigurationID         string
	ContentHash             string
	SourceDigest            string
	PromptVersion           int
	Prompt                  string
	Instructions            string
	Options                 RunOptions
	InputDigest             string
	OutputDigest            string
	Counts                  Counts

	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time

	BatchTotal int
	BatchDone  int
	Selected   bool
	// Output is loaded on demand from the stored artifact.
	Output *Output
}
