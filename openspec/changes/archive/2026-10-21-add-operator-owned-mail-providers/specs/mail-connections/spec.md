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
- **WHEN** a proposed connection matches an existing canonical sender address or verified provider account identity, including through another connection method or a different application registration
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
The system SHALL encrypt provider credentials at rest under a deployment-managed secret, including application client secrets and authorization tokens, restrict their use to the owning connection, and exclude secret values from logs, downloads, HTML and application APIs. A missing or incorrect key SHALL block affected connections without silently overwriting ciphertext.

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
The system SHALL report provider setup and authorization readiness per operator and connection. Missing or invalid settings for one operator SHALL NOT disable another operator's usable connection, including connections using the same provider. Unsupported provider selections SHALL be refused before credentials or sending state change.

#### Scenario: Titan without Microsoft registration
- **WHEN** an operator has a usable Titan connection and no Microsoft registration has been configured on the deployment
- **THEN** Titan operations remain available without Microsoft environment settings

#### Scenario: Unsupported provider
- **WHEN** an operator submits an unknown provider choice
- **THEN** no connection is installed and no sending occurs


#### Scenario: Incomplete setup for one Microsoft operator
- **WHEN** one operator has missing or invalid Microsoft application settings and another has a usable Microsoft connection
- **THEN** only the incomplete connection is reported as not ready
- **AND** the other operator's provider settings, tokens and sending readiness are unchanged

### Requirement: Operator-managed provider setup
The system SHALL let each operator enter, validate and save the settings required by their selected supported provider through the protected connection UI. Setup SHALL be owner-scoped and SHALL NOT require deployment-wide mailbox or provider application credentials. The same setup and privacy contract SHALL apply when another provider becomes supported.

#### Scenario: Microsoft setup belongs to its operator
- **WHEN** an operator selects Microsoft 365
- **THEN** they can supply their Tenant ID, Client ID and Client Secret before starting Microsoft authorization
- **AND** the saved settings are private to their connection and secret values are encrypted
- **AND** another operator can use a different tenant and application registration without changing deployment settings

#### Scenario: Titan setup belongs to its operator
- **WHEN** an operator selects GoDaddy Titan
- **THEN** they can save their mailbox address and password through the protected UI
- **AND** another operator cannot read, change or use those credentials

#### Scenario: Reopen setup after restart
- **WHEN** an operator returns after the server restarts with persistent data and the encryption key
- **THEN** their saved provider settings remain associated with their connection
- **AND** secret values are never returned to pre-fill the browser

#### Scenario: Add an operator using an existing provider
- **WHEN** an administrator provisions a new operator
- **THEN** that operator can set up any supported provider independently without changing server provider credentials or another operator's settings

#### Scenario: Another provider becomes supported
- **WHEN** a later release adds a provider
- **THEN** operators can complete that provider's required setup under the same ownership, encrypted-secret, exclusive-sender, approval and recovery rules
- **AND** existing operators retain their configured connections and private campaign workspaces

### Requirement: Application settings update without mailbox reset
The system SHALL allow the owner to correct or rotate provider application settings for the same verified mailbox without deleting campaign or sending data. New settings SHALL become active only after validation for that mailbox. Updating settings SHALL NOT transfer sender ownership, silently replace the mailbox or automatically resume paused work.

#### Scenario: Rotate Microsoft application credentials
- **WHEN** an operator validates replacement application credentials through sign-in to their same verified mailbox
- **THEN** that connection uses the new saved application settings for subsequent authorization and refresh
- **AND** campaigns, candidates, tasks, history and applicable formatting approvals are preserved
- **AND** in-flight requests settle before active credentials change

#### Scenario: Invalid settings update
- **WHEN** changed application settings fail validation
- **THEN** the existing saved settings, credentials, mailbox claim and application data remain unchanged
- **AND** no candidate email is transmitted during validation

#### Scenario: Changed settings identify a different mailbox
- **WHEN** sign-in under proposed application settings verifies a different mailbox
- **THEN** the application requires the explicit mailbox-replacement flow before changing the connection
- **AND** submitting application settings alone cannot trigger the destructive reset


### Requirement: Provider-first mailbox setup
The system SHALL require a provider choice before showing setup fields for an unconfigured mailbox. Only the selected provider's credentials and next action SHALL be shown. Visible provider names SHALL be Microsoft 365 and Titan. The user SHALL be able to change their selection without changing a saved connection or sending mail.

#### Scenario: Start mailbox setup
- **WHEN** an operator with no connected mailbox opens mailbox settings without selecting a provider
- **THEN** the page offers Microsoft 365 and Titan as distinct choices
- **AND** neither provider's credential form or premature sign-in action is displayed

#### Scenario: Select Microsoft 365
- **WHEN** the operator chooses Microsoft 365
- **THEN** the page shows only Microsoft setup, its callback guidance and a clear save-then-sign-in sequence
- **AND** Titan credential fields are absent

#### Scenario: Select Titan
- **WHEN** the operator chooses Titan
- **THEN** the page shows only the mailbox address, email password and Titan connection action
- **AND** Microsoft registration fields and sign-in actions are absent

#### Scenario: Save settings or correct an error
- **WHEN** settings are saved or selected-provider validation fails
- **THEN** the page retains that provider selection and shows the relevant next step or error
- **AND** secrets are not echoed back

#### Scenario: Existing mailbox management
- **WHEN** an operator manages an existing mailbox
- **THEN** its identity and current state remain visible with relevant management controls
- **AND** choosing another provider does not bypass replacement confirmation or an unfinished-reset block
