# email-dispatch Specification

## Purpose

Run approved personalized email tasks on the server with per-mailbox concurrency and pacing and accurate operator-visible progress.

## Requirements

### Requirement: Configuration-specific test approval
The system SHALL require a successfully submitted test email and the operator's explicit confirmation of received formatting for the exact configuration revision and current mailbox connection before candidate sending. A submission response alone SHALL NOT grant approval.

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

#### Scenario: Mailbox replacement invalidates approval
- **WHEN** the operator replaces their mailbox or provider while retaining campaign revisions
- **THEN** no prior formatting approval authorizes candidate sending
- **AND** each reused revision requires a newly submitted test and explicit received-format approval through the replacement connection

#### Scenario: Another operator's approval
- **WHEN** an operator attempts to use another operator's test or approval identifier
- **THEN** no approval is accepted and no candidate work is reserved or submitted

#### Scenario: Same-mailbox application settings rotation
- **WHEN** the owner validates new provider application credentials for the same verified mailbox
- **THEN** applicable exact-configuration formatting approvals remain valid for the unchanged mailbox
- **AND** changing application settings does not create another sender claim or automatically start or resume sending

### Requirement: Test sends at any time
The system SHALL let the operator explicitly send a test during setup, normal use and task interruption when authorization is usable. Tests SHALL use the selected sample content with a test-recipient override and SHALL NOT change candidate status or resume a task.

#### Scenario: Test after service interruption
- **WHEN** authorization is usable and the operator requests a test for a paused task
- **THEN** the test uses the owning mailbox's sending controls and its own attempt record
- **AND** the task remains paused until explicitly resumed

#### Scenario: Expired connection
- **WHEN** authorization cannot be refreshed
- **THEN** the UI requests reconnection before accepting a test submission

### Requirement: Explicit task submission authorization
The system SHALL show a fresh eligible count, sender, pinned configuration, approved test state and exclusions, and require an explicit start action for each task or retry batch. Enabling a future per-task schedule SHALL count as that task's explicit start authorization. Repeated delivery of the same action or schedule SHALL NOT create duplicate dispatch work.

#### Scenario: Double-click start
- **WHEN** the browser repeats an already accepted start request
- **THEN** it returns the existing action/task state rather than scheduling the candidates twice

#### Scenario: Schedule enabled once
- **WHEN** the operator enables a schedule for a validated, approved task with an acceptable future time
- **THEN** the recorded confirmation captures the owning operator, current mailbox connection, fresh counts and pinned configuration revision as that task's start authorization
- **AND** resubmitting the same schedule does not create a second start

### Requirement: Background execution
The system SHALL execute accepted tasks independently of browser connections and operator sessions while the server is running.

#### Scenario: Browser closes
- **WHEN** the operator closes the task tab or logs out
- **THEN** queued candidate work continues without a browser heartbeat
- **AND** reopening the authenticated task page shows current durable progress

### Requirement: Shared bounded concurrency
The system SHALL allow at most four simultaneous submission requests across all tasks, tests and retries for each mailbox connection. A mailbox already at that limit SHALL NOT delay launches for another mailbox. Multiple tasks SHALL share their mailbox's capacity fairly without bypassing campaign candidate reservations.

#### Scenario: Several tasks and a test
- **WHEN** multiple tasks are ready and the operator requests a test
- **THEN** that mailbox's in-flight requests never exceed four
- **AND** the test obtains a normal paced opportunity without bypassing the limit

#### Scenario: Several independent mailboxes
- **WHEN** multiple operators have ready tasks
- **THEN** each mailbox uses its own four submission slots regardless of the other mailboxes' in-flight work
- **AND** one mailbox's cooldown or saturated slots do not block another mailbox's paced opportunity
- **AND** the dispatcher does not indefinitely starve a ready mailbox

### Requirement: Configurable bounded pacing
The system SHALL default to 25 submission starts per minute and accept only configured integer rates from 1 through 30. Pacing SHALL be independent per mailbox and shared across its tasks, tests, automatic retries and manual actions, evenly spaced without restart or batch bursts.

