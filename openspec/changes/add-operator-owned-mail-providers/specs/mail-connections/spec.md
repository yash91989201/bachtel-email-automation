## Purpose

Manage exclusive operator-owned sending mailboxes across providers, with confidential credentials and explicit connection replacement that preserves campaign templates while resetting sending data.

## ADDED Requirements

### Requirement: One exclusive mailbox per operator
The system SHALL permit each operator to own at most one current sending mailbox, initially through Microsoft 365 or GoDaddy Titan. A mailbox SHALL belong to at most one operator, including while disconnected or requiring reconnection, until explicitly released or replaced.

#### Scenario: Independent provider choices
- **WHEN** one operator connects Microsoft 365 and another connects GoDaddy Titan
- **THEN** each sees and uses only their own connection
- **AND** connecting either account does not change the other operator's account

#### Scenario: Concurrent duplicate claims
- **WHEN** two operators concurrently attempt to claim the same mailbox
- **THEN** at most one claim succeeds
- **AND** the refused claim does not disclose the other owner's identity or replace their credentials

#### Scenario: Duplicate address or account identity
- **WHEN** a proposed connection matches an existing canonical sender address or verified provider account identity, including through another connection method
- **THEN** it cannot be assigned to a different operator
- **AND** sender aliases and shared-mailbox delegation are not accepted as alternate claims

### Requirement: Verified connection before use
The system SHALL validate mailbox control and establish its canonical sender before making a connection usable. The initial providers SHALL support only the directly authenticated mailbox as sender. Failed validation SHALL leave an existing connection and its application data unchanged.

#### Scenario: Invalid replacement credentials
- **WHEN** proposed replacement credentials cannot be validated or identify an already claimed mailbox
- **THEN** the current connection and retained data are unchanged
- **AND** no test or candidate email is sent as part of validation

### Requirement: Titan credential connection
The system SHALL let an operator connect GoDaddy Titan using their full mailbox address and email password over certificate-validated encrypted SMTP. It SHALL expose actionable connection errors without revealing credentials and SHALL NOT claim that SMTP validation confirms message delivery.

#### Scenario: Successful Titan authentication
- **WHEN** Titan accepts the supplied mailbox credentials over the supported encrypted connection
- **THEN** the operator can save that mailbox as their sending connection
- **AND** validation sends no message and does not approve campaign formatting

#### Scenario: Authentication or TLS failure
- **WHEN** Titan rejects authentication or its server certificate cannot be verified
- **THEN** the connection is not usable for sending
- **AND** the app does not downgrade to plaintext or disable certificate verification

### Requirement: Confidential per-mailbox credentials
The system SHALL encrypt provider credentials at rest under a deployment-managed secret, restrict their use to the owning connection, and exclude secret values from logs, downloads, HTML and application APIs. A missing or incorrect key SHALL block affected connections without silently overwriting ciphertext.

#### Scenario: Restart with original secret
- **WHEN** the server restarts with persistent data and the original encryption key
- **THEN** valid mailbox credentials remain usable without a reconnect solely because of restart

#### Scenario: Secret unavailable
- **WHEN** saved credentials cannot be decrypted
- **THEN** the affected operator sees an actionable connection error and cannot send
- **AND** stored ciphertext remains unchanged and is never returned to the browser

### Requirement: Reconnect without implicit replacement
The system SHALL distinguish reconnecting the same verified mailbox from replacing it with a different mailbox or provider. Reconnection SHALL preserve campaigns and sending history, SHALL NOT automatically resume work, and SHALL NOT install a different identity without the replacement flow.

#### Scenario: Same mailbox reconnects
- **WHEN** the operator renews authorization for the same mailbox
- **THEN** its data and still-applicable formatting approvals remain intact
- **AND** paused work requires explicit resume

#### Scenario: Different mailbox during reconnect
- **WHEN** reconnect validates a different mailbox identity or provider
- **THEN** it does not silently replace the current mailbox
- **AND** the UI directs the operator to the explicit replacement flow

### Requirement: Safe connection changes
The system SHALL authorize connection changes only for their owner and stop new launches for that connection before changing or removing usable credentials. Active submissions SHALL settle or be classified within bounded deadlines. A disconnected mailbox SHALL remain exclusively claimed and its work paused until reconnect or replacement.

#### Scenario: Disconnect while requests are active
- **WHEN** an operator confirms disconnect during dispatch
- **THEN** no further submissions start for that mailbox and existing requests settle before credentials are removed
- **AND** other operators' usable connections continue normally

#### Scenario: Competing start and connection change
- **WHEN** a start, scheduled fire or retry competes with a connection change
- **THEN** work cannot launch using credentials being replaced or removed

### Requirement: Replacement keeps templates and resets sending data
The system SHALL require explicit confirmation before replacing a mailbox. It SHALL preserve the operator's campaigns and template revisions but delete their candidates, tasks, uploads, approvals, attempts, resolutions, history and generated exports. It SHALL release the old claim, require fresh test approval, and preserve other operators' data.

#### Scenario: Review replacement consequences
- **WHEN** the operator reviews a validated replacement
- **THEN** the UI identifies the old and proposed mailbox and the exact retained and deleted data
- **AND** it explains that duplicate-prevention history is removed, so uploading those recipients again can allow new sends
- **AND** cancelling leaves the current connection and data unchanged

#### Scenario: Confirm replacement
- **WHEN** the operator confirms replacement and active submissions have settled
- **THEN** the selected reset completes and the new exclusive mailbox becomes current
- **AND** campaigns, source-header suggestions, result-column settings, original template DOCX files and immutable revisions remain
- **AND** candidate sending requires a new upload, a successful test and explicit received-format approval for the new connection
- **AND** no previous sender history is retained or synthesized from the new mailbox

#### Scenario: Pending schedules and export generation
- **WHEN** replacement starts its confirmed reset
- **THEN** old scheduled work cannot fire and export generation cannot republish deleted artifacts
- **AND** pending old-account send actions are refused

#### Scenario: Reset interrupted
- **WHEN** reset or artifact deletion fails or the server stops before replacement completes
- **THEN** the affected workspace remains blocked from sending and publishing old artifacts until reset is completed
- **AND** recovery never reports a completed replacement while stale sending data remains accessible

### Requirement: Provider readiness is independent
The system SHALL identify supported providers and their connection requirements. Missing Microsoft application configuration SHALL disable Microsoft connection readiness without preventing a correctly configured Titan operator from sending. Unsupported provider selections SHALL be refused before credentials or sending state change.

#### Scenario: Titan without Microsoft registration
- **WHEN** Titan is usable and Microsoft application configuration is absent
- **THEN** Titan operations remain available and the UI explains Microsoft's missing setup separately

#### Scenario: Unsupported provider
- **WHEN** an operator submits an unknown provider choice
- **THEN** no connection is installed and no sending occurs
