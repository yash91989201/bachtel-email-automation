# Excel Intake Delta

## MODIFIED Requirements

### Requirement: Workbook selection and bounds
The system SHALL accept XLSX uploads and allow selection of one worksheet and header row per task. It SHALL support up to 1,000 expanded recipient entries, counted before duplicate and invalid-address exclusions, and reject unsupported, encrypted, corrupt or oversized inputs with actionable errors.

#### Scenario: Workbook with multiple worksheets
- **WHEN** the operator uploads a workbook containing multiple worksheets
- **THEN** the UI lets the operator choose the worksheet and header row before validation
- **AND** only that worksheet is considered for candidate sending

#### Scenario: Supported task size
- **WHEN** the selected data produces exactly 1,000 recipient entries after splitting
- **THEN** the entry count alone does not prevent processing

#### Scenario: Too many candidate rows
- **WHEN** the selected data produces more than 1,000 recipient entries after splitting
- **THEN** task validation fails with an actionable expanded-recipient limit error before publishing any new candidate memberships or validation snapshot
- **AND** no candidate submission is scheduled

### Requirement: Campaign-level preflight
The system SHALL validate every mapped source header, including the source header selected by the revision's recipient mapping, against the selected worksheet header row before accepting sendable rows. Missing or ambiguous mapped headers SHALL block the task, while empty or invalid recipient entries SHALL only exclude those entries.

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

### Requirement: Immediate duplicate detection
The system SHALL detect duplicate recipient keys using trimmed lowercase email addresses across all expanded entries of the complete selected input before creating candidate records. Every entry in a duplicate group SHALL be excluded, including repetitions within one cell, identical source rows and entries with other validation errors.

#### Scenario: Mixed-case duplicate addresses
- **WHEN** rows 2 and 8 contain alex@example.com and an uppercase, space-padded form of that address
- **THEN** both entries are flagged immediately as duplicates with their original worksheet row numbers and positions within their recipient cells
- **AND** neither entry is selected as the preferred copy

#### Scenario: Duplicate also has missing data
- **WHEN** two expanded entries share a recipient key and one is missing a required value
- **THEN** both remain excluded as duplicates rather than letting the other entry bypass duplicate detection

#### Scenario: Duplicate inside one cell
- **WHEN** one cell contains `a@example.com, A@example.com, b@example.com`
- **THEN** both occurrences of a@example.com are held as duplicates and neither creates a membership
- **AND** b@example.com is evaluated independently and is eligible if its other checks pass

#### Scenario: Duplicate across single and multiple address cells
- **WHEN** one row contains `a@example.com, b@example.com` and another contains `a@example.com`
- **THEN** both occurrences of a@example.com are excluded and b@example.com proceeds independently

### Requirement: Duplicates excluded from candidate storage
The system SHALL NOT insert or update candidate, task-candidate or sending-attempt records for within-upload duplicate entries. Their details SHALL remain only in the original upload and file-based validation/report artifacts; task-level duplicate counts can be retained.

#### Scenario: Existing candidate matches a duplicate group
- **WHEN** an upload contains a duplicate group for an address already stored in the campaign
- **THEN** the upload does not modify that existing candidate or create memberships/attempts for the duplicate entries

### Requirement: Immediate duplicate download
The system SHALL offer a protected XLSX duplicate report as soon as validation finishes, containing all original columns for every duplicate entry plus its original row number, position in the recipient cell, individual address and duplicate reason.

#### Scenario: Correct while another task proceeds
- **WHEN** the upload has duplicate groups and other eligible recipients
- **THEN** the operator can immediately download the duplicates, start the eligible portion and create a new task from corrected data
- **AND** the original task snapshot remains unchanged

## ADDED Requirements

### Requirement: Comma-separated recipient expansion
The system SHALL split the selected recipient column on commas, trim surrounding whitespace per entry, and copy other mapped values for each entry. Every entry SHALL retain its source worksheet row and position within the cell. Commas SHALL be the only list delimiter; invalid entries SHALL NOT prevent valid unique siblings from being saved.

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
