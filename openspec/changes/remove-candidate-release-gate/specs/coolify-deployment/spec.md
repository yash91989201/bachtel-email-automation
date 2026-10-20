## MODIFIED Requirements

### Requirement: Release verification
The system SHALL provide repeatable simulated validation for the 1,000-row workflow and failure paths, plus a documented operator-driven real test-email check. Candidate sending SHALL require a successfully submitted test and explicit received-formatting approval for the exact configuration, but SHALL NOT require a global production release-verification unlock.

#### Scenario: Production readiness check
- **WHEN** the administrator follows the release checklist
- **THEN** it verifies authentication, mappings, formatting, duplicate exclusion, rate/concurrency bounds, interruption recovery, exports and redeploy persistence
- **AND** real candidate emails are not sent by automated tests

#### Scenario: Approved task without release unlock
- **WHEN** the operator confirms a candidate batch with approved test formatting for its exact configuration
- **THEN** missing global release-verification status does not block the batch
- **AND** existing authorization, eligibility and restore-review protections still apply
