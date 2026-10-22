# Tasks

All boxes track implementation and verification, not proposal authoring. Leave them unchecked until the described behavior and its verification pass. Use [the workspace spec](specs/campaign-task-workspaces/spec.md) for required UI behavior and [design.md](design.md) for route mappings, data sources and the fixture matrix. Keep application checks isolated from local `.env`, live databases and real mailboxes.

## 1. Authoritative workspace data and presentation rules

- [x] 1.1 Expose original workbook filename, creation/upload timestamp, saved revision and stable short reference through existing task read models; verify with intake/web tests that repeated filenames stay distinguishable, timestamps do not change on task updates and foreign-owner data is unavailable.
- [x] 1.2 Assemble owner-scoped campaign/task summaries using current mailbox-scoped approval and current sending eligibility, with batched list metadata; verify reuse across matching tasks, invalidation after mailbox replacement, same-mailbox settings retention, saved/current revision separation and no read-induced writes/attempts/exports.
- [x] 1.3 Implement the spec's status, blocker, next-action and action-visibility rules as small shared presentation helpers; verify table-driven cases for every matrix row, including mixed safe remainder/failure/uncertainty, active work with blockers, ready+missed versus paused+missed and export failure alongside sending outcomes.
- [x] 1.4 Define validation summary and attention-count math from the existing partition/row identities; verify Total equals the sum of exclusive validation categories, Excluded at validation equals Total minus Eligible, multi-reason rows count once and campaign attention counts follow the exact spec predicates.

## 2. Campaign navigation and explicit template editing

- [x] 2.1 Render campaign Tasks as the default in one shared campaign wrapper with the four prescribed sections and no header summary beyond the campaign name; make the campaign list show name, revision or “No template”, approval, task count and attention count instead of source headers, and the task table show the linked workbook, status badge, eligible count and next action, with reference, upload time, saved revision, status explanation and remaining counts in per-cell tooltips instead of worksheet/header-row columns; verify default selection, content order, exact labels, workbook links opening the task Overview, task-specific next links, tooltip content and keyboard reachability, empty states and worksheet/header detail remaining inside the task.
- [x] 2.2 Reorganize Template & preview into current configuration, explicit Create revision, sample preview and revision history; verify first-revision drafts, empty later-revision forms, preserved rejected mapping input, guidance outside placeholders for the mapping format and exactly one recipient role, the worked placeholder → mapping → rendered-value example, every invalid variable or mapping named on failure, and DOCX reselection guidance in campaign authoring tests.
- [x] 2.3 Put campaign reports/package/history in Activity & downloads and result labels/deletion in Settings; verify export scope/as-of/attempt kind, unchanged generated-only removal versus campaign deletion confirmations, active-data deletion refusal and no generation on GET.
- [x] 2.4 Preserve staging uploads before a template exists while prioritizing Add template and explaining the validation prerequisite; verify normal entry, direct existing upload URLs and staged-task recovery after template creation without requiring a second upload.

## 3. Unified task shell and recipient review

- [x] 3.1 Render one task shell with identity/context, separate statuses, next-action sentence, evidence-based lifecycle strip and Recipients/Test & approve/Sending/Results navigation; replace split-workspace assertions with shared-shell tests for staging, ready, active and settled tasks, including unavailable-section guidance.
- [x] 3.2 Reorganize guided upload/worksheet selection and Recipients summary, preview and row-reason/report regions in the prescribed order; verify the upload page's campaign context, labeled input with Upload workbook and Cancel, readable size and 1,000-row bounds ahead of a “Technical limits” disclosure, mandatory mapped columns including optional-value mappings when a revision exists, Add template guidance without one, and then actionable validation errors, retained selection, zero-eligible download-only behavior, every exclusion category and the “Already scheduled means another task claimed it” explanation in existing intake/web fixtures.
- [x] 3.3 Render the initial previewable row automatically and refresh previews on row selection; verify To/Subject/Body precede comparison details, recognizable row labels, selected-row failure/loading handling, immutable saved content and zero attempt creation in preview tests and browser interaction.

