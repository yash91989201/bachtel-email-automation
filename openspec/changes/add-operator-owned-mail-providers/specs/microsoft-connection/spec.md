## REMOVED Requirements

### Requirement: Fixed direct-sign-in mailbox
**Reason**: Each operator chooses their own directly authenticated Microsoft mailbox instead of sharing a fixed HR identity.
**Migration**: Connect and exclusively claim the verified Microsoft identity through the operator-owned connection flow; remove the fixed sender environment setting and HR address default.

### Requirement: Durable confidential credentials
**Reason**: Confidential credential storage applies to every provider.
**Migration**: Apply mail-connections' Confidential per-mailbox credentials requirement to Microsoft access and refresh tokens as well as Titan credentials.

### Requirement: Explicit connection changes
**Reason**: Connection ownership, draining and replacement are provider-independent.
**Migration**: Apply mail-connections' Safe connection changes and replacement requirements, including owner authorization and drain-before-change behavior.

## ADDED Requirements

### Requirement: Operator-selected direct Microsoft mailbox
The system SHALL let an operator connect their directly authenticated Microsoft 365 mailbox within the deployment's configured tenant. It SHALL verify the provider account identity and canonical sender before making the connection usable, without restricting all operators to a fixed HR address.

#### Scenario: Different Microsoft operators
- **WHEN** two operators authorize different Microsoft mailboxes in the configured tenant
- **THEN** each can exclusively connect their verified mailbox
- **AND** each sees only their own sender and connection status

#### Scenario: Another operator owns the mailbox
- **WHEN** Microsoft authorization identifies an already claimed account or canonical sender
- **THEN** the new claim is rejected without replacing the owner's connection

#### Scenario: Unsupported Microsoft tenant
- **WHEN** sign-in does not satisfy the configured tenant's authorization requirements
- **THEN** no usable connection is installed
- **AND** the app reports the configuration or consent requirement without bypassing tenant policy

## MODIFIED Requirements

### Requirement: Secure authorization callback
The system SHALL accept Microsoft authorization callbacks only for an operator-initiated, unexpired authorization attempt with valid state, proof-key verification and the configured redirect origin.

#### Scenario: Unsolicited or replayed callback
- **WHEN** a callback has missing, mismatched, expired or previously consumed state
- **THEN** no connection credentials are installed

#### Scenario: Callback belongs to another operator
- **WHEN** a signed-in operator receives a callback initiated by another operator
- **THEN** neither operator's connection is changed
- **AND** credential installation still requires the initiating operator's valid single-use authorization attempt


### Requirement: Refresh and reconnection
The system SHALL refresh expired access authorization when possible and expose a reconnection-required state when refresh or authorization definitively fails. Reconnecting SHALL NOT automatically resume interrupted candidate tasks.

#### Scenario: Expired access token
- **WHEN** usable refresh authorization exists and an access token expires
- **THEN** the connection obtains renewed authorization and durably saves any replacement refresh credential

#### Scenario: Revoked authorization
- **WHEN** Microsoft rejects refresh authorization
- **THEN** the app requests reconnection and blocks new submissions
- **AND** existing candidate and attempt history remains unchanged for that operator

#### Scenario: Independent refresh failures
- **WHEN** one mailbox's refresh authorization is revoked
- **THEN** only that mailbox requires reconnection and its new submissions are blocked
- **AND** another operator's valid Microsoft or Titan connection remains usable


### Requirement: Actionable registration instructions
The system SHALL provide setup instructions for Microsoft registration, HTTPS redirect URI, shared application client/tenant settings, secret provisioning and operator-driven connection testing.

#### Scenario: First server setup
- **WHEN** the administrator follows the setup instructions
- **THEN** an operator can connect their directly authenticated mailbox in the configured tenant without adding source-code credentials
- **AND** tenant-admin approval requirements, if imposed by Microsoft policy, are reported rather than bypassed
