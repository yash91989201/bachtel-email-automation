# Tasks

## 1. Share normalization without changing durable task behavior

- [x] 1.1 Extract the payload/inference/response-validation operation from `internal/preparation/worker.go` into a shared preparation operation; verify fake-provider tests cover exact mapped fields, prompt/instructions/options/session propagation, valid corrections/findings, omitted/duplicate/unknown fields and changed recipient rejection.
- [x] 1.2 Wire the durable worker through the shared operation while retaining captured run settings, batch order, cancellation/closure, progress and atomic publication; run focused `internal/preparation` regressions for successful multi-batch work, partial failure and interrupted recovery, confirming no partial output is published.

## 2. Implement temporary sample execution

- [x] 2.1 Add the owner-scoped one-row sample adapter using existing mapped-input, base-prompt, response and finding logic with no task instructions; verify deterministic fake-provider tests demonstrate the same normalization contract as a one-row task batch and no task/run/job/artifact writes.
- [x] 2.2 Capture confirmed AI configuration and matching credential consistently, retain generations privately and reject changed settings before dispatch/return; verify isolated `internal/ai`/sample tests for cross-owner denial, absent/unreadable credentials, configuration/key-change races and no cross-destination credential use or fallback.
- [x] 2.3 Add request-context execution with a 30-second overall deadline, one active sample per owner and four across the process, immediate busy refusal and cleanup on every exit; verify concurrency/cancellation/deadline tests prove bounded calls, no automatic retry, slot release and removal of inactive owner entries.

## 3. Add campaign actions and comparison UI

- [x] 3.1 Add the authenticated/CSRF-protected AI sample POST action alongside backend-only direct preview, validate revision identity, 1 MiB form size, cell bounds, required fields and single-recipient input before inference; verify handler tests for refusal without provider calls, cross-owner/unauthenticated access, stale revision, oversized data and unchanged direct preview behavior.
- [x] 3.2 Render validated prepared values through the existing renderer and expose original/prepared/effective comparisons, corrections, Needs review findings and safe errors; verify handler/view tests for optional fallback visibility, optional-field findings, missing required prepared data, invalid provider structure, recipient integrity, HTML escaping and subject injection refusal, including historical rendering compatibility.
- [x] 3.3 Extend the existing sample form/result with both explicit actions, destination/model/base-policy access, temporary provenance, external-processing/usage disclosure and final-snapshot distinction; verify view contracts and a local browser exercise against a simulated provider show original inputs preserved, accessible loading/errors and an Account settings remedy without blocking direct rendering.
- [x] 3.4 Invalidate/abort pending results when inputs change or another action supersedes them, prevent stale response swaps and recheck the revision before returning; verify a delayed fake-provider UI/handler scenario cannot overwrite a newer result, and dismiss/navigation/reload does not restore samples through app storage.
- [x] 3.5 Verify non-retention across success, provider failure, backend rejection, busy refusal and cancellation using temporary database/file fixtures, captured logs and no-store response assertions; inspect browser storage/history integration for sample persistence and confirm no snapshots, attempts, approvals, schedules or candidate state change and no email transport is called.
- [x] 3.6 Update relevant operator documentation for the two sample actions, temporary app retention, provider processing, base-policy-only behavior and unchanged task finalization; format and regenerate only changed `.templ` sources with the pinned tools and verify the documented flow matches the rendered UI.

## 4. Integrated verification

- [x] 4.1 Run focused affected-package tests and `make lint` after related edits are integrated; confirm existing task final preview/test/send use accepted snapshots without AI and worker regressions pass using simulated providers and isolated data, never local `.env` or real mailbox credentials.
- [x] 4.2 Run `bun install --frozen-lockfile` if implementation uses a fresh checkout, stage only intentional generated output for generation consistency, then run `make check`, `make vulncheck` and `make verify-converter` once on the integrated campaign change; resolve failures without weakening checks and record actual results or limitations.
- [x] 4.3 Run `openspec validate --all --strict --json --no-interactive`, review implementation against every delta scenario and update task evidence only for verified behavior; keep deployment and real email outside this change's automated checks.

## Verification notes

- Focused suites passed with fake providers and temporary data:
  `go test ./internal/preparation ./internal/ai ./internal/web ./internal/web/views ./internal/campaign ./internal/intake`.
- `make lint` reported 0 issues; `make check-format check-generated` passed and only
  `internal/web/views/campaign_templ.go` was regenerated from its `.templ` source.
- `make check` passed: format, generated consistency, lint, Tailwind lint, the
  production build, the JS tests and the full `go test -race ./...` suite.
- `make verify-converter` passed in Docker with the packaged Pandoc 3.x.
- `openspec validate --all --strict --json --no-interactive` passed (1 change, 15 specs).
- Local browser exercise against a simulated Responses API (temporary harness, removed
  afterwards): Account settings remedy without a connection, working direct render, AI
  result with provider/model/policy provenance, original/AI-prepared/effective values,
  corrections, optional-fallback visibility, accessible busy state (`aria-busy` plus a
  status line), result cleared on input edit, superseded request discarded, no result
  restored after reload, and zero task/run/job/snapshot/attempt/approval records.
- Limitation: `make vulncheck` fails on GO-2026-6866 and GO-2026-6860
  (`golang.org/x/net@v0.59.0`, fixed in v0.62.0) reachable through
  `campaign.rewriteBody`/`html.Parse`. The same two advisories fail at `HEAD` without this
  change and this change adds no dependency, so the upgrade was left to a dedicated
  change rather than bundled here.
- Outside these checks: deployment, hosted CI and real-email acceptance.
