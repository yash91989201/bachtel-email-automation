# Design

## Context

See proposal.md for motivation and user-visible scope. Current code stores `source_name` on `send_tasks` but has no separate task name. `internal/intake/service.go` stages the workbook, normalizes its filename through `sourceName`, and maps records into `intake.Task` for loading and listing. `internal/web/task.go` passes the multipart filename and sending preference to staging. Campaign rows and task headings/breadcrumbs use `SourceName` through their view models.

Task sections use ordinary GET paths and shared server-rendered navigation. The current sections are Overview, Recipients, Test & approve, Sending and Results. PocketBase migrations already add task fields and backfill existing records. This design changes persistence, intake, handlers and views, so a design artifact is warranted; none of the proposed behavior is implemented yet.

## Goals / Non-Goals

**Goals:** Keep source identity separate from editable presentation metadata; normalize and validate names at the service boundary; make updates independent of task lifecycle transitions; extend the existing section system.

**Non-Goals:** New naming abstractions, dependencies, uniqueness rules, live propagation into already-open browser pages, or an expanded settings framework. Settings initially contains only task naming. Processing controls stay in their established sections.

## Decisions

### Store one task name independently of the source filename

Add `name` as a PocketBase text field on `send_tasks`, capped at 255 Unicode code points, and expose it on `intake.Task`. Keep `source_name`, the private file name, task ID, digest and relationships unchanged. Store the resolved default rather than treating an empty name as a permanent display-only instruction.

Reusing `source_name` would overwrite source evidence. Deriving a name only in views would not meet the requested backfill and would duplicate logic across list/detail views.

### Resolve and validate names in intake

Use `strings.TrimSpace` on explicit names. For omitted or whitespace-only input, use the same normalized filename already saved as `source_name`, including its extension. Reject a resolved name longer than 255 Unicode code points with an actionable validation error; do not truncate it. Allow duplicate names and ordinary Unicode text, and render through Templ's normal escaping.

Extend the existing staging entry point and its callers with a name input rather than introducing parallel named/unnamed staging methods. Creation must save the resolved name with the staging record, avoiding a second update after creation. Share this small normalization/validation rule with renaming; no generalized metadata service is needed.

### Update only naming metadata when renaming

Add an intake operation that rechecks task ownership through its campaign and updates only `name` and the ordinary update timestamp. Resolve a blank new name from the task's stored `source_name`. Permit this metadata operation in every lifecycle state, including running, closing and closed. Do not route it through guards intended to prohibit preparation or dispatch on closed tasks.

Use a short transaction and a column-scoped update to avoid saving a stale whole task record over concurrent scheduler, preparation, closure or dispatch updates. No provider calls, filesystem writes or long-running work belong in this transaction. Last successful name save wins for concurrent renames; optimistic versioning would add complexity without a current requirement.

### Add a native Settings section and owner-only POST

Add `settings` after `results` to task section constants, navigation and section selection. Register `GET /campaigns/{id}/tasks/{task}/settings` in the existing route pattern and `POST /campaigns/{id}/tasks/{task}/name` for saving. Both use the existing operator authentication and campaign-task membership checks; the mutation rechecks ownership in intake. Reuse existing request-origin protections and bounded form parsing.

Settings renders only a labeled Task name field, visible blank/default guidance, a 255-character limit and Save changes. Prefill the current name. Success redirects to Settings using POST/Redirect/GET with visible save feedback. Validation or storage failure keeps Settings selected, preserves the submitted text, reports a safe error and leaves the previous name unchanged. GET navigation remains read-only. Reuse existing field/input/button components and page layout.

### Present the task name without losing workbook context

Carry task name and source filename separately in campaign/task view models. Change the campaign Tasks column label from Workbook to Task and use the name as its linked label. Add the source filename to the existing keyboard-accessible identity tooltip with reference, upload time and revision. Use the name in the shared task heading and final breadcrumb on every section, including Settings; show Workbook in Overview Task details. Preserve the reference/upload-time distinction when names repeat.

Existing links continue to use IDs. Subsequent page loads reflect a successful rename; no client-side router or cross-tab synchronization is introduced. Stored exports, historical records and download filenames are not rewritten.

## Risks / Trade-offs

- Duplicate names can remain ambiguous -> keep the existing reference and upload time accessible, plus the source filename.
- Long or markup-like text can disrupt layout or escape its context -> validate length, keep text escaping, wrap names, and check narrow layouts and keyboard access.
- Renaming active tasks could overwrite concurrent workflow updates -> use a transaction and update only the name column; verify that processing/approval/schedule state remains intact.
- An exceptional old record might lack `source_name` -> use the existing filename-normalization fallback `task.xlsx` without deriving a name from private storage paths.
- Browser `maxlength` counts UTF-16 units while the server limit is Unicode code points -> keep server-side counting authoritative and avoid client constraints that incorrectly reject names within the specified limit.

## Migration Plan

1. Add the `name` field using the established additive migration pattern. Backfill unnamed existing records from their exact stored `source_name`, across all lifecycle states. Use `task.xlsx` only when that source name is missing or whitespace-only. Preserve any already-populated name if migration setup is retried.
2. Perform the backfill within the migration transaction, updating only the new name column. Do not change upload timestamps, source files, configuration, approvals, schedules, execution state, candidate memberships or history. Test against isolated databases and rerun the backfill operation to confirm it preserves populated names.
3. Deploy the migration and application changes together through the ordinary migration mechanism. Subsequent creation and renaming persist resolved names; reads never perform backfill writes.
4. For application rollback, retain the additive column and its data, following the repository's existing forward-only task migrations. Older code can ignore it and display source filenames. Do not delete names or restore a database snapshot merely to roll back presentation behavior.
