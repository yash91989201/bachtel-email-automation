# Design

## Context

- `internal/preparation/worker.go` `publish` completes a run in one transaction: it stores the output artifact, sets the run completed and settles the job. It never touches the task's `selected_preparation_run`.
- `SelectRun` (`internal/preparation/service.go`) is the only writer besides preflight clearing: it refuses incomplete/foreign runs and non-runnable tasks, and when the selected run changes it clears `selected_final_snapshot`, `final_snapshot_digest` and `finalization_error`.
- The workspace already renders a completed run's corrections, findings and counts, and `finalizationData` even computes the backend review for that run, but approve/upload availability and both prepared downloads are gated on `EvidenceSelected`.
- Approval binds the stored selection: `seal` refuses when `selected_preparation_run` differs from the run that was classified.

## Goals / Non-Goals

**Goals:** One stored selection rule; review, approve/upload and prepared downloads available immediately after completion; explicit selection preserved for history; no change to classification, refusals or sending boundaries.

**Non-Goals:** Deriving selection from the newest run without storing it; auto-starting preparation; changing prepare/retry/rerun kinds; changing finalization or snapshot semantics.

## Decisions

### 1. Select inside the publish transaction, with the same guard as explicit selection

`publish` sets `selected_preparation_run` in the transaction that marks the run completed, but only when the task has no selected final snapshot, is not started, not scheduled, not closing or closed, and the run settled (not cancelled). It reuses one helper with `SelectRun` so the invalidation rule (clear snapshot fields only when the selected run changes) cannot diverge.

Rationale: selection is durable evidence and approval already trusts the stored id. An alternative — treating the newest completed run as implicitly selected at render time — would make approval accept an identity the task never stored, weakening the stale-run refusal that protects classification from a changed run.

### 2. A finalized dataset is not silently replaced

When a snapshot is selected, a completed run does not become selected evidence. Selecting an older or newer run then stays an explicit operator action with its existing consequence: the selected final snapshot and its digest are cleared, so no approval can authorize the previous values. Rationale: auto-selection must never discard a dataset the operator already approved; that invalidation is a decision, not a side effect of the AI finishing.

### 3. Task lifecycle wins over completion

A late publication for a started, scheduled, closing or closed task still settles the run but changes no selection. `publish` already refuses to publish for closing tasks; the remaining states are handled by the same `runnableTask`-equivalent guard used by `SelectRun`, evaluated inside the transaction so a stale page or interleaved action cannot select evidence for frozen content.

### 4. The workspace needs no new selection control

With the stored id present, `preparationData` reports `EvidenceSelected`, `finalizationData` publishes the review, `PreparedHref` and both prepared downloads, and `CanApprove` passes. Older runs keep their existing selection action. Only the copy that frames selection as required changes.

## Risks / Trade-offs

- An operator who wanted a different run reviewed gets the newest one until they select another → visible in the run history with the selected marker; selecting still works and still clears the final snapshot.
- A run completing while a finalization form is open → approve/upload rechecks the stored run and configuration in the seal transaction, so a stale form is refused rather than approved against changed evidence.
- Auto-selection hides that a selection exists → the run table keeps showing which run is selected, and the review header names the selected evidence.

## Migration Plan

No schema change. Existing tasks keep their stored selection; a task whose completed run predates this behavior selects on its next explicit select or rerun. Rollback is deleting the publish-time selection write; stored ids remain valid evidence.

## Open Questions

None.
