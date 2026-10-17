# Design

## Context

See proposal.md. Changes 1-3 provide the operator, fixed-mailbox token source, immutable campaign revisions, Excel snapshots and transactional candidate reservations. This is change 4; dispatch and recovery are one implementation unit. The app is a single Go/PocketBase process on Linux, not a distributed worker system.

## Goals / Non-Goals

**Goals:** Efficient I/O concurrency without duplicate automatic submissions; server-owned tasks; durable recovery and operator actions using the same dispatch controls.

**Non-Goals:** Exactly-once delivery guarantees, Redis/RabbitMQ, a custom queue platform, Graph JSON batching, mailbox reading or inferred delivery status.

## Decisions

### Record-backed work and standard Go concurrency

Use the existing SQLite store as the source of truth, Go goroutines for bounded request execution and one mailbox dispatcher. Add collections for submission_attempts, test_approvals and manual_resolutions, plus durable dispatcher settings/cooldown metadata. Reuse task/candidate claims from change 3.

A task moves through ready, running/waiting, stopping, stopped, paused/interrupted and completed states, with issue counts separate from its lifecycle. Candidate memberships track eligible, reserved, in-flight, submitted, definite-failure and review-required states. Exclusions stay distinguishable. Test attempts have an explicit kind and no candidate eligibility effect.

Do not invent a general message broker or repository abstraction. The necessary application glue is the domain state machine and transactional database claims. Acquire a process-lifetime advisory lock in the data directory using existing golang.org/x/sys/unix support; this prevents overlapping Coolify old/new dispatchers on the same Linux host. A second process must not dispatch. This is not a multi-host/replica design.

### Shared limiter with persisted start history

Use golang.org/x/time/rate with burst 1, not a hand-written rate limiter. A central start coordinator combines it with a four-slot semaphore and persisted actual-start times/next-eligible time. All candidate, test and retry work uses this coordinator. Enforce integer rates 1-30 in handlers and stored-settings validation, default 25.

Ensure actual starts remain evenly spaced even if a permit holder is delayed; do not allow accumulated reservations to launch together. Persist sufficient recent start history and cooldown to maintain the rolling allowance across restart and configuration changes, including lowering the rate. When a 429 arrives, update a shared cooldown from Retry-After (seconds or HTTP date); absent that, use capped exponential backoff with jitter. Do not sleep inside a database transaction.

Give ready tasks bounded round-robin opportunities; operator-requested tests get the next permitted opportunity without exceeding capacity. Multiple ready tasks are allowed, while same-campaign recipient claims prevent overlap. Other Outlook clients use the same provider mailbox capacity but are outside this app's observed usage; UI rate/capacity information must not pretend otherwise.

### Durable intent and conservative response classification

Before each HTTP request, transactionally confirm the claim, freeze exact recipient/subject/body, insert a unique attempt in an in-flight intent state and persist pacing state. Commit before launching. Store UTC instants internally and format user-facing RFC3339 values in Asia/Kolkata with +05:30.

Use the connected token source and a small net/http Graph adapter calling /me/sendMail with HTML, one recipient and no attachments. Retain default Sent Items behavior. A 202 with its empty response body is SUBMITTED, not delivered and not a provider message ID. Attempt/correlation IDs are local audit identifiers, not Graph idempotency guarantees.

Use an explicit bounded request timeout and no automatic send-POST retry, redirect replay or undocumented idempotency header. Automatically retry only explicit throttling or proven pre-submission failures, with at most three safe retries before pausing the affected task for operator action. Never classify a generic 5xx, timeout or disconnect after possible transmission as safely retryable. A definite recipient rejection affects that row; authorization/permission or storage failure stops further launches for the mailbox. Persist every outcome immediately and before releasing its claim.

If intent persistence fails, send nothing. If saving a response fails, stop launches; the saved unfinished intent remains conservatively uncertain. SQLite commits must be durable enough for the documented crash model; no unflushed in-memory result batch is authoritative.

### Approval and action boundaries

Store successful test attempt and operator approval separately, keyed to the exact configuration content hash. The operator checks the received email and then approves. A new configuration starts unapproved; additional Excel tasks using the same configuration do not require another formatting test. Tests use a selected valid sample or supplied sample values and an explicit test address; they never mark a candidate submitted.

Start, resume, retry-all and manual uncertain resend are distinct protected actions with durable action IDs and transactional idempotency against browser repeats. A count preview is not a reservation; confirm/recheck/claim atomically at the action boundary and report changed exclusions.

### Stop, crash and manual review

Manual stop cancels only future launches and waits for all in-flight requests under their own deadlines. Browser closure and logout do not cancel the worker context. On SIGTERM, stop launches and drain before PocketBase closes the database; use a request deadline shorter than the container grace period.

On startup, after obtaining ownership, mark unfinished intents REVIEW_REQUIRED and previously active tasks interrupted. Release only safe unstarted claims; never release uncertainty as ordinary eligible work. Explicit resume can continue safe candidates even when some are held. Reconnecting or a successful diagnostic test never resumes tasks automatically.

Retry and Retry all failed use unchanged task snapshots and exclude ineligible, active, submitted and uncertain recipients. Send again for uncertainty requires the duplicate-risk confirmation and creates a new attempt retaining the earlier record. Mark as already submitted records a manual resolution and resolution time without inventing a Microsoft acceptance timestamp.

### Live progress and export handoff

Reuse htmx polling every two seconds while a task is active; reconnect loads durable totals and recent results. This is UI batching only; outcomes are saved per response. Avoid WebSocket/SSE infrastructure for this single-operator view.

Record a result-generation-needed marker when work completes/stops/is recovered. Change 5 consumes it and publishes downloads idempotently. A failed export never requeues email work. Do not enable production candidate sending until change 5's release verification is complete.

## Risks / Trade-offs

- Crash between durable intent and actual transmission can produce false uncertainty -> prefer manual review over an automatic duplicate.
- Up to four requests can be uncertain after termination -> show each independently and allow safe remainder resume.
- Published provider limits are ceilings, not guaranteed throughput -> shared cooldown, conservative default and runtime error handling.
- SQLite is suited to this single instance and small submission rate -> use brief transactions and test concurrent starts with the race detector; replicas remain unsupported.

## Migration Plan

Require changes 1-3 first. Add attempt/approval/action/settings migrations and lifecycle hooks, then ship controls only after simulated failure and restart tests pass. Keep all real candidate sending gated until change 5. Rollback requires stopping/draining, preserving attempt history and restoring compatible application/schema together; do not roll back to code that interprets uncertain attempts as unsent.

## References

- [Graph sendMail and 202 semantics](https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0)
- [Outlook per-app/mailbox concurrency](https://learn.microsoft.com/en-us/graph/throttling-limits#outlook-service-limits)
- [Exchange Online sending limits](https://learn.microsoft.com/en-us/office365/servicedescriptions/exchange-online-service-description/exchange-online-limits#sending-limits)
- [Graph Retry-After guidance](https://learn.microsoft.com/en-us/graph/throttling)
- [Go rate limiter](https://pkg.go.dev/golang.org/x/time/rate)
