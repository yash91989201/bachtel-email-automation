# Spec Delta

## ADDED Requirements

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
