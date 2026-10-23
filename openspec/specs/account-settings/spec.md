# account-settings Specification

## Purpose
Provide operator-owned mailbox and AI setup in one account workspace, with clear connection readiness and protection against changing connections while task work remains pending.

## Requirements

### Requirement: Account connection sections
The system SHALL provide protected Account settings containing Mailbox at `/account/mailbox` and AI connection at `/account/ai` for the current operator. Each section SHALL show its own confirmed configuration, readiness and applicable setup or repair actions. Campaign settings SHALL retain campaign-specific controls.

#### Scenario: Open account settings
- **WHEN** the operator opens Account settings
- **THEN** Mailbox and AI connection are independently accessible with visible selected states and labeled controls
- **AND** the existing mailbox setup entry points lead to the Mailbox section while valid authorization callbacks continue working
- **AND** opening either section does not change settings or start AI processing or email

#### Scenario: Account isolation
- **WHEN** an operator requests another operator's connection settings or a settings mutation
- **THEN** the request is refused without disclosing configuration, credentials or private blocking-task metadata

#### Scenario: Direct section paths
- **WHEN** the owner directly visits, reloads or uses Back/Forward to reach `/account/mailbox` or `/account/ai`
- **THEN** the matching Mailbox or AI connection section is selected without relying on a section fragment
- **AND** Account navigation uses those paths with a programmatically identifiable current section
- **AND** the AI section keeps the existing provider, discovery, exact-model validation and explicit confirmation workflow
- **AND** routing cannot change a confirmed configuration or bypass the pending-task lock

### Requirement: Unused Account landing URLs are unserved
The system SHALL serve Mailbox and AI connection directly at their section URLs without requiring a parent landing page. GET `/account`, `/account/connections` and the former mailbox landing URL `/connection` SHALL return 404 without redirecting. Existing mailbox callbacks and action/replacement endpoints SHALL retain their existing contracts.

#### Scenario: Request an unused landing URL
- **WHEN** a client requests `/account`, `/account/connections` or `/connection`, with or without old provider, notice or chooser query values
- **THEN** the response is 404 without a compatibility redirect or a rendered settings page
- **AND** no provider request or settings mutation occurs
- **AND** internal navigation points directly to `/account/mailbox` or `/account/ai`

#### Scenario: OAuth callback and mailbox replacement still work
- **WHEN** an existing mailbox callback succeeds, fails or requires explicit replacement confirmation
- **THEN** its single-use authorization checks still execute before navigating to the existing replacement flow or the canonical Mailbox section
- **AND** callback codes, state tokens and credentials are absent from the resulting Mailbox URL
- **AND** a callback is never treated as a generic landing-page redirect
- **AND** replacement still requires the existing operator confirmation and pending-task checks

### Requirement: Account links and operation feedback use section destinations
Mailbox setup links, provider choices, connection remedies and successful operation return destinations SHALL use `/account/mailbox`; AI setup links and operation returns SHALL use `/account/ai`. Rejected forms SHALL keep their relevant Account section, safe error and valid non-secret inputs. Within-page anchors SHALL remain distinct from section selection.

#### Scenario: Mailbox setup, repair or disconnect returns
- **WHEN** the operator completes a mailbox settings, connect, disconnect or replacement action
- **THEN** its ordinary Mailbox return destination is `/account/mailbox` with the applicable safe notice or provider state
- **AND** required external authorization and explicit replacement confirmation remain separate destinations
- **AND** mailbox links from global navigation, home, task remedies and provider setup lead to the canonical Mailbox section
- **AND** a rejected form still renders Mailbox with its safe error, valid non-secret inputs and applicable shared lock warning

#### Scenario: Provider setup remains an in-page disclosure
- **WHEN** Change provider or another setup link opens the provider chooser
- **THEN** its URL uses `/account/mailbox` with the existing view-only chooser state and optional `#provider-setup` anchor
- **AND** the anchor only reveals or scrolls to setup within Mailbox
- **AND** chooser navigation neither disconnects the current mailbox nor bypasses switching restrictions

#### Scenario: AI validation and confirmation returns
- **WHEN** an owner fetches models, validates or confirms AI settings
- **THEN** the existing POST action contracts and exact-configuration checks remain enforced
- **AND** full-page feedback and successful returns select `/account/ai`, with no mailbox-section fallback
- **AND** no secret is prefilled, copied into a URL or returned by discovery
- **AND** moving between Account sections alone never calls discovery, synthetic validation, preparation or email dispatch

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

### Requirement: Provider-first AI setup
Account settings SHALL offer OpenCode Go, Xiaomi MiMo and Custom compatible setup. Supported presets SHALL ask for API key and editable endpoint before fetching models, then offer model selection without a manual API-format control. Custom setup SHALL retain explicit endpoint, supported API format, exact model ID and API key controls. Discovery SHALL preserve the setup inputs without returning the secret in a server response.

#### Scenario: Edit a verified default
- **WHEN** the owner selects OpenCode Go or Xiaomi MiMo
- **THEN** the verified default endpoint is editable and can be restored
- **AND** the page explains model fetching and the separate synthetic validation action

#### Scenario: Inputs change during discovery
- **WHEN** provider, endpoint or API key changes after fetching or while a request is pending
- **THEN** the previous discovered selection is cleared or made unavailable
- **AND** a late response cannot replace the models for the current inputs

#### Scenario: Saved connection repair
- **WHEN** an owner reopens settings for a confirmed connection
- **THEN** its non-secret configuration remains visible and the saved secret is not prefilled
- **AND** same-configuration key repair remains available even if model discovery is unavailable
- **AND** discovery cannot bypass the pending-task switching lock or activate a different configuration

#### Scenario: Custom manual configuration
- **WHEN** the owner chooses Custom compatible endpoint
- **THEN** manual API-format and exact-model controls remain available without requiring model discovery
- **AND** the exact submitted configuration still requires validation and confirmation
