# Proposal

## Why

The app must process up to 1,000 personalized emails efficiently while surviving interruptions without silently resending an accepted email. Sending, durable attempt tracking, approval, throttling and recovery must ship together.

## What Changes

- Require a manually verified test-email approval for the exact campaign configuration and an explicit start/remaining-count confirmation for every task.
- Run dispatch on the server with at most four in-flight requests, default pacing 25/minute and an enforced configurable maximum of 30/minute across all sends.
- Persist exact email content and a durable submission-intent record before each request; save each outcome immediately.
- Add batched live UI progress, graceful stop/drain, startup recovery with explicit resume, bounded safe retries, throttling waits and authentication pauses.
- Skip submitted candidates; hold ambiguous outcomes for review; support individual retry, retry-all definite failures, explicit uncertain resend and audited manual resolution.
- Allow test sends in normal and interrupted states without changing candidate outcomes or automatically resuming a task.
- Expose terminal-task result-artifact requests for change 5; artifact failure cannot trigger a send.

## Capabilities

### New Capabilities

- `email-dispatch`: Approved background tasks, Graph submission, shared concurrency/rate control and live progress.
- `sending-recovery`: Durable attempts, interruption handling, retries and explicit uncertain-outcome review.

### Modified Capabilities

None.

## Impact

Implementation order: **4 of 5**; requires changes 1-3. Change 5 completes downloads and production release validation. Do not deploy candidate sending until that release gate passes.

Use Go goroutines, existing SQLite/PocketBase transactions, `golang.org/x/time/rate`, and standard HTTP rather than an external broker, custom limiter or Graph batch abstraction. Disable hidden POST retries in the transport.

No delivery guarantee, read receipts, scheduled campaigns, attachments, Google integration or automatic resend of ambiguous attempts. The UI label `SUBMITTED` means Microsoft accepted the request.
