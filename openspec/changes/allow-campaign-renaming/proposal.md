# Proposal

## Why

Operators can name a campaign during creation but cannot correct or update that name afterward. Renaming in Campaign Settings lets them keep campaigns recognizable without recreating them or disturbing their operational records.

## What Changes

- Add a General area in Campaign Settings with a prefilled Campaign name field and an explicit Save name action.
- Allow only the owning operator to rename a campaign. Trim surrounding whitespace, require a nonblank name, preserve the existing 200-character limit and allow duplicate names.
- Reflect a saved name in subsequent campaign lists, campaign headings and campaign breadcrumbs, including task pages. Keep stable campaign/task URLs and task names.
- Treat renaming as metadata only, available even while campaign tasks are active. Preserve tasks, source data, selected snapshots, revisions, approval evidence, schedules, candidate history, attempts and existing generated artifacts.
- Show clear success feedback or retain the submitted name with a safe validation/storage error; a failed save keeps the prior name.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-task-workspaces`: Owner-only campaign renaming in Campaign Settings with consistent identity display and preservation of operational evidence.

## Impact

- Campaign service name validation/update operation; campaign Settings handler, view model and Templ source/generated output; focused service, handler and view tests.
- Reuse the existing campaign name field. No migration, backfill, dependency or prerequisite change is needed.
- No revision creation or new formatting approval is required merely to rename. Existing preparation and sending authorization remain separate.
- Out of scope: task renaming changes, nested Settings routing, source-workbook replacement, name uniqueness, campaign slugs, rewriting historical artifacts, and a campaign rename audit log.
