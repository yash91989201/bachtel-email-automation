# Design

## Context

See [proposal.md](proposal.md) for the problem and scope, and [the workspace UI contract](specs/campaign-task-workspaces/spec.md) for required names, ordering, wireframes and acceptance scenarios. This design is necessary because the change crosses campaign/task handlers, read models, templates, URL state, polling and mutation feedback.

Verified during the review on 2026-10-22:

- `campaign.templ` defaults to Template with current revision collapsed and the new revision form open. `task.templ` renders review/preview/rows/results; `sending.templ` renders another six-section workspace. Result controls and approval summaries are duplicated.
- `internal/web/task.go` and `sending.go` independently assemble task and sending data. Task detail currently uses configuration approval, while sending uses the current mailbox's approval lookup. A unified page must use the authoritative mailbox-scoped approval consistently.
- `internal/intake.Task` already exposes source filename, saved configuration/hash, worksheet, counts and schedule. PocketBase task records already have creation timestamps, but the public task read model does not expose upload time. Adding that read field is sufficient; no schema migration or new editable task name is needed.
- Validation `Counts` partitions input rows into eligible, ineligible, duplicate, invalid, already submitted, already scheduled and review required. A row may have several explanation reasons; reason frequencies must not be added as if they were distinct rows. `Counts.Excluded()` currently means file-only duplicate/invalid rows, not every non-eligible row.
- Schedule state (`none`, `scheduled`, `fired`, `missed`) is independent of execution status. Approval is bound to owner, configuration, content hash, connection and generation; it is reusable across tasks with the same scope. Result artifacts have independent pending/generating/ready/failed state.
- `web/js/workspace.js` already adapts owned tabs to URL fragments and preserves disclosure state after htmx swaps. Existing tests in `workspace_test.go`, `sending_test.go`, `schedule_test.go`, `task_test.go`, `campaign_test.go`, `exports_test.go` and `breadcrumbs_test.go` cover relevant behavior and must be revised rather than bypassed.
- Browser review exercised one ready/approved task, all current campaign/task/sending sections, preview and upload entry. Running/failure/recovery cases were inspected in source/specs; this is not live verification of those states.

## Goals / Non-Goals

**Goals:**

- One server-built presentation of identity, current approval, current action scope and results, reusable by full pages and polling fragments.
- Deterministic display/next-action derivation from existing facts, with behavior tests at the read-model and rendered HTTP boundaries.
- Retain Templ, locally owned UI components, htmx and the small workspace adapter; format/regenerate only changed templates.
- Support old bookmarks and action endpoints while generating new links into the canonical workspace.

**Non-Goals:**

- No persisted UI workflow state, “review completed” flag, new approval layer or dispatcher state-machine rewrite.
- No client-side authority for sending eligibility, no new framework, database schema or dependency.
- No new task editor, mapping editor, account setup redesign, or additional analytics/filtering system.

## Decisions

### 1. Compose shared workspaces from existing boundaries

Create one campaign wrapper and one task wrapper in the views package, with explicit section components rather than duplicating pages. Keep intake, sending and export services as the sources of domain facts. A compact presentation struct can hold section/status/action descriptors; do not build a generic workflow engine or registry. Preserve workbook staging before a template exists; prioritize Add template in the UI and withhold successful validation until there is a revision. Do not introduce a new upload prohibition.

| Surface | Required sections and ownership |
| --- | --- |
| Campaign | Tasks from intake/current task summaries; Template & preview from campaign authoring; Activity & downloads from campaign exports/attempts; Settings from existing label/deletion actions |
| Task | Overview from task identity, upload/revision/sender metadata, derived status/blocker, next action and lifecycle evidence; Recipients from immutable intake snapshot/manifest and preview; Test & approve from diagnostic tests and scoped approval; Sending from action preview/progress/schedule/recovery/pacing; Results from task artifact and attempt records |

