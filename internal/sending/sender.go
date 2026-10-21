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

// SendResult contains only application-owned reasons, never provider
// diagnostics. Submitted means accepted for processing, not delivered.
type SendResult struct {
	Kind               OutcomeKind
	RetryAfter         time.Time
	Error              string
	PauseMailbox       bool
	RequiresCorrection bool
}

// Sender submits one message through one provider connection. Implementations
// never retry: only the durable dispatcher schedules retries.
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
