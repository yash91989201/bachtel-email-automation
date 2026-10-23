# Spec Delta

## ADDED Requirements

### Requirement: Campaign sections have direct paths
The system SHALL expose campaign sections at `/campaigns/{id}/tasks`, `/campaigns/{id}/template`, `/campaigns/{id}/activity` and `/campaigns/{id}/settings`. Each path SHALL directly render its matching section with the existing campaign shell and selected navigation state. Opening `/campaigns/{id}` without an explicit section path SHALL show Tasks.

#### Scenario: Direct campaign section visit
- **WHEN** the owner opens or reloads any campaign section path
- **THEN** the matching Tasks, Template & preview, Activity & downloads or Settings section is selected without requiring a fragment
- **AND** section navigation, campaign/task breadcrumbs, template remedies and campaign action return destinations use section paths
- **AND** opening a section neither creates a revision nor generates exports nor changes campaign data

#### Scenario: Campaign entry and invalid section
- **WHEN** the owner opens the campaign root without an explicit section path
- **THEN** Tasks is shown and new links to that section use its explicit path
- **WHEN** an unknown section path is requested
- **THEN** it returns not found instead of silently selecting another section

### Requirement: Task sections have direct paths
The system SHALL expose task sections at `/campaigns/{id}/tasks/{task}/{section}`, where section is `overview`, `recipients`, `test`, `sending` or `results`. Each path SHALL directly select its matching section in the existing task shell. The task root without an explicit section path SHALL show Overview. All entries SHALL enforce operator ownership and campaign-task membership.

#### Scenario: Direct task section visit
- **WHEN** the owner opens or reloads a task section path
- **THEN** its matching section is selected with the same task identity, status and section navigation
- **AND** breadcrumbs, next-action links, lifecycle remedies and task-operation return destinations use section paths
- **AND** `sending_type=choose` retains its existing view-only meaning on the Sending path without changing the saved type
- **AND** unavailable actions retain their existing prerequisite explanations

#### Scenario: Invalid or foreign task navigation
- **WHEN** an unknown section, another operator's task or a task under the wrong campaign is requested
- **THEN** the request reveals no private task content and is refused using the existing access/not-found policy
- **AND** navigation never sends, approves, finalizes data, reserves work, schedules, resumes, retries or generates an export

### Requirement: Sections use native page navigation
Campaign and task sections SHALL use ordinary links and server-rendered pages, rendering only the selected section's content with the shared identity and navigation. Section navigation SHALL work without JavaScript interception or custom browser-history handlers. Unsaved form values and disclosure choices SHALL NOT be guaranteed to persist across full-page section changes.

#### Scenario: Navigate and use browser history
- **WHEN** an operator follows a section link, opens it in another tab, reloads or uses Back/Forward
- **THEN** ordinary browser navigation reaches the corresponding server-rendered section
- **AND** the selected link is visibly and programmatically identified as the current page
- **AND** inactive section forms, panels and polling regions are absent from that response
- **AND** section navigation works with JavaScript disabled and does not require a client-side section router

#### Scenario: Section-local progress and rejected action
- **WHEN** an update arrives within the selected section or an operation is rejected
- **THEN** live updates preserve valid inputs, disclosure choices and keyboard focus within that page while retaining existing stale-confirmation rules
- **AND** a rejected operation renders its relevant section with safe error feedback and valid non-secret submitted inputs
- **AND** successful operations return to their corresponding section GET path without replaying the operation
- **AND** no client-side history rewrite is required on a rejected POST response

### Requirement: Fragments do not select workspace sections
The system SHALL use paths rather than URL fragments to select campaign/task sections. Old section fragments and aliases SHALL NOT require translation or custom navigation handlers. Legitimate accessibility, within-section anchor and disclosure targets SHALL remain available.

#### Scenario: Old fragment bookmark
- **WHEN** the owner opens a campaign or task root with an obsolete section fragment or legacy alias
- **THEN** the server renders its ordinary default section without interpreting that fragment as section selection
- **AND** an explicit section path retains its selected section regardless of obsolete section fragments
- **AND** generated navigation and action return links use section paths

#### Scenario: Anchors and existing operation endpoints
- **WHEN** an operator follows a within-section anchor, or uses an existing upload, action, progress or download endpoint
- **THEN** the anchor remains a within-page target and the endpoint retains its existing authorization and method contract
- **AND** no section navigation mutates task state or invokes a provider

## MODIFIED Requirements

### Requirement: One persistent task workspace
The system SHALL give each task one workspace with Overview, Recipients, Test & approve, Sending, and Results, defaulting to Overview. The header SHALL show the filename and derived status. Overview SHALL integrate status, next-step guidance and blocker remedies into the lifecycle tracker before Task details. Controls and results SHALL each have one canonical location. Section changes SHALL use normal links and full-page server rendering with native browser history rather than client-side tab switching.

