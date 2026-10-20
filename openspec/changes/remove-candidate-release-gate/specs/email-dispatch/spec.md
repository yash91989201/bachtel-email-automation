## MODIFIED Requirements

### Requirement: Configuration-specific test approval
The system SHALL require a successfully submitted test email and the operator's explicit confirmation of received formatting for the exact configuration revision before candidate sending. A submission response alone SHALL NOT grant approval.

#### Scenario: First candidate batch
- **WHEN** a validated task has no approved test for its configuration
- **THEN** candidate start is disabled until a test is submitted to the designated test address and the operator approves its received formatting

#### Scenario: Configuration revised
- **WHEN** the subject, DOCX, mapping, fallback or normalization changes
- **THEN** the new revision requires a new test and approval
- **AND** an already running task keeps its original approved snapshot

#### Scenario: Candidate actions without release verification
- **WHEN** the operator requests a candidate start, resume, retry or resend without recorded production release verification
- **THEN** no global release gate blocks the action
- **AND** the action still requires exact-configuration test approval and the existing explicit confirmations

#### Scenario: Candidate action without approval
- **WHEN** the operator confirms a candidate start, resume, retry or resend without approval for the exact configuration
- **THEN** the action is refused without reserving candidates or creating candidate attempts

### Requirement: Single-shot scheduled start
The system SHALL start an authorized scheduled task once, when its stored time is reached with a running server, using the pinned configuration revision and its approved test, independently of browser connections and operator sessions. It SHALL start only the candidates free at that moment and SHALL NOT fire again after the task has started, completed or stopped.

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
- **WHEN** the pinned configuration revision or the approved test no longer matches at the scheduled time
- **THEN** the task does not start sending
- **AND** it requires revalidation and approval before any candidate sending

#### Scenario: Production release gate closed
- **WHEN** an explicitly authorized schedule becomes due with matching configuration and test approval
- **THEN** the task starts without requiring recorded production release verification
- **AND** all other scheduling, eligibility and dispatch protections apply

#### Scenario: Already started
- **WHEN** a scheduled task was already started, completed or stopped
- **THEN** its schedule does not fire again
