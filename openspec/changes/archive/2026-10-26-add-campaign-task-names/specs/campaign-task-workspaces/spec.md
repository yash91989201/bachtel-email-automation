# Spec Delta

## MODIFIED Requirements

### Requirement: Campaign entry and section hierarchy
The system SHALL open a campaign on Tasks and expose Tasks, Template & preview, Activity & downloads, and Settings in that order. It SHALL distinguish a reusable campaign template from a task that processes one uploaded workbook. The campaign page SHALL show only the campaign's identity before section content; the current revision and formatting-approval state SHALL live in Template & preview rather than repeating in the header.

#### Scenario: Open a campaign with a content revision
- **WHEN** the operator opens an existing campaign without selecting a section
- **THEN** Tasks is selected and the page shows, in order, linked breadcrumbs, campaign name, section navigation, and task content
- **AND** the header carries no campaign introduction paragraph and no template/formatting summary line
- **AND** no new-revision form is open by default
- **AND** the required information hierarchy is:

```text
Campaigns > Campaign name
Campaign name                                      [Create revision]

Tasks | Template & preview | Activity & downloads | Settings
---------------------------------------------------------
Tasks                                      [Upload workbook]
Task | Status | Recipients | Next action
```

#### Scenario: Campaign list gives operational context
- **WHEN** the operator opens the campaign list
- **THEN** each campaign shows its linked name, current template revision or “No template”, formatting-approval state, task count, and number of tasks needing attention
- **AND** a task needs attention when it has a recorded validation error, an unstarted snapshot stale against the current revision, a missed schedule, a paused/interrupted execution state, current unresolved uncertain rows, currently retry-eligible definite failures or a failed export; each task counts at most once
- **AND** ordinary worksheet selection or a future unblocked schedule alone does not count as a failure/attention item
- **AND** the approval summary uses the current revision and current mailbox scope, rather than a content-only approval flag
- **AND** source-header suggestions are available in Template & preview rather than occupying a primary campaign-list column
- **AND** opening the campaign name opens Tasks

### Requirement: Task sections have direct paths
The system SHALL expose task sections at `/campaigns/{id}/tasks/{task}/{section}`, where section is `overview`, `recipients`, `test`, `sending`, `results` or `settings`. Each path SHALL directly select its matching section in the existing task shell. The task root without an explicit section path SHALL show Overview. All entries SHALL enforce operator ownership and campaign-task membership.

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

### Requirement: Recognizable task list entries
The system SHALL identify each task by its saved task name, linked to that task's Overview, and show only the derived status and the eligible count in its row. Each row SHALL offer one next-action link into the section the workspace rule chose and SHALL disclose remaining detail through keyboard-reachable per-cell tooltips. Header-row and worksheet details stay inside the task instead of displacing operational information in its list.

#### Scenario: Row detail is progressively disclosed
- **WHEN** a task row is shown
- **THEN** the Task column shows the saved task name as its linked label, and its tooltip shows the original workbook filename, short task reference, upload date/time and saved revision when available
- **AND** the status tooltip shows the status explanation and any attention flag
- **AND** the recipients tooltip shows original source-row count, expanded recipient-entry count, submitted recipients and recipient entries needing attention
- **AND** each tooltip opens for pointer and keyboard focus and contains no interactive controls

#### Scenario: Two uploads use the same filename
- **WHEN** a campaign contains two tasks uploaded from recipients.xlsx, with no custom task names supplied
- **THEN** each row's task-name tooltip shows the distinguishing upload time and task reference without opening the task
- **AND** the task name opens that exact task at its Overview, while the next action opens the chosen task section
- **AND** the task name and filename are displayed as text and cannot supply active markup or a storage path

#### Scenario: Two tasks use the same name
- **WHEN** a campaign contains two tasks named October outreach, whether their workbook filenames match or differ
- **THEN** both tasks keep that name and each row's tooltip shows its source filename, upload time and task reference
- **AND** each name link still opens that exact task using its stable ID

#### Scenario: Task counts are not yet available
- **WHEN** a workbook has been uploaded but has no successful validation snapshot
- **THEN** its list entry shows “Needs worksheet selection” or “Validation failed” and “Not validated” for recipients rather than zero eligible recipients
- **AND** its action says “Validate workbook” and opens Recipients when a valid template exists; otherwise it says “Add template” and opens campaign Template & preview

#### Scenario: Ready or active task appears in a list
- **WHEN** a task has a validation snapshot
- **THEN** its Recipients cell shows the eligible count, and its tooltip distinguishes original source-row count, expanded recipient-entry count, submitted recipients and recipient entries needing attention
- **AND** its Status cell shows the derived status, and its tooltip carries the status explanation and any attention flag
- **AND** “Needs attention” counts distinct affected recipient entries rather than source rows or overlapping exclusion categories
- **AND** the next-action link uses the workspace action-selection rules, such as “Test & approve”, “View schedule”, “View progress”, “Resume sending”, “Review outcomes” or “View results”

