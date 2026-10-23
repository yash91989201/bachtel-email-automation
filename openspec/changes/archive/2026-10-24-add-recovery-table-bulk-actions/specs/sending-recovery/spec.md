# Spec Delta

## MODIFIED Requirements

### Requirement: Individual and bulk definite-failure retry
The system SHALL provide Retry for an eligible failed candidate, Retry selected for a confirmed selection of eligible failed candidates and Retry all failed, each with a refreshed count and explicit confirmation. All three SHALL create new attempts using the existing immutable task content and exclude submitted, invalid, duplicate, active and unresolved uncertain candidates.

#### Scenario: Mixed task outcomes
- **WHEN** the operator chooses Retry all failed
- **THEN** only eligible definite sending failures are queued under the owning mailbox's limits
- **AND** a candidate requiring data correction is directed to a corrected upload

#### Scenario: Candidate submitted through another action
- **WHEN** eligibility changes between retry preview and confirmation
- **THEN** the final atomic recheck excludes that candidate and refreshes the count

#### Scenario: Retry a confirmed selection
- **WHEN** the operator confirms Retry selected
- **THEN** only the selected eligible definite failures are queued under the owning mailbox's limits
- **AND** the confirmed count is the selected row count, and unselected failures stay unchanged

### Requirement: Explicit uncertain resend
The system SHALL offer Send again for each unresolved uncertain candidate and Send again selected for a confirmed selection of them. Both SHALL show the previous attempt details and possible duplicate-send consequence, require explicit confirmation and retain each earlier attempt.

#### Scenario: Confirm uncertain resend
- **WHEN** the operator confirms Send again
- **THEN** a new attempt uses the original personalized content and the owning mailbox's normal dispatch controls
- **AND** the review action is recorded without erasing the uncertain attempt

#### Scenario: Confirm a selected resend
- **WHEN** the operator confirms Send again selected
- **THEN** each selected candidate gets its own new attempt with its original personalized content and the owning mailbox's normal dispatch controls
- **AND** the duplicate-risk acknowledgement covers the confirmed selection and every earlier uncertain attempt stays recorded

### Requirement: Audited manual resolution
The system SHALL allow the operator to mark an uncertain candidate, or a confirmed selection of uncertain candidates, as already submitted after external verification, recording the actor, time and manual basis per candidate without inventing a provider-confirmed acceptance time.

#### Scenario: Verify in Outlook
- **WHEN** the operator marks a reviewed candidate already submitted
- **THEN** campaign eligibility excludes that candidate
- **AND** history distinguishes the manual resolution from a provider-confirmed response

#### Scenario: Resolve a confirmed selection
- **WHEN** the operator confirms Mark selected already submitted with one external-verification basis
- **THEN** every selected candidate records its own actor, resolution time and basis
- **AND** a candidate that is no longer uncertain refuses the whole request instead of resolving partially