Detail that belongs to a record does not occupy a list column. The campaign list keeps each campaign's name, revision or “No template”, approval state, task count and attention count; its source-header column becomes mapping guidance inside Template & preview. The campaign task table keeps the linked workbook name, the derived status badge, the eligible recipient count and the next action; the task reference, upload time, saved revision, status explanation and the remaining recipient counts are disclosed through per-cell tooltips instead of extra columns or stacked lines. The campaign header is identity only — breadcrumbs, name and the revision action — because the repeated introduction paragraph and the template/formatting summary line duplicated Template & preview. On a task page, the persistent title row shows the workbook filename with its derived status badge and omits the adjacent task ID; Overview holds the task reference, full identity details, worksheet/header details and the remaining upload/revision/sender metadata. Reuse the existing mapping-help content (format line, exactly one recipient role, worked placeholder → mapping → rendered-value example) where it now sits rather than rewriting it.

Keep `task.templ` and `sending.templ` as useful component boundaries if that produces the smaller clear diff; file consolidation is not an acceptance criterion. The shared task shell must render once. Reuse the existing breadcrumb primitive and owned tabs/collapsibles; do not modify their internals merely to restyle these pages.

Alternative rejected: keeping the two workspaces and renaming buttons. It leaves duplicate state derivation, split navigation and conflicting results availability.

### 2. Preserve routes and normalize section links

New links use canonical page roots and fragments. A task with no recognized section defaults to Overview; links that represent a specific next action may still target its relevant section. The existing adapter handles fragment selection and Back/Forward. Keep one section selection source in the owned tabs.

| Entry | Canonical content / mapping |
| --- | --- |
| `/campaigns/{id}` | Shared campaign wrapper, default Tasks; new fragments `#tasks`, `#template`, `#activity`, `#settings` |
| `/campaigns/{id}/tasks` | Existing list route renders the same campaign wrapper with Tasks selected; links generated inside use the campaign root |
| Campaign `#preview`, `#history` bookmarks | Map to Template & preview (sample preview region) and Activity & downloads respectively |
| `/campaigns/{id}/tasks/new` | Guided upload page with shared campaign context; successful upload opens the new task's Recipients |
| `/campaigns/{id}/tasks/{task}` | Shared task wrapper, default Overview; new fragments `#overview`, `#recipients`, `#test`, `#sending`, `#results` |
| Old task `#review`, `#preview`, `#rows` | Recipients, opening/scrolling to its summary, preview or row-reason region |
| Existing task `/sending` GET | Render the same shared wrapper with Sending selected; its old fragments map as below |
| Old sending `#send`, `#recovery`, `#pacing` | Sending, with the corresponding action/recovery/settings region visible |
| Old sending `#recent`, `#results` | Results, opening history or workbook region |

The upload form requires a native, unselected-by-default Direct/Scheduled radio choice for every new task, including staged uploads without a template. Validate it on the server and retain it when the form is re-rendered after an upload error. Persist the selected value as optional task field `sending_type` (`direct` or `scheduled`); older records may have no value. This field is the durable source of truth for the task's selected sending type before execution. Saving it creates no schedule time, schedule, authorization or candidate action.

GET navigation never changes or overrides saved `sending_type`. The canonical task URL carries section state only; all app links, redirects, polling responses and list actions derive the selected type from the saved task field. A `sending_type=choose` query may open the chooser for a staging/ready first-start task, but query values `direct` or `scheduled` never select or change the saved type. If an older task has no saved value, Sending may show the chooser. A POST to the task sending-type action validates and persists `direct` or `scheduled` before returning to Sending. Changes are allowed only while the task is staging or ready and has not started. Refuse a change to Direct while an enabled or missed schedule remains; require an explicit Clear schedule action first. Changing type never clears/cancels a schedule implicitly. Started tasks are immutable and retain their existing state-specific controls. A rejected start/schedule action returns to the saved type with the relevant submitted draft intact and confirmation unchecked.

Retain existing POST/download routes. Successful mutations redirect to the canonical root and relevant section. Validation/action failures render the full shared page in that section; the response's section must override an obsolete URL fragment for that render. Normalize legacy fragment aliases without replaying a request or mutating data. New section clicks must create navigable history entries rather than replace every selection, so Back/Forward returns to the prior section.

Do not redirect POSTs to GETs before executing the existing authorization/CSRF/idempotency/domain checks. GET aliases must never generate exports or submit mail. Keep route ownership and campaign-task matching checks in all entry points.

Alternative rejected: introducing a separate SPA route tree or moving every mutation URL. It adds migration and interaction work without improving the specified UI.

### 3. Build one authoritative, owner-scoped task presentation

