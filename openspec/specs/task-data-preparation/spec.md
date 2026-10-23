# task-data-preparation Specification

## Purpose
Prepare mapped workbook values through the confirmed account AI connection, preserving complete run evidence and unresolved findings for later operator review.

## Requirements

### Requirement: Mapped-field preparation scope
The system SHALL normalize only campaign-mapped fields during initial task preparation. Source headers SHALL guide field interpretation, supported by variable names and mapped row context. Problems in unmapped columns SHALL NOT affect eligibility. Preparing or reviewing data SHALL NOT send email.

#### Scenario: Header guides semantic checks
- **WHEN** a mapped Department cell contains `6lpa`
- **THEN** AI reports a field mismatch instead of inventing a department or moving the value
- **AND** a similar value in an unmapped column does not exclude the row

#### Scenario: Empty and entirely blank input
- **WHEN** mapped values are empty or a source row has no mapped data
- **THEN** AI does not invent values from names, email addresses or other records
- **AND** required/optional validation determines empty-value eligibility and entirely blank mapped rows remain ignored

### Requirement: Visible and extensible normalization policy
The system SHALL expose the application base prompt and allow optional instructions for the owning task. Task instructions SHALL extend or override normalization defaults without bypassing output validation, operator isolation, recipient integrity or sending authorization. Workbook contents SHALL be treated as data, never instructions.

#### Scenario: Task extension changes presentation
- **WHEN** the operator requests full expansion of a role abbreviation for one task
- **THEN** that task uses the instruction while another task without it uses the base defaults
- **AND** the exact base prompt and task extension used remain inspectable

#### Scenario: Instruction embedded in a cell
- **WHEN** a source cell requests that another row be deleted or email sent
- **THEN** its text is processed as cell data and grants no action or output-structure authority

### Requirement: Header-guided cleanup defaults
The base policy SHALL clean whitespace, casing, punctuation and clear spelling defects while preserving meaning, initials, recognizable acronyms, units and identifiers. Clear name-spacing repairs preserving letter order SHALL be allowed. Uncertain changes SHALL retain the supplied information and produce a review finding instead of inventing facts.

#### Scenario: Successful clear corrections
- **WHEN** values contain `H.P. R OY`, `ASSISTANT MANGER` and `PROJECT MANGMENT`
- **THEN** their intended normalized forms can be `H.P. Roy`, `Assistant Manager` and `Project Management`
- **AND** successful corrections alone do not exclude those entries

#### Scenario: Uncertain technical term
- **WHEN** the model cannot confidently determine the intended certificate term
- **THEN** it retains the term and reports the affected mapped field for review
- **AND** it does not invent a certificate, currency, date meaning or qualification

### Requirement: Multiple-value presentation
The base policy SHALL preserve multiple distinct values in their original order and separate them with comma followed by one space in every mapped column, including email fields. It SHALL distinguish lists from compound expressions and meaningful punctuation within a value. It SHALL NOT select one value unless task instructions request that selection.

#### Scenario: Location and role alternatives
- **WHEN** values contain `Pune/Nashik` and `General Manager / Assistant Vice President`
- **THEN** they become `Pune, Nashik` and `General Manager, Assistant Vice President`
- **AND** compound terms such as `QA/QC` and meaningful parenthetical details remain intact

#### Scenario: Multiple email values
- **WHEN** an email cell contains `a@example.com / B@example.com`
- **THEN** preparation produces `a@example.com, b@example.com` without dropping or inventing an address
- **AND** backend expansion subsequently evaluates each address independently

### Requirement: Findings determine preparation eligibility
The system SHALL exclude entries with unresolved AI findings in mapped fields from the prepared sending dataset and include them in the exclusion report. Successful corrections SHALL be distinguished from review findings. A shared-field finding SHALL hold all recipient siblings; an individual address finding SHALL hold only the affected entry.

#### Scenario: Optional field still contains unrelated data
- **WHEN** an optional mapped Department contains compensation and AI flags it
- **THEN** the entry is excluded even though the field is optional
- **AND** fallback text does not conceal the finding

#### Scenario: Shared and recipient-specific findings
- **WHEN** two email addresses share a row with an uncertain mapped name
- **THEN** both entries are held with that name finding
- **WHEN** only one recipient token is invalid or uncertain
- **THEN** the valid sibling can remain eligible if all shared-field checks pass

### Requirement: Preparation history and instruction drafts
The system SHALL separate editable task-instruction drafts from saved normalization runs. Each run SHALL retain the original input, exact base prompt and version, task instructions, confirmed AI connection/configuration identity, provider, endpoint, exact model and inference settings, actual outputs and findings. Editing a draft SHALL NOT modify a completed result; an explicit rerun SHALL create a new inspectable result.

#### Scenario: Instructions edited after preparation
- **WHEN** the operator saves different instructions after a completed run
- **THEN** the existing prepared values remain unchanged until an explicit rerun is completed and selected
- **AND** earlier run outputs and findings remain available for comparison

### Requirement: Incomplete and failed AI processing
The system SHALL make AI failures and incomplete or invalid responses visible and retryable. Missing, failed or structurally invalid results SHALL NOT become eligible through silent use of raw data or fallback text. Reopening a page, previewing, downloading or uploading a final sheet SHALL NOT invoke AI.

