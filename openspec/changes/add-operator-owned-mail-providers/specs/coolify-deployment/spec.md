## MODIFIED Requirements

### Requirement: Single-instance persistent service
The system SHALL support deployment as one application instance serving multiple private operator workspaces with its database, uploads, configuration history, attempts and generated downloads in persistent storage that survives container replacement.

#### Scenario: Coolify redeployment
- **WHEN** the application container is replaced while its configured data volume is retained
- **THEN** operator access, connection ciphertext, campaigns, uploads and sending history remain available
- **AND** interrupted tasks use recovery rather than automatic submission

### Requirement: Production origin and secrets
The system SHALL document the HTTPS application origin, trusted proxy configuration, Microsoft redirect URI and shared application credentials, Titan connection requirements and the provider-credential encryption secret required for Coolify deployment. Secrets SHALL be supplied outside source control and image layers.

#### Scenario: Missing production secret
- **WHEN** a required secret is absent or invalid
- **THEN** startup or connection readiness reports the missing setting without displaying its value
- **AND** dispatch remains disabled for connections that require that missing setting

#### Scenario: Distinct application and mailbox credentials
- **WHEN** an administrator configures the service
- **THEN** shared Microsoft application credentials and the encryption key are supplied outside source control
- **AND** operators connect their individual mailboxes through the protected UI rather than server environment credentials per mailbox
- **AND** setup identifies the configured Microsoft tenant boundary and Titan's supported encrypted SMTP endpoint


### Requirement: Recoverable backups
The system SHALL document and verify backup/restore of all operator ownership, mailbox claims, encrypted credentials, campaign data and stored files as a consistent set, with the credential-decryption secret protected separately. Backup restoration SHALL preserve submitted/uncertain history and start dispatch paused.

#### Scenario: Restore backup
- **WHEN** a tested backup and matching secret are restored to a replacement instance
- **THEN** historical files and outcomes are available
- **AND** the operator reviews recovered tasks before resuming

#### Scenario: Restore an older backup
- **WHEN** the backup might omit submissions made after its snapshot
- **THEN** restored candidate sending is blocked pending operator reconciliation of that gap
- **AND** incomplete backup history is not treated as proof that an email was never submitted

#### Scenario: Restore multiple workspaces
- **WHEN** a backup containing Microsoft and Titan connections is restored with the matching encryption key
- **THEN** each operator retains only their own records and exclusive mailbox claim
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
