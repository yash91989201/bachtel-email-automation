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
The system SHALL validate every mapped source header, including the source header selected by the revision's recipient mapping, against the selected worksheet header row before accepting sendable rows. Missing or ambiguous mapped headers SHALL block the task, while empty or invalid recipient values SHALL only exclude affected rows.

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
The system SHALL show row counts and reasons in Recipients and fresh action scope in Sending. The saved task `sending_type` SHALL be the source of truth for its first-start view. GET requests SHALL NOT change or override it; `sending_type=choose` may open the chooser without changing the saved value. A submitted type change SHALL be persisted before controls for the new type are shown. Show only the selected form. Type changes never authorize dispatch or mutate schedules; explicit confirmations and server checks remain required.

#### Scenario: Configuration changes after upload
- **WHEN** campaign content changes between upload validation and the first start
- **THEN** the task must be revalidated and previewed against the new current revision and approved configuration before starting
- **AND** Recipients identifies the saved/current revision difference and offers explicit revalidation; navigation alone does not update the snapshot

#### Scenario: Revalidation would reuse a prior schedule authorization
- **WHEN** the operator requests revalidation of an unstarted task with an enabled future schedule
- **THEN** revalidation is refused until the operator explicitly clears that schedule
- **AND** after revalidation the revised content needs applicable formatting approval and a new explicit start or schedule confirmation
- **AND** no old schedule authorization is transferred to revised content

#### Scenario: No eligible rows
- **WHEN** every row is excluded
- **THEN** the UI explains the exclusions, permits relevant downloads and does not offer a send action for zero emails
- **AND** the task workspace shows No eligible recipients with a link to its row reasons and exclusion report

#### Scenario: Schedule offered with fresh counts
- **WHEN** a validated task has an approved test and eligible rows
- **THEN** Sending uses the task's saved `sending_type` and shows only its selected form beside the same fresh eligible count, sender, saved revision, approval state and exclusions
- **AND** a task with no eligible rows offers neither Start nor a schedule
- **AND** Recipients continues to expose validation counts and row reasons without duplicating candidate start controls

#### Scenario: GET chooser leaves saved type and schedule untouched
- **WHEN** an eligible unstarted task is opened with the `sending_type=choose` query
- **THEN** the chooser is shown without changing the saved type or schedule
- **AND** Direct or Scheduled becomes the saved selection only after the sending-type POST succeeds
- **AND** an enabled or missed schedule remains active until the operator explicitly clears it or confirms the existing Send now instead action
- **AND** a paused downtime-missed task retains its explicit safe-resume path and cannot use the first-start type to resume

#### Scenario: Recheck at scheduled fire
- **WHEN** a schedule was enabled and the campaign configuration or approval changes before its time
- **THEN** the scheduled fire does not send from the stale snapshot
- **AND** the task returns to the review state that requires revalidation and approval
- **AND** the unified workspace explains the blocked/missed start and links to its required remedy without silently starting another schedule

### Requirement: Upload records initial sending preference
The system SHALL require a native Direct/Scheduled choice at task upload and persist it as optional task field `sending_type`. The saved value is durable task state and remains authoritative for selections and later changes before execution. Saving it creates no schedule, authorization or candidate action. Older tasks may have no value.

#### Scenario: New task stores a preference
- **WHEN** an operator uploads a workbook after selecting Direct or Scheduled
- **THEN** task creation stores that preference without a schedule time, schedule record or authorization
- **AND** an omitted or invalid choice is refused by server-side validation
- **AND** upload errors retain the submitted choice

#### Scenario: Sending reads the saved mode
- **WHEN** a staging or ready task opens Sending
- **THEN** the saved `sending_type` selects the corresponding controls regardless of URL query values
- **AND** an older task with no saved value may show the Direct/Scheduled chooser
- **AND** a `sending_type=choose` query may open that chooser without changing the saved field
- **AND** GET requests and navigation never write `sending_type`

#### Scenario: Change sending type before execution
- **WHEN** an operator submits Direct or Scheduled to the task sending-type POST while the task is staging or ready and has not started
- **THEN** the server persists the requested value before rendering or redirecting to its controls
- **AND** all task navigation, polling and list actions use the persisted value
- **AND** changing type does not start work, authorize dispatch, or create or modify a schedule
- **AND** a request to change to Direct is refused while an enabled or missed schedule remains, with an explicit Clear schedule remedy
- **AND** the schedule is never implicitly cleared or cancelled

#### Scenario: Sending type is immutable after start
- **WHEN** a task has started or is outside staging/ready first-start state
- **THEN** a type-change POST is refused and the existing state-specific Sending controls remain authoritative
- **AND** query values cannot change the saved type or expose first-start controls

#### Scenario: Started tasks ignore first-start preferences
- **WHEN** a task is running, waiting, stopping, paused, interrupted, stopped or completed
- **THEN** the saved type remains immutable and existing state-specific Sending controls remain visible

### Requirement: Original data preservation
The system SHALL retain the original workbook and row order, preserve original columns and values for results, and never treat imported status columns as authoritative sending history.

#### Scenario: Export from another campaign is uploaded
- **WHEN** an uploaded workbook already contains a SUBMITTED status from a different campaign
- **THEN** eligibility is determined from the current campaign's app history rather than that cell

#### Scenario: Result header collides with source data
- **WHEN** a configured result-column label already exists among the source columns
- **THEN** the app chooses and displays an unoccupied result label before start
- **AND** no original cell is overwritten
