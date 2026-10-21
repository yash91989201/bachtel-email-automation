# microsoft-connection Specification

## Purpose

Maintain secure delegated Microsoft authorization for each operator's own verified mailbox independently of browser sessions and campaign activity.

## Requirements

### Requirement: Operator-selected direct Microsoft mailbox
The system SHALL let an operator connect their directly authenticated Microsoft 365 mailbox using that operator's saved tenant and application registration settings. It SHALL verify the provider account identity and canonical sender before making the connection usable, without restricting all operators to a fixed HR address.

#### Scenario: Different Microsoft operators
- **WHEN** two operators authorize different Microsoft mailboxes using their own saved tenant and application settings
- **THEN** each can exclusively connect their verified mailbox
- **AND** each sees only their own sender and connection status

#### Scenario: Another operator owns the mailbox
- **WHEN** Microsoft authorization identifies an already claimed account or canonical sender
- **THEN** the new claim is rejected without replacing the owner's connection

#### Scenario: Unsupported Microsoft tenant
- **WHEN** sign-in does not satisfy that operator's selected tenant/application authorization requirements
- **THEN** no usable connection is installed
- **AND** the app reports the owner's configuration or consent requirement without bypassing tenant policy or affecting other operators

### Requirement: Saved Microsoft application settings
The system SHALL accept each operator's Tenant ID, Client ID and Client Secret through their protected connection UI, persist them with owner-scoped access, and encrypt the secret at rest. Microsoft connection and refresh SHALL use these saved settings without requiring or falling back to shared Microsoft tenant/client environment settings.

#### Scenario: Two independently configured tenants
- **WHEN** two operators supply separate tenant IDs, application IDs and secrets and complete valid sign-in
- **THEN** each connection and subsequent refresh use only its owner's saved registration settings
- **AND** neither operator needs to change the deployment's environment or restrict the other to their tenant

#### Scenario: Same mailbox through different application IDs
- **WHEN** another operator supplies a different application registration that authenticates an already claimed mailbox
- **THEN** the duplicate sender claim is rejected
- **AND** the original owner's connection and credentials are unchanged

#### Scenario: Missing fields or expired application secret
- **WHEN** an operator omits required application settings or Microsoft rejects their client secret
- **THEN** the UI identifies the corrective setup or reconnection action for that operator without exposing secret values
- **AND** other operators' configured Microsoft and Titan connections remain usable

#### Scenario: Settings change during authorization
- **WHEN** saved or proposed settings change after an authorization attempt starts
- **THEN** its callback cannot install tokens under the changed registration
- **AND** the operator starts a fresh attempt bound to the intended settings

### Requirement: Secure authorization callback
The system SHALL accept Microsoft authorization callbacks only for an operator-initiated, unexpired authorization attempt with valid state, proof-key verification and the deployment-derived redirect origin. The callback SHALL use the initiating operator's validated application settings and SHALL NOT install credentials from an attempt whose settings changed.

#### Scenario: Unsolicited or replayed callback
- **WHEN** a callback has missing, mismatched, expired or previously consumed state
- **THEN** no connection credentials are installed

#### Scenario: Callback belongs to another operator
- **WHEN** a signed-in operator receives a callback initiated by another operator
- **THEN** neither operator's connection is changed
- **AND** credential installation still requires the initiating operator's valid single-use authorization attempt

### Requirement: Minimum delegated permissions
The system SHALL request delegated sending and refresh authorization, plus the minimum identity access needed to verify the mailbox, without requesting application-wide sending or mailbox-reading access.

#### Scenario: Consent setup
- **WHEN** the operator configures their Microsoft application registration and connects their mailbox
- **THEN** setup instructions identify delegated Mail.Send, offline_access and identity-verification scopes
- **AND** shared-mailbox delegation and Google permissions are not required

### Requirement: Refresh and reconnection
The system SHALL refresh expired access authorization using the connection owner's saved tenant, application settings and refresh token when possible and expose a reconnection-required state when refresh or authorization definitively fails. Reconnecting SHALL NOT automatically resume interrupted candidate tasks.

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
The system SHALL provide operator-specific instructions for Microsoft registration, delegated consent, Tenant ID, Client ID, Client Secret and their entry in the protected UI. It SHALL display the deployment-derived HTTPS callback URI that must be registered and explain secret expiry and rotation.

#### Scenario: First server setup
- **WHEN** the operator follows the setup instructions
- **THEN** the operator can save their own tenant/application settings and connect their directly authenticated mailbox without server environment credentials
- **AND** tenant-admin approval requirements, if imposed by Microsoft policy, are reported rather than bypassed

#### Scenario: Registered callback differs
- **WHEN** the operator's application registration does not include the displayed callback URI
- **THEN** setup reports that it must match exactly
- **AND** the app does not redirect credentials or authorization responses to an operator-supplied destination
