# Spec Delta

## MODIFIED Requirements

### Requirement: Review before explicit start
The system SHALL show row counts and reasons in Recipients and fresh action scope in Sending. The saved task `sending_type` SHALL be the source of truth for its first-start view. GET requests SHALL NOT change or override it; `sending_type=choose` may open the chooser without changing the saved value. A submitted type change SHALL be persisted before controls for the new type are shown. Show only the selected form. Type changes never authorize dispatch or mutate schedules; explicit confirmations and server checks remain required.

### Requirement: Upload records initial sending preference
The system SHALL require a native Direct/Scheduled choice at task upload and persist it as optional task field `sending_type`. The saved value is durable task state and remains authoritative for selections and later changes before execution. Saving it creates no schedule, authorization or candidate action. Older tasks may have no value.

#### Scenario: New task stores a preference
- **WHEN** an operator uploads a workbook after selecting Direct or Scheduled
- **THEN** task creation stores that preference without a schedule time, schedule record or authorization
- **AND** an omitted or invalid choice is refused by server-side validation
- **AND** upload errors retain the submitted choice

#### Scenario: Sending reads the saved mode
- **WHEN** a staging or ready task opens Sending
- **THEN** the saved `sending_type` selects the corresponding controls regardless of URL query values
- **AND** an older task with no saved value may show the Direct/Scheduled chooser
- **AND** a `sending_type=choose` query may open that chooser without changing the saved field
- **AND** GET requests and navigation never write `sending_type`

#### Scenario: Change sending type before execution
- **WHEN** an operator submits Direct or Scheduled to the task sending-type POST while the task is staging or ready and has not started
- **THEN** the server persists the requested value before rendering or redirecting to its controls
- **AND** all task navigation, polling and list actions use the persisted value
- **AND** changing type does not start work, authorize dispatch, or create or modify a schedule
- **AND** a request to change to Direct is refused while an enabled or missed schedule remains, with an explicit Clear schedule remedy
- **AND** the schedule is never implicitly cleared or cancelled

#### Scenario: Sending type is immutable after start
- **WHEN** a task has started or is outside staging/ready first-start state
- **THEN** a type-change POST is refused and the existing state-specific Sending controls remain authoritative
- **AND** query values cannot change the saved type or expose first-start controls

#### Scenario: Started tasks ignore first-start preferences
- **WHEN** a task is running, waiting, stopping, paused, interrupted, stopped or completed
- **THEN** the saved type remains immutable and existing state-specific Sending controls remain visible

#### Scenario: Configuration changes after upload
- **WHEN** campaign content changes between upload validation and the first start
- **THEN** the task must be revalidated and previewed against the new current revision and approved configuration before starting
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

#### Scenario: GET chooser leaves saved type and schedule untouched
- **WHEN** an eligible unstarted task is opened with the `sending_type=choose` query
- **THEN** the chooser is shown without changing the saved type or schedule
- **AND** Direct or Scheduled becomes the saved selection only after the sending-type POST succeeds
- **AND** an enabled or missed schedule remains active until the operator explicitly clears it or confirms the existing Send now instead action
- **AND** a paused downtime-missed task retains its explicit safe-resume path and cannot use the first-start type to resume

#### Scenario: Recheck at scheduled fire
- **WHEN** a schedule was enabled and the campaign configuration or approval changes before its time
- **THEN** the scheduled fire does not send from the stale snapshot
- **AND** the task returns to the review state that requires revalidation and approval
- **AND** the unified workspace explains the blocked/missed start and links to its required remedy without silently starting another schedule
