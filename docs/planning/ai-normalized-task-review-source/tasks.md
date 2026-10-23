# Tasks

All items are implementation work and remain incomplete. Use temporary data and simulated providers; no local .env, live database, real mailbox credentials or real email in automated checks.

## 1. Data and configuration foundation

- [ ] 1.1 Add owner-scoped preparation runs, final snapshots, task selection/state/terminal-closure and mapping-version migrations; verify temporary-database migration tests preserve existing revisions, normalized memberships, attempts and started-task content.
- [ ] 1.2 Add one owner-scoped confirmed AI configuration with staged proposals, configuration generations and separately encrypted/versioned credentials using existing secret storage; verify temporary-database ownership, restart, missing-key, no-prefill and redaction tests and document Account setup without deployment-wide provider keys.
- [ ] 1.3 Verify current OpenCode Go deployment access, permitted usage, data handling and exact Muse/DeepSeek HTTP contracts independently of Pi; document readiness limitations and implement small Responses and Chat Completions adapters with OpenCode Go and Custom compatible setup, bounded requests, explicit model selection and no fallback. Verify simulated-server tests for both protocols, malformed output, timeout, throttling and size limits; surface any dependency or access-model change before adding it.
- [ ] 1.4 Implement synthetic exact-model validation and explicit configuration confirmation, staging outside activation transactions, same-configuration key repair and safe endpoint policy; verify failures/cancellation/stale callbacks retain active settings, private-address and cross-origin redirect requests cannot receive secrets, in-flight repair settles, failed runs require explicit retry and AI switching never resets task/history data.
- [ ] 1.5 Implement the account-wide pending-task predicate and owner-scoped serialization across task staging/reactivation, AI settings activation and every mailbox change/reset/disconnect path; verify race tests, cross-campaign blockers, staged/failed/paused/stopped/scheduled states, completed-task recovery reacquisition, settled-operation checks, initial setup and same-identity credential repair exceptions.
- [ ] 1.6 Implement idempotent terminal Close task with durable actor/time, queue/schedule cancellation, bounded settlement and worker/dispatch publication barriers; verify active/staged/stopped tasks, late AI responses, unknown submission outcomes, retained history/reservations, manual resolution, foreign requests and refusal of every subsequent processing/test/sending action. Document completion versus stop versus close.
- [ ] 1.7 Preserve existing mailbox settings/credentials/generations when relocating setup and applying locks to historical tasks; verify Microsoft callback, Titan reconnect, same-mailbox rotation and confirmed replacement/reset tests, including that pending tasks cannot be erased to bypass the lock and mailbox replacement retains AI settings.

## 2. Preparation runs and AI findings

- [ ] 2.1 Version and display the design appendix base prompt with header-first guidance, task overrides, comma-space lists and clear resolved-correction versus unresolved-finding output; verify synthetic fixture tests cover names, acronyms, compounds, empty markers, misplaced compensation, uncertain certificate text and untrusted cell instructions.
- [ ] 2.2 Implement structured output validation and mapped-fields-only payloads; verify tests reject unknown, omitted, repeated or mistyped rows/fields, invalid finding scope and changed/fabricated/dropped recipient occurrences, including invalid/empty/duplicate siblings.
- [ ] 2.3 Implement durable server-owned preparation jobs and explicit retry/rerun with brief publication transactions; verify tests for browser closure, restart, failed/incomplete batches, stale late responses and no silent raw-data fallback.
- [ ] 2.4 Persist instruction drafts separately from immutable source/prompt/connection-generation/endpoint/API-format/model/options/output/finding evidence; verify tests show draft edits and prompt/model updates do not alter completed results, and explicit run selection invalidates superseded finalization and every batch/retry retains its confirmed configuration.

## 3. Backend validation and both finalization paths

- [ ] 3.1 Extract shared backend validation for prepared and final data, retaining optional literal fallbacks, whitespace-only emptiness, mapped-field findings, rendering safety, address validation and campaign-history checks; verify tests distinguish optional empty, optional flagged, AI failure and required missing values.
- [ ] 3.2 Integrate AI email separator cleanup before existing expansion and classify complete expanded input before candidate mutation; verify the 1,000-entry limit, all-occurrence duplicate exclusions, shared-field sibling holds and independent valid/invalid siblings.
- [ ] 3.3 Implement idempotent owner-authorized Approve as-is with fresh backend checks and no reupload or AI call; verify tests for stale run/configuration, duplicate action IDs, zero eligible entries and ownership refusal.
- [ ] 3.4 Implement edited-final-sheet acceptance without AI or content reformatting, allowing edited/removed/added/restored entries; verify tests preserve operator wording, apply only backend rules, reject unsupported email separators and ignore imported status or approval claims.
- [ ] 3.5 Seal accepted final snapshots atomically and bind every accepted replacement upload to a fresh snapshot identity even for identical bytes; verify tests for mixed entry exclusions, atomic structural rejection, unsendable zero-accepted replacement, stale requests and unchanged historical evidence.
- [ ] 3.6 Refuse snapshot/run changes under enabled schedules or after candidate sending has started; verify scheduled, running, paused, stopped, interrupted and completed cases and document the explicit new-task remedy.