Load the authorized campaign/task once per full request, then obtain the existing intake snapshot, sending preview, mailbox summary and task artifact metadata as needed. Extend the existing intake read model with `UploadedAt` from the stored creation timestamp and expose the saved revision number and filename. For task references start with the first eight ID characters, extending to distinguish entries in the displayed list; keep the full ID in details. Render dates consistently as `22 Oct 2026, 11:12 AM +05:30` (using actual recorded values), and use the explicit +05:30 offset in confirmations/history. Do not infer an upload time from the latest task update.

Use sending's current connection/generation approval lookup throughout the campaign/task summaries. A campaign badge concerns its current revision; a task badge concerns its saved revision. An approved task must not inherit a campaign's newer revision status. Stale-before-first-start remains an independent blocker.

Revalidation with `schedule_state=scheduled` must be refused atomically at snapshot publication, not only by hiding the form. Check against concurrent schedule enablement inside the existing validation transaction; show Clear schedule first and preserve the old snapshot on refusal. After clear/revalidation, a newly confirmed schedule captures the new configuration. A missed schedule is already unable to fire, so it remains visible until explicitly cleared or handled through its existing first-start/resume path. This guard does not auto-clear schedules or change dispatch semantics.

Campaign task counts/attention counts must be scoped to the owning operator's campaign. Build aggregate summaries from existing records using bounded/batched queries; do not call a complete preview/render/export for every row of every task in the campaign list. Loading metadata must not generate artifacts, create attempts or change state. Erroring metadata is “Unavailable; retry” with a safe reason, never zero or success.

Do not compute current eligible counts solely from the validation snapshot. Use the same sending preview/atomic action recheck as the existing controls. Snapshot totals remain labeled as validation evidence. Calculate Excluded at validation as `Total - Eligible`, not the narrower `Counts.Excluded()` helper. Use the existing mutually exclusive category counts for the breakdown; show multiple row reasons without pretending their frequencies are an additive partition.

### 4. Derive display labels and allowed controls without adding domain states

Use small deterministic presentation helpers and table-driven behavioral tests. The status table and action matrix in the workspace spec are authoritative UI behavior, not new stored states. Preserve raw state/IDs in operator-accessible details when useful; use the prescribed plain-language labels in prominent UI.

Keep these sources separate:

| Fact | Existing source / interpretation |
| --- | --- |
| Execution | `staging`, `ready`, `running`, `waiting`, `stopping`, `paused`, `interrupted`, `stopped`, `completed` |
| Schedule | `none`, `scheduled`, `fired`, `missed`, stored instant and refusal/pause reason |
| Approval | Exact configuration/hash plus current connection/generation and qualifying submitted test |
| Current action scopes | Free eligible/safe remainder, retry-eligible definite failures, individual unresolved uncertainty |
| Artifact | Absent or pending/generating/ready/failed; separate saved as-of timestamp |

Resume must reflect the existing service contract: paused/interrupted/stopped work can resume its safe remainder; a completed task needs an earlier accepted start; a downtime-missed schedule has its explicit resume path. A live scheduled-fire refusal can leave `ready + missed`, which uses first-start checks or schedule clear/re-entry, not an invented resume transition. Manual Send now instead supersedes an unfired schedule through existing action semantics and explicitly warns the operator.

For a first-start task with no saved sending type, render a native server-side Direct/Scheduled chooser before either form. Persist a choice through the task sending-type POST before rendering its corresponding controls. A `choose` query can reopen the chooser for eligible unstarted tasks; other GET query values cannot override the saved field. Offer a type-change action only for staging/ready tasks that have not started. Refuse switching to Direct while an enabled or missed schedule remains and direct the operator to explicitly clear it first. Do not implicitly cancel schedules. A downtime miss that paused execution continues to show its safe-resume and clear-schedule controls, never first-start controls.

Changing the type persists the new task preference, but must not create, edit, clear or cancel a schedule, send candidates, reserve work or resume execution. Refuse changes while a schedule is enabled or missed when the requested value is Direct, and refuse every change after execution starts. GET requests cannot mutate the preference; ignore type query values except `choose`, which only opens the chooser for eligible unstarted tasks. Reset checked action confirmations and unsaved mode-specific input after a successful type change; preserve valid drafts through polling and action-error rendering. The current count, approval, mailbox, restore-review, ownership, repeat-request and positive-scope checks remain authoritative.

