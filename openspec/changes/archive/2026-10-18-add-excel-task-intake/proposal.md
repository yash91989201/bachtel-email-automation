# Proposal

## Why

Each email task starts with an uploaded Excel workbook, and erroneous or repeated rows must be caught before any send. Campaign-scoped candidate identity and immutable task data prevent duplicate submissions and inconsistent content across corrected or overlapping uploads.

## What Changes

- Accept an Excel workbook, select its worksheet/header row and validate it against a campaign revision before making any rows sendable.
- Support tasks of up to 1,000 non-empty candidate rows, configurable input mappings and immediate validation summaries.
- Detect duplicate trimmed/lowercase email keys across the entire selected input; hold all rows in each duplicate group and offer an immediate Excel duplicates download.
- Never create candidate, task-candidate or sending-attempt records for within-upload duplicates; keep their original data in the uploaded artifact/report only.
- Create internal IDs for unique valid-address candidates, retain raw/normalized task snapshots and record row-level eligibility/errors.
- Match candidates by campaign plus normalized email; exclude submitted, uncertain and already scheduled candidates using authoritative app history.
- Allow corrected uploads as new tasks while preserving earlier snapshots and allowing other unique eligible rows to proceed.

## Capabilities

### New Capabilities

- `excel-intake`: Workbook parsing, mapping validation, row validation and immediate duplicate reports.
- `campaign-candidates`: Campaign-scoped identity, immutable task membership and overlapping-task eligibility.

### Modified Capabilities

None.

## Impact

Implementation order: **3 of 5**; depends on changes 1 and 2. Change 4 adds dispatch and final transactional candidate claims.

Reuse Excelize for reading/writing XLSX and PocketBase transactions/indexes for candidate identity. Extend migrations, protected campaign/task views and tests. Store excluded-row details in file artifacts rather than database candidate records.

No Google Sheets integration, source-sheet ID column, inline spreadsheet editor, legacy XLS/macro workbook support or real email sending. Imported sending-status columns never replace campaign history in the app.
