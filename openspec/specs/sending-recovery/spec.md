# sending-recovery Specification

## Purpose

Preserve a durable account of every submission attempt and provide safe interruption, retry and uncertainty-resolution actions without automatic duplicate sends.

## Requirements

### Requirement: Durable intent before submission
The system SHALL durably save the attempt identity, exact recipient, subject/body, owning operator, mailbox connection and configuration/task references and submission-start intent before transmitting a candidate or test request. Every definite outcome SHALL be saved immediately.

#### Scenario: Persistence fails before transmission
- **WHEN** the attempt intent cannot be durably recorded
- **THEN** no submission request is launched and the task pauses with the storage error

#### Scenario: Acceptance cannot be saved
- **WHEN** a provider accepts a request but durable outcome recording fails
- **THEN** further launches stop
- **AND** recovery treats any unconfirmed stored intent conservatively as REVIEW_REQUIRED

### Requirement: Stop drains in-flight work
The system SHALL stop launching new requests immediately after a manual stop, let all already launched requests settle within their bounded deadlines, persist their outcomes and leave unstarted work resumable.

#### Scenario: Four requests active
- **WHEN** the operator clicks Stop while four requests are in flight
- **THEN** no fifth request starts
- **AND** the task reaches its stopped state after the active requests are recorded or classified as uncertain

### Requirement: Automatic progress recovery with manual resume
The system SHALL recover interrupted tasks on startup, retain confirmed outcomes, identify all unfinished submission intents as REVIEW_REQUIRED, and leave safe unstarted work paused until explicit resume.

#### Scenario: Crash after transmission
- **WHEN** the server restarts with an unfinished submission intent
- **THEN** that candidate is held for review and is not automatically resubmitted

#### Scenario: Safe remaining candidates
- **WHEN** an interrupted task also has candidates without submission intents
- **THEN** the UI offers Resume with their eligible count
- **AND** unresolved uncertain candidates do not block the safe remainder

### Requirement: Definite and ambiguous outcomes
The system SHALL distinguish definite rejection from possible acceptance. Timeouts, disconnects or server failures after possible submission SHALL be REVIEW_REQUIRED unless non-submission can be established. No automatic retry SHALL occur for an ambiguous outcome.

#### Scenario: Connection ends without response
- **WHEN** request transmission might have completed but no definite response is available
- **THEN** the attempt is REVIEW_REQUIRED rather than a retryable definite failure

#### Scenario: Explicit recipient rejection
- **WHEN** the provider definitively rejects one candidate request before acceptance
- **THEN** that candidate records a failure with a safe error description
- **AND** unrelated eligible candidates can continue

### Requirement: Task-wide service pauses
The system SHALL pause the affected mailbox for authentication/permission failures or persistent provider problems, preserving results and presenting an actionable reason. Inability to durably save sending state SHALL stop launches for every mailbox whose outcome recording is affected.

#### Scenario: Authorization fails during concurrency
- **WHEN** one active request establishes an authentication failure
- **THEN** no further requests are launched for that mailbox
- **AND** already active requests are independently recorded before the task waits for reconnection and explicit resume

#### Scenario: One mailbox loses authorization
- **WHEN** one operator's Microsoft authorization or Titan credentials fail
- **THEN** other operators' valid mailbox connections can continue sending
- **AND** reconnecting the failed mailbox does not automatically resume its tasks

#### Scenario: Deployment-wide storage failure
- **WHEN** a database or storage failure prevents reliable outcome persistence across mailboxes
- **THEN** new launches stop across the affected application
- **AND** existing intents are recovered conservatively rather than automatically retried

### Requirement: Individual and bulk definite-failure retry
The system SHALL provide Retry for an eligible failed candidate, Retry selected for a confirmed selection of eligible failed candidates and Retry all failed, each with a refreshed count and explicit confirmation. All three SHALL create new attempts using the existing immutable task content and exclude submitted, invalid, duplicate, active and unresolved uncertain candidates.