Relevant but blocked controls show their reason/link, while irrelevant controls are omitted. Restore review blocks new dispatch but must not remove Stop on active work or read-only review/history. A paused unusable mailbox links to reconnection and continues to require explicit resume afterward. Tests retain their independent explicit submission path and do not resume candidates. Definite-failure retry, safe resume and uncertain resend retain distinct count scopes and confirmation text.

Preserve Stop for paused/interrupted work as a secondary settling action, including when the operator wants results without resuming. Keep audited Mark already submitted available for a qualifying uncertain row while other work is active; resolution is not dispatch, requires no formatting approval and retains its existing ownership/mailbox/basis checks. Restore review blocks resend, not manual resolution. Clearing a schedule preserves its existing action records without adding a schedule-action-history screen.

An accepted action is still governed by the existing service transaction. UI visibility is guidance, not a substitute for server validation. Report specific safe refusal reasons through the existing typed errors or a small known-error mapping; replace stale generic “check release readiness” guidance with actual prerequisites without exposing raw provider responses.

### 5. Keep the shell stable and update only dynamic evidence

The compact task-name/status row and section triggers stay outside polling replacement regions. The Overview lifecycle strip and summary survive or refresh with their section content. Reuse stable component IDs for dynamic status/counts, approval evidence, action scopes, schedule and recovery rows. The selected panel must survive swaps through the existing adapter; changing progress must not switch sections or refocus the page.

Preserve valid draft fields/disclosure choices using existing primitives and htmx preservation. Action confirmation identity must include every relevant scope fact: task execution, expected count, configuration/hash, current mailbox/generation, approval and schedule version/state. When scope changes, invalidate that confirmation even if the displayed numeric count is unchanged. A successful sending-type POST clears checked first-start confirmations and unsaved mode-specific input. On page entry, clear sending confirmations through the checkbox change event so Back/Forward cache restoration cannot revive prior authorization. Polling preserves unrelated draft time/test-recipient fields; do not preserve a checked confirmation across scope or type changes. Encode the complete sending control key as hexadecimal before including it in preserved-form IDs: htmx restores preserved elements with CSS ID selectors, so raw mailbox addresses and content hashes must not enter those IDs.

Initial recipient preview uses the first existing previewable membership and the same pure rendering path used by the current preview handler. Row selection updates only the preview area. Return a loading indicator and identify failed selected-row previews so stale prior content cannot be mistaken for the new selection. A GET or preview-only endpoint may render content, but it must remain attempt-free and owner-authorized.

Alternative rejected: polling/replacing the entire task page or keeping an independent Alpine workflow state. Both increase draft loss and make the selected section compete with server state.

### 6. Consolidate results and retain automatic export semantics

Use a single task-results component/view model, backed by the exports service. Remove duplicate generation controls from the old sending component. Reuse the existing eligibility rule (`completed`, `stopped`, recovered `interrupted`) and automatic pending activation; keep ordinary failure retry explicit and interrupted-generation startup rebuild automatic.

Distinguish a saved ready file from current final results when sending has resumed. Keep the as-of time visible and withhold generation while ineligible. Artifact metadata alone does not prove all candidate work succeeded. Retain generated-only deletion confirmation and the distinction from campaign deletion. Campaign-wide exports stay in Activity & downloads and task-specific attempt history stays in Results; overlap in the underlying audit records is expected, duplication of task controls is not.

Wrap the existing Task history content in the owned accordion, collapsed by default, with stable root/item IDs and `data-workspace-persist`. Retain the existing attempt/resolution items, empty state and submission/timezone guidance inside its panel. The workspace adapter expands an accordion targeted by a legacy fragment before scrolling to it and remembers that choice for later swaps.

### 7. Preserve the visual system while reducing layout competition

Keep the existing fonts, colors, branded header, component styling and blue linked breadcrumbs. Use compact summaries and section spacing instead of adding another dashboard or a metric-card grid. The spec defines information order and hierarchy; exact CSS values can be tuned within the supported widths without changing behavior.

