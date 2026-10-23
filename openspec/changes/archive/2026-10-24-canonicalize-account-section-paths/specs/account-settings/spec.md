# Spec Delta

## MODIFIED Requirements

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

## ADDED Requirements

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
