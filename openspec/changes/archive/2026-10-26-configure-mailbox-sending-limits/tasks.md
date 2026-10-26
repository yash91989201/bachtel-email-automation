# Tasks

## 1. Persist and validate mailbox sending settings

- [x] 1.1 Add a new dispatcher-state migration for integer concurrency 1–10, backfill existing rows to 2, and retain stored rates/history/cooldowns; verify upgrade, fresh database and rollback behavior with temporary-database migration tests, including saved pacing 25 and a custom rate.
- [x] 1.2 Extend `sending.Settings`, defaults and new-row initialization to concurrency 2 and pacing 15; validate both persisted values and update pacing tests to verify default spacing, range boundaries and rolling allowance without weakening existing history/cooldown checks.
- [x] 1.3 Replace rate-only persistence with an owner-scoped atomic settings-pair update and adapt its internal callers; verify valid boundaries, missing/fractional/out-of-range values, other-owner isolation, missing current mailbox, rollback on save failure and preservation of already saved values/history through focused store tests.

## 2. Enforce configured concurrency in the dispatcher

- [x] 2.1 Replace both hardcoded four-slot checks in `nextWork` and `launchPrepared` with the current mailbox limit; update `worker_test.go` and `acceptance_test.go` to verify default 2, explicit limits 1 and 10, shared task/test/retry capacity, independent mailboxes and saturated-mailbox event waiting.
- [x] 2.2 Integrate settings saves with the existing launch synchronization and wake path; use controlled simulated providers to verify lowering 4 to 2 drains existing requests, raising the cap admits additional paced work, and sender preparation racing with a save rechecks the final limit.
- [x] 2.3 Verify settings changes and restart preserve recent starts, cooldowns, applicable approvals and durable outcomes; add focused regression cases showing that settings saves neither resume paused/interrupted work nor bypass fatal storage, uncertainty or explicit sending authorization restrictions. Run affected sending tests and `make lint` after the related edits are integrated.

## 3. Expose both settings in the task Sending section

- [x] 3.1 Extend the existing `/sending/rate` handler to require and save both values under the dispatcher barrier; verify owner/CSRF checks, rejection of stale rate-only submissions, atomic failure, safe error feedback and successful redirect with focused `internal/web/sending_test.go` coverage.
- [x] 3.2 Populate saved concurrency and separate submitted drafts in the task view; extend the existing accordion with both labeled numeric controls, specified ranges/defaults, mailbox scope, dynamic concurrency summary and one Update sending settings action. Verify rendered values and validation/error/success states in view tests, then run `go tool templ fmt internal/web/views/task.templ` and `go tool templ generate -f internal/web/views/task.templ`.
- [x] 3.3 Verify both drafts and focus survive live progress refreshes, failed saves open the accordion, and success reloads show the saved pair across tasks sharing the mailbox; check keyboard use and 360-pixel layout with isolated test data and record the result. Update any directly affected sending-settings guidance to match the form.

## 4. Integrated verification

- [x] 4.1 Run the relevant focused migration, sending, web and view test selections with temporary storage and simulated providers; record actual commands/results and confirm no automated check reads local secrets or sends real mail.
- [x] 4.2 On a fresh checkout run `bun install --frozen-lockfile`; stage only this change's intentional generated output before generation checking, then run `make check` and `make vulncheck` once after integration. Run `make verify-converter` if implementation expands into converter, campaign, dependency, Docker or release changes; record any applicable skipped coverage and preserve unrelated work.
- [x] 4.3 Run `openspec validate --all --strict --json --no-interactive` and `git diff --check`; reconcile artifacts with implemented behavior and mark tasks complete only after their implementation and required verification pass.
