# Proposal

## Why

Task validation currently classifies raw workbook rows before any AI preparation runs. A worksheet cell that preparation would have cleaned — `/`-joined addresses, an address written as a name, a padded address — is excluded at validation and never becomes a candidate, while the prepared dataset re-classifies the same row from cleaned values. The operator sees `Excluded at validation` rows that later appear eligible, provider usage is spent on rows whose membership was already refused, and two disagreeing counts describe one dataset. Current role-mapping revisions already define required/optional, address and duplicate checks as post-preparation work, so validation is the only stage that contradicts that contract.

## What Changes

- Validation of a current role-mapping task pins the worksheet, header row and campaign revision and records source-row evidence only. It creates no candidate, membership, manifest, exclusion report, validation category counts or selected dataset, and it still blocks on a missing or ambiguous mapped header.
- Row eligibility for current role-mapping tasks is classified only from reviewed cleaned data: a completed preparation run selected for review, or an edited final upload. The same classification is re-evaluated at dataset approval.
- Duplicate, address, required/optional, campaign-history and safety outcomes describe the reviewed dataset, not the raw upload. The prepared sending sheet contains only backend-accepted entries and the prepared exclusion report only backend-held entries.
- The task workspace shows source rows and a pending-eligibility state until a preparation review exists, then accepted/held counts, accepted entries and held reasons. Validation-time totals, pre-AI row tables and the "excluded at validation" label remain only for legacy format-mapping tasks.
- The exclusion download resolves the selected final snapshot first, then the selected prepared review, then the legacy validation report.
- Dataset approval binds to the pinned source digest in addition to the configuration and content hash, so a stored-workbook mismatch is refused like a stale revision.
- Legacy format-mapping (version 1) tasks keep their historical validation snapshot, counts, rows and exclusion report unchanged.
- Operator documentation for intake and final snapshots describes this order.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `excel-intake`: validation for current mappings becomes a header/revision preflight without row eligibility; expansion, duplicate and address outcomes move to the cleaned-data review while comma-only splitting and the expanded-entry bound stay unchanged.
- `campaign-candidates`: candidate identities and task memberships for current mappings are created from accepted cleaned entries at dataset approval, and invalid cleaned addresses remain evidence-only.
- `task-data-preparation`: the prepared review defines accepted and held entries by combining AI findings with backend checks, and prepared downloads report backend outcomes.
- `campaign-task-workspaces`: Recipients presents pending eligibility before review and prepared-review evidence afterwards, with status labels that distinguish "eligibility pending" from "no eligible recipients".

## Impact

- Affected code: `internal/intake` (preflight publication and the mapped-row source boundary), `internal/finalization` (prepared review reused by approval, downloads and reports), `internal/web` and `internal/web/views` (Recipients, finalization and download presentation), with focused tests in `internal/intake`, `internal/finalizationtest`, `internal/web` and `internal/web/views`.
- No schema migration, new dependency, prompt change or AI call is introduced. Review, download and upload remain deterministic and never invoke AI.
- Prerequisites: the implemented AI connection, task data preparation and final-sending-snapshot capabilities. There is no active prerequisite change.
- Authorization is unchanged: no global release unlock, no new approval gate. Dataset approval still grants neither received-format approval nor sending authorization.
- Out of scope: changing recipient comma-only parsing, the normalization prompt or response contract, provider/model selection, legacy version 1 task behavior, and email delivery.
