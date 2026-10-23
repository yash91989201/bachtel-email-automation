# Spec Delta

## ADDED Requirements

### Requirement: Task preparation review separates corrections and findings
The task workspace SHALL present successful AI corrections separately from unresolved findings, and expose the visible application instructions with optional task-specific instructions during preparation.

#### Scenario: Review initial preparation
- **WHEN** the operator opens a task after initial preparation
- **THEN** the workspace shows successful corrections and unresolved findings in distinct groups
- **AND** the visible base instructions and saved task-specific instructions are available for review
- **AND** the operator can review mapped values and findings without authorizing any email action

#### Scenario: Preparation fails
- **WHEN** preparation fails or cannot complete
- **THEN** the workspace identifies the failure, preserves the task and instruction draft, and offers an explicit retry
- **AND** it does not present partial output as reviewed or final

### Requirement: Task finalization offers both review paths
The task workspace SHALL let the operator finalize prepared data by approving it as-is or uploading an edited final sheet. Both paths SHALL show the backend validation outcome and the accepted values before finalization is reported as successful.

#### Scenario: Approve prepared data as-is
- **WHEN** the operator chooses Approve as-is for the prepared data
- **THEN** the workspace shows validation results and an email preview for the resulting final snapshot
- **AND** it offers download of the accepted sheet without requiring a reupload

#### Scenario: Upload edited final sheet
- **WHEN** the operator uploads an edited final sheet
- **THEN** the workspace identifies that validation is deterministic and does not run AI or rewrite uploaded values
- **AND** task-level structural validation failure shows remedies without replacing the prior final snapshot
- **AND** on success it shows the accepted values, email preview and downloadable accepted sheet

#### Scenario: Final upload has mixed accepted and rejected entries
- **WHEN** a structurally valid final upload contains entries accepted by backend validation and entries rejected by required-value, fallback, recipient-address, duplicate, campaign-history or safety checks
- **THEN** the workspace identifies accepted entries in the new final snapshot and rejected entries with their reasons in a separate exclusion report
- **AND** it exposes no send action when zero entries are accepted

#### Scenario: Final upload fails structurally
- **WHEN** a final upload has missing mapped headers, corrupt content or exceeds task limits
- **THEN** the workspace explains the structural failure and does not replace the prior final snapshot
- **AND** it does not report partial acceptance

### Requirement: Dataset review and sending approvals remain distinct
The task workspace SHALL label dataset approval, exact-final-snapshot test and received-format approval, and candidate-send authorization as separate states and actions.

#### Scenario: Dataset is approved
- **WHEN** the operator approves a final dataset
- **THEN** the workspace identifies the accepted snapshot and its download
- **AND** it states that dataset approval alone does not approve email formatting, send a test, or authorize candidate sending or scheduling

#### Scenario: Test approval is bound to the exact final snapshot
- **WHEN** a final snapshot is replaced or a new final upload is accepted
- **THEN** the workspace requires a successful test of that exact snapshot, explicit approval of its received formatting, and the applicable campaign revision and mailbox generation
- **AND** it shows any previous snapshot-specific approval as no longer applicable
- **AND** candidate start or scheduling remains a separate explicit operator action

#### Scenario: Started task input is frozen
- **WHEN** candidate sending has started
- **THEN** the workspace prevents edits or replacement of preparation instructions and final sending values for that task
- **AND** it continues to show the frozen snapshot used for the task

### Requirement: Explicit task closure releases pending work
The owning operator SHALL be able to explicitly close a task without deleting its evidence. Closure SHALL prevent new work, cancel queued operations and enabled schedules, and settle in-flight operations before releasing its account-settings lock. Closed tasks SHALL NOT be reopened, prepared, finalized, tested, resumed, retried or resent.

#### Scenario: Close an abandoned task
- **WHEN** the operator confirms Close task for an unvalidated, failed-preparation, review-pending, paused or stopped task
- **THEN** closure becomes terminal after outstanding operations settle and the task no longer blocks connection changes
- **AND** the UI explains that future processing requires a new task and existing recipient-history checks still apply
- **AND** the task remains inspectable with downloads and recorded outcomes

#### Scenario: Close an active task
- **WHEN** the owner confirms closure while preparation, a test, candidate sending or a schedule is pending
- **THEN** new launches stop, queued work and enabled schedules are canceled and closing remains a blocker until in-flight operations settle
- **AND** late AI responses cannot select results and stale start, retry or schedule requests cannot launch work
- **AND** uncertain submissions remain uncertain and are not reported as canceled or successfully completed

#### Scenario: Closed task preserves audit evidence
- **WHEN** a closed task contains submitted, definite-failure or uncertain attempts
- **THEN** original inputs, final values, attempts, approvals and existing history/reservation protections remain intact
- **AND** manual outcome classification remains available without reopening the task or authorizing an email
- **AND** repeated valid closure requests are idempotent and foreign closure requests are refused

## MODIFIED Requirements

### Requirement: Connected mailbox setup is disclosed explicitly
The Account settings Mailbox section SHALL hide provider selection and connection forms on normal connected-state navigation and offer Change provider beside Disconnect. Pending-task restrictions SHALL be visible at both controls. Disconnected or reconnect-required mailboxes SHALL retain same-mailbox repair access.

#### Scenario: Change a connected provider
- **WHEN** the operator activates Change provider and no account task blocks changes
- **THEN** the existing provider chooser opens without disconnecting, changing credentials or replacing the mailbox
- **AND** provider selections and rejected connection forms remain visible during setup
- **AND** same-mailbox reconnect and explicit replacement confirmation retain their existing checks
- **AND** an unfinished replacement continues to block provider setup

#### Scenario: Connected settings are locked
- **WHEN** pending account tasks block a provider change or disconnect
- **THEN** those actions show the shared named-task warning and cannot mutate the connection
- **AND** same-mailbox credential repair remains accessible without exposing an unrestricted provider switch
