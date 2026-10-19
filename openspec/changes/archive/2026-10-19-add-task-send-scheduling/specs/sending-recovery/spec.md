# sending-recovery Delta

## ADDED Requirements

### Requirement: Missed schedule pauses for the operator
The system SHALL persist an unfired schedule across restarts. When a stored start time passed while no dispatcher owned the persistent data, it SHALL NOT start sending: it SHALL pause the task with an operator-visible reason and keep the schedule recorded as missed until the operator resumes the safe remainder or clears it.

#### Scenario: Downtime over the scheduled time
- **WHEN** the server starts after an unfired scheduled time passed
- **THEN** the task is paused with a missed-schedule reason
- **AND** no submission attempt is created for it

#### Scenario: Operator resumes after a missed schedule
- **WHEN** the operator reviews the fresh eligible count and confirms Resume
- **THEN** the safe remainder starts through the normal resume path
- **AND** the schedule does not fire again

#### Scenario: Restart before the scheduled time
- **WHEN** the server restarts before an unfired scheduled time
- **THEN** the same stored time still fires later
- **AND** the task is not paused for the restart alone
