# excel-intake Delta

## MODIFIED Requirements

### Requirement: Review before explicit start
The system SHALL show total, eligible, invalid, duplicate, already-submitted, already-scheduled and review-required counts with row-specific reasons. It SHALL recheck the configuration, approval and campaign eligibility before accepting an explicit start or enabling a schedule, and again when a scheduled start fires.

#### Scenario: Configuration changes after upload
- **WHEN** campaign content changes between upload validation and the first start
- **THEN** the task must be revalidated and previewed against the new current revision and approved configuration before starting

#### Scenario: No eligible rows
- **WHEN** every row is excluded
- **THEN** the UI explains the exclusions, permits relevant downloads and does not offer a send action for zero emails

#### Scenario: Schedule offered with fresh counts
- **WHEN** a validated task has an approved test and eligible rows
- **THEN** the review shows the schedule control beside the same fresh counts as Start
- **AND** a task with no eligible rows offers neither Start nor a schedule

#### Scenario: Recheck at scheduled fire
- **WHEN** a schedule was enabled and the campaign configuration or approval changes before its time
- **THEN** the scheduled fire does not send from the stale snapshot
- **AND** the task returns to the review state that requires revalidation and approval
