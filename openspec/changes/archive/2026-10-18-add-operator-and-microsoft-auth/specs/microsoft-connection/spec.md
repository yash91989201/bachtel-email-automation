# Spec Delta

## Purpose

Maintain secure delegated Microsoft authorization for the fixed HR sender independently of browser sessions and campaign activity.

## ADDED Requirements

### Requirement: Fixed direct-sign-in mailbox
The system SHALL connect Microsoft by direct delegated sign-in as hr@bachtelcorporation.com and verify the connected mailbox identity before enabling email operations.

#### Scenario: Correct mailbox connected
- **WHEN** the operator completes Microsoft authorization for the configured HR mailbox
- **THEN** the UI displays the connected identity and fixed sender address

#### Scenario: Different mailbox connected
- **WHEN** authorization completes for a mailbox whose verified address does not match the fixed sender
- **THEN** the connection is rejected for sending
- **AND** no existing valid connection is silently replaced

### Requirement: Secure authorization callback
The system SHALL accept Microsoft authorization callbacks only for an operator-initiated, unexpired authorization attempt with valid state, proof-key verification and the configured redirect origin.

#### Scenario: Unsolicited or replayed callback
- **WHEN** a callback has missing, mismatched, expired or previously consumed state
- **THEN** no connection credentials are installed

### Requirement: Minimum delegated permissions
The system SHALL request delegated sending and refresh authorization, plus the minimum identity access needed to verify the mailbox, without requesting application-wide sending or mailbox-reading access.

#### Scenario: Consent setup
- **WHEN** the administrator registers and connects the Microsoft application
- **THEN** setup instructions identify delegated Mail.Send, offline_access and identity-verification scopes
- **AND** shared-mailbox delegation and Google permissions are not required

### Requirement: Durable confidential credentials
The system SHALL store refresh credentials encrypted at rest under a deployment-managed secret, restrict access to stored credentials and exclude credential values from logs, exports, HTML and application APIs.

#### Scenario: Restart with valid secret
- **WHEN** the server restarts with its persistent data and original encryption secret
- **THEN** the connection can be restored without requiring another interactive sign-in solely because of the restart

#### Scenario: Missing or incorrect encryption secret
- **WHEN** the server cannot decrypt stored credentials
- **THEN** email operations remain disabled with an actionable administrative error
- **AND** the server does not overwrite the stored ciphertext or expose its contents

### Requirement: Refresh and reconnection
The system SHALL refresh expired access authorization when possible and expose a reconnection-required state when refresh or authorization definitively fails. Reconnecting SHALL NOT automatically resume interrupted candidate tasks.

#### Scenario: Expired access token
- **WHEN** usable refresh authorization exists and an access token expires
- **THEN** the connection obtains renewed authorization and durably saves any replacement refresh credential

#### Scenario: Revoked authorization
- **WHEN** Microsoft rejects refresh authorization
- **THEN** the app requests reconnection and blocks new submissions
- **AND** existing candidate and attempt history remains unchanged

### Requirement: Explicit connection changes
The system SHALL restrict connect, reconnect and disconnect actions to the operator and prevent credential replacement while submission requests are in flight.

#### Scenario: Disconnect during dispatch
- **WHEN** the operator requests disconnect while requests are in flight
- **THEN** dispatch stops launching requests and settles or classifies the active requests before removing the connection

### Requirement: Actionable registration instructions
The system SHALL provide setup instructions for Microsoft registration, HTTPS redirect URI, client/tenant settings, secret provisioning and operator-driven connection testing.

#### Scenario: First server setup
- **WHEN** the administrator follows the setup instructions
- **THEN** the fixed mailbox can be connected without adding source-code credentials
- **AND** tenant-admin approval requirements, if imposed by Microsoft policy, are reported rather than bypassed
