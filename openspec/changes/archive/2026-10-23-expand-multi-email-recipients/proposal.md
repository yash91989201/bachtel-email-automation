# Proposal

## Why

Recipient workbooks can contain several email addresses in one recipient cell. The current import rejects those rows, forcing operators to manually duplicate the person's details before upload; instead, each address should become an independently validated task recipient.

## What Changes

- Expand comma-separated bare addresses from the explicitly selected recipient column into individual entries, copying the other mapped values and preserving the original worksheet row reference.
- Exclude only invalid entries; valid unique addresses from the same source row are saved normally and remain subject to existing required-value and campaign-history checks.
- Apply the existing trimmed-lowercase duplicate rule after expansion across the complete upload. Hold every occurrence, including repetitions within one cell; do not keep a preferred copy or store duplicate candidates/memberships.
- **BREAKING:** Replace the 1,000-source-row task-size contract with a maximum of 1,000 expanded recipient entries, counted before duplicate/invalid exclusions. Oversized input fails validation atomically.
- Show source-row and recipient counts distinctly, with separate previews and reasons for each entry.
- Preserve original worksheets and add a collision-safe `Recipient results` worksheet containing copied source data and individual recipient outcomes. Do not overwrite a source row with one recipient's outcome.
- Preserve explicit test approval and start/schedule authorization. Importing, splitting, previewing and generating reports never send email.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `excel-intake`: Expansion, the post-expansion limit, per-entry validation and duplicate reporting.
- `campaign-candidates`: Independent immutable memberships sharing a source row while retaining campaign/email identity.
- `email-personalization`: Single-address rendering and recipient placeholders for each expanded entry, with original cell visibility.
- `history-exports`: Original workbook preservation plus a recipient-level results worksheet, including file-only exclusions.
- `campaign-task-workspaces`: Upload guidance, count units, per-recipient previews and exclusion visibility.

## Impact

Intake planning/publication, task-candidate collection constraints and snapshot provenance, rendering inputs in task preview/sending, file manifests and exclusion reports, XLSX results and related task UI/tests change. The existing renderer's single-address validation, candidate reservations, provider adapters, pacing and attempt ownership remain reusable. No new dependency or provider API is needed.

This change is independent of the active `save-provider-sent-copies` proposal; neither requires the other. Preserve its artifacts and unrelated dependency edits. Out of scope: CC/BCC or multiple To recipients in one message, semicolon/whitespace-only delimiters, display-name address syntax, inferred identity by name, automatic sending, and retroactive expansion of saved task snapshots.