At desktop width, use the current sidebar pattern for five task-section choices, with Overview first and selected by default. Integrate all Overview next-step guidance into the lifecycle tracker before Task details; do not render a separate Next steps card or a separate status/next-action/blocker summary before the tracker. Keep the existing next-action priority derivation. Render five equal-width stages connected by a line. Put a 16px info icon in a 24px hit area beside each stage name. Non-current stages show concise generic guidance in a tooltip; the current stage tooltip uses `StatusExplanation` instead. Limit tooltips to 240px wide and 11px text; keep them concise and free of controls. Use the existing tooltip component with keyboard focus/activation and touch/click support. Timeline triggers opt into press-to-open through `data-templ-tooltip-open-on-press`, while other tooltip triggers keep their default press-to-dismiss behavior. Connect each trigger to its tooltip with `aria-describedby` and `role="tooltip"`. Show each stage's unchanged state and matching `blockerLabel` beneath it, with the corresponding `Remedy` link when available. Only the recommended stage has a primary navigation button, using `NextLabel`/`NextHref`; other stages have no button because the sidebar navigates to them. Map external template prerequisites to Recipients and external mailbox prerequisites to Sending. When a primary destination is external, retain a secondary `Open <stage>` link to its task section. Color the recommended stage name with the primary color, without a Next badge; completed stages keep their evidence checks. Upload's task-section destination is Overview. Buttons and links only navigate and never submit, approve, schedule or send. Stack the tracker vertically on mobile, keep status text visible and provide focus treatment in addition to color. Below the current mobile breakpoint, wrap section choices into readable rows above content. Long filenames/mailboxes/hashes wrap, while wide data tables scroll within their own labeled container.

No mapping editor is introduced: keep the pipe-delimited form/draft/error contract and preserve its visible syntax/placement examples, naming every invalid variable or mapping on failure. The initial campaign creation fields and result-column defaults remain; Create campaign may explicitly navigate to Template & preview as its next setup step, while ordinary campaign entry still defaults to Tasks.

Keep the guided upload page as one form: labeled file input, required native Direct/Scheduled preference radios, Upload workbook and Cancel, campaign context with its revision or “No template”, and readable size/1,000-row bounds ahead of a “Technical limits” disclosure (archive/XML limits, cached formulas, unsupported input). Preserve the selected radio if upload fails. When a revision exists, list every mapped column as mandatory, including optional-value mappings; without one, explain that staging is possible and link to Add template instead of claiming validation.

## Risks / Trade-offs

- **Presentation accidentally changes authorization** → Derive controls from current service facts and verify forged/old POSTs still refuse invalid approval, ownership, counts and action scope.
- **Prepared versus running snapshots are confused** → Show saved/current revisions separately; test revalidation only before first start and immutable content after start.
- **Two types of missed schedule are flattened** → Include both `ready + missed` live-fire refusal and `paused + missed` downtime fixtures with their different permitted actions.
- **List summaries become expensive or drift from detail** → Reuse one status/action derivation and batch summary data; keep final action counts authoritative at the transaction boundary.
- **Polling retains stale consent or loses draft input** → Test equal-count mailbox/config changes as well as changing counts; use browser interaction checks for focus and form retention.
- **An old result looks current during resume** → Always display as-of evidence and label saved snapshots; use a single readiness rule in every entry point.
- **Route/fragment migration breaks bookmarks** → Keep GET aliases and a finite explicit legacy-fragment map; test all mappings and relevant error redirects.

## Migration Plan

1. Add presentation/read fields and tests first; keep domain state and stored records unchanged.
2. Build campaign/task shared wrappers and move existing section content into them, preserving generated-source discipline and mutation/download endpoints.
3. Switch new links/redirects to canonical fragments and retain legacy GET/fragment aliases. No user-data migration is required.
4. Verify with temporary databases, synthetic workbooks and simulated providers; do not load the local `.env`, touch live task data or send real mail.
5. Complete the browser fixture matrix and required repository checks before calling implementation complete. Record real results and skipped coverage in tasks.
6. Deploy as the normal application build. Rollback reverts presentation/read-model/asset changes; existing records and action history remain compatible because no stored state or schema was changed.

## Verification Matrix

Use synthetic owner-scoped fixtures and an isolated server. Cover the normative spec scenarios; this table identifies combinations that a linear happy-path walkthrough misses.

