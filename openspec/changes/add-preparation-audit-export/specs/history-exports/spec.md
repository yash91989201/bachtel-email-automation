# Spec Delta

## ADDED Requirements

### Requirement: Preparation sanitization report
The system SHALL provide a protected XLSX sanitization report for the selected completed preparation run with one row per prepared recipient occurrence, accepted and held. Each row SHALL identify its source row, position, prepared recipient, backend outcome, every exclusion reason, its source row's AI corrections and findings, and each mapped value as an original and prepared pair.

#### Scenario: Download the sanitization report
- **WHEN** the operator downloads the sanitization report of a completed run
- **THEN** every prepared recipient occurrence appears once with its source row, position, prepared recipient and backend outcome
- **AND** held entries carry every exclusion reason and accepted entries carry none
- **AND** each mapped value appears as an original and prepared column pair with no duplicated column label

#### Scenario: Shared and recipient-specific findings
- **WHEN** two addresses share a source row whose AI finding is shared
- **THEN** both occurrences report that finding and are held
- **WHEN** a finding addresses only one occurrence
- **THEN** only that occurrence reports it and its siblings are unaffected

#### Scenario: Corrections stay distinguishable from findings
- **WHEN** the model corrected a value without leaving an unresolved issue
- **THEN** the report lists the correction separately from the findings
- **AND** the occurrence remains accepted unless another check holds it

### Requirement: Sanitization report is read-only inert evidence
Generating or downloading the sanitization report SHALL invoke no AI, rewrite no value and change no candidate, membership, attempt, approval, schedule or prepared value. Every cell SHALL be written as inert text, and the report SHALL be available only to the owning operator.

#### Scenario: Spreadsheet formula-like text
- **WHEN** an uploaded or prepared value, correction or reason begins with a formula prefix
- **THEN** it is stored as text rather than promoted to an executable formula

#### Scenario: Refused download
- **WHEN** the task has no selected completed preparation run, or the run or task belongs to another operator
- **THEN** no report content or private task metadata is returned

#### Scenario: Download changes no sending evidence
- **WHEN** the report is generated for any selected run
- **THEN** the prepared sending sheet, exclusion report and final snapshots keep their contents
- **AND** sending eligibility, approvals and authorization are unchanged
