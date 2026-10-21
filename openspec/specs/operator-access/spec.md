# operator-access Specification

## Purpose

Protect each operator's candidate information and email operations behind administrator-provisioned accounts while retaining a minimal deployment health endpoint.

## Requirements

### Requirement: Administratively provisioned operators
The system SHALL support multiple operator accounts created through a protected administrative mechanism, with unique login identities, login and logout. It SHALL NOT offer public registration, default credentials or operator-to-operator account administration.

#### Scenario: Provision another operator
- **WHEN** the administrator provisions a new operator with a previously unused login identity
- **THEN** the new operator can authenticate independently with an empty private workspace
- **AND** existing operators' credentials and records remain unchanged

#### Scenario: Duplicate provisioning
- **WHEN** the administrator provisions an already used normalized login identity
- **THEN** provisioning is refused without changing that account's password or creating a duplicate

#### Scenario: Anonymous provisioning
- **WHEN** an anonymous visitor or ordinary operator attempts administrative account creation
- **THEN** no operator account is created

#### Scenario: Invalid login
- **WHEN** a visitor submits invalid credentials
- **THEN** access is denied without disclosing whether a particular account exists
- **AND** repeated login attempts are rate limited

### Requirement: Private application access
The system SHALL require operator authentication for campaign, candidate, task, mailbox connection, history, upload and download access, including direct file URLs and background progress endpoints.

#### Scenario: Anonymous direct access
- **WHEN** an unauthenticated visitor requests a candidate page, uploaded workbook or export URL
- **THEN** the private content is not returned

#### Scenario: Authorized operator
- **WHEN** the authenticated operator accesses an application feature
- **THEN** the feature can access only that operator's private records and files under an owner-specific authorization boundary

#### Scenario: Cross-operator record or direct file access
- **WHEN** an authenticated operator uses another operator's campaign, candidate, task, connection, attempt, upload, progress or export identifier
- **THEN** no private content is returned and no mutation, export generation or sending action occurs
- **AND** responses do not disclose whether the other operator's record exists

#### Scenario: Forged ownership or linked record
- **WHEN** an operator submits another operator's owner identifier or links a new record to their campaign or revision
- **THEN** the app rejects the request before recording data or queuing work
- **AND** ownership for new records is established from the authenticated operator

#### Scenario: Independent workspaces
- **WHEN** two operators create campaigns and upload the same recipient addresses
- **THEN** each sees only their own campaigns, candidates, history, counts and downloads
- **AND** one operator's work does not alter the other's candidate eligibility

#### Scenario: Provider settings belong to the operator
- **WHEN** one operator requests or changes another operator's saved provider setup
- **THEN** no settings, identifiers or secrets are disclosed and no update occurs
- **AND** an owner can manage only their own connection settings without permission to administer other operators

### Requirement: Protected mutations
The system SHALL authorize every state-changing action and protect browser-cookie-authenticated mutations against cross-site request forgery.

#### Scenario: Forged send or deletion action
- **WHEN** a cross-origin request lacks valid anti-forgery proof
- **THEN** the action is rejected before any upload, send, connection change or deletion occurs

#### Scenario: Expired operator session
- **WHEN** an expired session requests a mutation
- **THEN** the mutation is rejected and the UI requests sign-in

### Requirement: Secure operator sessions
The system SHALL use server-validated sessions and production cookies restricted to secure HTTP transport, inaccessible to page scripts and limited in cross-site use. Logout SHALL revoke or clear browser access without cancelling already running background tasks.

#### Scenario: Production login
- **WHEN** the operator logs in through the production HTTPS origin
- **THEN** the session cookie is Secure and HttpOnly with an explicit SameSite policy
- **AND** sensitive session credentials are not stored in browser local storage

#### Scenario: Logout during a task
- **WHEN** the operator logs out while a task is running
- **THEN** private views require login again
- **AND** the server task continues independently of the browser session

### Requirement: Minimal public health information
The system SHALL allow an unauthenticated health check without exposing candidate data, mailbox credentials, internal database information or private application state.

#### Scenario: Deployment health probe
- **WHEN** the deployment platform requests the health endpoint
- **THEN** it receives only the information needed to determine application health
