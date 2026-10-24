# Spec Delta

## ADDED Requirements

### Requirement: Completed preparation run becomes selected evidence
Publishing a completed preparation run SHALL select it as the task's prepared evidence when the task has no selected final snapshot, in the transaction that completes the run. Automatic selection SHALL change no candidate, membership, attempt, approval or schedule state and SHALL invoke no AI.

#### Scenario: Automatic selection after completion
- **WHEN** a preparation run completes for an unstarted task with no selected final snapshot
- **THEN** that run becomes the task's selected prepared evidence
- **AND** the backend review, approve/upload controls and prepared downloads refer to it without a further selection action

#### Scenario: Finalized dataset keeps its evidence
- **WHEN** a preparation run completes while a final snapshot is selected
- **THEN** the selected prepared evidence and the final snapshot with its digest stay unchanged
- **AND** selecting a run explicitly still replaces the selected evidence and clears the final snapshot with its digest

#### Scenario: Unusable completion leaves selection unchanged
- **WHEN** a run fails, is incomplete or cancelled, or the task is started, scheduled, closing or closed while it runs
- **THEN** no selection changes
- **AND** existing preparation evidence, final snapshots and lifecycle refusals remain

#### Scenario: Older run remains selectable
- **WHEN** the operator explicitly selects an older completed run
- **THEN** that run becomes the selected prepared evidence under the existing invalidation rules
- **AND** no candidate, membership, attempt, approval or schedule is created or changed by the selection itself
