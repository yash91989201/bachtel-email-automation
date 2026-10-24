# Design

## Context

Current verified behavior:

- `internal/intake/service.go` `validate` classifies rows with `planRows` and publishes a candidate snapshot for every mapping version, before any AI call. Duplicate and invalid-address exclusions, counts, manifest and the exclusion report are written at that point.
- `internal/preparation/service.go` `start` builds its input from `intake.MappedRows`, which today requires an admitted task. Preparation therefore runs only after the intake snapshot exists, while its input covers every mapped and non-blank source row regardless of that snapshot's exclusions.
- `internal/finalization/validate.go` `classify` already evaluates required/optional values, recipient validity, duplicates, campaign history and template safety from prepared values, and `internal/finalization/store.go` publishes candidate memberships from the accepted entries when a final snapshot is created. The prepared sheet and prepared exclusion report are derived from raw prepared rows, not from that classification.
- `internal/web/task.go` builds `HeldRows` from the intake manifest for non-role tasks and labels them "Excluded at validation"; role-mapping tasks hide the intake rows but the workspace still derives the main status from sending progress.
- `internal/presentation/presentation.go` `MainStatus` maps a ready, finalization-required, unfinalized task with zero eligible and zero remaining recipients to "No eligible recipients". Before any admission evidence exists this label is wrong.
- Legacy format-mapping (version 1) tasks are still readable and complete; their validation snapshot must not change.

Proposed behavior is the capability deltas. This design only records how to reach it.

## Goals / Non-Goals

**Goals:** One eligibility classification for role-mapping tasks, computed from reviewed cleaned values, reused by review, approval and final upload; no candidate evidence before an operator-approved dataset; no AI outside preparation; unchanged legacy behavior; a workspace that never presents stale pre-AI eligibility as current.

**Non-Goals:** Changing recipient comma-only splitting, the prompt/response contract, provider or model selection, the final-upload structural rules, the expanded-entry bound, or any schema.

## Decisions

### 1. Validation becomes a preflight for current mappings

For a revision with the role mapping, `validate` pins the worksheet, header row, configuration identity, content hash and source-row count, clears any prior manifest, exclusion artifact, category counts, selected preparation result and selected final snapshot, and creates no candidates. A current task that was already validated under the old path keeps its evidence until an explicit revalidation, which clears it through the same preflight. Legacy revisions keep `planRows` and the published snapshot verbatim.

Rationale: the alternative — keep classifying raw rows and let preparation override them — leaves two disagreeing datasets, spends provider usage on rows that can never be admitted, and cannot repair a cleaned address that intake already refused. Legacy retention avoids rewriting history for tasks whose stored evidence operators may already reference.

### 2. One classification function, three callers

`finalization.classify` stays the only eligibility rule set. Review and approval both classify prepared values; final upload keeps classifying operator-authored values. A read-only review wraps the existing classification and returns counts, accepted entries and exclusions without creating records. Approval repeats the classification inside its publication transaction, so a review never becomes admission evidence by itself.

Alternatives rejected: adding a second validator for review (drift), or storing review results (duplicates durable state that can go stale between select and approve).

### 3. Provenance stays with the evidence, not with the candidate

Prepared rows are paired with the original mapped values of their source row, so a held entry reports the uploaded cell alongside the cleaned proposal. Accepted entries carry cleaned values into memberships. No new storage: the preparation run already captures input and output.

### 4. Approval binds the stored workbook

`ApproveAsIs` refuses when the preparation run's source digest differs from the task's pinned digest, in addition to the existing configuration and content-hash checks. A re-uploaded or tampered workbook cannot be approved through stale run evidence.

### 5. Workspace states follow available evidence

Role-mapping tasks present three states: pending eligibility (source rows only), prepared review (counts, accepted entries, held reasons) and final snapshot (accepted and excluded rows). The main status gains a pending-eligibility case ahead of the zero-eligible case, using the same facts the workspace already loads. Exclusion downloads resolve final snapshot, then prepared review, then the legacy validation report.

### 6. No schema change

Preflight uses existing task fields and clears stale evidence instead of adding columns. Candidate creation at snapshot publication already exists, so deferring it needs no new collection.

## Risks / Trade-offs

- Operators lose early duplicate feedback for role-mapping uploads. Mitigated by the pending-eligibility status naming preparation as the next step and by review counts appearing as soon as a run is selected.
- Two behaviors coexist while legacy revisions remain. Removing the legacy path requires its own change with a migration for remaining version 1 tasks.
- The expanded-entry bound now triggers at review or final upload for current mappings, so an oversized dataset is refused later than before; the refusal still publishes nothing.
- Status derivation and view models must change together. Shipping the eligibility change without the pending-eligibility label reports a misclassified task.
- Review runs a full classification on every page load for the selected run. It is bounded by the existing entry limit and performs no network or write work.