## 4. Approval, sending and recovery actions

- [x] 4.1 Consolidate scoped test/approval evidence in Test & approve, showing valid reusable approval as complete and another test as secondary; verify qualifying submitted-test selection, explicit received-format confirmation, rejection of another owner's evidence and independent testing without candidate start/resume.
- [x] 4.2 Implement state-specific Sending controls and distinct fresh scopes for first start, safe resume, definite retry and uncertain resend; verify HTTP/rendered behavior for every matrix state, no zero-recipient actions, fresh-count refusal, repeated-action idempotency and server refusal of forged/old forms despite hidden controls.
- [x] 4.3 Present explicit Send now/Schedule alternatives, labeled 12-hour India-time entry and one-shot authorization guidance; verify invalid/too-soon input retention, explicit editing/clearing, Send now supersession, live-fire refusal and downtime-miss recovery using the existing schedule fixtures with simulated providers.
- [x] 4.4 Refuse revalidation atomically while an unstarted task has an enabled schedule and direct the operator to Clear schedule first; add intake/scheduling regressions for concurrent schedule enablement, unchanged snapshot on refusal and fresh approval/authorization after clear and revision change.
- [x] 4.5 Place applicable definite-failure/correction/uncertainty groups and Sending settings under Sending; verify safe resume excludes unresolved uncertain rows, paused/interrupted tasks retain secondary Stop, correction rows require corrected upload, uncertain resend requires duplicate-risk confirmation, manual resolution remains available during other active work/restore review without formatting approval and retains its basis/history, and pacing/cooldown scope remains per mailbox.
- [x] 4.6 Replace generic rejected-action/release-readiness copy with known safe reasons and remedies in the correct unified section; verify missing approval, unusable mailbox, restore review, changed count, storage failure and invalid schedule paths do not claim success or expose provider secrets.

## 5. Canonical results and attempt history

- [x] 5.1 Use one task result component in Results and remove duplicate workbook controls; verify unavailable, pending, generating, ready, ordinary failed and removed states against terminal eligibility, automatic activation, explicit ordinary-failure retry and no premature generation while stopping.
- [x] 5.2 Show as-of saved snapshots during resumed work and task-scoped attempt/resolution history with explicit +05:30 times and provider-acceptance wording; verify original-workbook preservation, tests excluded from candidate totals, historical stored HTML, generated-only deletion confirmation and no sending/eligibility change from artifact actions.

## 6. URL state, polling and responsive interaction

- [x] 6.1 Implement the design's finite canonical/legacy route-and-fragment mapping and section-specific mutation redirects/errors; verify every listed old bookmark, browser Back/Forward, reload, campaign-task ownership mismatch and unauthenticated/foreign-owner entry point without read-side mutations.
- [x] 6.2 Keep the shared shell/navigation outside progress swaps and preserve drafts/focus/disclosure choices through grouped updates; verify count changes and equal-count configuration/mailbox/approval/schedule changes invalidate confirmations while unrelated test/date input and the selected section remain intact in browser interaction checks.
- [x] 6.3 Apply the existing visual system with compact summaries, wrapping identity/lifecycle/navigation and contained wide tables; verify every campaign/task section at 360, 768 and 1440 CSS pixels, keyboard section/form operation, label/error associations, focus visibility, no whole-page overflow and the 1440-by-900 first-screen requirement.

## 7. Integration and required project verification

- [x] 7.1 Run the complete design fixture matrix on an isolated server with temporary records/workbooks and simulated providers; record fixtures, assertions and representative desktop/mobile screenshots, verify no real mail or live task mutation, and stop the agent-started server when finished.
- [x] 7.2 Format/regenerate only changed Templ sources, build assets and stage only intentional generated output as required for generation consistency; run `make lint`, then `make check`, `make vulncheck` and `make verify-converter` (`npm ci` first on a fresh checkout), recording actual results and any unrelated blockers without weakening checks.
- [x] 7.3 Reconcile any implementation discoveries across proposal/design/deltas/tasks, run `openspec validate --all --strict --json --no-interactive` and `git diff --check`, and confirm the delivered UI matches the normative section/status/action scenarios before marking implementation complete; passing schema validation alone does not satisfy application verification.

