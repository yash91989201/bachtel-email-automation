## MODIFIED Requirements

### Requirement: Immutable task snapshots
The system SHALL retain, for every admitted task-candidate, the mapped values that render its personalized content, its source row location, recipient position within that row, individual recipient value and configuration revision. The stored workbook SHALL remain the record of the uploaded original values, and review evidence SHALL retain the original cell alongside any cleaned proposal. For current role-mapping revisions, admission SHALL occur when the operator approves a final dataset, so memberships and their personalized content come from the accepted cleaned entries and never from raw workbook text. Once sending begins, later uploads and campaign edits SHALL NOT alter that task's personalized content or prior attempts.

#### Scenario: Corrected designation in a new upload
- **WHEN** a later task provides different details for an existing candidate
- **THEN** those details belong to the later task snapshot
- **AND** the earlier task and its email content remain unchanged

#### Scenario: Several recipients share a worksheet row
- **WHEN** one source row expands into two unique valid addresses
- **THEN** each has an independent task membership and personalized recipient value with the same copied non-recipient values
- **AND** both retain the source row and their distinct positions within its recipient cell
- **AND** previews, attempts and outcomes distinguish the two recipients

#### Scenario: Current mapping admits only accepted cleaned entries
- **WHEN** a current-mapping task is approved from a prepared dataset or an edited final upload
- **THEN** memberships carry the accepted cleaned values that previews, tests and sending render
- **AND** the original uploaded cell stays available in the stored workbook and in review evidence for held entries
- **AND** held entries create no membership while remaining visible in the exclusion report

#### Scenario: Existing saved tasks are not expanded automatically
- **WHEN** this capability becomes available for tasks validated earlier
- **THEN** their stored memberships, exclusions and historical attempts remain unchanged
- **AND** only an explicitly permitted revalidation or a new upload applies the new import behavior

### Requirement: Invalid-address rows remain excluded
The system SHALL retain invalid-address entry feedback in the reviewed dataset and report artifacts without creating a candidate identity for an unusable address. Validity SHALL be judged from the reviewed recipient value. Unique valid-address entries with other missing required values can be retained as held entries with reasons.

#### Scenario: Empty recipient
- **WHEN** a reviewed row produces an entry with no usable recipient address
- **THEN** that entry appears in the prepared or final review as invalid with its original source location
- **AND** no candidate, task membership or sending attempt is created for that entry
- **AND** valid unique sibling entries remain independently admissible