#### Scenario: Invalid setting
- **WHEN** the operator submits a rate above 30, below 1 or a non-integer
- **THEN** both UI validation and server validation reject it

#### Scenario: Concurrent dispatch
- **WHEN** four request slots are available at a configured rate of 30 per minute
- **THEN** new starts remain paced rather than launching four simultaneously
- **AND** no rolling 60-second interval exceeds that mailbox's configured allowance

#### Scenario: Different mailbox rates
- **WHEN** two operators choose different valid rates
- **THEN** each mailbox observes its own rolling allowance and even spacing
- **AND** tests, retries and schedules cannot bypass that mailbox's controls

### Requirement: Provider throttling
The system SHALL stop affected mailbox launches for a provider's indicated retry interval or bounded exponential backoff when no interval is supplied. Throttling SHALL be visible to the owner, SHALL NOT consume candidate eligibility, and SHALL NOT pause other usable mailboxes. Automatic retry SHALL require evidence that the provider did not accept the submission.

#### Scenario: One worker receives throttling
- **WHEN** Microsoft returns 429 with Retry-After
- **THEN** all work for that mailbox observes the interval before new requests
- **AND** other usable mailboxes retain normal paced opportunities

#### Scenario: Titan temporarily rejects a submission
- **WHEN** Titan returns a definite temporary rejection without a retry interval
- **THEN** the mailbox observes bounded backoff and the existing retry limit
- **AND** a missing response after possible acceptance is instead held for review

### Requirement: Submission semantics
The system SHALL send individually addressed HTML messages without attachments from the owning operator's current verified mailbox. It SHALL record SUBMITTED only after definite provider acceptance and explain that acceptance does not confirm delivery. Provider diagnostics SHALL NOT expose credentials or private server responses to operators.

#### Scenario: Accepted request
- **WHEN** Graph returns 202 Accepted
- **THEN** the attempt is recorded as SUBMITTED with its timestamp
- **AND** the UI does not label it delivered

#### Scenario: Accepted Titan submission
- **WHEN** Titan confirms acceptance after the complete SMTP message data
- **THEN** the attempt is recorded as SUBMITTED with its timestamp
- **AND** a later connection-cleanup failure does not undo that acceptance

#### Scenario: Uncertain SMTP completion
- **WHEN** the complete message might have reached Titan but its final acceptance response is unavailable
- **THEN** the attempt is held for review and is not automatically retried

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
The system SHALL start an authorized scheduled task once, when its stored time is reached with a running server, using the pinned configuration revision, owning mailbox connection and its approved test, independently of browser connections and operator sessions. It SHALL start only the candidates free at that moment and SHALL NOT fire again after the task has started, completed or stopped.

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

#### Scenario: Connection changed after authorization
- **WHEN** a scheduled task references a connection that was disconnected, replaced or reset
- **THEN** it does not send using a newly connected account or a different operator's account
- **AND** fresh validation, test approval and operator authorization are required before candidate sending

### Requirement: Sent-copy convention for every provider
Every supported sending provider SHALL arrange Sent-folder saving for candidate and test emails, either through its native sending operation or a separate post-submission save. Its existing setup and sending configuration SHALL incorporate this requirement. Adding a provider SHALL require documenting and verifying its Sent-copy behavior; silently omitting it SHALL NOT be a supported mode.

#### Scenario: Native provider-managed saving
- **WHEN** a provider saves Sent copies as part of its sending service
- **THEN** the app keeps that native behavior enabled and does not create a second copy
- **AND** provider acceptance does not claim that asynchronous copy saving or recipient delivery has completed

#### Scenario: Application-managed saving
- **WHEN** a provider requires a separate Sent-copy operation
- **THEN** the shared dispatch flow invokes that provider's save operation only after definite acceptance is durably recorded
- **AND** it uses the same copy-failure and interruption rules as other application-managed providers

#### Scenario: New provider is introduced
- **WHEN** an adapter for another sending provider is added
- **THEN** its setup and sending configuration must satisfy the same Sent-copy convention and verify whether saving is native or separate
- **AND** existing authorization, ownership and duplicate-prevention rules continue to apply

