# Spec Delta

## MODIFIED Requirements

### Requirement: Task preparation review separates corrections and findings
The task workspace SHALL present successful AI corrections separately from unresolved findings, and expose the visible application instructions with optional task-specific instructions during preparation. For current role-mapping tasks it SHALL present the completed run's backend review, approve/upload controls and prepared downloads without requiring a separate selection action, keep explicit selection available for older runs, and SHALL identify that backend validation is still pending before a completed run exists.

#### Scenario: Review initial preparation
- **WHEN** the operator opens a task after initial preparation
- **THEN** the workspace shows successful corrections and unresolved findings in distinct groups
- **AND** the visible base instructions and saved task-specific instructions are available for review
- **AND** the operator can review mapped values and findings without authorizing any email action

#### Scenario: Sanitization report is offered with the review
- **WHEN** a completed run's backend review is displayed for a current role-mapping task
- **THEN** the workspace links a sanitization report download that explains it holds one row per recipient entry with its outcome, corrections, findings, reasons and original and prepared values
- **WHEN** no completed run is selected or the review is unavailable
- **THEN** the download is not offered and the existing review remedy is shown

#### Scenario: Backend outcome replaces provisional counts
- **WHEN** a completed preparation run is selected for a current role-mapping task
- **THEN** accepted and held recipient counts come from the backend review of the cleaned values
- **AND** the preparation history continues to report corrections and AI findings without presenting them as admission
- **WHEN** the dataset is already finalized
- **THEN** the selected final snapshot supplies accepted and excluded rows and the preparation history remains labeled as history

#### Scenario: Preparation fails
- **WHEN** preparation fails or cannot complete
- **THEN** the workspace identifies the failure, preserves the task and instruction draft, and offers an explicit retry
- **AND** it does not present partial output as reviewed or final
