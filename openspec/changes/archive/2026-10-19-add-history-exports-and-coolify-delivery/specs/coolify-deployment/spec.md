# Spec Delta

## Purpose

Operate the single-operator application on Coolify with durable private storage, controlled shutdown and verifiable recovery and backup procedures.

## ADDED Requirements

### Requirement: Single-instance persistent service
The system SHALL support deployment as one application instance with its database, uploads, configuration history, attempts and generated downloads in persistent storage that survives container replacement.

#### Scenario: Coolify redeployment
- **WHEN** the application container is replaced while its configured data volume is retained
- **THEN** operator access, connection ciphertext, campaigns, uploads and sending history remain available
- **AND** interrupted tasks use recovery rather than automatic submission

### Requirement: Production origin and secrets
The system SHALL document the HTTPS application origin, trusted proxy configuration, Microsoft redirect URI, application credentials and token-encryption secret required for Coolify deployment. Secrets SHALL be supplied outside source control and image layers.

#### Scenario: Missing production secret
- **WHEN** a required secret is absent or invalid
- **THEN** startup or connection readiness reports the missing setting without displaying its value
- **AND** email dispatch remains disabled

### Requirement: Runtime document dependencies
The system SHALL package the established DOCX converter and required runtime assets in the production image, with pinned/reproducible dependency selection and a bounded conversion health check.

#### Scenario: Fresh runtime image
- **WHEN** a fresh image is deployed without development Node, Word or LibreOffice installed on the server
- **THEN** the documented DOCX-to-email conversion works using the packaged converter

### Requirement: Graceful platform shutdown
The system SHALL respond to deployment shutdown by stopping launches, allowing bounded in-flight request completion and persisting outcomes before exit where possible. Forced termination SHALL remain recoverable through the uncertain-attempt rules.

#### Scenario: Platform sends termination
- **WHEN** Coolify stops the container during a task
- **THEN** the app drains active requests within the configured shutdown allowance
- **AND** unstarted candidates are not automatically sent by the replacement process

### Requirement: Health and protected administration
The system SHALL expose a minimal health endpoint suitable for Coolify, run without root privileges and document restricted administrative access while keeping candidate pages and files behind operator authentication.

#### Scenario: Public health check
- **WHEN** Coolify probes application health
- **THEN** it receives a health response without private task or mailbox details

### Requirement: Recoverable backups
The system SHALL document and verify backup/restore of the database and stored files as a consistent set, with the credential-decryption secret protected separately. Backup restoration SHALL preserve submitted/uncertain history and start dispatch paused.

#### Scenario: Restore backup
- **WHEN** a tested backup and matching secret are restored to a replacement instance
- **THEN** historical files and outcomes are available
- **AND** the operator reviews recovered tasks before resuming

#### Scenario: Restore an older backup
- **WHEN** the backup might omit submissions made after its snapshot
- **THEN** restored candidate sending is blocked pending operator reconciliation of that gap
- **AND** incomplete backup history is not treated as proof that an email was never submitted

### Requirement: Release verification
The system SHALL provide repeatable simulated validation for the 1,000-row workflow and failure paths, plus a documented operator-driven real test-email check before any production candidate batch.

#### Scenario: Production readiness check
- **WHEN** the administrator follows the release checklist
- **THEN** it verifies authentication, mappings, formatting, duplicate exclusion, rate/concurrency bounds, interruption recovery, exports and redeploy persistence
- **AND** real candidate emails are not sent by automated tests

### Requirement: Deletion and backup boundaries
The system SHALL explain that deleting live application records does not rewrite already existing backups and SHALL document manual backup cleanup separately.

#### Scenario: Campaign removed from live app
- **WHEN** an operator deletes a campaign
- **THEN** the app does not falsely claim its data has also been removed from independently retained backups
