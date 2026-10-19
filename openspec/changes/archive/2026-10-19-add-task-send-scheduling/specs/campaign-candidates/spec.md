# campaign-candidates Delta

## MODIFIED Requirements

### Requirement: Atomic eligibility at start
The system SHALL atomically recheck and reserve candidate eligibility when accepting start, resume, retry or scheduled-start actions. Concurrent actions SHALL NOT create multiple active candidate submissions for the same campaign/email key.

#### Scenario: Two prepared tasks start together
- **WHEN** both contain the same still-eligible candidate
- **THEN** only one action reserves that candidate
- **AND** the other action reports the candidate as already scheduled with refreshed counts

#### Scenario: Scheduled start competes with an operator start
- **WHEN** a scheduled fire and a manual start for another task in the campaign reserve the same candidate at the same time
- **THEN** only one of them reserves that candidate
- **AND** the other excludes it and reports Already scheduled while its other candidates proceed
