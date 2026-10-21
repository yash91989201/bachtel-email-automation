## MODIFIED Requirements

### Requirement: Campaign-scoped recipient identity
The system SHALL identify a candidate by an operator-owned campaign plus trimmed lowercase email, generate internal IDs without requiring an input ID column, and allow the same email in a different campaign.

#### Scenario: Existing candidate reappears
- **WHEN** a unique valid-address row has a key already present in the campaign
- **THEN** it references the same internal candidate identity rather than creating a second candidate

#### Scenario: Same recipient in a new campaign
- **WHEN** a recipient submitted in one campaign appears in a new campaign
- **THEN** the prior campaign does not make the new campaign's candidate ineligible

#### Scenario: Same recipient across operators
- **WHEN** two operators import the same address into their own campaigns
- **THEN** each campaign retains independent candidate identity and eligibility
- **AND** neither operator sees the other's candidate or submission state


### Requirement: Corrected uploads preserve history
The system SHALL allow a new task with corrected data without deleting prior task snapshots or sending attempts. A changed normalized email SHALL be treated as a different candidate key rather than matched by name.

#### Scenario: Correct a required value
- **WHEN** a previously ineligible candidate is re-uploaded with the same address and complete required data, without an active, submitted or uncertain attempt
- **THEN** the new snapshot is eligible and the earlier validation result remains available

#### Scenario: Address changes
- **WHEN** a new upload uses a different normalized address for the same name
- **THEN** it is treated as a new candidate key and the app does not infer identity from the name

#### Scenario: Explicit mailbox replacement reset
- **WHEN** the owner confirms mailbox replacement through its reset flow
- **THEN** that operator's candidates and task history are deleted as disclosed
- **AND** their campaigns and template revisions remain available for new uploads
- **AND** an ordinary corrected upload or same-mailbox reconnect cannot trigger that reset

