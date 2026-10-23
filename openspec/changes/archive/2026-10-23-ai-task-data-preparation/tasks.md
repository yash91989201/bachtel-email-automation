# Tasks

Planning only; all tasks remain incomplete. Use temporary data and simulated providers.

## 1. Implementation and focused verification

- [x] 1.1 Add owner-scoped preparation-run/job migrations without publishing candidates; verify durable restart, owner isolation and task-closure cancellation with temporary-database tests.
- [x] 1.2 Version and display the design appendix base prompt with header-first guidance, task overrides, comma-space lists and clear resolved-correction versus unresolved-finding output; verify synthetic fixture tests cover names, acronyms, compounds, empty markers, misplaced compensation, uncertain certificate text and untrusted cell instructions.
- [x] 1.3 Implement structured output validation and mapped-fields-only payloads; verify tests reject unknown, omitted, repeated or mistyped rows/fields, invalid finding scope and changed/fabricated/dropped recipient occurrences, including invalid/empty/duplicate siblings.
- [x] 1.4 Implement durable server-owned preparation jobs and explicit retry/rerun with brief publication transactions; verify tests for browser closure, restart, failed/incomplete batches, stale late responses and no silent raw-data fallback.
- [x] 1.5 Persist instruction drafts separately from immutable source/prompt/connection-generation/endpoint/API-format/model/options/output/finding evidence; verify tests show draft edits and prompt/model updates do not alter completed results, and run selection remains preparation-only until snapshot integration and every batch/retry retains its confirmed configuration.

## 2. Integration verification

- [x] 2.1 Verify this change independently with temporary databases and simulated providers, including ownership, failures, races and the proposal dependency/activation boundary; record results and do not enable downstream unfinished workflows.
- [x] 2.2 Run npm ci on a fresh checkout, format/regenerate only changed templ files, stage intentional generated output and run make check, make vulncheck, make verify-converter and openspec validate --all --strict --json --no-interactive; report actual local results and skipped coverage without claiming deployment.

## Workflow follow-up

- Begin only on an explicit apply request and after verified prerequisites.
- Archive only after implementation and required verification.

## Verification record

2026-10-23 — implementation and independent verification with temporary databases and simulated providers:

- `go test ./internal/preparation/ ./internal/migrations/ ./internal/ai/ ./internal/intake/ ./internal/sending/ ./internal/web/ ./cmd/server/` — pass. Covered ownership isolation, concurrent prepare, queued/running closure cancellation, restart interruption with explicit retry, failed batches, stale late responses, draft/model immutability of completed evidence, preparation-only selection, mapped-only payloads, complete response coverage and the synthetic normalization fixtures.
- `npm ci` — pass.
- `make check` — pass: format, generation consistency, lint with 0 issues, `go test -race ./...` and the production binary build.
- `make vulncheck` — pass: no reachable vulnerabilities. Six advisories exist in required modules but are not reachable from this code.
- `make verify-converter` — pass (packaged-Pandoc converter test image).
- `openspec validate --all --strict --json --no-interactive` — pass, 16/16 items.

Boundary: prepared output stays run evidence. Preparation writes no candidate,
membership, attempt, schedule or approval record, and no finalization, download
or sending route was enabled. `reviewed-task-sending-snapshots` remains
unimplemented, so prepared values do not authorize sending.
