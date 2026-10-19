# email-dispatch Specification

## Purpose

Run approved personalized email tasks on the server with bounded concurrency, shared mailbox pacing and accurate operator-visible progress.

## Requirements

### Requirement: Configuration-specific test approval
The system SHALL require a successfully submitted test email and the operator's explicit confirmation of received formatting for the exact configuration revision before candidate sending. A submission response alone SHALL NOT grant approval.

#### Scenario: First candidate batch
- **WHEN** a validated task has no approved test for its configuration
- **THEN** candidate start is disabled until a test is submitted to the designated test address and the operator approves its received formatting

#### Scenario: Configuration revised
- **WHEN** the subject, DOCX, mapping, fallback or normalization changes
- **THEN** the new revision requires a new test and approval
- **AND** an already running task keeps its original approved snapshot

### Requirement: Test sends at any time
The system SHALL let the operator explicitly send a test during setup, normal use and task interruption when authorization is usable. Tests SHALL use the selected sample content with a test-recipient override and SHALL NOT change candidate status or resume a task.

#### Scenario: Test after service interruption
- **WHEN** authorization is usable and the operator requests a test for a paused task
- **THEN** the test uses the shared sending controls and its own attempt record
- **AND** the task remains paused until explicitly resumed

#### Scenario: Expired connection
- **WHEN** authorization cannot be refreshed
- **THEN** the UI requests reconnection before accepting a test submission

### Requirement: Explicit task submission authorization
The system SHALL show a fresh eligible count, sender, pinned configuration and exclusions, and require an explicit start action for each task or retry batch. Repeated delivery of the same action SHALL NOT create duplicate dispatch work.

#### Scenario: Double-click start
- **WHEN** the browser repeats an already accepted start request
- **THEN** it returns the existing action/task state rather than scheduling the candidates twice

### Requirement: Background execution
The system SHALL execute accepted tasks independently of browser connections and operator sessions while the server is running.

#### Scenario: Browser closes
- **WHEN** the operator closes the task tab or logs out
- **THEN** queued candidate work continues without a browser heartbeat
- **AND** reopening the authenticated task page shows current durable progress

### Requirement: Shared bounded concurrency
The system SHALL allow at most four simultaneous submission requests across all tasks, tests and retries for the mailbox. Multiple tasks SHALL share this capacity without bypassing campaign candidate reservations.

#### Scenario: Several tasks and a test
- **WHEN** multiple tasks are ready and the operator requests a test
- **THEN** total in-flight requests never exceed four
- **AND** the test obtains a normal paced opportunity without bypassing the limit

### Requirement: Configurable bounded pacing
The system SHALL default to 25 submission starts per minute and accept only configured integer rates from 1 through 30. Pacing SHALL be shared across tasks, tests, automatic retries and manual actions, evenly spaced without restart or batch bursts.

#### Scenario: Invalid setting
- **WHEN** the operator submits a rate above 30, below 1 or a non-integer
- **THEN** both UI validation and server validation reject it

#### Scenario: Concurrent dispatch
- **WHEN** four request slots are available at a configured rate of 30 per minute
- **THEN** new starts remain paced rather than launching four simultaneously
- **AND** no rolling 60-second interval exceeds the configured application allowance

### Requirement: Provider throttling
The system SHALL stop launching affected mailbox requests for Microsoft's Retry-After interval when throttled. Without that header it SHALL use bounded exponential backoff. Throttling SHALL be visible and SHALL NOT consume or clear candidate submission eligibility.

#### Scenario: One worker receives throttling
- **WHEN** Microsoft returns 429 with Retry-After
- **THEN** the shared mailbox dispatcher observes that cooldown before new requests
- **AND** concurrent workers do not independently continue launching submissions

### Requirement: Submission semantics
The system SHALL send individually addressed HTML emails from hr@bachtelcorporation.com, without attachments, and record SUBMITTED only when Microsoft returns a definite acceptance. It SHALL explain that acceptance does not confirm delivery.

#### Scenario: Accepted request
- **WHEN** Graph returns 202 Accepted
- **THEN** the attempt is recorded as SUBMITTED with its timestamp
- **AND** the UI does not label it delivered

### Requirement: Batched live task progress
The system SHALL refresh progress in grouped updates every few seconds, including submitted, remaining, invalid, duplicate, failed, held and in-flight counts, recent outcomes and pause/wait reasons. Reconnection SHALL reconstruct progress from durable state.

#### Scenario: Results finish out of row order
- **WHEN** concurrent requests complete in a different order from the source rows
- **THEN** counts and candidate associations remain accurate
- **AND** downloadable results can retain original row order

### Requirement: Explicit India timestamps
The system SHALL expose submission and attempt timestamps with an explicit India offset of +05:30 in operator-visible history and reports.

#### Scenario: Timestamp rendered
- **WHEN** a submission time is shown or exported
- **THEN** it contains the date, time and +05:30 offset rather than an ambiguous local time