#### Scenario: Partial response or timeout
- **WHEN** a provider times out or omits, duplicates or misidentifies a requested row or mapped field
- **THEN** affected results remain unavailable for sending with a processing reason
- **AND** the operator can explicitly retry failed preparation without changing previously saved run evidence

#### Scenario: Server interruption
- **WHEN** preparation is interrupted by restart or browser closure
- **THEN** completed evidence remains durable and reopening shows the actual processing state
- **AND** unresolved work never appears successfully prepared or finalized

### Requirement: Preparation uses the confirmed account AI connection
Initial preparation and every retry SHALL use only the owning operator's confirmed AI provider and model, retaining the run's configuration identity. No automatic provider/model fallback SHALL occur. Unusable AI setup SHALL block preparation with a repair remedy, without preventing backend-only final upload or access to existing snapshots.

#### Scenario: Account model becomes unavailable
- **WHEN** the selected model fails during a preparation batch
- **THEN** the run shows the failure and does not call another model or silently accept raw values
- **AND** the operator can repair the same configuration and explicitly retry, or close pending tasks before switching and creating new work

### Requirement: Private preparation and review evidence
Preparation inputs, task prompts, run outputs, exclusions and final datasets SHALL be accessible only to their owning operator. Findings and provider failures SHALL expose useful remedies without leaking credentials, other operators' data or private provider responses.

#### Scenario: Foreign artifact or action
- **WHEN** another operator guesses a run, finalization or download identifier
- **THEN** access is refused without disclosing its content or metadata

### Requirement: Approve prepared data as-is
The owning operator SHALL be able to approve the accepted prepared dataset as-is without downloading or reuploading it. The system SHALL recheck backend eligibility and create an immutable final sending snapshot from accepted prepared values. Dataset approval SHALL NOT grant received-format approval or sending authorization.

#### Scenario: Prepared sheet needs no edits
- **WHEN** the operator reviews prepared values and selects Approve as-is
- **THEN** accepted values become the final snapshot without AI or a required reupload
- **AND** its download and email previews are available but sending requires that snapshot's own test approval and explicit start or schedule

#### Scenario: Stale or repeated approval
- **WHEN** approval references a superseded preparation result, changed campaign revision or a foreign task
- **THEN** finalization is refused with a review remedy and no content or sending state changes
- **WHEN** the same valid finalization action is delivered twice
- **THEN** it returns the same snapshot rather than creating a second finalization

### Requirement: Edited final upload without AI
The operator SHALL be able to upload an edited final sheet, including edits, removals and corrected or added entries. That upload SHALL receive backend validation only and accepted entries SHALL become a new final snapshot. Content SHALL remain operator-authored apart from existing optional fallbacks and recipient trim/lowercase rules. Rejected entries SHALL have a downloadable exclusion report.

#### Scenario: Human correction is authoritative
- **WHEN** the operator uploads an edited prepared sheet with valid mapped values
- **THEN** the app does not call AI, reapply casing rules, standardize content lists or infer semantic fitness
- **AND** accepted values drive final download, preview, test and sending

#### Scenario: Mixed accepted and rejected final rows
- **WHEN** a structurally valid final upload contains eligible entries and entries failing backend validation
- **THEN** accepted entries form the new final snapshot and rejected entries appear with reasons in its exclusion report
- **AND** importing a previously AI-flagged value does not reuse the old AI finding; the operator owns semantic review of the final upload

#### Scenario: Structural failure or zero accepted entries
- **WHEN** a final upload has missing mapped headers, corrupt content or exceeds the task limits
- **THEN** it is rejected without replacing the previous final snapshot
- **WHEN** a structurally valid upload has zero accepted entries
- **THEN** exclusions remain downloadable, current finalization is not sendable and prior approval cannot authorize the attempted replacement

### Requirement: Final snapshot identity and replacement
Both finalization paths SHALL identify the exact dataset accepted by the operator. Each accepted replacement upload SHALL create a new final snapshot requiring fresh test and received-format approval, even if its values match an earlier upload. Changing the final dataset SHALL invalidate prior start or schedule authorization and preserve historical snapshots.

#### Scenario: Replacement under the same template
- **WHEN** an unstarted task receives a new final upload under an unchanged campaign revision
- **THEN** previous snapshot approval does not authorize its test approval or candidate sending
- **AND** previous data, test records and approvals remain historical evidence

#### Scenario: Enabled schedule protects its snapshot
- **WHEN** the operator attempts rerun selection, final replacement or revalidation while a future schedule is enabled
- **THEN** the operation is refused until the schedule is explicitly cleared
- **AND** clearing it never transfers its authorization to new data

### Requirement: Started task content is frozen
After candidate sending has started, the system SHALL refuse preparation changes, final-sheet replacement and dataset reapproval that would change the saved sending content. Paused, stopped, interrupted and completed tasks SHALL retain their original content; different data SHALL use a new task.

#### Scenario: Edit after a settled stop
- **WHEN** an operator uploads a revised final sheet to a previously started, now stopped task
- **THEN** replacement is refused with guidance to create a new task
- **AND** retry, resend and recovery retain the original snapshot and their existing confirmations