## 4. Direct final-value rendering and approval

- [ ] 4.1 Remove format choices from new campaign mapping authoring, drafts and help, preserving recipient/required/optional/fallback behavior and versioned historical reads; verify mapping/parser/hash and legacy-rendering regression tests with direct campaign sample preview.
- [ ] 4.2 Route final task preview, selected-row tests and candidate work creation through accepted final values without normalization; verify identical subject/body values across download/preview/test/send and zero provider-normalization calls after final upload.
- [ ] 4.3 Persist task/snapshot/configuration/mailbox-generation identity on tests and received-format approvals; verify tests reject cross-task, old-snapshot, old-mailbox, pre-finalization and arbitrary-sample approvals, including queued old tests and identical-byte replacement uploads.
- [ ] 4.4 Enforce current finalization, snapshot approval and explicit authorization in start, schedule creation/fire, resume, retry and resend; verify forged/stale requests cannot reserve or dispatch work and existing pacing, restore review, uncertain-outcome and mailbox-isolation behavior stays intact.
- [ ] 4.5 Implement the documented historical-task transition without automatic AI calls, rewritten content or inherited new-snapshot approvals; verify migration/integration fixtures for existing unstarted schedules and already-started legacy tasks and document deployment/rollback guards.

## 5. Downloads and task review UI

- [ ] 5.1 Generate prepared and final sending XLSX sheets containing accepted mapped values only, one recipient per row; verify Excelize round-trip tests for exact values/order, unique mapping labels (including shared source headers), no excluded/status rows and no formula execution.
- [ ] 5.2 Extend protected exclusion reports with original/proposed values, affected fields, all reasons, reason source and precise source/recipient provenance; verify fixtures combine AI/backend/processing reasons, preserve originals and deny foreign artifact access.
- [ ] 5.3 Preserve original and final upload provenance and frozen final values in post-execution task results; verify added/removed/reordered final rows, worksheet-name collisions, duplicate file-only evidence and existing automatic result-generation semantics.
- [ ] 5.4 Add task preparation progress, visible base prompt, instruction draft/history, correction/finding review, downloads and both finalization controls within existing workspace sections; verify view/handler tests and document operator responsibilities, failure remedies and backend-only final validation.
- [ ] 5.5 Clearly separate dataset approval, test/received-format approval and Start/Schedule states; verify UI/handler tests for unfinalized, replaced, zero-eligible, failed-preparation and approved-as-is paths with valid drafts and focus preserved.
- [ ] 5.6 Inspect every affected campaign/task page with synthetic data at 360, 768 and 1440 CSS pixels and by keyboard; verify both review paths, downloads, errors and historical views remain usable. Do not use live mail or operator workbook personal data for fixtures.

- [ ] 5.7 Add Account settings navigation and Mailbox/AI sections with provider-first setup, exact model/API-format guidance, confirmation, status, non-secret saved values and repair controls; verify owner-only routes, legacy mailbox navigation, validation feedback and named blocking-task links across campaigns. Document future preset/adapter additions and provider data handling; native MiMo/Fireworks presets remain future work.
- [ ] 5.8 Add explicit Close task confirmation, closing/closed states and retained history/download access; verify UI/handler tests for terminal action refusal and pending-task lock release only after settlement, plus responsive/keyboard checks for all affected Account and task pages.

## 6. Integration verification

- [ ] 6.1 Run end-to-end scenarios with temporary XLSX files/databases and simulated AI/mail providers covering initial prepare -> exclusions -> approve as-is -> test/approval -> explicit send, and edited-final-upload -> no AI -> new snapshot -> fresh test/approval; record results and verify exact stored content and recipient identity. Include owner-isolated AI setup, completed/closed lock release, recovery reacquisition, same-identity repair and model switching without changing earlier final snapshots.
- [ ] 6.2 Run npm ci on a fresh checkout, format/regenerate only changed templ sources, stage intentional generated output, and run make check plus make vulncheck; record actual commands/results without weakening checks or exposing secrets.
- [ ] 6.3 Run make verify-converter and openspec validate --all --strict --json --no-interactive; record actual local results and any skipped coverage without claiming hosted CI, deployment or live acceptance.

## Workflow follow-up

- Review and refine these artifacts before implementation; begin only on an explicit apply request.
- Archive only after implementation and required verification are complete.
