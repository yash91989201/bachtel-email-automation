# Verification

No real emails were sent. All test data and providers are temporary/simulated.
The existing Air watcher (PID 1376162) was paused with SIGSTOP before code edits
to prevent automatic activation. During verification it and its child server
were replaced outside this task's tool actions by a new Air/server process.
A subsequent read-only database check showed one ready, unscheduled task and
no candidate submission attempts (only two existing submitted test attempts).
No live tasks, schedules, approvals or mailbox settings were changed by this task.

## Results

- Focused sending, schedule, approval and rendered-control tests: passed. The approval and schedule/restore-review regression subset also passed under `go test -race` (30.646 seconds).
- `make fmt`: passed.
- `make lint`: passed, zero issues, including after the restore-review guard.
- `make check`: passed (formatting, generated-output consistency, lint, full race suite and production build). The full race suite began before the final restore-review guard; the changed approval/schedule/restore-review subset was then checked under the race detector, final lint passed, and the full packaged-converter suite reran against the final code.
- `make verify-converter`: passed, including a full packaged-Pandoc test-suite rerun after the restore-review guard.
- `openspec validate --all --strict --json --no-interactive`: passed, 12/12 items.
- `make vulncheck`: failed with four reachable advisories in unchanged Excelize v2.11.0: GO-2026-6689, GO-2026-6688, GO-2026-6685 and GO-2026-6684. Dependency manifests were not changed. This is an outstanding repository check, not a clean release acceptance.

Review identified a due-schedule path that lacked a direct restore-review check.
A refusal and simulated regression coverage were added so a restored schedule
cannot reserve recipients or enter running while reconciliation is active.

## Remaining verification limitation

Implementation tasks 1.1–1.3 are complete. Task 2.1 remains unchecked because
`make vulncheck` did not pass. Resolving the existing dependency advisories is
separate from removing the release gate. No vulnerability check was weakened,
and this work does not claim production release acceptance.
