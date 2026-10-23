# Email Dispatch Delta

## MODIFIED Requirements

### Requirement: Configuration-specific test approval
The system SHALL require a successfully submitted test email and the operator's explicit confirmation of received formatting for the exact task final sending snapshot, configuration revision and current mailbox connection generation before candidate sending. Dataset approval alone SHALL NOT grant formatting approval. A submission response alone SHALL NOT grant approval.

#### Scenario: First candidate batch
- **WHEN** a validated task has no approved test for its final snapshot and configuration
- **THEN** candidate start is disabled until a test is submitted to the designated test address and the operator approves its received formatting

#### Scenario: Configuration revised
- **WHEN** the subject, DOCX, mapping, fallback or normalization changes
- **THEN** the final snapshot under the new revision requires its own new test and approval
- **AND** an already running task keeps its original approved snapshot

#### Scenario: Candidate actions without release verification
- **WHEN** the operator requests a candidate start, resume, retry or resend without recorded production release verification
- **THEN** no global release gate blocks the action
- **AND** the action still requires exact-configuration test approval and the existing explicit confirmations

#### Scenario: Candidate action without approval
- **WHEN** the operator confirms a candidate start, resume, retry or resend without approval for the exact final snapshot, configuration and mailbox generation
- **THEN** the action is refused without reserving candidates or creating candidate attempts

#### Scenario: Mailbox replacement invalidates approval
- **WHEN** the operator replaces their mailbox or provider while retaining campaign revisions
- **THEN** no prior formatting approval authorizes candidate sending
- **AND** each final snapshot requires a newly submitted test and explicit received-format approval through the replacement connection

#### Scenario: Another operator's approval
- **WHEN** an operator attempts to use another operator's test or approval identifier
- **THEN** no approval is accepted and no candidate work is reserved or submitted

#### Scenario: Same-mailbox application settings rotation
- **WHEN** the owner validates new provider application credentials for the same verified mailbox
- **THEN** applicable exact-configuration formatting approvals remain valid for the unchanged mailbox
- **AND** changing application settings does not create another sender claim or automatically start or resume sending

#### Scenario: Final dataset approved as-is
- **WHEN** the operator accepts a prepared sheet without reuploading
- **THEN** that creates a final snapshot but does not satisfy its test or received-format approval
- **AND** test submission remains an explicit operator action to a designated test address

#### Scenario: Replacing final data invalidates approval
- **WHEN** a new final upload is accepted under the same campaign revision
- **THEN** its new final snapshot requires a fresh submitted test and received-format approval
- **AND** a test from an earlier snapshot is refused even if both files contain identical values

#### Scenario: Snapshot changed while a test was queued
- **WHEN** a test for a previous final snapshot is queued, submitted or awaiting approval after replacement
- **THEN** it remains evidence for its original snapshot and cannot approve the new one

#### Scenario: Cross-task approval reuse
- **WHEN** an operator attempts to reuse another task's submitted test or approval for matching content
- **THEN** the action is refused because approval belongs to the exact task final snapshot


### Requirement: Test sends at any time
The system SHALL let the operator explicitly send a test during setup, normal use and task interruption when authorization is usable and the task is not closing or closed. Tests SHALL use the selected sample content with a test-recipient override and SHALL NOT change candidate status or resume a task.

#### Scenario: Closed task test is refused
- **WHEN** a task is closing or closed and a new or stale test request targets it
- **THEN** no test is queued or submitted and no formatting approval is created
- **AND** existing test history remains inspectable

#### Scenario: Test after service interruption
- **WHEN** authorization is usable and the operator requests a test for a paused task
- **THEN** the test uses the owning mailbox's sending controls and its own attempt record
- **AND** the task remains paused until explicitly resumed

#### Scenario: Expired connection
- **WHEN** authorization cannot be refreshed
- **THEN** the UI requests reconnection before accepting a test submission

#### Scenario: Test used for final approval
- **WHEN** the operator submits a test to approve a finalized task
- **THEN** its sample content comes from an accepted row in that exact final snapshot with only the destination overridden
- **AND** arbitrary sample tests, pre-finalization diagnostics and tests for other snapshots cannot grant that final snapshot's approval


### Requirement: Explicit task submission authorization
The system SHALL show a fresh eligible count, sender, pinned configuration and final snapshot, approved test state and exclusions, and require an explicit start action for each task or retry batch. Enabling a future per-task schedule SHALL count as that task's explicit start authorization. Repeated delivery of the same action or schedule SHALL NOT create duplicate dispatch work.

#### Scenario: Double-click start
- **WHEN** the browser repeats an already accepted start request
- **THEN** it returns the existing action/task state rather than scheduling the candidates twice

#### Scenario: Schedule enabled once
- **WHEN** the operator enables a schedule for a validated, approved task with an acceptable future time
- **THEN** the recorded confirmation captures the owning operator, current mailbox connection, fresh counts and pinned configuration revision and final snapshot as that task's start authorization
- **AND** resubmitting the same schedule does not create a second start

#### Scenario: Data review is not dispatch consent
- **WHEN** the operator approves prepared data as-is or uploads an edited final sheet
- **THEN** no candidate is reserved, test submitted, schedule created or email sent
- **AND** start and schedule requests before finalization or current snapshot approval are refused


### Requirement: Single-shot scheduled start
The system SHALL start an authorized scheduled task once, when its stored time is reached with a running server, using the pinned configuration revision, owning mailbox connection and its final snapshot and matching approved test, independently of browser connections and operator sessions. It SHALL start only the candidates free at that moment and SHALL NOT fire again after the task has started, completed or stopped.

#### Scenario: Scheduled time reached
- **WHEN** the stored time is reached while the server runs and the task is still scheduled
- **THEN** candidate sending begins without a further operator confirmation
- **AND** each attempt is recorded durably before its request is transmitted

#### Scenario: Candidates taken by another task
- **WHEN** part of the task's candidates were reserved, queued or submitted by another task before the time
- **THEN** those candidates are excluded as already scheduled
- **AND** the remaining free candidates still start

#### Scenario: No candidate is free
- **WHEN** every candidate is excluded at the scheduled time
- **THEN** the task does not start sending
- **AND** the UI explains the exclusions

#### Scenario: Configuration or approval no longer matches
- **WHEN** the pinned configuration revision, final snapshot or the approved test no longer matches at the scheduled time
- **THEN** the task does not start sending
- **AND** it requires revalidation and approval before any candidate sending

#### Scenario: Production release gate closed
- **WHEN** an explicitly authorized schedule becomes due with matching configuration and test approval
- **THEN** the task starts without requiring recorded production release verification
- **AND** all other scheduling, eligibility and dispatch protections apply

#### Scenario: Already started
- **WHEN** a scheduled task was already started, completed or stopped
- **THEN** its schedule does not fire again

#### Scenario: Connection changed after authorization
- **WHEN** a scheduled task references a connection that was disconnected, replaced or reset
- **THEN** it does not send using a newly connected account or a different operator's account
- **AND** fresh validation, test approval and operator authorization are required before candidate sending
