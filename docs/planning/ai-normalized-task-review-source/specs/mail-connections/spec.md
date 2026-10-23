# Mail Connections Delta

## MODIFIED Requirements

### Requirement: Safe connection changes
The system SHALL authorize connection changes only for their owner. Switching, replacement or disconnect SHALL be refused while any account task is pending under the Account settings lock. Same-mailbox reconnect and verified credential repair SHALL remain available, stopping new launches before active credentials change. Active submissions SHALL settle or be classified within bounded deadlines. A disconnected mailbox SHALL remain exclusively claimed and its work paused until reconnect or replacement.

#### Scenario: Disconnect while requests are active
- **WHEN** an operator requests disconnect during dispatch or while another task in the account remains pending
- **THEN** disconnect is refused with named blocking tasks and credentials remain installed
- **AND** other operators' usable connections continue normally

#### Scenario: Competing start and connection change
- **WHEN** a start, scheduled fire or retry competes with a connection change
- **THEN** work cannot launch using credentials being replaced or removed

#### Scenario: Pending stopped task and same-mailbox repair
- **WHEN** a stopped or paused task prevents switching but its mailbox needs reconnection
- **THEN** the owner can verify the same sender and repair credentials under the existing settlement safeguards
- **AND** a different sender or provider remains blocked until all pending tasks complete or are explicitly closed

#### Scenario: Disconnect with no pending work
- **WHEN** every account task is completed or closed and outstanding operations have settled
- **THEN** the owner can explicitly disconnect without releasing the exclusive sender claim
- **AND** reconnect or replacement remains explicit

### Requirement: Replacement keeps templates and resets sending data
The system SHALL require explicit confirmation before replacing a mailbox and refuse activation until all account tasks are completed or explicitly closed and outstanding operations have settled. It SHALL preserve the operator's campaigns and template revisions but delete their candidates, tasks, uploads, approvals, attempts, resolutions, history and generated exports. It SHALL release the old claim, require fresh test approval, and preserve other operators' data.

#### Scenario: Review replacement consequences
- **WHEN** the operator reviews a validated replacement
- **THEN** the UI identifies the old and proposed mailbox and the exact retained and deleted data
- **AND** it explains that duplicate-prevention history is removed, so uploading those recipients again can allow new sends
- **AND** cancelling leaves the current connection and data unchanged

#### Scenario: Confirm replacement
- **WHEN** the operator confirms replacement, all account tasks are completed or closed, and outstanding operations have settled
- **THEN** the selected reset completes and the new exclusive mailbox becomes current
- **AND** campaigns, source-header suggestions, result-column settings, original template DOCX files and immutable revisions remain
- **AND** candidate sending requires a new upload, a successful test and explicit received-format approval for the new connection
- **AND** no previous sender history is retained or synthesized from the new mailbox

#### Scenario: Pending schedules and export generation
- **WHEN** replacement starts its confirmed reset
- **THEN** old scheduled work cannot fire and export generation cannot republish deleted artifacts
- **AND** pending old-account send actions are refused

#### Scenario: Reset interrupted
- **WHEN** reset or artifact deletion fails or the server stops before replacement completes
- **THEN** the affected workspace remains blocked from sending and publishing old artifacts until reset is completed
- **AND** recovery never reports a completed replacement while stale sending data remains accessible

#### Scenario: Replacement cannot discard pending tasks to bypass the lock
- **WHEN** any account task remains pending when replacement is validated or confirmed
- **THEN** replacement and its reset are refused with named blocking tasks
- **AND** the reset itself cannot be used to erase blockers and install the new mailbox

#### Scenario: Mailbox replacement leaves AI configuration intact
- **WHEN** an allowed mailbox replacement completes its existing confirmed data reset
- **THEN** the operator's confirmed AI connection remains configured
- **AND** AI connection changes never trigger this mailbox data reset
