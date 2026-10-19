# email-dispatch Delta

## MODIFIED Requirements

### Requirement: Explicit task submission authorization
The system SHALL show a fresh eligible count, sender, pinned configuration, approved test state and exclusions, and require an explicit start action for each task or retry batch. Enabling a future per-task schedule SHALL count as that task's explicit start authorization. Repeated delivery of the same action or schedule SHALL NOT create duplicate dispatch work.

#### Scenario: Double-click start
- **WHEN** the browser repeats an already accepted start request
- **THEN** it returns the existing action/task state rather than scheduling the candidates twice

#### Scenario: Schedule enabled once
- **WHEN** the operator enables a schedule for a validated, approved task with an acceptable future time
- **THEN** the recorded confirmation captures the fresh counts and the pinned configuration revision as that task's start authorization
- **AND** resubmitting the same schedule does not create a second start

## ADDED Requirements

### Requirement: Schedule entry rules
The system SHALL accept one per-task schedule as a date and time entered in 12-hour form, store it as a single absolute timestamp, reject any time less than 30 minutes after the current time, and show the stored time with the explicit +05:30 offset. A schedule SHALL never repeat.

#### Scenario: Time too soon
- **WHEN** the operator submits a date and time less than 30 minutes after the current time
- **THEN** both the entry form and the server reject it with that reason
- **AND** no schedule is stored

#### Scenario: Twelve-hour entry
- **WHEN** the operator enters 7:30 PM on a future date
- **THEN** the stored time is that instant, shown back with the +05:30 offset

#### Scenario: Schedule cleared
- **WHEN** the operator clears a schedule that has not fired
- **THEN** nothing is scheduled for the task
- **AND** its manual Start action remains available

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
- **WHEN** candidate sending is disabled by the production release gate
- **THEN** a scheduled fire does not transmit candidate email
- **AND** the task shows the gate reason

#### Scenario: Already started
- **WHEN** a scheduled task was already started, completed or stopped
- **THEN** its schedule does not fire again