## Workflow follow-up

- [x] 8.1 Add Overview as the default first task section; move detailed task metadata, current-state/blocker explanation, next action and lifecycle strip into its content; replace the ID beside the task name with the derived status badge while retaining task references in list rows and Overview details; verify default and explicit section links, status visibility, lifecycle evidence and the desktop first-screen requirement.

- [x] 8.2 Fix preserved sending-form identifiers so polling restores forms inside their original section; verify selector-safe IDs, repeated progress swaps, draft/focus retention, stale-confirmation reset and no forms stranded below the footer.
- [x] 8.3 Replace the plain lifecycle line with a five-stage evidence tracker: equal desktop columns with connectors, completion checks or pending step numbers, stage labels above the unchanged state text, links to existing sections (Upload to Overview), subtle emphasis for an incomplete `NextHref` stage and a vertical mobile layout; verify evidence-derived markers, navigation-only emphasis, keyboard/readable state cues and responsive layout without introducing workflow state or actions.
- [x] 8.4 Implement the initial URL-based Direct/Scheduled first-start chooser and controls; this earlier behavior is superseded by task 8.9, which makes the task record authoritative and persists type changes.
- [x] 8.5 Integrate all next-step guidance into the tracker, with no preceding status/next-action/blocker summary or separate Next steps card. Give each stage a 16px info icon in a 24px hit area and a concise tooltip (max 240px, 11px text); current `StatusExplanation` replaces generic guidance. Show matching `blockerLabel` and `Remedy` links beneath states. Only the recommended stage gets a `NextLabel`/`NextHref` button; others rely on the sidebar. Map external template/mailbox prerequisites to Recipients/Sending and retain secondary Open-stage links for external primary destinations. Use primary color on the recommended stage name without a Next badge. Keep tooltips noninteractive and navigation read-only; verify tooltip access, button/link placement, blocker remedies, evidence markers and no task/sending mutations.
- [x] 8.6 Require a native Direct/Scheduled preference on workbook upload, persist it as optional task `sending_type`, and retain it on upload errors; ensure it creates no schedule, time, authorization or candidate action. Sending reads this saved preference; legacy tasks with no value may show the chooser. Verify upload enforcement, error retention and persistence in the deferred batch.

- [x] 8.7 Match campaign task-list first-start actions to the saved sending preference: Direct shows Send and Scheduled shows Choose sending time, opening the matching confirmation controls. Preserve prerequisite/progress/saved-schedule/recovery labels and use a properly padded primary button with readable single-line text. Align task timeline labels and verify in the deferred batch.
- [x] 8.8 Compact the campaign header to breadcrumbs, name and the revision action, dropping the introduction paragraph and the template/formatting summary line. Make the campaign task list disclose detail progressively: the workbook name opens the task Overview, each row shows only the status badge and the eligible count, and per-cell tooltips carry the reference, upload time, saved revision, status explanation, attention flag and remaining recipient counts. Restore the form spacing a checkbox label lost to the `form > .cn-label` margin reset. Verify tooltip content and keyboard reachability, overview versus next-action destinations and the checkbox-to-action gap in a rendered browser check.
- [x] 8.9 Make saved task `sending_type` authoritative for all pre-execution selection and navigation. Add/retain a POST that saves Direct or Scheduled before showing the corresponding controls; GET values never override the field, while `sending_type=choose` may only reopen the chooser. Permit changes only for staging/ready tasks that have not started. Refuse changing to Direct while any enabled or missed schedule exists and require explicit schedule clearing; never cancel implicitly. Keep started tasks immutable and derive app navigation from the saved value. Verify persistence, GET no-mutation, each state boundary, schedule refusal/clear behavior, redirects/polling/list links, and existing dispatch safety in the queued batch.