#### Scenario: Mixed task outcomes
- **WHEN** the operator chooses Retry all failed
- **THEN** only eligible definite sending failures are queued under the owning mailbox's limits
- **AND** a candidate requiring data correction is directed to a corrected upload

#### Scenario: Candidate submitted through another action
- **WHEN** eligibility changes between retry preview and confirmation
- **THEN** the final atomic recheck excludes that candidate and refreshes the count

#### Scenario: Retry a confirmed selection
- **WHEN** the operator confirms Retry selected
- **THEN** only the selected eligible definite failures are queued under the owning mailbox's limits
- **AND** the confirmed count is the selected row count, and unselected failures stay unchanged

### Requirement: Explicit uncertain resend
The system SHALL offer Send again for each unresolved uncertain candidate and Send again selected for a confirmed selection of them. Both SHALL show the previous attempt details and possible duplicate-send consequence, require explicit confirmation and retain each earlier attempt.

#### Scenario: Confirm uncertain resend
- **WHEN** the operator confirms Send again
- **THEN** a new attempt uses the original personalized content and the owning mailbox's normal dispatch controls
- **AND** the review action is recorded without erasing the uncertain attempt

#### Scenario: Confirm a selected resend
- **WHEN** the operator confirms Send again selected
- **THEN** each selected candidate gets its own new attempt with its original personalized content and the owning mailbox's normal dispatch controls
- **AND** the duplicate-risk acknowledgement covers the confirmed selection and every earlier uncertain attempt stays recorded

### Requirement: Audited manual resolution
The system SHALL allow the operator to mark an uncertain candidate, or a confirmed selection of uncertain candidates, as already submitted after external verification, recording the actor, time and manual basis per candidate without inventing a provider-confirmed acceptance time.

#### Scenario: Verify in Outlook
- **WHEN** the operator marks a reviewed candidate already submitted
- **THEN** campaign eligibility excludes that candidate
- **AND** history distinguishes the manual resolution from a provider-confirmed response

#### Scenario: Resolve a confirmed selection
- **WHEN** the operator confirms Mark selected already submitted with one external-verification basis
- **THEN** every selected candidate records its own actor, resolution time and basis
- **AND** a candidate that is no longer uncertain refuses the whole request instead of resolving partially

### Requirement: Durable pacing and single dispatcher ownership
The system SHALL prevent overlapping dispatcher ownership of the persistent application data and preserve separate per-mailbox pacing/cooldown state across restarts. Recovery SHALL NOT release an uncertain candidate for automatic sending.

#### Scenario: Second app process
- **WHEN** another process attempts to dispatch against the same data directory
- **THEN** it cannot launch email requests while the first dispatcher owns that store

#### Scenario: Restart during cooldown
- **WHEN** the server restarts before a recorded cooldown expires
- **THEN** resumed work for the affected mailbox still observes its remaining cooldown

#### Scenario: Independent cooldowns after restart
- **WHEN** one mailbox has a recorded cooldown and another has none
- **THEN** recovery does not transfer the first mailbox's cooldown or credentials to the other
- **AND** work still requires the applicable explicit resume and approval

### Requirement: Missed schedule pauses for the operator
The system SHALL persist an unfired schedule across restarts. When a stored start time passed while no dispatcher owned the persistent data, it SHALL NOT start sending: it SHALL pause the task with an operator-visible reason and keep the schedule recorded as missed until the operator resumes the safe remainder or clears it.

#### Scenario: Downtime over the scheduled time
- **WHEN** the server starts after an unfired scheduled time passed
- **THEN** the task is paused with a missed-schedule reason
- **AND** no submission attempt is created for it

#### Scenario: Operator resumes after a missed schedule
- **WHEN** the operator reviews the fresh eligible count and confirms Resume
- **THEN** the safe remainder starts through the normal resume path
- **AND** the schedule does not fire again

#### Scenario: Restart before the scheduled time
- **WHEN** the server restarts before an unfired scheduled time
- **THEN** the same stored time still fires later
- **AND** the task is not paused for the restart alone

### Requirement: Result generation is independent of sending
The system SHALL make completed, stopped and recovered-interrupted task results available for artifact generation. Failure or repetition of workbook generation SHALL NOT change sending eligibility or create email attempts.

