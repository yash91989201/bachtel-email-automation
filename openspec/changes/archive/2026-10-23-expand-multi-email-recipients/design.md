# Design

## Context

See proposal.md for the user problem and confirmed scope. Current implementation findings:

- `internal/intake/service.go:planRows` creates one admission per worksheet row, groups duplicate keys before validating recipients, and publishes the snapshot transactionally.
- `internal/campaign/normalize.go:NormalizeRecipient` deliberately accepts only one bare address. `Template.Render` and the sending worker render from the saved member's raw mapping.
- `internal/migrations/1792233050_intake_collections.go` enforces `UNIQUE(task, source_row)` for task memberships. Campaign identity already has `UNIQUE(campaign, email_key)`.
- Intake manifests, exclusion reasons and duplicate groups identify source rows. `publish` currently uses a source-row-keyed reason map.
- `internal/exports/task_xlsx.go` indexes members by source row and writes result columns into that original row, so simply expanding admissions would either violate the database constraint or overwrite/misassociate results.
- Sending work and attempts already reference task-member and candidate IDs. Provider adapters do not need list-recipient support.

## Goals / Non-Goals

**Goals:** Expand once at the intake boundary, preserve original source evidence, and reuse single-recipient rendering, candidate claims and dispatch. Keep every admission/exclusion and outcome identifiable even when source row numbers repeat.

**Non-Goals:** General address-list parsing, new provider behavior, a separate contact/person model, or rewriting historical memberships/attempts. Campaign sample previews and diagnostic test destinations remain single-address inputs.

## Decisions

### 1. Expand before duplicate classification and stop at the resource bound

For each row carrying mapped data, split only the selected recipient cell with the standard library comma splitter. Keep each token's original text and its 1-based position; trim for duplicate identity and address validation. Spaces alone and semicolons are not separators. Preserve empty tokens as invalid entries; a wholly empty cell yields one invalid entry. Continue to ignore rows whose mapped fields are all blank.

Count every expanded entry before exclusions and stop as soon as a 1,001st entry is encountered, before candidate storage or snapshot publication. Retain existing file, archive, XML and structural worksheet limits. Replace the earlier physical candidate-row gate with the definitive expanded-entry check after identifying mapped data; rows containing only unmapped values must not consume the recipient limit or trigger the old 1,000-row gate.

Run the existing trimmed-lowercase duplicate grouping over the complete expanded input, including nonempty invalid tokens and entries missing other required values. Exclude every group member, even within the same cell, before other per-entry classification. Empty keys remain invalid rather than forming a duplicate group. Valid unique entries pass through existing required-value and candidate-history eligibility rules.

Alternative rejected: splitting in the provider or renderer would bypass intake identity, duplicate detection and per-entry history. Silently deduplicating would change the confirmed all-occurrences-held policy.

### 2. Separate source provenance from recipient identity

Add a recipient position to admission, membership snapshots, manifest entries, exclusion-report rows and duplicate-group references. The selected worksheet plus `(source_row, recipient_index)` identifies an input occurrence, including file-only invalid or duplicate entries. Continue to use member ID for dispatch/attempts and campaign/email key for candidate identity; source coordinates are provenance, not a candidate identity.

Use a forward migration replacing the unique `(task, source_row)` constraint with unique `(task, candidate)`. Multiple addresses can then share a source row while one candidate still has at most one membership in a task. Preserve campaign/email uniqueness and reservation transactions. Use source row then recipient position for deterministic display/report order; retain existing dispatch selection indexes unless a measured/query requirement needs adjustment.

Keep the original workbook unmodified. For each expanded admission, clone mapped values and put only that occurrence's raw token into the selected recipient variable. Store the full original recipient cell alongside the membership for comparison previews. All other mapped variables retain their original cell values, even if an ordinary content mapping references the same source column. Normalize each entry independently; never share a mutable raw/normalized map between siblings.

This lets the existing `Template.Render` consume a single recipient without relaxing its validation or introducing a second renderer. Comparison UI labels the original full cell separately from the individual raw token and normalized address.

Alternative rejected: using synthetic Excel row numbers would lose source provenance. Retaining the full list as the renderer's recipient input would still fail single-address validation.

### 3. Publish occurrence-aware artifacts atomically

