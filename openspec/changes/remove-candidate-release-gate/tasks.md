# Tasks

## 1. Remove the release gate

- [x] 1.1 Remove the store release switch, error and guards; simplify simulated fixtures and obsolete gated tests. Verify default-store candidate dispatch after approval, approval refusals for candidate action kinds, and schedule approval/restore-review checks with focused sending tests.
- [x] 1.2 Remove release-gate view state and warnings, retain approval-based controls, and update task guidance and view tests. Verify rendered start and schedule controls require approval and the old warning is absent.
- [x] 1.3 Update active README, runtime/release guidance, AGENTS.md and OpenSpec context to the operator-approved policy. Preserve historical verification evidence; verify active guidance describes test approval and explicit authorization without a global release lock.

## 2. Integration verification

- [ ] 2.1 Run make fmt, make lint, make check, make vulncheck, make verify-converter and strict OpenSpec validation. Record actual outcomes and any blockers without sending real emails or restarting the live application.
