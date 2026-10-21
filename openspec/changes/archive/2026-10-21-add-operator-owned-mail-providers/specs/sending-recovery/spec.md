## MODIFIED Requirements

### Requirement: Durable intent before submission
The system SHALL durably save the attempt identity, exact recipient, subject/body, owning operator, mailbox connection and configuration/task references and submission-start intent before transmitting a candidate or test request. Every definite outcome SHALL be saved immediately.

#### Scenario: Persistence fails before transmission
- **WHEN** the attempt intent cannot be durably recorded
- **THEN** no submission request is launched and the task pauses with the storage error

#### Scenario: Acceptance cannot be saved
- **WHEN** a provider accepts a request but durable outcome recording fails
- **THEN** further launches stop
- **AND** recovery treats any unconfirmed stored intent conservatively as REVIEW_REQUIRED

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


### Requirement: Audited manual resolution
The system SHALL allow the operator to mark an uncertain candidate as already submitted after external verification, recording the actor, time and manual basis without inventing a provider-confirmed acceptance time.

#### Scenario: Verify in Outlook
- **WHEN** the operator marks a reviewed candidate already submitted
- **THEN** campaign eligibility excludes that candidate
- **AND** history distinguishes the manual resolution from a provider-confirmed response

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

