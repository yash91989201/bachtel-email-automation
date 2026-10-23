# Campaign Candidates Delta

## MODIFIED Requirements

### Requirement: Immutable task snapshots
The system SHALL retain raw and normalized mapped values, source row location, recipient position within that row, individual recipient value and configuration revision for each admitted task-candidate. Once sending begins, later uploads and campaign edits SHALL NOT alter that task's personalized content or prior attempts.

#### Scenario: Corrected designation in a new upload
- **WHEN** a later task provides different details for an existing candidate
- **THEN** those details belong to the later task snapshot
- **AND** the earlier task and its email content remain unchanged

#### Scenario: Several recipients share a worksheet row
- **WHEN** one source row expands into two unique valid addresses
- **THEN** each has an independent task membership and personalized recipient value with the same copied non-recipient values
- **AND** both retain the source row and their distinct positions within its recipient cell
- **AND** previews, attempts and outcomes distinguish the two recipients

#### Scenario: Existing saved tasks are not expanded automatically
- **WHEN** this capability becomes available for tasks validated earlier
- **THEN** their stored memberships, exclusions and historical attempts remain unchanged
- **AND** only an explicitly permitted revalidation or a new upload applies the new import behavior

### Requirement: Invalid-address rows remain excluded
The system SHALL retain invalid-address entry feedback in the upload/report artifacts without creating a candidate identity for an unusable address. Unique valid-address entries with other missing required values can be retained as ineligible task-candidates.

#### Scenario: Empty recipient
- **WHEN** a source row produces an entry with no usable recipient address
- **THEN** that entry appears in validation/results as invalid with its original source location
- **AND** no candidate, task membership or sending attempt is created for that entry
- **AND** valid unique sibling entries remain independently admissible