Keep the classify-before-storage and transactional publication flow. Replace row-only reason/member lookups with occurrence-aware references throughout publication, manifests, duplicate groups, preview preparation and exports. Persist independent normalized values for admitted entries. Invalid and duplicate entries remain file-only; include token, source row and recipient position in their manifest/report data so no excluded occurrence needs a candidate record.

Advance the manifest format to version 2. New artifacts explicitly identify occurrences. Treat version 1 manifest rows as single occurrences at position 1 in one reader boundary; do not resplit their saved source cells. Preserve the version 1 status/outcome association. No background migration or revalidation changes an existing task's eligibility or content.

Exclusion downloads retain every original source column and append collision-safe individual-recipient, source-coordinate and reason columns. Multiple excluded occurrences from one source row produce multiple report rows. If an operator re-uploads a corrected report, the original recipient column remains authoritative: importing appended report columns never overrides mappings or sending history.

### 4. Generate a separate recipient-results sheet

Keep all original sheets/cells/order intact and add `Recipient results`, or the first deterministic unoccupied numbered variant using case-insensitive worksheet-name collision checks. Apply this layout consistently to regenerated single-address tasks too; previously saved artifacts are unchanged until an explicit eligible regeneration.

Write one result row per manifest occurrence in source-row/recipient-position order. Copy all original source columns and append source worksheet, original row, recipient position, individual recipient and the configured status/time/error columns. Preserve the full original list in its copied source column; the explicit individual-recipient column identifies the outcome. Resolve metadata/result label collisions against both source and other added labels before showing/pinning labels. Write generated candidate text with the existing inert string APIs.

Associate admitted results by member ID or task/candidate identity carried/resolved through the manifest, never just source row. Resolve file-only exclusions directly from the manifest/source artifact. Keep existing consistent snapshots, as-of timestamps, atomic publication, export recovery and explicit retry behavior. Do not collapse multiple recipient outcomes into one original-row status.

Alternative rejected: duplicating rows in the original worksheet would alter its structure, formulas and ordering. Writing one chosen or aggregated status into a multi-recipient row would conceal individual outcomes.

### 5. Make count units and review actions explicit

Record original contributing source-row count separately from the expanded `Counts.Total`; category counts continue to sum to expanded entries. Update upload guidance, task summaries/tooltips, Recipients lists, preview selectors, reason/report text and action scope labels. Attention counts count distinct affected entries rather than overlapping reasons or physical rows. Sending progress and confirmations already describe candidate work; verify that multiple siblings contribute independently.

Do not add a new setting or mapping syntax: the existing explicitly selected recipient role supplies expansion. Keep the UI's current sections, visual components and approval/start/schedule controls.

## Risks / Trade-offs

- Repeated source row numbers can overwrite lookup values or produce unstable selection -> require occurrence-based joins and regression cases spanning intake, preview, sending and export.
- A shared map could leave both siblings targeting the last address -> copy per-entry maps and verify recipient placeholders and simulated sends for both siblings.
- Empty tokens from trailing commas are visible invalid entries and consume the task limit -> state this input rule in visible guidance and test it explicitly.
- A corrected exclusion report may still contain the full source list -> show the individual excluded entry clearly and explain correcting the mapped recipient cell before re-upload; do not silently replace original source data in reports.
- Manifest evolution can affect old tasks and export recovery -> support singleton version 1 reads at one boundary and test historical exports without changing saved attempts.
- The new result location changes downstream workbook consumption -> document the separate results sheet, including for single-address inputs, and preserve original worksheets.

## Migration Plan

1. Add provenance fields and backfill existing memberships with position 1 and their saved single-recipient raw value as original-cell comparison data. Do not resplit source workbooks or change candidate/attempt IDs.
2. Replace membership uniqueness while preserving existing members and candidate reservation invariants; ship the version-aware manifest reader alongside version 2 writes.
3. Enable expansion, occurrence-aware publication, UI and results together. Existing stored result files stay available; future eligible generation uses the new layout and can read old singleton manifests.
4. Validate with temporary databases/workbooks and simulated providers, including old singleton fixtures and mixed outcomes. No live mailbox action or local environment secrets are needed.
5. Once multi-recipient memberships exist, the old unique-row constraint cannot be restored safely. Roll back through a pre-migration database/files backup and matching binary after stopping active work, rather than deleting sibling memberships or merging outcomes.