| Fixture | Required visible outcome and operation check |
| --- | --- |
| Empty campaign; new revision; existing approved revision | Correct Tasks default, setup guidance, explicit revision form, preview/history placement |
| Task opened without a section; explicit next-action link; legacy section fragment | Overview default with compact title/status badge; explicit and legacy links retain their intended section |
| Same filename in two tasks; long filename | Distinct references/upload times; no identity clipping |
| Staging; invalid header; all held; mixed exclusions | Specific errors, retained selection, exact partition totals, row reasons/report, zero sends |
| Ready unapproved; reusable approved; stale prepared task | Correct next section; scoped approval reuse; explicit revalidation |
| Disconnected; same-mailbox credential rotation; replacement generation; restore review | Actual blocker and intended action; no false approval reuse or implicit resume |
| Scheduled; clear/edit; Send now instead; live-fire refusal; downtime miss | One-shot timing/authorization, explicit +05:30, correct Start versus Resume, no duplicate work |
| Running; cooldown waiting; stopping | Compact live progress, reason, Stop only while applicable, no early workbook |
| Paused/interrupted/stopped; safe remainder plus uncertain rows | Safe resume scope independent of uncertain review; no automatic uncertain resend |
| Definite/auth failure; correction required; manually resolved outcome | Correct retry scope, corrected-upload route, historical evidence retained |
| Completed success; finished with failures; no remaining work | Finished does not imply delivered; appropriate recovery/results link |
| Export pending/generating/ready/failed/removed; resumed task with old artifact | One results location, automatic preparation, explicit failure retry, as-of snapshot labeling |
| Poll while editing date/test recipient; equal-count scope change | Section/focus/input retention and stale-confirmation invalidation |
| Every legacy link; Back/Forward; mutation error; foreign-owner route | Correct section/context and error; no cross-owner disclosure or read-induced mutation |

Run browser checks at 360, 768 and 1440 CSS pixels; use 1440 by 900 for the first-screen requirement. Capture representative screenshots and verify computed overflow plus keyboard focus/section semantics. Do not mark fixture coverage passed merely because static markup rendered.

For an otherwise eligible first start, replace the generic Choose sending time recommendation with Send for Direct or Choose sending time for Scheduled. These buttons only navigate to their matching controls; blockers, an already saved schedule, active progress and recovery keep their existing action labels.

Approved Test & approve pages use a native details/summary disclosure labelled Change formatting around both diagnostic and approval cards. It is collapsed on normal navigation and opens on action errors. Unapproved pages retain visible required forms. The disclosure changes visibility only; existing approval and explicit action/service contracts remain authoritative. Include the existing campaign template revision link and explain revalidation and immutable started-task revisions.

Connected mailbox pages show Change provider beside Disconnect and omit provider setup by default. The read-only change_provider=1 query reveals the existing setup card; a selected provider also reveals it for form continuity and errors. Other connection states continue to expose setup. Replacement blocking and confirmation flows are unchanged.

Schedule entry uses native date and time inputs with minute precision (step=60). The browser may localize the displayed time; its submitted HH:MM value is parsed with the date in the fixed India zone and persisted as UTC. Replace separate hour/minute/meridiem request and draft fields throughout; do not retain a second form contract. Saved schedule editing and action-error drafts populate the same two fields. Existing schedule authorization, minimum lead and fresh-count checks remain unchanged.

Sending settings separates current pacing, concurrent-submission limit and cooldown into a responsive definition list. An empty cooldown displays No active cooldown. An unboxed update form places the bounded number input above a separate Update pacing action row, with persistent range/default/mailbox-scope guidance. Mailbox capacity and delivery/background-work explanations remain available as secondary copy. Existing pacing mutation, form preservation and server limits remain unchanged.

Schedule forms keep time first and date second in the input row, followed by a full-width confirmation row and a separate full-width action row.

Sending settings uses the existing accordion primitive: the entire padded header is its trigger with the component chevron and keyboard semantics. Its stable root/item IDs support workspace persistence through progress swaps, alongside existing collapsibles. Scheduled sending keeps India time, minimum lead and one-time authorization in a compact visible note; detailed eligibility, downtime and change-type explanations move to How scheduling works. Missing/stale schedule warnings remain visible. The help disclosure keeps its open state through progress updates.
