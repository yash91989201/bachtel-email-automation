# campaign-candidates Specification

## Purpose

Maintain campaign-scoped candidate identity and immutable task data so later uploads cannot cause duplicate or inconsistent submissions.

## Requirements

### Requirement: Campaign-scoped recipient identity
The system SHALL identify a candidate by campaign plus trimmed lowercase email, generate internal IDs without requiring an input ID column, and allow the same email in a different campaign.

#### Scenario: Existing candidate reappears
- **WHEN** a unique valid-address row has a key already present in the campaign
- **THEN** it references the same internal candidate identity rather than creating a second candidate

#### Scenario: Same recipient in a new campaign
- **WHEN** a recipient submitted in one campaign appears in a new campaign
- **THEN** the prior campaign does not make the new campaign's candidate ineligible

### Requirement: Immutable task snapshots
The system SHALL retain raw and normalized mapped values, source row location and configuration revision for each admitted task-candidate. Once sending begins, later uploads and campaign edits SHALL NOT alter that task's personalized content or prior attempts.

#### Scenario: Corrected designation in a new upload
- **WHEN** a later task provides different details for an existing candidate
- **THEN** those details belong to the later task snapshot
- **AND** the earlier task and its email content remain unchanged

### Requirement: Invalid-address rows remain excluded
The system SHALL retain invalid-address row feedback in the upload/report artifacts without creating a candidate identity for an unusable address. Unique valid-address rows with other missing required values can be retained as ineligible task-candidates.

#### Scenario: Empty recipient
- **WHEN** a row has no usable recipient address
- **THEN** it appears in validation/results as invalid
- **AND** no candidate or sending attempt is created for the row

### Requirement: Submitted and uncertain eligibility
The system SHALL skip candidates submitted within the campaign and hold candidates whose unresolved attempt has an uncertain outcome. Re-uploading or editing data SHALL NOT clear either state automatically.

#### Scenario: Re-upload after confirmed submission
- **WHEN** the same recipient is uploaded again in the same campaign with changed details
- **THEN** it is marked already submitted and is not sent again

#### Scenario: Re-upload after uncertainty
- **WHEN** an unresolved uncertain candidate appears in a corrected upload
- **THEN** it remains review-required until an explicit review action resolves it

### Requirement: Cross-task scheduled exclusion
The system SHALL exclude candidates reserved, queued or in flight in another task within the campaign, display Already scheduled, and permit other eligible candidates in the new task to proceed.

#### Scenario: Corrected upload overlaps running work
- **WHEN** a new task contains one candidate currently queued elsewhere and one unscheduled candidate
- **THEN** the queued candidate is excluded and the unscheduled candidate can be started

### Requirement: Atomic eligibility at start
The system SHALL atomically recheck and reserve candidate eligibility when accepting start, resume or retry actions. Concurrent actions SHALL NOT create multiple active candidate submissions for the same campaign/email key.

#### Scenario: Two prepared tasks start together
- **WHEN** both contain the same still-eligible candidate
- **THEN** only one action reserves that candidate
- **AND** the other action reports the candidate as already scheduled with refreshed counts

### Requirement: Corrected uploads preserve history
The system SHALL allow a new task with corrected data without deleting prior task snapshots or sending attempts. A changed normalized email SHALL be treated as a different candidate key rather than matched by name.

#### Scenario: Correct a required value
- **WHEN** a previously ineligible candidate is re-uploaded with the same address and complete required data, without an active, submitted or uncertain attempt
- **THEN** the new snapshot is eligible and the earlier validation result remains available

#### Scenario: Address changes
- **WHEN** a new upload uses a different normalized address for the same name
- **THEN** it is treated as a new candidate key and the app does not infer identity from the name
