# Spec Delta

## Purpose

Provide durable, protected task and campaign downloads that preserve original data and historical email content without changing submission state.

## ADDED Requirements

### Requirement: Full task results workbook
The system SHALL generate and store an XLSX task-results workbook after completion, manual stop or interrupted-task recovery, preserving all original input rows, columns and values in their original order and adding the task's configured result columns.

#### Scenario: Mixed results including excluded duplicates
- **WHEN** a task has submitted, failed, invalid, duplicate, already-scheduled and review-required rows
- **THEN** every original row appears with the appropriate status, timestamp where known and clear error/exclusion reason
- **AND** duplicate data is read from file artifacts without creating candidate records

#### Scenario: Existing result columns
- **WHEN** the source workbook already contains columns with the desired output labels
- **THEN** original columns remain intact and new results use the collision-free labels shown before start

### Requirement: Campaign reporting
The system SHALL offer a campaign XLSX report with candidate summary and separate attempt history, identifying task, configuration revision, normalized recipient, outcome and timestamp without mixing test submissions into candidate totals.

#### Scenario: Candidate has several attempts
- **WHEN** a candidate has a definite failure, an uncertain attempt and a later accepted submission
- **THEN** the summary shows its current campaign state
- **AND** the history sheet retains each distinct attempt and any manual resolution

### Requirement: Historical email downloads
The system SHALL provide an HTML download for each recorded personalized email attempt, displaying its stored recipient, sender, subject, body and outcome metadata without recomputing from current campaign or candidate data.

#### Scenario: Template changed after sending
- **WHEN** the operator downloads an earlier attempt after the campaign template changes
- **THEN** the HTML contains the earlier attempt's stored subject/body
- **AND** it labels uncertain or manually resolved outcomes accurately

### Requirement: Campaign ZIP package
The system SHALL provide a ZIP containing the campaign report and historical HTML emails, identify each file by stable candidate/attempt references, and distinguish tests and failed/uncertain attempts from confirmed submissions.

#### Scenario: Campaign download
- **WHEN** the operator requests the campaign package
- **THEN** each included email can be matched to its candidate and attempt in the report
- **AND** archive entries cannot escape their intended directory

### Requirement: Export consistency and retry
The system SHALL create downloads from a consistent recorded snapshot and publish files atomically. Failed generation SHALL be retryable independently of sending; exports SHALL never create or retry an email attempt.

#### Scenario: Concurrent progress
- **WHEN** a download is requested while results are changing
- **THEN** the download declares its as-of time and contains a consistent set of outcomes

#### Scenario: Generation interruption
- **WHEN** the server stops while writing an export
- **THEN** an incomplete file is not presented as complete
- **AND** generation can be retried from saved data without resubmission

### Requirement: Private and inert downloads
The system SHALL authorize every original-file and generated-file download, exclude tokens/secrets, escape generated report text and provide static HTML that cannot execute candidate-supplied scripts.

#### Scenario: Spreadsheet formula-like text
- **WHEN** a candidate name or error beginning with a formula prefix is written into a generated report
- **THEN** it is stored as text rather than promoted to an executable formula

#### Scenario: Direct private file URL
- **WHEN** an unauthenticated visitor accesses a stored download URL
- **THEN** no file content is returned

### Requirement: Manual retention and deletion
The system SHALL retain uploads, revisions, candidate/task history, attempts and generated artifacts without automatic expiry. Manual deletion SHALL clearly distinguish generated-file removal from deleting campaign records and require confirmation of the chosen scope.

#### Scenario: Remove generated workbook
- **WHEN** the operator deletes only a generated workbook
- **THEN** candidate eligibility and attempt history remain unchanged
- **AND** the workbook can be regenerated from retained records

#### Scenario: Delete campaign history
- **WHEN** the operator confirms deletion of an inactive campaign and all associated data
- **THEN** its live records and files are removed
- **AND** the UI has explained that its duplicate-prevention history is also removed and future campaigns can send again

#### Scenario: Delete active data
- **WHEN** deletion would remove data referenced by queued or in-flight work
- **THEN** deletion is rejected until the work is stopped and active requests settle