- Review the delivered implementation and verification evidence before syncing/archiving this change through the appropriate OpenSpec workflow.
- Deployment, production acceptance or real diagnostic email is separate from these implementation checks and requires its applicable operator authorization.

## Verification record

- Fixtures (isolated server, temporary data directory, simulated mailbox and locally generated XLSX/DOCX):
  campaign with and without a template; ready/approved task; task with a missed scheduled start during
  downtime; finished task with ready, failed and removed result states; definite-failure, uncertain and
  resolution recovery rows; disconnected mailbox; restore-review restriction; staged workbook before and
  after template creation.
- Browser assertions at 1440x900, 768 and 360 CSS pixels: no whole-page horizontal overflow
  (`scrollWidth == innerWidth`), identity/status/next action/section navigation above the 900px first
  screen, lifecycle strip visible, keyboard focus ring on section triggers, Back/Forward and legacy
  fragments (`#review`, `#rows`, `#preview`, `#send`, `#recovery`, `#pacing`, `#recent`, campaign
  `#preview`/`#history`) resolving to the canonical section, polling preserving the selected section
  and schedule draft values, and preview row selection replacing the previous preview without creating
  attempts.
- Screenshots: representative desktop and mobile captures were taken with the browser tool during this
  session (campaign list/tasks/template/activity/settings, task recipients/test/sending/results, recovery,
  paused-missed, disconnected mailbox and restore-review states). They are temporary session evidence and
  are not committed; the assertions above are the durable record.
- Automated checks actually run and passed: `go test ./...` (including new intake, sending, presentation,
  web and view tests), `make lint` (0 issues), `make check` (format, generation consistency, lint with
  `-race` tests, and the production build), `make vulncheck` (no called vulnerabilities; 6 advisories in
  required modules are not reachable), `make verify-converter` (packaged Pandoc Docker stage),
  `openspec validate --all --strict` (12/12 items) and `git diff --check`. The agent-started fixture server
  and its temporary data directory were stopped/removed after verification.


### Overview follow-up verification — 22 Oct 2026

- Overview is the first/default task section; task details, next steps, blocker remedies and lifecycle evidence live in its panel. The persistent title and breadcrumb omit the task reference; a status badge sits beside the filename. References and full IDs remain in technical details.
- Rendered-view and HTTP regressions verify the compact header, Overview content, default selection and explicit/error sections. Browser checks used an isolated rendered task fixture with local assets, no live database or provider: all five sections at 360, 768 and 1440 CSS pixels had no page overflow. Next-step links, keyboard selection, reload and Back to the unsectioned Overview were verified; next steps and lifecycle fit the 1440x900 first screen.
- Local verification passed: `make fmt`, `make lint`, `make check`, `make vulncheck`, `make verify-converter`, final `make check-format check-generated lint` (generation consistency rerun after staging the canonical generated output), focused rendering/HTTP tests, final `go test -race ./internal/web ./internal/web/views`, strict OpenSpec validation (12/12), and `git diff --check`. Vulnerability analysis reports zero reachable vulnerabilities and six advisories in required modules that the application does not call. No hosted CI run or real mail was involved.
- Temporary browser previews were closed and the isolated preview server was stopped. The existing development server was left running; its served CSS hash matches the current built asset. Desktop/mobile screenshots are temporary session evidence at `/tmp/bachtel-task-overview-desktop.png` and `/tmp/bachtel-task-overview-mobile.png`.


### Preserved-form polling regression — 22 Oct 2026

