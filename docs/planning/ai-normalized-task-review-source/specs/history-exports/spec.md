# Spec Delta

## MODIFIED Requirements

### Requirement: Full task results workbook
The system SHALL generate and store an XLSX task-results workbook after completion, manual stop or interrupted-task recovery, preserving all original worksheets, input rows, columns and values in their original order. It SHALL add a separate recipient-results worksheet with copied source columns, original source location, individual recipient and the task's configured result columns for every expanded entry.

#### Scenario: Mixed results including excluded duplicates
- **WHEN** a task has submitted, failed, invalid, duplicate, already-scheduled and review-required rows
- **THEN** every expanded entry appears in the recipient-results worksheet with its appropriate status, timestamp where known and clear error/exclusion reason
- **AND** its source worksheet and original row/column values remain intact
- **AND** duplicate data is read from file artifacts without creating candidate records

#### Scenario: Existing result columns
- **WHEN** the source workbook already contains columns with the desired output labels
- **THEN** original columns remain intact and added metadata/results use collision-free labels shown before start in the recipient-results worksheet

#### Scenario: Different outcomes from the same source row
- **WHEN** a source cell contains two recipients and one has a submitted outcome while the other has a definite failure
- **THEN** the results worksheet has two separate entries with the same source row and their own address, position, status, timestamp and reason
- **AND** neither recipient's outcome overwrites the other's or is presented as the sole outcome for the source row

#### Scenario: Invalid and duplicate siblings remain reportable
- **WHEN** one row contains valid, invalid and duplicated entries
- **THEN** each occurrence has its own result entry, including repeated occurrences within one cell
- **AND** copied source data is retained beside the individual recipient without requiring candidate records for invalid or duplicate entries

#### Scenario: Results worksheet name already exists
- **WHEN** an input worksheet is named Recipient results, regardless of case
- **THEN** that input worksheet is preserved and the new results worksheet receives a deterministic unoccupied name
- **AND** no existing worksheet is replaced or renamed

#### Scenario: Single-address import
- **WHEN** each source row has only one recipient
- **THEN** the generated workbook still uses the separate recipient-results worksheet with one entry per source row
- **AND** existing original worksheets remain unmodified

#### Scenario: Edited final upload preserves original and final sources
- **WHEN** an accepted final upload reorders, adds or removes recipient entries from the prepared sheet
- **THEN** the original uploaded workbook remains available and unchanged
- **AND** the recipient-results worksheet represents each entry in the frozen final snapshot with its own source location and outcome

## ADDED Requirements

### Requirement: Preparation sending sheets contain only accepted mapped values
The system SHALL provide protected preparation and accepted-final sending workbooks containing only accepted mapped values, one row per recipient, in a form that can be uploaded for final review.

#### Scenario: Download preparation sending sheet
- **WHEN** the operator downloads the prepared sending sheet
- **THEN** it contains only accepted mapped values with one row per recipient entry
- **AND** it contains no original unmapped columns or excluded entries
- **AND** the sheet can be conveniently uploaded for final review

#### Scenario: Several variables share a source header
- **WHEN** two mappings reference the same source header
- **THEN** their output columns have distinct recognizable labels tied to their variables and can be imported without merging or overwriting values
- **AND** missing or ambiguous expected final-upload labels are reported before any snapshot replacement

#### Scenario: Download accepted final sheet
- **WHEN** the operator finalizes a valid snapshot through either review path
- **THEN** the accepted-final sending sheet contains only the accepted mapped values with one row per recipient entry
- **AND** the task email preview and subsequent email content use those accepted final values
- **AND** excluded entries are absent from the sending sheet

### Requirement: Preparation exclusions preserve source and correction evidence
The system SHALL provide a separate protected exclusion report that preserves original source columns and identifies before and after mapped values with each finding or exclusion reason.

#### Scenario: Download exclusion report
- **WHEN** the operator downloads the preparation or final-upload exclusion report
- **THEN** each rejected recipient entry is identified by source row, position and address
- **AND** the report includes original source columns, before and after mapped values, and applicable reasons
- **AND** shared-field and address-specific findings are attributed to the affected recipients

#### Scenario: No exclusions
- **WHEN** preparation or final validation has no excluded entries
- **THEN** the workspace reports that no exclusion rows exist without adding excluded entries to the sending sheet

#### Scenario: Download is refused
- **WHEN** an operator without access to the task requests a preparation, accepted-final or exclusion workbook
- **THEN** no file content or private task metadata is returned
