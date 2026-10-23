# Tasks

Implementation is complete. Integration verification records the actual local commands and results; use temporary data and simulated providers.

## 1. Implementation and focused verification

- [x] 1.1 Add owner-scoped final snapshots, task selection/state and mapping-version migrations; verify temporary-database migration tests preserve existing revisions, normalized memberships, attempts and started-task content.
- [x] 1.2 Extract shared backend validation for prepared and final data, retaining optional literal fallbacks, whitespace-only emptiness, mapped-field findings, rendering safety, address validation and campaign-history checks; verify tests distinguish optional empty, optional flagged, AI failure and required missing values.
- [x] 1.3 Integrate AI email separator cleanup before existing expansion and classify complete expanded input before candidate mutation; verify the 1,000-entry limit, all-occurrence duplicate exclusions, shared-field sibling holds and independent valid/invalid siblings.
- [x] 1.4 Implement idempotent owner-authorized Approve as-is with fresh backend checks and no reupload or AI call; verify tests for stale run/configuration, duplicate action IDs, zero eligible entries and ownership refusal.
- [x] 1.5 Implement edited-final-sheet acceptance without AI or content reformatting, allowing edited/removed/added/restored entries; verify tests preserve operator wording, apply only backend rules, reject unsupported email separators and ignore imported status or approval claims.
- [x] 1.6 Seal accepted final snapshots atomically and bind every accepted replacement upload to a fresh snapshot identity even for identical bytes; verify tests for mixed entry exclusions, atomic structural rejection, unsendable zero-accepted replacement, stale requests and unchanged historical evidence.
- [x] 1.7 Refuse snapshot/run changes under enabled schedules or after candidate sending has started; verify scheduled, running, paused, stopped, interrupted and completed cases and document the explicit new-task remedy.
- [x] 1.8 Remove format choices from new campaign mapping authoring, drafts and help, preserving recipient/required/optional/fallback behavior and versioned historical reads; verify mapping/parser/hash and legacy-rendering regression tests with direct campaign sample preview.
- [x] 1.9 Route final task preview, selected-row tests and candidate work creation through accepted final values without normalization; verify identical subject/body values across download/preview/test/send and zero provider-normalization calls after final upload.
- [x] 1.10 Persist task/snapshot/configuration/mailbox-generation identity on tests and received-format approvals; verify tests reject cross-task, old-snapshot, old-mailbox, pre-finalization and arbitrary-sample approvals, including queued old tests and identical-byte replacement uploads.
- [x] 1.11 Enforce current finalization, snapshot approval and explicit authorization in start, schedule creation/fire, resume, retry and resend; verify forged/stale requests cannot reserve or dispatch work and existing pacing, restore review, uncertain-outcome and mailbox-isolation behavior stays intact.
- [x] 1.12 Implement the documented historical-task transition without automatic AI calls, rewritten content or inherited new-snapshot approvals; verify migration/integration fixtures for existing unstarted schedules and already-started legacy tasks and document deployment/rollback guards.
- [x] 1.13 Generate prepared and final sending XLSX sheets containing accepted mapped values only, one recipient per row; verify Excelize round-trip tests for exact values/order, unique mapping labels (including shared source headers), no excluded/status rows and no formula execution.
- [x] 1.14 Extend protected exclusion reports with original/proposed values, affected fields, all reasons, reason source and precise source/recipient provenance; verify fixtures combine AI/backend/processing reasons, preserve originals and deny foreign artifact access.
- [x] 1.15 Preserve original and final upload provenance and frozen final values in post-execution task results; verify added/removed/reordered final rows, worksheet-name collisions, duplicate file-only evidence and existing automatic result-generation semantics.
- [x] 1.16 Add task preparation progress, visible base prompt, instruction draft/history, correction/finding review, downloads and both finalization controls within existing workspace sections; verify view/handler tests and document operator responsibilities, failure remedies and backend-only final validation.
- [x] 1.17 Clearly separate dataset approval, test/received-format approval and Start/Schedule states; verify UI/handler tests for unfinalized, replaced, zero-eligible, failed-preparation and approved-as-is paths with valid drafts and focus preserved.
- [x] 1.18 Inspect every affected campaign/task page with synthetic data at 360, 768 and 1440 CSS pixels and by keyboard; verify both review paths, downloads, errors and historical views remain usable. Do not use live mail or operator workbook personal data for fixtures.

## 2. Integration verification

- [x] 2.1 Verify this change independently with temporary databases and simulated providers, including ownership, failures, races and the proposal dependency/activation boundary; record results and do not enable downstream unfinished workflows.
- [x] 2.2 Run npm ci on a fresh checkout, format/regenerate only changed templ files, stage intentional generated output and run make check, make vulncheck, make verify-converter and openspec validate --all --strict --json --no-interactive; report actual local results and skipped coverage without claiming deployment.
- [x] 2.3 Run both complete end-to-end paths: prepare -> exclusions -> approve as-is -> exact-snapshot test/approval -> explicit send; edited upload -> backend validation only -> new snapshot -> fresh test/approval. Verify identical values across download/preview/test/send and no AI after final upload, with simulated providers.

## Workflow follow-up

- Begin only on an explicit apply request and after verified prerequisites.
- Archive only after implementation and required verification.
