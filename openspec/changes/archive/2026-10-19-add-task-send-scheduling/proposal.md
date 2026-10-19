# Proposal

## Why

Operators must currently watch a validated task and press Start when they want sending to begin. They need to authorize one task in advance and have the server begin that send at a chosen future time, without a second confirmation, while the existing safety boundary (approval, pinned configuration, fresh eligibility, durable attempt recording) stays intact.

## What Changes

- Add a per-task single-shot schedule. The operator enables it and gives a future date and time entered in 12-hour form, at least 30 minutes after the current time; the schedule is stored as one absolute timestamp and never repeats.
- Enabling the schedule is that task's explicit start authorization: the operator sees the fresh eligible, invalid, duplicate, already-submitted, already-scheduled and review-required counts, the sender, the pinned configuration revision and the approved test before confirming.
- A disabled schedule keeps today's manual `Start` action. Validation success does not start sending on its own.
- The dispatcher fires due schedules from durable state, so a schedule survives restarts and never depends on a browser session.
- The fire rechecks the production release gate, the test approval for the exact configuration revision and campaign eligibility, then performs the same atomic candidate reservation as a start action, taking only candidates free at that moment.
- A schedule whose time passed while the server was not running, or whose configuration revision or approval no longer matches, pauses the task with an actionable reason and requires an operator action. No task starts unattended after downtime or a configuration change.
- A scheduled fire starts a first candidate batch only. Definite-failure retries, `Retry all failed` and uncertain resends stay explicit operator actions with their current confirmations.

## Capabilities

### New Capabilities

None. Scheduled start is when sending begins, so the behavior belongs to the capabilities that already own dispatch, intake review, eligibility and recovery.

### Modified Capabilities

- `email-dispatch`: a scheduled start is an accepted explicit authorization and a new single-shot scheduled start requirement defines the fire, its eligibility and its refusal cases.
- `excel-intake`: task review must offer the schedule control with the same fresh counts, and it must recheck configuration, approval and eligibility at fire time rather than only at the operator's confirmation.
- `campaign-candidates`: the atomic start-time reservation now also covers a scheduled fire, and the already-scheduled display covers candidates taken before a scheduled task fires.
- `sending-recovery`: recovery must pause a task whose scheduled time passed while the server was down instead of starting it, and preserve the single-shot schedule state.

## Impact

Affected code: `internal/intake` (task schedule fields and transitions), `internal/sending` (store fire path, dispatcher deadline and wake handling, restart recovery), `internal/web` (schedule route, task and sending view data), `internal/web/views` (`.templ` sources with regenerated output), one new `internal/migrations` migration adding the schedule fields to `send_tasks`, plus focused tests in those packages.

Depends on: the durable sending change (dispatcher, attempts, approvals, actions) and the delivery change's production release gate. Candidate sending stays disabled by code until that release verification passes, and the scheduled fire obeys the same gate as the manual start action.

No new dependency. Time is stored in UTC and shown with the explicit `+05:30` offset already used for operator-visible timestamps.

Out of scope: recurring or daily schedules, multiple schedules per task, scheduling retries, resends or result generation, a general-purpose job queue, timezone selection, and starting a task that has not been validated and approved.
