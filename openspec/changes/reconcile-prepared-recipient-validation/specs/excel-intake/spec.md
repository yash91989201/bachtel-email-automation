## MODIFIED Requirements

### Requirement: Campaign-level preflight
The system SHALL validate every mapped source header, including the source header selected by the revision's recipient mapping, against the selected worksheet header row before AI preparation or final-upload acceptance. For current role-mapping revisions, that preflight SHALL pin the worksheet, header row and campaign revision and record source-row evidence only: it SHALL create no candidate, task membership, validation manifest, exclusion report, validation category count or selected dataset, and it SHALL NOT classify row eligibility. Required/optional checks SHALL run after AI preparation or directly on final uploads, which SHALL NOT call AI. Missing or ambiguous mapped headers SHALL block the task. For legacy format-mapping revisions the historical validation snapshot SHALL be retained unchanged.

#### Scenario: Missing mapped column
- **WHEN** a configured source header is absent
- **THEN** the UI lists the missing header and the task cannot start

#### Scenario: Duplicate header labels
- **WHEN** two columns have the same label after surrounding whitespace is trimmed
- **THEN** the app requests an unambiguous header selection or corrected upload before the task can start

#### Scenario: Current mapping publishes no row eligibility
- **WHEN** a task whose revision uses the role mapping has all mapped headers present
- **THEN** the task becomes ready for data preparation with its worksheet, header row, pinned revision and source-row count
- **AND** no candidate identity, membership, manifest, exclusion report or eligible/excluded entry count is created at that point

#### Scenario: Header failure leaves the task unselected
- **WHEN** a mapped header is missing or ambiguous during preflight
- **THEN** the task remains awaiting worksheet selection with the stated reason and no pinned revision

#### Scenario: Candidate missing a required value
- **WHEN** all mapped columns exist but a required field is empty in one cleaned entry
- **THEN** each expanded recipient from that source entry is held with a reason and other valid unique recipients remain accepted

#### Scenario: Empty recipient value
- **WHEN** a cleaned entry in the selected recipient mapping's source column is empty or not one usable address
- **THEN** only that entry is held with its source row and recipient validation reason
- **AND** other valid unique entries, including those from the same source row, remain accepted

#### Scenario: Mapped AI findings are combined with backend checks
- **WHEN** a completed preparation run contains a mapped-field uncertainty or semantic mismatch
- **THEN** affected entries are held alongside missing required values, invalid addresses, duplicates and campaign-history restrictions
- **AND** all applicable reasons are retained, while unmapped-field problems do not affect eligibility

#### Scenario: Optional empty versus optional flagged
- **WHEN** an optional reviewed value is empty
- **THEN** its existing fallback or an empty replacement is used
- **WHEN** preparation flags a populated optional mapped value
- **THEN** it is held rather than hidden by fallback

### Requirement: Immediate duplicate detection
The system SHALL detect duplicate recipient keys using trimmed lowercase email addresses across all expanded entries of the complete reviewed dataset before creating candidate records. The reviewed dataset SHALL be the selected preparation result or an edited final upload, and the check SHALL run again when the dataset is approved. Every entry in a duplicate group SHALL be held, including repetitions within one cell, identical source rows and entries with other validation errors. Legacy format-mapping revisions SHALL retain their validation-time duplicate snapshot.

#### Scenario: Mixed-case duplicate addresses
- **WHEN** the reviewed dataset contains alex@example.com and an uppercase, space-padded form of that address
- **THEN** both entries are held as duplicates with their original worksheet row numbers and positions within their recipient cells
- **AND** neither entry is selected as the preferred copy

#### Scenario: Duplicate also has missing data
- **WHEN** two reviewed entries share a recipient key and one is missing a required value
- **THEN** both remain held as duplicates rather than letting the other entry bypass duplicate detection

#### Scenario: Duplicate inside one cell
- **WHEN** one cleaned cell contains `a@example.com, A@example.com, b@example.com`
- **THEN** both occurrences of a@example.com are held as duplicates and neither creates a membership
- **AND** b@example.com is evaluated independently and is accepted if its other checks pass

#### Scenario: Duplicate across single and multiple address cells
- **WHEN** one reviewed row contains `a@example.com, b@example.com` and another contains `a@example.com`
- **THEN** both occurrences of a@example.com are held and b@example.com proceeds independently

#### Scenario: Duplicate detection follows cleaned values
- **WHEN** preparation joins separator-only variants of one address into repeated occurrences in the reviewed dataset
- **THEN** the duplicate group is determined from those cleaned values before any candidate is created

