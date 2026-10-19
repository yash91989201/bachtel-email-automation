# Design

## Context

See `proposal.md` - Why, and the delta specs for behavior. Current constraints the approach must fit:

- `internal/sending` is one durable store plus one server-owned dispatcher. `Store.Act` is the only start path today: one transaction rechecks the campaign revision, test approval, release gate and candidate eligibility, then reserves rows. The dispatcher runs `NextWork` only for tasks already `running`; `NextWorkDeadline` only reports test/candidate `retry_at` times and the run loop sleeps until a wake or that deadline.
- `internal/intake` owns task status `staging` and `ready`; `ready` never sends by itself.
- `Store.Recover` runs at dispatcher start after the ownership lock and marks `running`, `waiting` and `stopping` tasks `interrupted`. No path auto-resumes.
- Candidate sending is compiled off (`sending.Options{}` leaves `AllowCandidateSending` false) until the delivery change's release verification. Any schedule path must obey the same gate.
- Operator timestamps are shown in fixed `Asia/Kolkata` (+05:30); there is no timezone setting. A Docker server often runs UTC, so a wall-clock time must not be interpreted in the process's local zone.

## Goals / Non-Goals

Goals: one durable, single-shot schedule per task; unattended fire that reserves exactly what is free at that instant; schedule survives restart without a new goroutine or a second scheduler; operator-visible state for scheduled, fired and missed.

Non-Goals: recurring schedules, multiple schedules per task, scheduling retries/resends/result generation, timezone selection, moving the manual Start path, changing pacing or concurrency.

## Decisions

### Store the schedule on `send_tasks`

Add `schedule_at` (date, UTC instant) and `schedule_state` (`none`, `scheduled`, `fired`, `missed`) to `send_tasks` in one new migration. A task has at most one schedule, so a separate collection only adds joins and cleanup. `schedule_at` stays after fire as history; `schedule_state` is the single source of truth for whether a fire is pending.

Alternative rejected: recurrence rules. The operator asked for one run; recurrence changes recovery, cancellation and the reservation story.

### Enabling a schedule performs the start authorization

`Act` gains a `schedule` kind, or an equivalent store method, used by the web route. It runs in one transaction with the same preconditions as `start` (ready status, current campaign revision, approved test for the pinned hash, release gate, at least one eligible candidate), rejects a time earlier than now+30 minutes, writes a `sending_actions` row with the operator's counts, and stores the instant. It does not reserve candidates.

The route runs behind `Dispatcher.Synchronize` like `stop` and `rate`, then calls `Wake`, so setting a schedule cannot interleave with a launch.

The 12-hour form maps date + hour + minute + AM/PM to an instant in the fixed +05:30 zone, then stores UTC. Rejecting in both the form and the server keeps a stale page from storing a past time.

### Fire reuses the reservation transaction, at fire time

Add a store entry point, e.g. `FireScheduled`, that runs one transaction:

1. load the task; require status `ready` and state `scheduled` and `schedule_at <= now`;
2. recheck release gate, campaign revision, approval and eligibility exactly as `start` does;
3. reserve the currently eligible rows with the existing compare-and-swap claim; excluded rows keep their reason (`Already scheduled` for rows taken by another task);
4. require at least one free candidate, set status `running`, set state `fired`, save the action row.

The action ID is derived deterministically from task ID and `schedule_at`, so a repeated fire or a retried transaction cannot create a second start. If the task already left `ready`, the fire is a no-op.

Alternative rejected: calling `Act` with a synthesized confirmation. `Act` compares the claimed count to the operator's expected count and would abort whenever eligibility changed, which is exactly the unattended case.

### Dispatcher observes the earliest schedule as a deadline

`NextWorkDeadline` also returns the earliest `schedule_at` among `ready` tasks with state `scheduled`, and the run loop fires it when due before continuing normal launches. No new goroutine, ticker or ownership: the existing wake channel plus deadline sleep already covers schedule set, restart and the fire moment. The schedule write wakes the dispatcher so the new deadline is seen immediately.

Fire order: due schedules are evaluated before candidate launches in the same opportunity, so a due task starts even while another task is running, sharing the same pacing and in-flight limits.

### Missed schedule is detected in recovery, not by the live loop

`Recover` runs once at startup after the lock, before the coordinator loop. Any `scheduled` row with `schedule_at` in the past at that point is a downtime miss: set status `paused`, write an operator-facing reason naming the missed time, and set state `missed`. Nothing was started, so no attempt exists.

The live loop never treats a due schedule as missed: it fires normally. This keeps restart before the time working (state stays `scheduled`) and avoids sending after downtime, matching `sending-recovery`.

### Refusal cases do not pause a never-started task

When the revision, approval or release gate fails at fire time, the fire sends nothing and records state `missed` with the reason, while the task stays `ready` so the operator can revalidate, re-approve, reschedule or use Start. Only the downtime miss sets `paused`, because that is the case the recovery spec names and the operator Resume flow exists for it.

### Fire starts a first batch only

Retries, `Retry all failed`, resends and resolution keep their explicit confirmations. A scheduled fire never calls those paths, so it cannot create a duplicate send from an uncertain outcome.

## Risks / Trade-offs

- [Operator sees a count that is lower at fire time] -> the sending and task pages show the scheduled instant and state; excluded rows keep their reasons; the review copy states that only candidates free at that time start.
- [Approval or revision changes between schedule and fire] -> the fire transaction rechecks both and refuses instead of sending stale content.
- [A fire and a manual start race for the same candidate] -> both go through the existing atomic claim; the loser reports Already scheduled.
- [Server clock jumps or a long restart straddles the time] -> recovery compares the persisted UTC instant with the current clock; no local zone is used.
- [Schedule set exactly while the dispatcher is launching] -> `Synchronize` serializes the write with launch; the derived action ID makes any repeat idempotent.
- [Candidate sending remains gated in production] -> the fire path returns the same release-gate refusal; scheduling UI is usable only for tests until verification passes.
- [Stale form time just over 30 minutes] -> the server is authoritative and rejects times that are no longer far enough ahead.

## Migration Plan

One additive PocketBase migration adds the two fields with defaults; existing tasks get `none` and behave exactly as before. Deploy application and schema together. There is no down migration that drops the fields, matching the existing sending migrations: rollback restores a database backup taken before deployment, after stopping the service and draining in-flight work. Candidate dispatch stays gated, so deployment cannot start a real batch.

## Open Questions

None. The UI location (sending page beside pacing and controls, with state mirrored on the task page) and the deterministic action ID format are implementation details that do not change the specs or the approach.