- Reproduced the reported footer leak with the rendered task fixture: htmx threw an invalid-selector error for a preserved form ID containing the mailbox address, leaving three forms in its preservation pantry after the document body. The shared sending control key now uses lossless hexadecimal encoding, covering all eight dynamic preserved sending/recovery/pacing forms while retaining scope-change invalidation.
- Added rendered-form regression coverage for start/schedule, enabled schedules and recovery/stop forms with punctuation in mailbox addresses and content hashes. The new test failed against the original IDs and passes with the fix. Existing scope-change tests include mailbox punctuation changes so sanitization cannot silently merge distinct identities.
- Browser verification used actual rendered task/progress components on an isolated static fixture server with synthetic data and local assets. Repeated polls produced zero swap errors, zero parked forms and zero forms outside main/header; the selected section, 9:45 schedule draft, focused minute field and current confirmation survived unchanged scopes. Changing the safe count from one to two cleared the old confirmation. Desktop (1440px) and mobile (360px) Overview stayed free of visible send controls and page overflow. Screenshots: `/tmp/bachtel-polling-fixed-desktop.png` and `/tmp/bachtel-polling-fixed-mobile.png`.
- Passed `make fmt`, `make lint` (0 issues), `make check` (including the complete race suite and production build), `make vulncheck` (zero reachable vulnerabilities; six advisories in unused required-module code), strict OpenSpec validation (12/12), and `git diff --check`. The temporary fixture server was stopped and its browser tab closed. No real email or live task mutation was used; the existing development server remains running.


### Lifecycle tracker refinement — 22 Oct 2026

- Replaced the inline lifecycle sentence with five connected stages: completion checks follow the existing evidence flag, incomplete stages retain their stage number, and each label appears above its unchanged status. An incomplete stage targeted by the next-action link receives a ring and visible Next label. Stage links only navigate existing sections; Upload returns to Overview. The tracker stacks vertically when horizontal space is limited.
- Rendered-view tests verify stage order, state text, evidence flags, next-action emphasis, existing-section destinations and external-remedy behavior. Browser checks covered ready/approved, staging and interrupted-with-results examples at 360, 768 and 1440 CSS pixels: no page overflow, readable status text, five equal desktop columns and one vertical column on narrower screens. Every stage link and keyboard activation selected the expected tab. Repeated polling left no stranded forms or browser errors.
- Passed `make fmt`, view tests, `make lint` (0 issues), `npm run lint:tailwind` (69 templates, 0 diagnostics), `make check` (including the full race suite and production build), `make vulncheck` (zero reachable vulnerabilities), strict OpenSpec validation (12/12), and `git diff --check`. The development server serves the current CSS hash. Temporary fixture server/tab were stopped/closed; no real mail or live task mutation was used. Screenshots: `/tmp/bachtel-lifecycle-desktop.png` and `/tmp/bachtel-lifecycle-mobile.png`.


### Sending type chooser follow-up (task 8.4)

- Added native navigation-only Direct/Scheduled selection, exclusive first-start forms, Change sending type, selected-type polling/redirects and saved-schedule editing defaults. Selection never changes a stored schedule; Direct explicitly cancels it without sending or confirms Send now instead. Started tasks retain their execution-state controls, and missed starts retain their existing first-start or safe-resume remedies.
- Temporary-store HTTP tests verify choice/poll GETs create no attempts/actions, saved schedules survive navigation and prefill editing, Direct cancellation preserves ready state, and forged Scheduled selection cannot expose first-start controls after starting. View tests cover defaults, invalid choices, exclusivity and state restrictions. Existing dispatch approval, mailbox, restore-review and atomic schedule checks remain unchanged.
- Browser checks at 1440, 768 and 360 CSS pixels verified native choices, Change sending type, reload/Back/Forward, draft/confirmation retention through polling, fresh unchecked confirmation on type changes and history restoration, no stranded forms, no horizontal overflow and no browser errors. The existing checkbox change event resets restored sending confirmations while retaining drafts. Preview screenshots: `/tmp/bachtel-sending-chooser-desktop.png`, `/tmp/bachtel-sending-chooser-mobile.png` and `/tmp/bachtel-sending-scheduled-desktop.png`.
- Passed `make fmt`, `make lint` (0 issues), `npm run lint:tailwind` (69 templates, 0 diagnostics), JavaScript syntax check, focused HTTP/race tests, `make check` (full race suite and production build, final run 6m2.647s), `make vulncheck` (0 reachable vulnerabilities, 14.393s), strict OpenSpec validation (12/12), and `git diff --check`. The first full check identified a stale paused-task test fixture expecting Start; it now asserts Resume and retains all confirmation-identity assertions. The corrected targeted race run and full check passed.
- Verified port 8090 serves the current CSS and workspace JavaScript after replacing its stale development process. Read-only inspection confirmed no active candidate work or saved schedule before restart. The temporary preview server/tab were stopped/closed. Automated checks used temporary data and simulated dispatch; no real email or live task action was submitted. Hosted CI was not run.


