# History Exports Delta

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

