# Proposal

## Why

Operators currently identify workbook tasks by filename, making repeated uploads difficult to recognize. An optional task name lets the creator describe a task's purpose while retaining the workbook filename as a useful default and source reference.

## What Changes

- Add a labeled, optional Task name field to workbook-task creation. Trim surrounding whitespace; an omitted or whitespace-only name defaults to the stored workbook filename, including its extension.
- Allow duplicate task names and names up to 255 characters. Preserve the submitted name when creation fails and explain invalid input inline.
- Use the saved task name in the campaign Tasks list, task heading and task breadcrumb. Retain the original workbook filename as source information and the existing reference/upload time for distinguishing repeated names.
- Backfill existing tasks with their stored workbook filenames, including completed and closed tasks, without changing task identity or processing state.
- Add Settings after Results in the task sidebar, with an owner-only form to update the task name in any lifecycle state. Clearing the name restores the workbook-name default; successful updates appear on subsequent task and list views.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-task-workspaces`: Optional task naming during upload, recognizable names throughout the task workspace, workbook-name defaults for new and existing tasks, and a Settings section for renaming.

## Impact

- Additive task-schema migration and existing-record backfill; intake staging, task loading and name updates; upload/settings handlers and error feedback; task section navigation; campaign/task view models, Templ sources and generated output; focused migration, service, handler and rendering tests.
- No new dependency or prerequisite OpenSpec change. This change is independent of the active `add-ai-sample-preview` change.
- Naming does not grant access or authorize validation, preparation, approval, scheduling or sending. Existing ownership and action checks continue to apply.
- Out of scope: name uniqueness, search/filtering, task identifier or existing URL changes, other task settings, account-settings warning redesign, and changes to historical exports or download filenames.
