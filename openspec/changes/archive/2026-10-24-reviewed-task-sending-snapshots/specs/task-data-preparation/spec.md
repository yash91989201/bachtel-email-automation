# Spec Delta

## Purpose

Finalize prepared or human-edited workbook data into immutable sending snapshots with deterministic validation and explicit operator acceptance.

## ADDED Requirements

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