### Requirement: Duplicates excluded from candidate storage
The system SHALL NOT insert or update candidate, task-candidate or sending-attempt records for duplicate entries of the reviewed dataset. Their details SHALL remain only in the selected preparation evidence, the final snapshot evidence and the downloadable reports; dataset-level duplicate counts can be retained.

#### Scenario: Existing candidate matches a duplicate group
- **WHEN** a reviewed dataset contains a duplicate group for an address already stored in the campaign
- **THEN** the dataset does not modify that existing candidate or create memberships/attempts for the duplicate entries

### Requirement: Immediate duplicate download
The system SHALL offer a protected XLSX exclusion report as soon as a reviewed dataset exists, containing all original columns for every held entry plus its original row number, position in the recipient cell, individual address and held reason. The report SHALL describe the selected final snapshot when one exists, otherwise the selected preparation review, and only legacy format-mapping tasks SHALL fall back to the validation-time report.

#### Scenario: Correct while another task proceeds
- **WHEN** the reviewed dataset holds duplicate groups and accepts other recipients
- **THEN** the operator can download the held entries, approve the accepted portion as-is or through an edited final upload, and use corrected data in a final upload or new task
- **AND** the original task snapshot remains unchanged

#### Scenario: No review evidence yet
- **WHEN** a current-mapping task has no selected preparation review and no final snapshot
- **THEN** the exclusion download is unavailable instead of returning a validation-time report

### Requirement: Comma-separated recipient expansion
After initial AI preparation or directly on a final upload, the system SHALL split the selected recipient column on commas, trim surrounding whitespace per entry, and copy other mapped values for each entry. Every entry SHALL retain its source worksheet row and position within the cell. Commas SHALL be the only list delimiter; invalid entries SHALL NOT prevent valid unique siblings from being accepted. Expansion SHALL run on reviewed values and SHALL NOT be repeated against raw values for the same dataset.

#### Scenario: Two valid addresses share details
- **WHEN** a row contains `Person 1 | a@example.com, b@example.com | Designation 1`
- **THEN** two recipients are accepted with the same name and designation, separate addresses and the same original source row
- **AND** commas with no following space or with extra surrounding spaces produce the same recipients
- **AND** no email is sent merely by saving them

#### Scenario: One invalid address among valid addresses
- **WHEN** a recipient cell contains `a@example.com, invalid-address, b@example.com`
- **THEN** the two valid unique entries are accepted under the usual required-value and campaign-history checks
- **AND** the invalid entry is retained only in review/report artifacts with its own reason and source location

#### Scenario: Empty list entries
- **WHEN** a reviewed row contains `a@example.com,, b@example.com,` in its recipient cell
- **THEN** the two empty entries are reported individually as invalid and the valid unique addresses are processed normally
- **AND** empty entries count toward the expanded-entry limit
- **AND** an entirely empty recipient cell on a row carrying mapped data produces one invalid entry; a row with no mapped data remains ignored

#### Scenario: Other columns and unsupported separators
- **WHEN** an ordinary mapped text column contains commas or a recipient entry contains a semicolon-separated or space-separated address list
- **THEN** ordinary mapped data is copied without splitting
- **AND** unsupported recipient lists are invalid entries rather than silently expanded

#### Scenario: Exclusions do not bypass the task limit
- **WHEN** splitting a reviewed dataset produces 1,001 entries even though duplicates or invalid values would leave fewer than 1,000 sendable recipients
- **THEN** the review refuses the complete dataset and exposes no partial acceptance

#### Scenario: AI separator cleanup occurs before expansion
- **WHEN** initial AI preparation recognizes two addresses separated by a slash
- **THEN** its verified comma-and-space result is passed to backend expansion
- **AND** each supplied address is retained apart from trim/lowercase and no AI-generated address can silently replace the source identity

#### Scenario: Edited final upload has unsupported separators
- **WHEN** a final email cell contains slash-separated addresses
- **THEN** no AI is invoked to repair it and backend comma-only parsing reports the entry as invalid with a correction remedy

### Requirement: Review before explicit start
The system SHALL show recorded row counts and reasons in Recipients and fresh action scope in Sending. A current role-mapping task SHALL show its source-row count and a pending-eligibility state until a prepared review or final snapshot exists, and SHALL show reviewed counts and reasons once one does. New tasks SHALL require approve as-is or acceptance of an edited final upload before sending controls become usable. Final-snapshot test approval and explicit start or schedule confirmation SHALL remain separate prerequisites.

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

#### Scenario: Counts appear with their evidence
- **WHEN** a current role-mapping task has no prepared review or final snapshot
- **THEN** Recipients reports no entry counts and no exclusions while naming data preparation as the next step
- **WHEN** a prepared review or final snapshot exists
- **THEN** Recipients reports the accepted and held entry counts recorded by that evidence
