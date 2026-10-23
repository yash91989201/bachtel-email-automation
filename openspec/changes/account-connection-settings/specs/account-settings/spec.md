# Account Settings

## Purpose

Provide operator-owned mailbox and AI setup in one account workspace, with clear connection readiness and protection against changing connections while task work remains pending.

## ADDED Requirements

### Requirement: Account connection sections
The system SHALL provide protected Account settings containing Mailbox and AI connection sections for the current operator. Each section SHALL show its own confirmed configuration, readiness and applicable setup or repair actions. Campaign settings SHALL retain campaign-specific controls.

#### Scenario: Open account settings
- **WHEN** the operator opens Account settings
- **THEN** Mailbox and AI connection are independently accessible with visible selected states and labeled controls
- **AND** the existing mailbox setup entry points lead to the Mailbox section while valid authorization callbacks continue working
- **AND** opening either section does not change settings or start AI processing or email

#### Scenario: Account isolation
- **WHEN** an operator requests another operator's connection settings or a settings mutation
- **THEN** the request is refused without disclosing configuration, credentials or private blocking-task metadata

### Requirement: Pending tasks lock connection switching
The system SHALL refuse changes to a confirmed AI provider, model, endpoint or inference configuration, and mailbox switching, replacement or disconnect, while any owning-account task is pending. Tasks remain pending until completed or explicitly closed, and outstanding task operations SHALL settle before release. The lock SHALL apply across all campaigns and to server-side actions.

#### Scenario: Pending task states
- **WHEN** an account has an uploaded, preparing, review-pending, ready, scheduled, running, waiting, stopping, paused, interrupted, failed-preparation or stopped task that is neither completed nor closed
- **THEN** both connection sections refuse switching even if that task is not actively making network requests
- **AND** disabling a button is not the only enforcement

#### Scenario: Last pending task ends
- **WHEN** all account tasks are completed or explicitly closed and no associated queued or in-flight operation remains
- **THEN** the connection-change lock is released
- **AND** release itself changes no connection, resumes no task and sends no email

#### Scenario: Initial setup and credential repair
- **WHEN** pending tasks exist but a connection has never been confirmed
- **THEN** initial verified setup for that missing connection remains available
- **WHEN** the existing configuration needs a same-configuration credential repair or same-mailbox reconnect
- **THEN** the owner can validate that repair without switching provider, model, endpoint or verified sender
- **AND** changing identity through a repair request is refused while the lock applies

### Requirement: Named blocking-task warning
Locked Account settings SHALL show the number and names of blocking tasks, linked to their workspaces with campaign and distinguishing task references. The warning SHALL state that mailbox and AI configuration changes require all pending tasks to complete or be explicitly closed. It SHALL preserve access to repair actions and task review.

#### Scenario: Multiple campaigns block settings
- **WHEN** two pending tasks from different campaigns prevent connection changes
- **THEN** both connection sections identify both tasks, their states and links
- **AND** matching workbook filenames remain distinguishable
- **AND** the operator can navigate to finish or close the work without losing access to saved connection status

### Requirement: Connection changes and task activation are atomic
The system SHALL prevent task creation, reactivation or queued work from racing a connection change. Stale settings forms and delayed validation callbacks SHALL NOT install a connection after pending work has appeared or overwrite a newer confirmed configuration. Reactivated work SHALL reacquire the account lock before being queued.

#### Scenario: Task appears during settings validation
- **WHEN** proposed connection settings validate while another request creates or reactivates a pending task
- **THEN** activation rechecks the account lock and expected current configuration
- **AND** either the change commits before the task binds its configuration or the change is refused, without mixed configuration or automatic task execution

#### Scenario: Completed task is retried
- **WHEN** an otherwise authorized recovery action reactivates a completed task
- **THEN** it verifies the task's mailbox and immutable sending scope and makes the task pending before queuing work
- **AND** a competing connection change cannot bypass the renewed lock
