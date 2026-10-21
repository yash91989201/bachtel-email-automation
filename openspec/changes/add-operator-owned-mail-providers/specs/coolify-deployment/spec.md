## MODIFIED Requirements

### Requirement: Single-instance persistent service
The system SHALL support deployment as one application instance serving multiple private operator workspaces with its database, uploads, configuration history, attempts and generated downloads in persistent storage that survives container replacement.

#### Scenario: Coolify redeployment
- **WHEN** the application container is replaced while its configured data volume is retained
- **THEN** operator access, connection ciphertext, campaigns, uploads and sending history remain available
- **AND** interrupted tasks use recovery rather than automatic submission

### Requirement: Production origin and secrets
The system SHALL document deployment-wide origin, trusted proxy, derived callback URI and credential-encryption key settings separately from operator-managed provider setup. Microsoft tenant/application settings and Titan mailbox credentials SHALL be entered and persisted per operator through the protected UI, not required as shared deployment environment credentials. Secrets SHALL remain outside source control and image layers.

#### Scenario: Missing production secret
- **WHEN** a required secret is absent or invalid
- **THEN** startup or connection readiness reports the missing setting without displaying its value
- **AND** dispatch remains disabled for connections that require that missing setting

#### Scenario: Distinct application and mailbox credentials
- **WHEN** an administrator configures the service
- **THEN** the deployment origin and encryption key are supplied outside source control
- **AND** operators enter their own Microsoft tenant/application settings or Titan credentials through the protected UI
- **AND** setup shows the callback URI for each Microsoft operator to register and identifies Titan's supported encrypted SMTP endpoint
- **AND** no shared Microsoft tenant, Client ID or Client Secret environment value is required


### Requirement: Recoverable backups
The system SHALL document and verify backup/restore of all operator ownership, mailbox claims, saved provider application settings, encrypted credentials, campaign data and stored files as a consistent set, with the credential-decryption secret protected separately. Backup restoration SHALL preserve submitted/uncertain history and start dispatch paused.

#### Scenario: Restore backup
- **WHEN** a tested backup and matching secret are restored to a replacement instance
- **THEN** historical files and outcomes are available
- **AND** the operator reviews recovered tasks before resuming

#### Scenario: Restore an older backup
- **WHEN** the backup might omit submissions made after its snapshot
- **THEN** restored candidate sending is blocked pending operator reconciliation of that gap
- **AND** incomplete backup history is not treated as proof that an email was never submitted

#### Scenario: Restore multiple workspaces
- **WHEN** a backup containing independently configured Microsoft registrations and Titan connections is restored with the matching encryption key
- **THEN** each operator retains their own saved provider settings, private records and exclusive mailbox claim
- **AND** restoration does not reassign credentials or automatically send work


### Requirement: Release verification
The system SHALL provide repeatable simulated validation for the 1,000-row workflow, operator isolation and both initial providers' failure paths, plus a documented operator-driven real test-email check. Candidate sending SHALL require a successfully submitted test and explicit received-formatting approval for the exact configuration, but SHALL NOT require a global production release-verification unlock.

#### Scenario: Production readiness check
- **WHEN** the administrator follows the release checklist
- **THEN** it verifies authentication, mappings, formatting, duplicate exclusion, rate/concurrency bounds, interruption recovery, exports and redeploy persistence
- **AND** real candidate emails are not sent by automated tests

#### Scenario: Approved task without release unlock
- **WHEN** the operator confirms a candidate batch with approved test formatting for its exact configuration
- **THEN** missing global release-verification status does not block the batch
- **AND** existing authorization, eligibility and restore-review protections still apply


#### Scenario: Independent setup verification
- **WHEN** the simulated release checks configure two Microsoft operators in different tenants and a Titan operator without shared Microsoft environment credentials
- **THEN** each completes only their own setup, authorization, refresh and approved sending workflow
- **AND** a failed or expired registration for one operator does not disable the other connections
