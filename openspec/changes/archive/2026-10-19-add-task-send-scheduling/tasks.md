# Tasks

## 1. Schedule state

- [x] 1.1 Add one additive migration adding `schedule_at` (date) and `schedule_state` (text, default `none`) to `send_tasks`, with indexes only if a query needs one; verify with a migration test that a fresh database has both fields and an existing task defaults to `none`.
- [x] 1.2 Expose the schedule on the intake task model (`ScheduleAt`, `ScheduleState`) in `LoadTask`/`ListTasks`; verify with an intake service test that a task created before and after the migration loads `none`.

## 2. Schedule authorization and entry

- [x] 2.1 Add the sending store method that enables or clears a task schedule inside one transaction: require status `ready`, current campaign revision, approved test for the pinned hash, release gate when enabling, at least one eligible candidate, and a time at least 30 minutes ahead; record a `sending_actions` row and set `scheduled` or `none`. Verify with store tests covering too-soon, unapproved, stale revision, zero eligible, enable, replace and clear.
- [x] 2.2 Add `POST /campaigns/{id}/tasks/{task}/sending/schedule` behind `requireOperator`, run it behind `Dispatcher.Synchronize`, then `Wake`; verify with a web test that a valid schedule stores the instant, a too-soon time returns 400 with the reason, and a forged or repeat submission cannot create a second start.
- [x] 2.3 Add the 12-hour schedule form (future date, hour 1-12, minute, AM/PM, enable/clear) to the sending page beside the existing controls, show the stored time with `+05:30` and the scheduled/fired/missed state on the sending and task pages, edit the `.templ` sources and regenerate committed output; verify with a rendered-view test and `make check` generation consistency.

## 3. Scheduled fire

- [x] 3.1 Add the store fire path in one transaction: load the task, require `ready` + `scheduled` + due, recheck release gate, revision and approval, reserve only currently eligible candidates with the existing atomic claim, require at least one free candidate, set status `running` and state `fired`, and save the action under an ID derived from task and instant. Verify with store tests: eligible rows claimed, taken rows reported `Already scheduled`, repeated fire creates no second action or claim, and not-ready or not-scheduled is a no-op.
- [x] 3.2 Extend `NextWorkDeadline` to return the earliest pending `schedule_at` and make the run loop fire due schedules before normal launches; verify with a worker test using a fake clock and `After` that a wake at the stored instant starts the task, and that a schedule set while idle is observed without waiting for the old deadline.
- [x] 3.3 Handle refusal at fire time: closed release gate, changed revision or missing approval, and zero free candidates send nothing, record the reason and set state `missed` while the task stays `ready`; verify with store tests asserting no attempt and the operator-visible reason for each case.

## 4. Recovery

- [x] 4.1 Extend `Recover` so a `scheduled` task whose instant passed while the process was down becomes `paused` with a missed-schedule reason and state `missed`, creating no attempt; verify with a recovery test that restarts a store whose clock passed the instant.
- [x] 4.2 Verify an unfired future schedule survives restart unchanged, and that the operator Resume path after a missed schedule clears the schedule state and starts the safe remainder through the existing resume action; verify with a store/worker restart test exercising both cases.

## 5. Documentation and integration verification

- [x] 5.1 Update `docs/background-sending.md` and the README sending section with the single-shot schedule, the 30-minute minimum, the +05:30 display, the missed-window pause and the unchanged release gate; verify the documented flow against the implemented routes and states.
- [x] 5.2 Run `make check`, `make vulncheck` and `openspec validate --all --strict --json --no-interactive`; verify all pass and report any skipped coverage.
- [x] 5.3 Add one integration test using a temporary database and simulated sender: schedule a task, restart across the stored instant, confirm the task is paused with no attempt, then resume and confirm sends occur only for candidates free at resume time.

## Workflow follow-up

- Archive the change after the project's review requirements are satisfied.
- Run `make verify-converter` if implementation touches the converter or campaign content path (not expected for this change).
