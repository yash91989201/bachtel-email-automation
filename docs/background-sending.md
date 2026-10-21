# Background sending and recovery

There is no global release unlock or environment switch for candidate sending.
Each candidate start requires explicit operator authorization and approval of
the received formatting for a successfully submitted test of that exact
configuration. Diagnostic test emails are explicit real submissions when a
usable mailbox connection is configured; automated checks use simulated
senders and never send real email.

## Test, approve and start

1. Validate an Excel task and open its **Sending and recovery** page. Review
   the connected mailbox address, pinned configuration hash, eligible count
   and exclusions.
2. Choose a valid sample row and enter the test recipient. The subject and HTML
   use the pinned campaign renderer; only the recipient is overridden. A test
   has its own attempt record and never changes campaign candidate eligibility.
3. Check the email in the test inbox. Explicitly confirm its subject, values
   and formatting, then approve that exact configuration. A `202 Accepted`
   response alone never grants approval.
4. Confirm the fresh recipient count for Start. The server rechecks approval,
   current configuration, row eligibility and campaign reservations in the
   transaction that accepts the action. A changed count requires confirmation
   again. Repeated action IDs return the previously accepted action.

An additional task using the same approved configuration can reuse approval.
Changing the subject, DOCX, mapping, fallback or normalization creates a new
configuration requiring another test and approval. An already started task
continues to use its pinned snapshot for resume and retries.

## Scheduled start

A validated task accepts one single-shot schedule: a date and a 12-hour clock
time (hour, minute, AM/PM) stored as one absolute UTC instant. The instant must
be at least 30 minutes ahead; operator-visible times carry the explicit India
offset, such as `2026-10-20T09:30:00+05:30`. A schedule never repeats, and
enabling one is that task's explicit start authorization. A disabled schedule
leaves the ordinary manual Start action in place, and validation never starts
sending by itself.

Confirming a schedule shows the same fresh counts as Start — eligible,
invalid, duplicate, already submitted, already scheduled and review required —
plus the sender, pinned configuration revision and approved test. The
dispatcher fires due schedules from durable state; it survives restarts and
does not depend on a browser session. At fire time it rechecks the pinned
configuration revision, the approved test for that exact hash and current
eligibility, then reserves only candidates free at that moment. Candidates
taken in the meantime are excluded as already scheduled; the remaining free
candidates start. With no free candidate, nothing starts.

A fire-time refusal — changed revision, missing approval or no free candidate —
sends nothing, records the reason, marks the schedule missed and leaves the task
ready for revalidation, re-approval, rescheduling or a manual Start. Firing
starts the first candidate batch only: retries, Retry all definite failures,
resends and uncertain-outcome resolutions remain explicit operator actions.
Scheduling is an explicit start authorization and requires the same
exact-configuration test approval as manual start.

## Durable states

Task lifecycle and row outcomes are separate. A task may complete with failures
or held rows; completion does not imply every recipient was accepted.

| Task state | Meaning |
| --- | --- |
| `staging` | Workbook is not validated and cannot send |
| `ready` | Snapshot validated; an explicit start is required |
| `running` | Server owns the accepted work and may launch safe rows |
| `stopping` | Future launches stopped; active requests are draining |
| `stopped` | Drained; safe remainder needs explicit resume |
| `paused` | Authorization, service, storage or retry limit needs attention |
| `interrupted` | Startup recovered active work; safe remainder needs explicit resume |
| `completed` | Accepted work settled; inspect failures and held counts |

The schedule on `send_tasks` is a separate axis from the task lifecycle. Its
`schedule_state` records where the single-shot instant stands:

| Schedule state | Meaning |
| --- | --- |
| `none` | No schedule; start uses the manual action |
| `scheduled` | Single-shot instant stored; the dispatcher owns firing |
| `fired` | The dispatcher started the first batch from the schedule |
| `missed` | Fire-time refusal or a passed instant; reason recorded, no send |

The original intake membership state and raw/normalized values stay separate
from `dispatch_state`. Invalid and duplicate rows remain file-only exclusions.

| Dispatch/attempt state | Meaning |
| --- | --- |
| `reserved` | Candidate is claimed for this task; no request has started |
| `queued` | Explicit test waiting for a paced opportunity |
| `in_flight` | Exact content and intent committed before network submission |
| `submitted` | Microsoft returned definite acceptance (`202`) |
| `definite_failure` | Definite non-acceptance; eligible failures may be retried explicitly |
| `correction_required` | Definite recipient/data rejection; correct the source and upload again |
| `retryable` | Proven non-submission or explicit throttling; bounded safe retry |
| `auth_failure` | Authorization failed; reconnect and take an explicit action |
| `review_required` | Acceptance cannot be ruled out; automatic retry prohibited |
| `manually_submitted` | Operator verified prior submission; audited separately |

`submission_attempts` retains exact recipient, subject, HTML, configuration,
task, candidate association and local attempt identity for each request.
`test_approvals` records received-format approval; `sending_actions` records
idempotent operator actions; `manual_resolutions` records actor, basis and time.
`dispatcher_state` retains the configured rate, recent starts and cooldown.
All collection API rules remain private.

