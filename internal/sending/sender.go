package sending

import (
	"context"
	"time"
)

// Message is the immutable content saved with the submission intent.
type Message struct {
	Recipient string
	Subject   string
	HTML      string
}

// OutcomeKind is the provider-neutral result class of one submission.
type OutcomeKind string

const (
	// OutcomeSubmitted means the provider definitively accepted the request.
	// It never means delivered.
	OutcomeSubmitted OutcomeKind = "submitted"
	// OutcomeDefiniteFailure means the provider definitively refused it.
	OutcomeDefiniteFailure OutcomeKind = "definite_failure"
	// OutcomeSafeRetry means submission provably did not happen.
	OutcomeSafeRetry OutcomeKind = "safe_retry"
	// OutcomeAuthFailure means credentials were rejected.
	OutcomeAuthFailure OutcomeKind = "auth_failure"
	// OutcomeUncertain means the request might have been accepted.
	OutcomeUncertain OutcomeKind = "uncertain"
)

// SaveCopy saves a provider's own copy of an already accepted message in the
// mailbox that submitted it. It exists for providers whose sending operation
// does not save a Sent copy, and it is called at most once per submission, only
// after the acceptance is durably recorded. The context is bounded by the
// submission deadline and the timestamp is the recorded acceptance time, so the
// copy reflects the send rather than a later moment. A nil error means the copy
// was confirmed; a cleanup failure after confirmation is not an error.
type SaveCopy func(context.Context, time.Time) error

// SendResult contains only application-owned reasons, never provider
// diagnostics. Submitted means accepted for processing, not delivered.
type SendResult struct {
	Kind               OutcomeKind
	RetryAfter         time.Time
	Error              string
	PauseMailbox       bool
	RequiresCorrection bool
	// SaveCopy is set only by a provider whose submission does not save a Sent
	// copy itself. It stays nil when the provider's native sending operation
	// saves one: Microsoft 365 retains Graph's default Sent Items saving, so no
	// second copy is requested. A provider added later must establish one of
	// those two conventions; there is no mode that silently skips the copy. The
	// hook is process-local and never persisted, replayed or retried.
	SaveCopy SaveCopy
}

// Sender submits one message through one provider connection. Implementations
// never retry: only the durable dispatcher schedules retries. A sender that
// cannot save Sent copies natively returns them through SendResult.SaveCopy;
// one that can leaves it nil.
type Sender interface {
	Send(context.Context, Message) SendResult
}

// PreparingSender resolves potentially blocking authorization before the
// dispatcher grants a paced start. The returned sender must not refresh
// authorization.
type PreparingSender interface {
	Prepare(context.Context) Sender
}

// Resolver binds a loaded mailbox connection to its concrete sender. It is the
// only place provider selection happens; an unknown provider fails closed
// before any queue or credential mutation.
type Resolver interface {
	Sender(ctx context.Context, connectionID string) (Sender, error)
}