#### Scenario: Operator opens and navigates through a task
- **WHEN** the operator opens a task without a section or moves between task sections
- **THEN** Overview is selected by default, the task name is shown without its task ID beside it, and a derived status badge replaces that adjacent ID
- **AND** the short task reference and full ID remain available in Overview details, while task lists retain their short reference
- **AND** the compact task-name/status-badge row, breadcrumbs and section navigation are rendered consistently on each section page while only that section's main content is included
- **AND** detailed identity/context, upload and revision metadata, sender/connection state, current status and blocking/wait reason, next-action sentence, and lifecycle strip appear in Overview content rather than a dense global header
- **AND** Overview is a summary section, not a lifecycle step; the lifecycle order remains Upload, Recipients, Test & approve, Sending, Results
- **AND** its content order is breadcrumbs; task name and status badge; section navigation; selected content
- **AND** the required default shell is:

```text
Campaigns > Campaign name > Tasks > Workbook name
Workbook name                                      [Ready to send]
Overview | Recipients | Test & approve | Sending | Results
----------------------------------------------------------
Overview
Upload (ⓘ)              Recipients (ⓘ)     Test & approve (ⓘ)    [Sending (ⓘ), primary color]   Results (ⓘ)
Stored                  Validated           Approved              Not started          Not available yet
[Choose sending time → Sending]
Current-stage tooltip: Sending still requires explicit start or schedule authorization.

Task details
Uploaded: date/time +05:30 | Saved revision: 2 | Worksheet: People
Sender: mailbox address (Connected) | Formatting: Approved
Technical details: short reference and full task ID
```

#### Scenario: Links and navigation preserve context
- **WHEN** the operator follows an existing task review/sending/results link, reloads a section, or uses browser Back/Forward
- **THEN** an entry without a section opens Overview; explicit section links open their corresponding section with linked campaign and Tasks breadcrumbs
- **AND** navigating alone never sends, approves, reserves, resumes, retries or generates an export
- **AND** section-local polling does not override the section selected by the page path
- **AND** Recipients orders its content as validation/setup or snapshot summary, preview, then row reasons/report; Test & approve orders it as approval summary, test submission, then received-format approval when needed
- **AND** Sending orders its content as current progress/wait reason, fresh scoped send/schedule/resume controls, applicable recovery groups, then Sending settings; Results orders it as workbook availability/actions, then task attempt history

#### Scenario: A section cannot yet perform its action
- **WHEN** a staging task opens Test & approve, Sending or Results
- **THEN** navigation remains understandable and the selected section explains “Validate the workbook in Recipients first” with a link
- **AND** unavailable candidate/test/generation controls are not presented as usable actions

### Requirement: Task results have one canonical location
The system SHALL place the task result workbook, export state, as-of time and task attempt history in Results. It SHALL distinguish unavailable, pending/generating, ready, failed and intentionally removed output, preserve automatic generation after completion/stop/interruption, and never equate export generation with sending or retrying email.

#### Scenario: Task has not reached result eligibility
- **WHEN** a task is staging, ready, running, waiting, paused or stopping without an existing ready artifact
- **THEN** Results explains that automatic generation follows completion, settled stop or recovered interruption and offers no usable generate/download action
- **AND** stopping specifically explains that active outcomes must settle first
- **AND** any recorded attempts can still be inspected in Results

#### Scenario: Eligible result generation changes state
- **WHEN** a task reaches completion, stopped or recovered-interrupted state
- **THEN** its automatically requested output is shown as “Preparing results” while pending/generating and no incomplete download is offered
- **AND** pending identifies durably queued/startup-rebuild work and generating identifies active generation in the available status details; neither is an ordinary failed generation
- **AND** ready output shows “Download results workbook” with its as-of time and source-workbook preservation scope
- **AND** ordinary failure shows its safe error plus “Retry result generation”, without automatically retrying an ordinary failure
- **AND** eligible absent/intentionally removed output offers “Generate results workbook” without implying that sending must be repeated

#### Scenario: Historical output exists during resumed work
- **WHEN** a previously generated workbook remains available while sending resumes
- **THEN** Results identifies it as a saved snapshot with its as-of time rather than current final results
- **AND** refresh/generation availability follows the same task eligibility rule in every entry point
- **AND** generated-file removal explains that input, history and eligibility remain intact, requires confirmation and allows later regeneration

#### Scenario: Attempt history is inspected
- **WHEN** the operator opens recorded task outcomes
- **THEN** Results shows test/candidate kind, recipient, attempt state, date/time +05:30 and available stored-email/resolution details
- **AND** Task history is a collapsed-by-default accordion with a full-width, keyboard-operable header and expansion indicator; opening it reveals the existing entries, resolutions and empty-state guidance
- **AND** its expansion choice survives updates within the Results page, and history remains accessible through its ordinary disclosure control without requiring legacy-fragment section selection
- **AND** “Submitted” is explained as accepted by the provider, not confirmed delivery
- **AND** test totals, manual resolutions and uncertain attempts are distinguished from provider-confirmed candidate submissions