### Timeline simplification follow-up (task 8.5)

- Removed the separate Next steps card. The recommended stage alone has a navigation button; its name uses the primary color, and the literal Next badge is removed. Information icons are 16px with a larger hit area, and tooltips use concise text and a 240px maximum width. Current status replaces duplicate generic guidance; blocker explanations and remedy links remain available.
- Updated rendered-view regression expectations for the queued final verification. Templates and assets were regenerated, and a review screenshot was captured at `/tmp/bachtel-timeline-compact.png`.
- At the user's explicit request, stopped the in-progress full check and deferred all further tests/validation until the queued UI tasks are finished. Task 8.5 remains unchecked pending that batch verification; no final test pass is claimed for these latest adjustments.


### Creation preference and matching next-action follow-up (task 8.6)

- Added a required native Direct/Scheduled field to task upload. The initial preference is saved with the staging record and retained on parsed-form upload errors; it does not create a schedule, reserve recipients, authorize dispatch or start sending. Old tasks with no preference keep the chooser. The earlier schedule default and URL override behavior is superseded by task 8.9.
- The eligible first-start timeline action uses Review & send for Direct and Set sending schedule for Scheduled, linking to matching controls. Existing blocker, active-progress, saved-schedule and recovery actions retain their labels. Updated test fixtures and added regression coverage for the final queued verification, without executing tests or validation.
- Regenerated the template/assets and captured `/tmp/bachtel-create-sending-type.png` and `/tmp/bachtel-selected-sending-action.png`. Verification remains deferred at the user's request until the queued tasks are finished; task 8.6 remains unchecked.



### Campaign task action follow-up (task 8.7)

- First-start task-list and timeline labels now agree: Direct uses Send, Scheduled uses Choose sending time. Task-list links explicitly open the matching type's controls; a saved schedule, blocker, active execution or recovery retains the established action and destination.
- Replaced the cramped outline link with the shared primary button, using proper vertical padding and a single-line label. Added queued regression expectations without running tests or validation, as requested. Generated assets/templates and captured `/tmp/bachtel-campaign-sending-actions.png`. Task 8.7 remains pending the final batch verification.


### Campaign task list disclosure follow-up (task 8.8)

- The campaign header is identity only: breadcrumbs, name and Create revision/Add template. The repeated introduction paragraph and the `Template: Revision N | Formatting: …` line are gone; Template & preview still owns that state.
- The campaign task table keeps the linked workbook name, the status badge, the eligible count and the next action. Reference, upload time, saved revision, status explanation, the attention flag and the input/submitted/attention counts moved into per-cell tooltips that show on hover and focus and stay keyboard reachable. The workbook name now opens the task Overview; only the next action deep-links into its section.
- `presentation.RecipientSummary` reports the compact `Label()` and the disclosed `Details()` instead of one combined sentence.
- The approval form's checkbox row regained its 12px gap: `form > .cn-label` no longer resets the bottom margin that the form's `space-y-3` relies on.
- Verified in a rendered fixture with local assets and synthetic data: workbook, status and recipients tooltips resolved with the expected content; the checkbox-to-action gap measured 12px at desktop and 560px widths with wrapped label text and no page overflow. Rendered-view regressions cover the tooltip triggers, their describing content, the Overview link and the trimmed header.
- `go test ./internal/web/views ./internal/presentation` and `make vulncheck` passed. `make check` reached the race suite but still fails on pre-existing sending-mode, schedule-refusal and upload-rejection regressions from the unfinished tasks 8.4/8.6 work in this change; those failures are unrelated to the header, list and spacing edits here. Strict OpenSpec validation still reports the pre-existing `excel-intake` MODIFIED requirement without scenarios.

