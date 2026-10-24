# Spec Delta

## MODIFIED Requirements

### Requirement: Task preparation review separates corrections and findings
The task workspace SHALL present successful AI corrections separately from unresolved findings and expose the application and saved task-specific instructions. For current role-mapping tasks it SHALL present a completed run's backend review, approve/upload controls and prepared downloads without a separate selection action, keep explicit selection for older runs, and mark backend validation as pending before any run completes.

#### Scenario: Review initial preparation
- **WHEN** the operator opens a task after initial preparation
- **THEN** the workspace shows successful corrections and unresolved findings in distinct groups
- **AND** the visible base instructions and saved task-specific instructions are available for review
- **AND** the operator can review mapped values and findings without authorizing any email action

#### Scenario: Review follows completion without a selection step
- **WHEN** a preparation run completes with no selected final snapshot
- **THEN** the completed run's backend review counts, accepted and held entries and prepared downloads are available on the next page load
- **AND** approve and upload controls refer to that run without an additional selection action
- **WHEN** an older completed run is explicitly selected
- **THEN** the workspace presents that run's evidence and any selected final snapshot is cleared

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