### Requirement: Workbook upload and selection remain guided
The system SHALL show campaign context throughout workbook upload and worksheet/header selection. It SHALL put common input constraints and required mapped header names before technical details. Upload SHALL require a native Direct/Scheduled preference choice and retain it on error. Upload and validation SHALL remain separate explicit actions, with no candidate sending caused by either action.

#### Scenario: Operator opens Upload workbook
- **WHEN** a campaign opens the upload page
- **THEN** the page shows campaign/task-list breadcrumbs, the campaign name and template revision or “No template”, a labeled file input, a labeled “Task name (optional)” input with workbook-name default guidance, “Upload workbook”, and “Cancel”
- **AND** visible guidance gives XLSX format, the effective upload-size limit in readable units and the 1,000-recipient-entry bound after comma splitting, counted before invalid/duplicate exclusions
- **AND** a visible example explains comma-separated recipient cells, copied row details and per-email exclusions
- **AND** when a revision exists it lists all mapped column names as mandatory, including optional-value mappings whose columns are mandatory
- **AND** a required native Direct/Scheduled radio group has no implicit selection and its selected value is retained if upload fails
- **AND** readable submitted task-name text is retained if upload fails; file reselection is requested when needed
- **AND** the saved preference creates no schedule time, authorization or candidate action
- **AND** without a revision it explains that an upload can be staged now, links to Add template and does not claim that mapped header names or validation are available yet
- **AND** archive/XML limits, cached-formula behavior and unsupported-input details remain accessible under “Technical limits”

#### Scenario: Worksheet or header validation fails
- **WHEN** an uploaded task has missing/ambiguous headers or an invalid worksheet/header selection
- **THEN** Recipients shows the worksheet/header form, the submitted selection and a specific error with the offending header names and remedy
- **AND** its primary form action is “Validate workbook” and later sections explain their unmet prerequisites
- **AND** no successful validation, approval or candidate sending is implied

#### Scenario: Staged workbook has no campaign template yet
- **WHEN** an uploaded task is opened before its campaign has a valid template
- **THEN** Recipients explains that the workbook is stored, offers Add template and retains its worksheet/header choices
- **AND** validation is unavailable with the reason “Add a template before validating this workbook”
- **AND** after template creation the same staged workbook can be validated without another upload

### Requirement: One persistent task workspace
The system SHALL give each task one workspace with Overview, Recipients, Test & approve, Sending, Results, and Settings, defaulting to Overview. The header SHALL show the saved task name and derived status. Overview SHALL integrate status, next-step guidance and blocker remedies into the lifecycle tracker before Task details. Controls and results SHALL each have one canonical location. Section changes SHALL use ordinary links and full-page server rendering with native browser history.

#### Scenario: Operator opens and navigates through a task
- **WHEN** the operator opens a task without a section or moves between task sections
- **THEN** Overview is selected by default, the task name is shown without its task ID beside it, and a derived status badge replaces that adjacent ID
- **AND** the short task reference and full ID remain available in Overview details, while task lists retain their short reference
- **AND** the compact task-name/status-badge row, breadcrumbs and section navigation are rendered consistently on each section page while only that section's main content is included
- **AND** Overview Task details shows the original workbook filename separately from the editable task name
- **AND** detailed identity/context, upload and revision metadata, sender/connection state, current status and blocking/wait reason, next-action sentence, and lifecycle strip appear in Overview content rather than a dense global header
- **AND** Overview is a summary section, not a lifecycle step; the lifecycle order remains Upload, Recipients, Test & approve, Sending, Results
- **AND** its content order is breadcrumbs; task name and status badge; section navigation; selected content
- **AND** the required default shell is:

