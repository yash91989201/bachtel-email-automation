# Spec Delta

## ADDED Requirements

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
