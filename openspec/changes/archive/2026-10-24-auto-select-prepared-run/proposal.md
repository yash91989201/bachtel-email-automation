# Proposal

## Why

Preparation already publishes a completed run with its cleaned values, corrections, findings and occurrence outcomes, and the backend review already computes accepted and held entries from it. The operator must still click **Select for review** before the review can be approved or any prepared download is offered, even though the review counts are already displayed for that run. That extra click is the only manual step between AI output and the approve/upload decision, and it is repeated after every rerun.

## What Changes

- A preparation run that completes becomes the task's selected prepared evidence when the task has no selected final snapshot, in the same transaction that publishes the run.
- With a selected final snapshot present, automatic selection does not happen: selecting a different run already invalidates that snapshot, so that choice stays explicit.
- The workspace then shows the backend review as the immediate next step, with approve/upload and the prepared downloads available without a prior selection action.
- Older completed runs remain selectable, and selecting one keeps its current meaning: it replaces the selected evidence and clears a selected final snapshot with its digest and approvals binding.
- Selection stays preparation-only evidence: no candidate, membership, attempt, approval, schedule or sending authorization changes.
- Automatic selection never publishes, approves or sends anything, and never invokes AI.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `task-data-preparation`: completing a run selects it as prepared evidence when no final snapshot exists, and the recorded selection keeps its invalidation and refusal rules.
- `campaign-task-workspaces`: the review, approve/upload controls and prepared downloads follow the completed run without a separate selection action; explicit selection remains available for history.

## Impact

- `internal/preparation` worker publication and `SelectRun` share the selection rule; `internal/web` view state stops treating an unselected completed run as review-without-approval.
- Refusals to preserve: started, scheduled, closing or closed tasks; stale or foreign runs; incomplete runs. A run that publishes after the task started sending must not alter finalized evidence.
- No new dependency, collection or migration. No change to prompt, provider, classification rules, sending sheets or exclusion reports.
- Out of scope: starting preparation automatically after validation, guessing worksheet/header on upload, merging audit columns into sending sheets, and moving backend validation after approval.
- Ordering: implement and verify this change before `add-preparation-audit-export`, whose download link assumes prepared evidence is selected without an extra click.