#### Scenario: Workbook generation fails
- **WHEN** a task's result workbook cannot be written
- **THEN** durable sending history remains available and the UI offers a download-generation retry
- **AND** that retry makes no email submission

### Requirement: Submission is durable before Sent-copy saving
The system SHALL durably record definite provider acceptance before beginning any separate application-managed Sent-copy operation. Such copy work SHALL finish within a bounded deadline and SHALL NOT delay recording the confirmed submission. If submission recording fails, existing storage-failure protections SHALL apply and no separate copy work SHALL begin. Native provider saving SHALL remain part of that provider's sending service.

#### Scenario: Ordering of accepted submission and copy
- **WHEN** Titan confirms SMTP acceptance
- **THEN** the app saves the attempt as SUBMITTED with its acceptance timestamp before attempting the Sent copy

#### Scenario: Cannot record acceptance
- **WHEN** Titan accepts the email but the app cannot durably record that result
- **THEN** further affected launches stop under the existing storage-failure policy
- **AND** no Sent copy is attempted and unfinished stored intent remains subject to conservative review on recovery

### Requirement: Sent-copy failures do not change sending outcomes
The system SHALL retain SUBMITTED and its acceptance timestamp if a Sent copy fails or its completion is unknown. It SHALL record a credential-free copy-saving warning in existing attempt details without making the candidate eligible again, changing test approval, or pausing sending solely for the copy failure. It SHALL NOT automatically retry a copy operation or submit another email to repair a missing copy.

#### Scenario: Copy saving is refused or unavailable
- **WHEN** authentication, TLS, mailbox discovery, quota, timeout or append failure prevents saving a Sent copy
- **THEN** the accepted email remains SUBMITTED with unchanged sending counts and eligibility
- **AND** its attempt details explain that sending was accepted but the Sent copy could not be confirmed
- **AND** other eligible sending can continue and no resend or copy-retry work is created

#### Scenario: Sent folder cannot be identified safely
- **WHEN** the app cannot identify the sending mailbox's existing Sent folder unambiguously
- **THEN** it records a copy-saving warning without saving to another folder or creating a new folder
- **AND** the confirmed sending outcome is retained

#### Scenario: Lost copy confirmation
- **WHEN** Titan may have saved the copy but the app does not receive its completion response
- **THEN** the app does not attempt another append or email submission
- **AND** the warning states that copy saving could not be confirmed without claiming that the copy is definitely absent

#### Scenario: Copy cleanup fails after confirmation
- **WHEN** Titan confirms the copy was saved and only session cleanup fails
- **THEN** that cleanup failure does not invalidate the confirmed copy or sending outcome
- **AND** no duplicate operation is scheduled

#### Scenario: Copy warning cannot be saved
- **WHEN** the copy operation fails and storage also prevents recording its warning
- **THEN** the already saved SUBMITTED state and acceptance timestamp are preserved
- **AND** the warning persistence failure is reported through safe server diagnostics and existing storage-failure protections apply
- **AND** the email is not made eligible for retry

### Requirement: Interrupted copy saving does not replay mail
The system SHALL retain a durably recorded submission after interruption before or during Sent-copy handling. Startup recovery SHALL NOT downgrade that result, resend its message or replay a separate copy operation. Application-managed saving is a single post-submission attempt and SHALL NOT promise recovery of missing copies after a process failure. Native provider saving SHALL NOT be replayed by the app.

#### Scenario: Crash after saved acceptance
- **WHEN** the server stops after recording SUBMITTED but before it confirms a Sent copy
- **THEN** recovery retains SUBMITTED and its acceptance timestamp
- **AND** no email or copy operation is replayed for that attempt even if the Sent copy is missing

#### Scenario: Stop drains copy work
- **WHEN** stop, shutdown, disconnect or replacement occurs while copy saving is active
- **THEN** new submissions follow the existing stop rules and launched copy work settles within its bounded deadline
- **AND** all previously saved submission results remain intact regardless of copy completion