- [x] 8.10 Hide both formatting forms behind Change formatting once approval is current; preserve direct access for unapproved configurations, show the template revision path, and reopen on action errors. Implementation and focused regression coverage added; testing deferred until the queued work is finished at the user's request.

- [x] 8.11 Hide provider setup for connected mailboxes until Change provider beside Disconnect is activated. Preserve selected-provider/error/reconnect paths and replacement safeguards. Implementation and regression coverage added; verification deferred until queued work is finished.

- [x] 8.12 Replace separate scheduling hour/minute/AM-PM controls with native date and time inputs, update server parsing and saved/error drafts, and retain India-time interpretation and all schedule authorization checks. Implementation and regression coverage added; testing deferred until queued work is finished.

- [x] 8.13 Restructure Sending settings into current pacing/concurrency/cooldown summaries, a clearly grouped update form with aligned action and visible allowed range, and concise supporting notes. Preserve mailbox scope and pacing checks. Implementation added; batch verification deferred at the user's request.

- [x] 8.14 Use the owned accordion for Sending settings with a full-width header trigger, chevron and preserved expansion across progress updates. Reduce Scheduled sending copy using progressive disclosure while retaining visible timezone, minimum lead, authorization and applicable warnings. Implementation added; verification deferred until queued work is finished.

- [x] 8.15 Keep Schedule start disabled until its confirmation is checked, retaining approval/count eligibility, reset-on-scope-change and server confirmation checks. Implementation added; verification deferred with the queued work.

- [x] 8.16 Show Task history in a collapsed-by-default owned accordion with a full-width header, chevron, keyboard operation and preserved expansion through progress updates. Keep the existing attempts, resolutions, empty state and time/submission guidance; legacy history bookmarks open the accordion. Implementation and rendered-view regression coverage added; verification deferred with the queued work.


### Deferred batch verification — 22 Oct 2026

- Ran the queued batch: `make check` (format, generation consistency, lint with the race suite, production build), `make vulncheck`, `openspec validate --all --strict`, and `git diff --check`. `make check` and `make vulncheck` pass with zero reachable vulnerabilities; strict OpenSpec validation reports zero errors and three pre-existing long-requirement warnings inside approved requirement text.
- Fixed generation consistency: `internal/web/views/task_templ.go` had been generated from the repository root, so it embedded `internal/web/views/task.templ` while every other generated file embeds `views/…`. Regenerated through the canonical target and staged the output.
- Removed the unused `sendingModeHasFormIDPrefix` helper that lint flagged.
- Updated stale expectations to the approved contracts rather than the retired ones: the sending-type chooser is a POST form (task 8.9), test uploads carry the required preference (task 8.6), Schedule start renders disabled with confirmation and eligibility bindings (task 8.15), and the Sending-settings accordion root carries the restoration marker (task 8.14).
- Added upload coverage for an omitted Direct/Scheduled preference: the request is refused as a client error and stores no task.
- Behavior verified for tasks 8.5–8.16 through rendered-view and HTTP tests: tracker tooltips, next-action button and evidence markers; campaign-list and timeline next actions; upload preference enforcement and persistence; saved `sending_type` authority plus its state guards; Change formatting and Change provider disclosure; native schedule date/time inputs with saved and rejected drafts; Sending-settings summaries and accordion; schedule confirmation gating; and the Task-history accordion.
- Limitations: this batch used no browser session (earlier browser evidence covers the shared surfaces), no hosted CI, and no real email or live task mutation. Deployment and release acceptance remain separate and unauthorized.
