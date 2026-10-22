# Proposal

## Why

Campaigns currently foreground revision creation, while one task's review and sending controls occupy two different workspaces with conflicting statuses, step numbering and results controls. Operators need to recognize the workbook they are handling, understand its current state and take the correct next action without reconstructing the workflow across pages.

## What Changes

- Make the campaign workspace open on **Tasks**, with persistent **Tasks**, **Template & preview**, **Activity & downloads** and **Settings** sections. Show the current template revision and its formatting approval before offering explicit revision editing.
- Keep workbook upload and worksheet/header selection guided with campaign context, readable bounds, mandatory mapped columns and technical limits separated from common constraints. Require a native Direct/Scheduled preference when creating a task; persist it without scheduling or authorizing candidate work and retain it on upload errors.
- Replace task review/sending navigation with one task workspace that opens on **Overview**, followed by **Recipients**, **Test & approve**, **Sending** and **Results**. Keep a compact task-name/status-badge row and breadcrumbs across sections; integrate current status, next action, blocker remedies and evidence into the Overview lifecycle tracker before detailed metadata, using a single recommended-stage navigation button.
- Distinguish recipient validation, task execution, schedule, exact-content/mailbox approval and result-file availability. Specify exact display labels, precedence, required reasons and state-specific controls rather than a generic “Ready” badge.
- Make start/schedule, stop, safe resume, definite-failure retry and uncertain-outcome review appear only in applicable contexts. Preserve every required confirmation, fresh-count check, ownership check and existing sending safeguard.
- Persist Direct or Scheduled as the task's durable `sending_type` preference, both at upload and when changed before execution. Sending views and all navigation derive from this saved value; GET requests never override it. A `choose` query may open the chooser without changing the saved value.
- Permit sending-type changes only for staging/ready tasks that have not started. Refuse a change to Direct while an enabled or missed schedule exists until the operator explicitly clears it; never cancel a schedule implicitly. Started tasks keep their existing sending type and state-specific controls.
- Make campaign/task lists useful for continuing work: recognizable workbook identity, upload time, revision, meaningful status, current recipient summary and a destination-specific action. Keep worksheet/header detail inside the task and move source-header suggestions into **Template & preview** instead of a campaign-list column.
- Consolidate task previews/exclusions under Recipients, recovery/progress under Sending and task downloads/history under Results. Explain automatic export generation and offer explicit retry for generation failures.
- Specify content order, representative wireframes, empty/error/blocked states, keyboard and narrow-screen behavior, and a fixture/acceptance matrix. Preserve the current visual identity and locally owned components.

## Capabilities

### New Capabilities

- `campaign-task-workspaces`: Operator-facing information architecture, identity, navigation, status presentation, action hierarchy and responsive behavior for campaign and task workspaces.

### Modified Capabilities

- `excel-intake`: Place validation review in Recipients and start/schedule confirmation in Sending beside fresh eligibility. Require clearing an enabled schedule before repinning a prepared task to revised content, preserving explicit authorization and existing preflight/exclusion rules.

## Impact

- Presentation and view-model work in `internal/web/views/campaign.templ`, `task.templ`, `sending.templ`, shared breadcrumbs/navigation, corresponding web handlers and focused tests. Supporting read models must expose workbook identity, timestamps, current approval and eligibility without weakening operator isolation.
- Existing campaign/task/sending links must resolve into the unified navigation; submitting an old action URL must retain ownership, CSRF, idempotency and server-side validation. Sending type comes from the task record, with no mode override in navigation URLs.
- Depends on the implemented campaign templates, Excel intake, campaign candidates, email dispatch, recovery, exports and mailbox ownership capabilities. There are no active prerequisite changes at proposal creation.
- Candidate start, future scheduling, resume, retry and resend continue to require applicable exact-configuration/current-mailbox approval and explicit operator authorization. No new global release unlock or implicit sending is introduced.
- No new runtime dependency, provider change, dispatch/recovery algorithm, data-retention policy or mailbox-reset behavior is planned. The revalidation refusal for an enabled schedule is necessary supporting work so revision changes cannot reuse old authorization. No live email is required for implementation verification.
- **Out of scope:** redesigning mailbox/account setup; a new mapping-table editor or mapping syntax; task renaming, search/filter/pagination features; new analytics or delivery tracking; changing data-import bounds or domain state transitions. Existing mapping controls retain visible format/examples and are reorganized with the template, not replaced in this change.

This change creates a UI behavior contract and an implementation plan; the proposal is not evidence that the redesigned UI or application verification is complete.
