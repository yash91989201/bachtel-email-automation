# history-exports Specification

## Purpose

Provide durable, protected task and campaign downloads that preserve original data and historical email content without changing submission state.

## Requirements

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

### Requirement: Automatic export activation and idle behavior
The system SHALL automatically generate task results when committed task state becomes completed, stopped or recovered-interrupted, independently of browser requests. When no export work is pending and no export recovery operation has failed, it SHALL wait without recurring export-work discovery queries. Rolled-back task changes SHALL NOT activate export generation.

#### Scenario: Completion without an open browser
- **WHEN** a task completes while the operator has closed the browser
- **THEN** its committed results become available for automatic generation without a browser refresh or manual generation action

#### Scenario: Stop waits for final outcomes
- **WHEN** a stop is requested while email submissions remain in flight
- **THEN** automatic task-results generation waits until the task reaches its stopped state and the final outcomes have been recorded

#### Scenario: Rolled-back completion
- **WHEN** a transaction that would mark a task completed is rolled back
- **THEN** that uncommitted completion does not activate export generation

#### Scenario: No export work
- **WHEN** startup recovery and pending generation have finished successfully and no new export work becomes eligible
- **THEN** export-work discovery executes no recurring database queries

### Requirement: Campaign reporting
The system SHALL offer a campaign XLSX report with candidate summary and separate attempt history, identifying task, configuration revision, normalized recipient, outcome and timestamp without mixing test submissions into candidate totals.

#### Scenario: Candidate has several attempts
- **WHEN** a candidate has a definite failure, an uncertain attempt and a later accepted submission
- **THEN** the summary shows its current campaign state
- **AND** the history sheet retains each distinct attempt and any manual resolution

### Requirement: Historical email downloads
The system SHALL provide an HTML download for each recorded personalized email attempt, displaying its stored recipient, subject, body and outcome metadata without recomputing from current campaign or candidate data. It SHALL NOT substitute the currently connected sender as historical evidence; retaining previous sender identities is not required.

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
The system SHALL publish consistent-snapshot exports atomically, recover pending work and automatically rebuild crash- or shutdown-interrupted generation on startup. Ordinary failures SHALL remain visible for explicit retry, not automatic retry on idle or restart. Export generation and recovery SHALL never create or retry email attempts or change sending eligibility.

#### Scenario: Concurrent progress
- **WHEN** a download is requested while results are changing
- **THEN** the download declares its as-of time and contains a consistent set of outcomes

#### Scenario: Generation interruption
- **WHEN** the server stops while writing an export
- **THEN** an incomplete file is not presented as complete
- **AND** the next startup automatically rebuilds the interrupted export from saved data without resubmission or an operator retry action

#### Scenario: Crash before export activation
- **WHEN** the server crashes after export work is durably recorded but before generation is activated
- **THEN** startup discovers and processes that work automatically

#### Scenario: Task recovery is still in progress
- **WHEN** startup recovery has recorded only some of a task's recovered sending outcomes
- **THEN** automatic export generation waits until sending recovery completes before taking its snapshot
- **AND** the generated results include the task's fully recovered outcomes

#### Scenario: Export recovery fails to generate a file
- **WHEN** an automatically recovered export encounters an ordinary generation failure
- **THEN** the failure is recorded and the UI offers an explicit generation retry
- **AND** later idle periods or server restarts do not automatically repeat that failed generation

#### Scenario: Finished or intentionally deleted export
- **WHEN** the server starts with an already published export or an export that the operator intentionally deleted without requesting regeneration
- **THEN** startup recovery does not regenerate it merely because the server restarted

#### Scenario: Requested campaign export is interrupted
- **WHEN** the server stops during an explicitly requested campaign report or ZIP generation
- **THEN** startup automatically rebuilds the interrupted output without requiring the request to be repeated
- **AND** campaign exports that were never requested are not generated merely because the server restarted

### Requirement: Private and inert downloads
The system SHALL authorize every original-file and generated-file download against its owning operator, exclude tokens/secrets, escape generated report text and provide static HTML that cannot execute candidate-supplied scripts.

#### Scenario: Spreadsheet formula-like text
- **WHEN** a candidate name or error beginning with a formula prefix is written into a generated report
- **THEN** it is stored as text rather than promoted to an executable formula

#### Scenario: Direct private file URL
- **WHEN** an unauthenticated visitor accesses a stored download URL
- **THEN** no file content is returned

#### Scenario: Another operator guesses an artifact URL
- **WHEN** an authenticated operator requests another operator's upload, duplicate report, task result, campaign report, ZIP or attempt HTML
- **THEN** no content or private metadata is returned
- **AND** the same ownership boundary applies to generation, regeneration and deletion actions

#### Scenario: Background export ownership
- **WHEN** an export is generated or recovered without an open browser
- **THEN** its input snapshot and output belong only to the recorded owner
- **AND** direct file storage access cannot bypass the owner check

#### Scenario: Provider application credentials excluded
- **WHEN** task reports, campaign reports, historical HTML or ZIP packages are generated after provider setup or application-secret rotation
- **THEN** no client secret, mailbox password, access token or refresh token is included
- **AND** same-mailbox settings changes do not erase or relabel existing submission outcomes

### Requirement: Manual retention and deletion
The system SHALL retain uploads, revisions, candidate/task history, attempts and generated artifacts without automatic expiry, except for the explicitly confirmed mailbox-replacement reset. Manual deletion SHALL clearly distinguish generated-file removal from deleting campaign records and require confirmation of the chosen scope.

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

#### Scenario: Replace mailbox and retain templates
- **WHEN** an operator confirms mailbox replacement
- **THEN** their tasks, candidate data, original candidate workbooks, duplicate reports, approvals, attempts, resolutions and generated results/history are deleted
- **AND** their campaigns, configuration revisions and original template DOCX files are retained
- **AND** other operators' records and artifacts are unchanged

#### Scenario: Late exporter after reset
- **WHEN** a generation worker completes work for a task deleted by mailbox replacement
- **THEN** the obsolete artifact is not published or made downloadable
- **AND** reset recovery cleans remaining deleted-scope files without deleting retained templates

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