UTC instants are stored internally. Operator-visible attempt times use RFC3339
with the explicit India offset, such as `2026-10-18T12:30:00+05:30`.
SUBMITTED means acceptance for processing, never delivery. A manual resolution
does not fabricate a provider acceptance timestamp or provider message ID.
Local attempt IDs are audit identifiers, not Microsoft idempotency guarantees.

## Pacing and provider responses

Each mailbox has one coordinator using `golang.org/x/time/rate` with burst 1.
At most four submission requests run at once per mailbox; there is no
application-wide cap. A mailbox defaults to 25 starts per minute; browser,
server and stored settings accept only integers 1 through 30. Starts are evenly
spaced, and persisted recent starts enforce the rolling allowance after rate
changes and restarts. When there is no eligible work, the coordinator waits for
an operator action, a submission result or the earliest persisted retry time.
Tests receive a normal paced opportunity before queued candidate work.
Candidate tasks receive round-robin opportunities.

Microsoft `429` responses establish a mailbox cooldown from Retry-After seconds
or HTTP date. Missing headers use bounded exponential backoff with jitter.
Only explicit throttling and proven pre-submission failures permit automatic
retries, with at most three retries. Authorization and permission failures
pause the affected mailbox's tasks. Generic service failures, timeouts and
disconnects that might follow transmission require review, never an automatic
resend. Cooldowns, throttling, authorization failures and service pauses affect
only the affected mailbox; a shared storage failure stops all launches.

The adapter sends one HTML email to one recipient through `/me/sendMail`, with
no attachments, no redirect replay, no hidden send-POST retry and a bounded
deadline. Sent Items uses Microsoft's default behavior. Other Outlook clients
use mailbox capacity outside this application's observations, so the configured
rate does not guarantee provider throughput.

## Stop, interruption and recovery

Stop prevents future launches and drains active requests under their independent
deadlines. Closing a tab, losing a browser connection or logging out does not
cancel the worker. Progress polls durable totals and recent outcomes every two
seconds; reopening the page reconstructs the same state from SQLite.

At startup the dispatcher first obtains an advisory lock in the data directory.
A second process using the same directory cannot dispatch. This is a single
Linux-host design; separate hosts or replicas sharing storage are unsupported.
The production database uses WAL and FULL synchronous commits on every pooled
connection. Intent persistence failure sends nothing. Outcome persistence
failure halts launches, leaving the unfinished intent for conservative recovery.

SIGTERM stops launches and drains before PocketBase closes SQLite. Requests
have a 20-second deadline; the dispatcher gets 25 seconds within Compose's
30-second grace period. A forced kill or host interruption can leave unfinished
intents. Startup marks each one REVIEW_REQUIRED, preserves definite outcomes
and pacing/cooldown, releases only safe unstarted claims and marks active tasks
interrupted. Even a crash before actual transmission may require review: this
conservative choice avoids an automatic duplicate.

A schedule instant that passed while the server was not running pauses the
task with a missed-schedule reason and creates no attempt. The operator
resumes the safe remainder explicitly, which clears the schedule state, or
reschedules. A restart before the stored instant keeps the same stored time.

Reconnect the mailbox (Microsoft 365 or Titan) when needed, then explicitly
Resume the safe remainder.
Held recipients do not block safe rows. A successful diagnostic test or
reconnection never resumes a candidate task automatically.

## Retry and uncertain-outcome review

Retry one definite failure or confirm **Retry all definite failures** using the
refreshed count. These actions use unchanged task snapshots and exclude
submitted, invalid, duplicate, active and unresolved uncertain candidates.
Rows requiring corrected source data need a corrected upload.
The failed total includes those rows, while the Retry all confirmation counts
only retryable definite failures. After a retry or review batch completes,
Resume can still continue the safe remainder of a previously started task.

For an uncertain candidate, inspect its prior attempt and check externally:

- **Send again** requires explicit possible-duplicate confirmation and creates a
  new attempt under the normal pacing controls. The old uncertain attempt stays
  in history.
- **Mark as already submitted** requires a verification basis. It records the
  operator and resolution time, excludes the candidate from future submission,
  and leaves provider-confirmed acceptance time unset.

Completed, stopped and recovered tasks carry a durable
`result_generation_needed` marker. The process-owned export worker consumes it,
and the task page offers generation or retry when no ready file is published.
Artifact failure or retry never creates an email attempt or changes
eligibility; see [history-exports.md](history-exports.md).

Rollback requires stopping and draining, preserving all attempt history, then
restoring compatible code and schema together. Sending migrations intentionally
do not delete audit history on rollback. Never restore code that interprets
uncertain attempts as ordinary unsent candidates.

## Verification

Run `make test` and `go test -race ./...`. Sending tests use temporary SQLite
stores, real XLSX intake, controlled time and simulated Microsoft endpoints.
The acceptance suite processes 1,000 candidates, checks cross-task claims and
repeated actions, drains four requests and recovers three interruption points.

An opt-in browser harness is available without adding debug routes to the app:

```sh
BACHTEL_BROWSER_HARNESS=/tmp/bachtel-sending-browser \
  go test ./internal/sending -run '^TestAcceptanceBrowserHarness$' \
  -v -count=1 -timeout=20m
```

The directory's `harness.json` contains the temporary task URLs and dummy login.
Its `control.json` supports `hold`, `reject` and `uncertain` outcomes, an integer
`restart` counter, and `done: true` to exit. Every harness submission is simulated;
it does not contact Microsoft or send email.