### Requirement: Microsoft 365 native Sent Items saving
The system SHALL keep native Sent Items saving enabled for Microsoft 365 candidate and test submissions. It SHALL rely on the sending service's own copy operation, require no additional mailbox-reading permission, and SHALL NOT create an additional copy or resend because a Sent item is not immediately visible. Acceptance SHALL continue to mean accepted for processing, not confirmed delivery or completed copy saving.

#### Scenario: Microsoft candidate or test submission
- **WHEN** an authorized Microsoft 365 candidate or test send is submitted
- **THEN** its request retains native Sent Items saving for the authenticated sending mailbox
- **AND** the provider's post-submission handling performs no additional email send or manual copy operation

#### Scenario: Microsoft processing is asynchronous
- **WHEN** Microsoft accepts the request before its Sent item is visible
- **THEN** the attempt remains SUBMITTED without polling the mailbox, adding permissions or issuing a duplicate request
- **AND** the app does not claim to have confirmed the item's presence or delivery

### Requirement: Automatic Titan Sent copies
The system SHALL automatically attempt to save one copy of each future, definitely accepted Titan candidate or test email in the sending mailbox's existing Sent folder after durably recording its submission. The copy SHALL preserve the submitted message content and headers, appear as read and use the acceptance time as its mailbox timestamp. A Sent copy SHALL NOT be presented as proof of recipient delivery.

#### Scenario: Accepted candidate message
- **WHEN** Titan definitely accepts a candidate email and its submission result is saved
- **THEN** the app automatically attempts to save the submitted message in that mailbox's Sent folder
- **AND** after a successful save the operator can inspect the recipient, subject and personalized HTML in Titan webmail without an additional action
- **AND** in the uninterrupted completion path exactly one copy-saving attempt is made for that accepted submission

#### Scenario: Accepted test message
- **WHEN** Titan definitely accepts an explicitly requested test and its submission result is saved
- **THEN** its Sent copy contains the actual test recipient and sample content submitted
- **AND** saving the copy does not approve formatting, change candidate state or resume a task

#### Scenario: Rejected or uncertain submission
- **WHEN** a Titan submission is rejected before acceptance or its final acceptance is uncertain
- **THEN** the app does not create a Sent copy for that attempt
- **AND** the existing failure, safe-retry or review-required behavior applies

### Requirement: Sent copies stay within the sending connection
The system SHALL save a Sent copy only in the mailbox bound to its submission, using that connection's confidential authorization. Separate copy-saving operations SHALL use certificate-validated encryption and SHALL NOT fall back to another operator's mailbox or a replacement connection. Connection validation SHALL NOT create Sent copies.

#### Scenario: Independent Titan operators
- **WHEN** two operators send through separate Titan connections
- **THEN** each copy is saved only in the mailbox that submitted its message
- **AND** neither operator's credentials or message content are used for the other mailbox

#### Scenario: Connection changes during copy saving
- **WHEN** disconnect or replacement is requested while an accepted message's copy operation is active
- **THEN** that operation settles or ends within its deadline before the old connection is removed
- **AND** the copy is never redirected to the replacement mailbox

#### Scenario: Validate connection without sending
- **WHEN** an operator validates or reconnects a Titan mailbox without submitting an email
- **THEN** no email or Sent copy is created

#### Scenario: Microsoft submission
- **WHEN** a message is sent through Microsoft 365
- **THEN** the common provider contract retains Microsoft native Sent Items saving without a Titan copy-saving operation

### Requirement: Forward-only Sent-folder saving
The system SHALL apply the provider Sent-copy contract only to submissions performed by the updated sending path. It SHALL leave earlier attempts and the first run untouched and SHALL NOT introduce a historical backfill, copy-retry control, settings toggle or additional operator action.

#### Scenario: Deploy after the first sending run
- **WHEN** the updated app starts with existing submission history
- **THEN** it does not append copies of earlier messages, resend them or change their stored outcomes
- **AND** future accepted sends use automatic Sent-folder saving without a new button or setup step

#### Scenario: Existing authorization still applies
- **WHEN** an operator has not approved received formatting for the exact configuration or explicitly authorized candidate sending
- **THEN** automatic Sent-folder saving does not authorize a test, candidate start, retry or resend
