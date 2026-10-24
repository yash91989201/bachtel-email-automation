## MODIFIED Requirements

### Requirement: Findings determine preparation eligibility
The system SHALL exclude entries with unresolved AI findings in mapped fields from the accepted prepared dataset and include them in the prepared exclusion report. Successful corrections SHALL be distinguished from review findings. A shared-field finding SHALL hold all recipient siblings; an individual address finding SHALL hold only the affected entry. Accepted and held entries SHALL describe the cleaned values, not the raw upload.

#### Scenario: Optional field still contains unrelated data
- **WHEN** an optional mapped Department contains compensation and AI flags it
- **THEN** the entry is held even though the field is optional
- **AND** fallback text does not conceal the finding

#### Scenario: Shared and recipient-specific findings
- **WHEN** two email addresses share a row with an uncertain mapped name
- **THEN** both entries are held with that name finding
- **WHEN** only one recipient token is invalid or uncertain
- **THEN** the valid sibling can remain accepted if all shared-field checks pass

### Requirement: Approve prepared data as-is
The owning operator SHALL be able to approve the accepted prepared dataset as-is without downloading or reuploading it. The system SHALL recheck backend eligibility against the pinned task source, configuration and content revision, and create an immutable final sending snapshot from accepted cleaned values. Dataset approval SHALL NOT grant received-format approval or sending authorization.

#### Scenario: Prepared sheet needs no edits
- **WHEN** the operator reviews prepared values and selects Approve as-is
- **THEN** accepted values become the final snapshot without AI or a required reupload
- **AND** its download and email previews are available but sending requires that snapshot's own test approval and explicit start or schedule

#### Scenario: Stale or repeated approval
- **WHEN** approval references a superseded preparation result, changed campaign revision, changed stored workbook or a foreign task
- **THEN** finalization is refused with a review remedy and no content or sending state changes
- **WHEN** the same valid finalization action is delivered twice
- **THEN** it returns the same snapshot rather than creating a second finalization

## ADDED Requirements

### Requirement: Prepared-data backend review
The system SHALL compute a read-only backend review of a completed preparation run before that run can be approved. The review SHALL combine AI findings with required/optional, recipient-address, duplicate, campaign-history and template-safety checks over the prepared values. Selecting a run for review SHALL be distinct from approving a dataset, and the review SHALL create no candidate, membership or sending attempt.

#### Scenario: Review a completed run
- **WHEN** a completed preparation run is selected for review
- **THEN** the workspace shows accepted recipient entries with source row and position, held entries with reasons, and counts for source rows, entries, accepted and held
- **AND** no candidate, membership, attempt, approval or schedule changes

#### Scenario: Review is refused
- **WHEN** the review targets an incomplete run, a foreign task or run, or evidence that no longer matches the task's source digest, configuration or content revision
- **THEN** the review is refused with a review remedy and no acceptance evidence is presented

### Requirement: Prepared downloads exclude held entries
The prepared sending sheet SHALL contain only backend-accepted entries and the prepared exclusion report only backend-held entries with their reasons, without invoking AI or rewriting values.

#### Scenario: Prepared downloads report backend outcomes
- **WHEN** the operator downloads the prepared sending sheet or its exclusion report
- **THEN** the sheet contains only backend-accepted entries and the report contains only backend-held entries with their reasons
- **AND** no AI call, recasing or value rewrite occurs during the download