```text
Campaigns > Campaign name > Tasks > Task name
Task name                                      [Ready to send]
Overview | Recipients | Test & approve | Sending | Results | Settings
----------------------------------------------------------
Overview
Upload (ⓘ)              Recipients (ⓘ)     Test & approve (ⓘ)    [Sending (ⓘ), primary color]   Results (ⓘ)
Stored                  Validated           Approved              Not started          Not available yet
[Choose sending time → Sending]
Current-stage tooltip: Sending still requires explicit start or schedule authorization.

Task details
Workbook: original workbook filename
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

#### Scenario: Settings is available before and after processing
- **WHEN** the owner opens a task in any lifecycle state, including staging, running, completed, closing or closed
- **THEN** Settings appears after Results in task navigation and contains the name-editing form
- **AND** Settings is not a lifecycle step and does not replace any existing processing section or action
- **AND** opening Settings is read-only and does not require a validated workbook or template

## ADDED Requirements

### Requirement: Optional task name at creation
The system SHALL let the owning operator supply an optional task name when creating a workbook task. It SHALL trim surrounding whitespace and use the stored workbook filename, including its extension, when the name is omitted or whitespace-only. It SHALL accept duplicate names and up to 255 Unicode code points, and reject longer resolved names without creating a task.

#### Scenario: Operator supplies a descriptive name
- **WHEN** the owner uploads recipients.xlsx with task name “  October outreach  ”
- **THEN** the new task's saved name is “October outreach” and its original workbook filename remains recipients.xlsx
- **AND** the name persists across reloads and appears in the campaign task list, task heading and task breadcrumb
- **AND** naming grants no approval or authorization to process or send

#### Scenario: Operator leaves the name blank
- **WHEN** the owner uploads recipients.xlsx with no task name or only surrounding whitespace
- **THEN** the new task's saved name is recipients.xlsx
- **AND** a browser-supplied path is not included in the name

#### Scenario: Name validation and upload failure
- **WHEN** a resolved task name exceeds 255 Unicode code points
- **THEN** creation is refused with a clear name-length error and no partial task
- **AND** no silently truncated name is saved
- **WHEN** a valid name accompanies a missing, invalid or rejected workbook or sending preference
- **THEN** the upload form preserves readable submitted name text and other valid non-file input with actionable feedback
- **AND** no task is reported as created

#### Scenario: Unicode and markup-like names
- **WHEN** the owner supplies a name containing non-ASCII characters or markup-like text within the length limit
- **THEN** the name is stored as text and shown safely as text in lists, forms, headings and breadcrumbs
- **AND** names wrap without causing whole-page overflow, and the full name remains inspectable
- **AND** exactly 255 Unicode code points are accepted while 256 are refused

### Requirement: Existing tasks receive workbook-name defaults
Existing tasks without a saved name SHALL receive their stored original workbook filename as their task name, including its extension, across all lifecycle states. If that filename is missing or whitespace-only, the name SHALL be task.xlsx. Backfilling SHALL preserve already-populated names, task identity, source data, upload time, processing state, approvals, schedules and recorded history.

#### Scenario: Upgrade existing tasks
- **WHEN** the change is applied to existing unnamed tasks, including active, completed and closed tasks
- **THEN** each receives its own stored workbook filename as its saved name, or task.xlsx if that filename is unavailable
- **AND** task links still address the same tasks and no processing or email action is triggered
- **AND** existing evidence, configuration and history remain intact

#### Scenario: Populated names are preserved
- **WHEN** the backfill operation encounters a task that already has a nonblank name, including on a repeated run
- **THEN** it retains that name rather than replacing it with the workbook filename
- **AND** ordinary reads and page navigation do not perform backfill writes

### Requirement: Owner can rename a task in Settings
The owning operator SHALL be able to edit the task name in Settings in any lifecycle state. Saving SHALL apply the same whitespace, default, duplicate-name and length rules as creation. A successful save SHALL persist the new name for subsequent task/list views, provide visible success feedback and preserve the workbook filename, task identity, processing state, approvals, schedules and history. A refused save SHALL leave the previous name intact.

#### Scenario: Save a new task name
- **WHEN** the owner opens Settings and saves “November follow-up” for an existing task
- **THEN** the form initially shows its current name and explains the workbook-name default and length limit
- **AND** success returns to Settings with save feedback and the updated name in the shared heading and breadcrumb
- **AND** subsequent task-section and campaign-list page loads show “November follow-up”
- **AND** refreshing Settings does not replay the update

#### Scenario: Restore workbook-name default
- **WHEN** the owner saves an empty or whitespace-only name for a task whose source workbook is recipients.xlsx
- **THEN** recipients.xlsx becomes its saved name
- **AND** the source workbook and its stored filename remain unchanged

#### Scenario: Rename active or terminal task
- **WHEN** the owner renames a running, scheduled, completed, closing or closed task
- **THEN** the metadata save succeeds without stopping or reopening the task or modifying its processing state
- **AND** it neither clears nor changes a schedule, invalidates approval, reruns preparation, regenerates results, nor sends or retries an email
- **AND** concurrent processing updates and recorded outcomes remain intact

#### Scenario: Invalid input or storage failure
- **WHEN** a rename exceeds the length limit or cannot be saved
- **THEN** Settings remains selected with the submitted text, a safe error and a retry/correction remedy
- **AND** the prior saved name remains authoritative and no success is reported

#### Scenario: Unauthorized task settings access
- **WHEN** an anonymous user, another operator, or an owner using a task under the wrong campaign requests Settings or submits a name update
- **THEN** the existing access/not-found policy refuses the request without revealing private task metadata or modifying a task
- **AND** names are never used as an authorization key or to replace stable task identifiers
