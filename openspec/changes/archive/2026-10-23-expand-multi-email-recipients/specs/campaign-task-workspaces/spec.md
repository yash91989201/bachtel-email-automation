# Campaign Task Workspaces Delta

## MODIFIED Requirements

### Requirement: Workbook upload and selection remain guided
The system SHALL show campaign context throughout workbook upload and worksheet/header selection. It SHALL put common input constraints and required mapped header names before technical details. Upload SHALL require a native Direct/Scheduled preference choice and retain it on error. Upload and validation SHALL remain separate explicit actions, with no candidate sending caused by either action.

#### Scenario: Operator opens Upload workbook
- **WHEN** a campaign opens the upload page
- **THEN** the page shows campaign/task-list breadcrumbs, the campaign name and template revision or “No template”, a labeled file input, “Upload workbook”, and “Cancel”
- **AND** visible guidance gives XLSX format, the effective upload-size limit in readable units and the 1,000-recipient-entry bound after comma splitting, counted before invalid/duplicate exclusions
- **AND** a visible example explains comma-separated recipient cells, copied row details and per-email exclusions
- **AND** when a revision exists it lists all mapped column names as mandatory, including optional-value mappings whose columns are mandatory
- **AND** a required native Direct/Scheduled radio group has no implicit selection and its selected value is retained if upload fails
- **AND** the saved preference creates no schedule time, authorization or candidate action
- **AND** without a revision it explains that an upload can be staged now, links to Add template and does not claim that mapped header names or validation are available yet
- **AND** archive/XML limits, cached-formula behavior and unsupported-input details remain accessible under “Technical limits”

#### Scenario: Worksheet or header validation fails
- **WHEN** an uploaded task has missing/ambiguous headers or an invalid worksheet/header selection
- **THEN** Recipients shows the worksheet/header form, the submitted selection and a specific error with the offending header names and remedy
- **AND** its primary form action is “Validate workbook” and later sections explain their unmet prerequisites
- **AND** no successful validation, approval or candidate sending is implied

#### Scenario: Staged workbook has no campaign template yet
- **WHEN** an uploaded task is opened before its campaign has a valid template
- **THEN** Recipients explains that the workbook is stored, offers Add template and retains its worksheet/header choices
- **AND** validation is unavailable with the reason “Add a template before validating this workbook”
- **AND** after template creation the same staged workbook can be validated without another upload

### Requirement: Recognizable task list entries
The system SHALL identify each task by its original workbook filename, linked to that task's Overview, and show only the derived status and the eligible count in its row. Each row SHALL offer one next-action link into the section the workspace rule chose and SHALL disclose remaining detail through keyboard-reachable per-cell tooltips. Header-row and worksheet details stay inside the task instead of displacing operational information in its list.

#### Scenario: Row detail is progressively disclosed
- **WHEN** a task row is shown
- **THEN** the workbook tooltip shows the short task reference, upload date/time and saved revision when available
- **AND** the status tooltip shows the status explanation and any attention flag
- **AND** the recipients tooltip shows original source-row count, expanded recipient-entry count, submitted recipients and recipient entries needing attention
- **AND** each tooltip opens for pointer and keyboard focus and contains no interactive controls

#### Scenario: Two uploads use the same filename
- **WHEN** a campaign contains two tasks uploaded from recipients.xlsx
- **THEN** each row's workbook tooltip shows the distinguishing upload time and task reference without opening the task
- **AND** the workbook name opens that exact task at its Overview, while the next action opens the chosen task section
- **AND** the filename is displayed as text and cannot supply active markup or a storage path

#### Scenario: Task counts are not yet available
- **WHEN** a workbook has been uploaded but has no successful validation snapshot
- **THEN** its list entry shows “Needs worksheet selection” or “Validation failed” and “Not validated” for recipients rather than zero eligible recipients
- **AND** its action says “Validate workbook” and opens Recipients when a valid template exists; otherwise it says “Add template” and opens campaign Template & preview

#### Scenario: Ready or active task appears in a list
- **WHEN** a task has a validation snapshot
- **THEN** its Recipients cell shows the eligible count, and its tooltip distinguishes original source-row count, expanded recipient-entry count, submitted recipients and recipient entries needing attention
- **AND** its Status cell shows the derived status, and its tooltip carries the status explanation and any attention flag
- **AND** “Needs attention” counts distinct affected recipient entries rather than source rows or overlapping exclusion categories
- **AND** the next-action link uses the workspace action-selection rules, such as “Test & approve”, “View schedule”, “View progress”, “Resume sending”, “Review outcomes” or “View results”

## ADDED Requirements

### Requirement: Expanded recipient review
The system SHALL distinguish original source-row counts from expanded recipient-entry counts throughout validation and task summaries. Recipients SHALL identify each entry by source row, position within its cell and individual address, provide independent eligible-entry previews, and expose entry-specific exclusions and protected report downloads.

#### Scenario: Mixed recipient cell is reviewed
- **WHEN** one source row contains two eligible addresses and one invalid entry
- **THEN** the task shows one source row, three recipient entries, two eligible recipients and one invalid entry
- **AND** each eligible recipient is independently selectable for preview with its own To address and copied details
- **AND** the invalid entry's address, source location and reason remain visible without disabling its eligible siblings

#### Scenario: Duplicate siblings need attention
- **WHEN** two duplicate entries and one eligible entry come from the same source row
- **THEN** two entries need attention and one recipient is eligible
- **AND** both duplicates appear with distinct positions and the eligible sibling remains selectable

#### Scenario: Action scope and result guidance
- **WHEN** the operator reviews Sending or Results for an expanded task
- **THEN** action confirmations and progress use recipient counts rather than original row counts
- **AND** Results explains that original worksheets are preserved and individual outcomes are in the added recipient-results worksheet
- **AND** all start, schedule, approval and recovery prerequisites still apply
