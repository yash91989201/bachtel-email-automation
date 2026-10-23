# Excel Intake Delta

## MODIFIED Requirements

### Requirement: Campaign-level preflight
The system SHALL validate every mapped source header, including the source header selected by the revision's recipient mapping, against the selected worksheet header row before AI preparation or final-upload acceptance. Required/optional checks SHALL run after AI preparation or directly on final uploads, which SHALL NOT call AI. Missing or ambiguous mapped headers SHALL block the task, while empty or invalid recipient entries SHALL only exclude those entries.

#### Scenario: Missing mapped column
- **WHEN** a configured source header is absent
- **THEN** the UI lists the missing header and the task cannot start

#### Scenario: Duplicate header labels
- **WHEN** two columns have the same label after surrounding whitespace is trimmed
- **THEN** the app requests an unambiguous header selection or corrected upload before the task can start

#### Scenario: Candidate missing a required value
- **WHEN** all mapped columns exist but a required field is empty in one row
- **THEN** each expanded recipient from that source row is ineligible with a reason and other valid unique recipients remain eligible

#### Scenario: Empty recipient value
- **WHEN** a source row produces an empty or invalid entry in the selected recipient mapping's source column
- **THEN** only that entry is held with its source row and recipient validation reason
- **AND** other valid unique entries, including those from the same source row, remain eligible

#### Scenario: Mapped AI findings are combined with backend checks
- **WHEN** initial preparation finishes with a mapped-field uncertainty or semantic mismatch
- **THEN** affected entries are excluded alongside missing required values, invalid addresses, duplicates and campaign-history restrictions
- **AND** all applicable reasons are retained, while unmapped-field problems do not affect eligibility

#### Scenario: Optional empty versus optional flagged
- **WHEN** an optional prepared or final value is empty
- **THEN** its existing fallback or an empty replacement is used
- **WHEN** initial preparation flags a populated optional mapped value
- **THEN** it is excluded rather than hidden by fallback


### Requirement: Immediate duplicate download
The system SHALL offer a protected XLSX duplicate report as soon as validation finishes, containing all original columns for every duplicate entry plus its original row number, position in the recipient cell, individual address and duplicate reason.

#### Scenario: Correct while another task proceeds
- **WHEN** the upload has duplicate groups and other eligible recipients
- **THEN** the operator can immediately download the duplicates, finalize the eligible portion through approve as-is or an edited final upload before testing and explicitly starting it, and use corrected data in a final upload or new task
- **AND** the original task snapshot remains unchanged

### Requirement: Review before explicit start
The system SHALL show row counts and reasons in Recipients and fresh action scope in Sending. New tasks SHALL require approve as-is or acceptance of an edited final upload before sending controls become usable. Final-snapshot test approval and explicit start or schedule confirmation SHALL remain separate prerequisites.

#### Scenario: Configuration changes after upload
- **WHEN** campaign content changes between upload validation and the first start
- **THEN** the unstarted task must be explicitly revalidated and reviewed against the new current revision, finalized again, and given its own successful test and received-format approval before starting
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

#### Scenario: Recheck at scheduled fire
- **WHEN** a schedule was enabled and the campaign configuration, final snapshot or approval changes before its time
- **THEN** the scheduled fire does not send from the stale snapshot
- **AND** the task returns to the review state that requires revalidation and approval
- **AND** the unified workspace explains the blocked/missed start and links to its required remedy without silently starting another schedule

### Requirement: Comma-separated recipient expansion
After initial AI preparation or directly on a final upload, the system SHALL split the selected recipient column on commas, trim surrounding whitespace per entry, and copy other mapped values for each entry. Every entry SHALL retain its source worksheet row and position within the cell. Commas SHALL be the only list delimiter; invalid entries SHALL NOT prevent valid unique siblings from being saved.

#### Scenario: Two valid addresses share details
- **WHEN** a row contains `Person 1 | a@example.com, b@example.com | Designation 1`
- **THEN** two recipients are saved with the same name and designation, separate addresses and the same original source row
- **AND** commas with no following space or with extra surrounding spaces produce the same recipients
- **AND** no email is sent merely by saving them

#### Scenario: One invalid address among valid addresses
- **WHEN** a recipient cell contains `a@example.com, invalid-address, b@example.com`
- **THEN** the two valid unique entries are saved under the usual required-value and campaign-history checks
- **AND** the invalid entry is retained only in validation/report artifacts with its own reason and source location

#### Scenario: Empty list entries
- **WHEN** a data row contains `a@example.com,, b@example.com,` in its recipient cell
- **THEN** the two empty entries are reported individually as invalid and the valid unique addresses are processed normally
- **AND** empty entries count toward the expanded-entry limit
- **AND** an entirely empty recipient cell on a row carrying mapped data produces one invalid entry; a row with no mapped data remains ignored

#### Scenario: Other columns and unsupported separators
- **WHEN** an ordinary mapped text column contains commas or a recipient entry contains a semicolon-separated or space-separated address list
- **THEN** ordinary mapped data is copied without splitting
- **AND** unsupported recipient lists are invalid entries rather than silently expanded

#### Scenario: Exclusions do not bypass the task limit
- **WHEN** splitting produces 1,001 entries even though duplicates or invalid values would leave fewer than 1,000 sendable recipients
- **THEN** validation refuses the complete new snapshot and publishes no partial import

#### Scenario: AI separator cleanup occurs before expansion
- **WHEN** initial AI preparation recognizes two addresses separated by a slash
- **THEN** its verified comma-and-space result is passed to backend expansion
- **AND** each supplied address is retained apart from trim/lowercase and no AI-generated address can silently replace the source identity

#### Scenario: Edited final upload has unsupported separators
- **WHEN** a final email cell contains slash-separated addresses
- **THEN** no AI is invoked to repair it and backend comma-only parsing reports the entry as invalid with a correction remedy
