# Design

## Context

See proposal.md for motivation. The existing campaign schema has a required `name` text field with a maximum of 200 characters and no uniqueness constraint. Creation trims and requires a name. The campaign service loads campaigns by ID and owner, and campaign/task pages derive the campaign name from the campaign record.

Campaign Settings currently contains result-column editing and campaign deletion. Its forms use ordinary server-rendered GET/POST navigation. The implemented task rename operation provides a nearby pattern for a short transaction, ownership check and column-scoped metadata update. Campaign renaming needs a service operation, handler and view updates; these cross-module authorization and preservation concerns justify this design. The rename behavior described here is proposed, not implemented.

## Goals / Non-Goals

**Goals:** Update campaign identity text through the existing Settings page, keep ownership enforcement in the service, and prove that the write cannot alter concurrent workflow state.

**Non-Goals:** Schema changes, generic settings infrastructure, nested routes, live updates to already-open pages, or a new name audit/history system. The other Settings controls keep their existing operations and layout conventions.

## Decisions

### Reuse the existing field and validation conventions

Save the trimmed name in the campaign's existing `name` field. Use the existing 200-character storage bound, measured as Unicode code points for the rename operation, and reject blank/oversized input with fixed actionable validation messages. Keep duplicates valid. Do not share task-name defaulting: campaigns require an explicit nonblank name, and their storage limit differs from tasks.

No migration or backfill is needed. A second label field or a unique slug would duplicate existing identity information and change scope.

### Write only name metadata under an ownership check

Add a campaign service rename method accepting context, trusted owner ID, campaign ID and name. In a brief transaction, load the owner-scoped campaign and issue a parameterized update of only `name` and `updated`. Propagate request cancellation. Use the same not-found result for missing and foreign campaigns. Do not save a stale whole campaign record, since it could overwrite a concurrent current-revision pointer or result-label update.

Follow the established task metadata update pattern without introducing an interface or metadata service. Do not reuse deletion's active-work guards: rename is allowed with active tasks because no processing data changes. Concurrent renames use last successful save wins. Do not invalidate approval hashes, rewrite tasks, or publish jobs/events for a name change.

### Extend the existing Campaign Settings page

Add a General card before result-column settings, with a labeled Campaign name input prefilled from the current name, visible required/200-character guidance and Save name. Keep the existing `/campaigns/{id}/settings` GET. Add an authenticated `POST /campaigns/{id}/name` using bounded form parsing and existing request-origin protections.

The handler and service both enforce access through the existing owner-scoped campaign boundary. Successful saves redirect to Settings with visible feedback using the established POST/Redirect/GET pattern. Validation and storage errors render Settings with a separate name draft/error, preserving the submitted text while the shared heading still identifies the saved campaign. Keep error output safe and provide a correction or retry remedy. Avoid treating a query parameter as evidence that storage succeeded.

Reuse field, input, button and card components. Templ escaping keeps names inert text. Use wrapping and keyboard-accessible controls; client-side length handling must not incorrectly reject supplementary Unicode characters that fit the server's code-point limit.

### Let existing reads display the current name

The list, campaign shell and task campaign breadcrumbs already load campaign data. Verify that fresh responses use the new name rather than copying it into child records or caches. Preserve campaign IDs, task IDs, task names and all existing URLs. Existing name-based campaign-list ordering may naturally change after a rename.

Stored historical email content and ready exports remain immutable snapshots; renaming does not rewrite or regenerate them. Later explicitly requested exports continue using their existing generation rules.

## Risks / Trade-offs

- A whole-record save could clobber revision or settings changes -> use a transaction and a name-only parameterized update, with preservation tests.
- Active-task restrictions could accidentally block harmless metadata editing -> keep rename independent of task lifecycle guards and test active/scheduled/terminal task fixtures.
- Name drafts could replace saved identity in an error response -> carry the draft separately from the page's persisted campaign name.
- Unicode length handling can differ between browser and backend -> count code points on the server, cover the boundary with Unicode tests and avoid a stricter UTF-16 client limit.
- Existing exports can show the former campaign label -> retain their recorded contents; any new export remains an explicit action under existing rules.

## Rollout

Ship the service, route and Settings UI together. No database migration is required. Rolling back application code removes the edit control while retaining the latest name in the existing field; tasks and operational records require no rollback.
