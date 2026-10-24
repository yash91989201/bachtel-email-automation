# Tasks

## 1. Preflight validation for current mappings

- [ ] 1.1 Publish a header/revision preflight for role-mapping revisions: pin worksheet, header row, configuration, content hash and source-row count, clear prior manifest, exclusion artifact, category counts and selected preparation/final datasets, and create no candidates; retain the existing published snapshot for legacy format-mapping revisions.
- [ ] 1.2 Verify with focused `internal/intake` and `internal/finalizationtest` tests that a current-mapping validation yields zero candidates and zero duplicate/invalid counts, that missing or ambiguous headers still block while leaving the task staging with no pinned revision, that revalidation clears previously published selection, and that a legacy revision still publishes its historical snapshot and report.

## 2. Mapped rows as the preparation and review source

- [ ] 2.1 Accept header-checked and frozen tasks in the mapped-row source boundary while still refusing staging tasks, and keep the stored-workbook digest check.
- [ ] 2.2 Verify that preparation input covers every mapped non-blank row independently of any admission snapshot, that a staging task is refused with an actionable message, and that a mismatched stored workbook is refused.
- [ ] 2.3 Confirm that a preflighted task with no memberships is not reported as an unpublished validation snapshot by the start preflight, and that starting stays unavailable until a final snapshot exists.

## 3. Prepared review, approval and downloads

- [ ] 3.1 Add a read-only prepared review that classifies the selected completed run's cleaned values, returns source rows, entries, accepted entries and held entries with reasons, and creates no candidate, membership or attempt; refuse incomplete, foreign or stale runs.
- [ ] 3.2 Derive the prepared sending sheet and prepared exclusion report from reviewed accepted and held entries respectively, and re-evaluate classification at approval, including a source-digest mismatch refusal.
- [ ] 3.3 Publish memberships whose stored values are the accepted cleaned values that preview, test and sending render, keeping original uploaded values in the workbook and held-entry review evidence.
- [ ] 3.4 Verify review counts for duplicates, invalid addresses, missing values, campaign history and AI findings; verify the entry bound refusal, operator isolation, prepared sheet and report contents, approval idempotency and stale-digest refusal, and that an edited final upload keeps its deterministic behavior and report.
- [ ] 3.5 Verify a rendered preview, test and sending body use cleaned values for a membership created from prepared data, and that held entries remain absent from memberships.

## 4. Workspace presentation

- [ ] 4.1 Present current role-mapping tasks with source rows and pending eligibility before review, reviewed counts, accepted entries and held reasons afterwards, and final snapshot counts once finalized; keep legacy validation totals, rows and report available only for legacy revisions.
- [ ] 4.2 Derive a pending-eligibility main status before the zero-eligible case and resolve exclusion downloads as final snapshot, then prepared review, then legacy validation report.
- [ ] 4.3 Regenerate only the changed templ output with the pinned tools, and verify view tests cover both role-mapping states, legacy retention, download precedence, and the pending-eligibility status label.

## 5. Documentation and required checks

- [ ] 5.1 Update `docs/task-intake.md` and `docs/final-sending-snapshots.md` for the preflight, review, approval-bound and download order, including the retained legacy behavior.
- [ ] 5.2 Run focused tests for `internal/intake`, `internal/finalizationtest`, `internal/web` and `internal/web/views`, then `make lint` and `openspec validate --all --strict --json --no-interactive`, and stage intentional generated output before the repository-wide checks.
- [ ] 5.3 Run `make check` and `make vulncheck` once on the integrated change and record actual results; do not weaken checks to pass.
