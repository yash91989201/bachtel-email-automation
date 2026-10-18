# operator-access Specification

## Purpose

Protect candidate information and email operations behind one operator account while retaining a minimal deployment health endpoint.

## Requirements

### Requirement: Single protected operator
The system SHALL support one operator account, controlled initial provisioning, login and logout, without public registration or default credentials.

#### Scenario: Initial provisioning
- **WHEN** an unprovisioned installation is configured by its administrator
- **THEN** one operator can be provisioned through a protected administrative mechanism
- **AND** anonymous visitors cannot register or provision another operator

#### Scenario: Invalid login
- **WHEN** a visitor submits invalid credentials
- **THEN** access is denied without disclosing whether a particular account exists
- **AND** repeated login attempts are rate limited

### Requirement: Private application access
The system SHALL require operator authentication for campaign, candidate, task, Microsoft connection, history, upload and download access, including direct file URLs and background progress endpoints.

#### Scenario: Anonymous direct access
- **WHEN** an unauthenticated visitor requests a candidate page, uploaded workbook or export URL
- **THEN** the private content is not returned

#### Scenario: Authorized operator
- **WHEN** the authenticated operator accesses an application feature
- **THEN** the feature can access only the application's private records under the same authorization boundary

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
