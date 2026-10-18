# excel-intake Specification

## Purpose

Validate uploaded workbooks against campaign configuration and provide immediate, actionable row and duplicate feedback before any email task can send.

## Requirements

### Requirement: Workbook selection and bounds
The system SHALL accept XLSX uploads and allow selection of one worksheet and header row per task. It SHALL support up to 1,000 non-empty candidate rows and reject unsupported, encrypted, corrupt or oversized inputs with actionable errors.

#### Scenario: Workbook with multiple worksheets
- **WHEN** the operator uploads a workbook containing multiple worksheets
- **THEN** the UI lets the operator choose the worksheet and header row before validation
- **AND** only that worksheet is considered for candidate sending

#### Scenario: Supported task size
- **WHEN** the selected data contains 1,000 non-empty candidate rows
- **THEN** the row count alone does not prevent processing

#### Scenario: Too many candidate rows
- **WHEN** the selected data contains more than 1,000 non-empty candidate rows
- **THEN** task validation fails and no candidate submission is scheduled

### Requirement: Campaign-level preflight
The system SHALL validate every mapped source header, including the source header of the selected recipient mapping, against the selected worksheet header row before accepting sendable rows. Missing or ambiguous mapped headers SHALL block the task, while empty or invalid recipient values SHALL only exclude affected rows.

#### Scenario: Missing mapped column
- **WHEN** a configured source header is absent
- **THEN** the UI lists the missing header and the task cannot start

#### Scenario: Duplicate header labels
- **WHEN** two columns have the same label after surrounding whitespace is trimmed
- **THEN** the app requests an unambiguous header selection or corrected upload before the task can start

#### Scenario: Candidate missing a required value
- **WHEN** all mapped columns exist but a required field is empty in one row
- **THEN** that row is excluded with a reason and other valid unique rows remain eligible

#### Scenario: Empty recipient value
- **WHEN** a row has an empty or invalid value in the selected recipient mapping's source column
- **THEN** only that row is held with a recipient validation reason
- **AND** other valid unique rows remain eligible

### Requirement: Immediate duplicate detection
The system SHALL detect duplicate recipient keys using trimmed lowercase email addresses across the complete selected input before creating candidate records. Every row in a duplicate group SHALL be excluded, including identical rows and rows with other validation errors.

#### Scenario: Mixed-case duplicate addresses
- **WHEN** rows 2 and 8 contain alex@example.com and an uppercase, space-padded form of that address
- **THEN** both rows are flagged immediately as duplicates with their original worksheet row numbers
- **AND** neither row is selected as the preferred copy

#### Scenario: Duplicate also has missing data
- **WHEN** two rows share a recipient key and one is missing a required value
- **THEN** both remain excluded as duplicates rather than letting the other row bypass duplicate detection

### Requirement: Duplicates excluded from candidate storage
The system SHALL NOT insert or update candidate, task-candidate or sending-attempt records for within-upload duplicate rows. Their details SHALL remain only in the original upload and file-based validation/report artifacts; task-level duplicate counts can be retained.

#### Scenario: Existing candidate matches a duplicate group
- **WHEN** an upload contains a duplicate group for an address already stored in the campaign
- **THEN** the upload does not modify that existing candidate or create memberships/attempts for the duplicate rows

### Requirement: Immediate duplicate download
The system SHALL offer a protected XLSX duplicate report as soon as validation finishes, containing all original columns for every duplicate row plus its original row number and duplicate reason.

#### Scenario: Correct while another task proceeds
- **WHEN** the upload has duplicate groups and other eligible rows
- **THEN** the operator can immediately download the duplicates, start the eligible portion and create a new task from corrected data
- **AND** the original task snapshot remains unchanged

### Requirement: Review before explicit start
The system SHALL show total, eligible, invalid, duplicate, already-submitted, already-scheduled and review-required counts with row-specific reasons. It SHALL recheck the configuration, approval and campaign eligibility before accepting an explicit start.

#### Scenario: Configuration changes after upload
- **WHEN** campaign content changes between upload validation and the first start
- **THEN** the task must be revalidated and previewed against the new current revision and approved configuration before starting

#### Scenario: No eligible rows
- **WHEN** every row is excluded
- **THEN** the UI explains the exclusions, permits relevant downloads and does not offer a send action for zero emails

### Requirement: Original data preservation
The system SHALL retain the original workbook and row order, preserve original columns and values for results, and never treat imported status columns as authoritative sending history.

#### Scenario: Export from another campaign is uploaded
- **WHEN** an uploaded workbook already contains a SUBMITTED status from a different campaign
- **THEN** eligibility is determined from the current campaign's app history rather than that cell

#### Scenario: Result header collides with source data
- **WHEN** a configured result-column label already exists among the source columns
- **THEN** the app chooses and displays an unoccupied result label before start
- **AND** no original cell is overwritten
