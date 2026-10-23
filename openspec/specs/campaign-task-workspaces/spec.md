# campaign-task-workspaces Specification

## Purpose
Make campaign and workbook-task operation understandable through consistent page structure, explicit status meanings, applicable actions and accessible navigation, while retaining the existing approval, sending, recovery and export contracts.

## Requirements

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
Workbook | Status | Recipients | Next action
```

#### Scenario: Campaign list gives operational context
- **WHEN** the operator opens the campaign list
- **THEN** each campaign shows its linked name, current template revision or “No template”, formatting-approval state, task count, and number of tasks needing attention
- **AND** a task needs attention when it has a recorded validation error, an unstarted snapshot stale against the current revision, a missed schedule, a paused/interrupted execution state, current unresolved uncertain rows, currently retry-eligible definite failures or a failed export; each task counts at most once
- **AND** ordinary worksheet selection or a future unblocked schedule alone does not count as a failure/attention item
- **AND** the approval summary uses the current revision and current mailbox scope, rather than a content-only approval flag
- **AND** source-header suggestions are available in Template & preview rather than occupying a primary campaign-list column
- **AND** opening the campaign name opens Tasks

### Requirement: Recognizable task list entries
The system SHALL identify each task by its original workbook filename, linked to that task's Overview, and show only the derived status and the eligible count in its row. Each row SHALL offer one next-action link into the section the workspace rule chose and SHALL disclose remaining detail through keyboard-reachable per-cell tooltips. Header-row and worksheet details stay inside the task instead of displacing operational information in its list.

#### Scenario: Row detail is progressively disclosed
- **WHEN** a task row is shown
- **THEN** the workbook tooltip shows the short task reference, upload date/time and saved revision when available
- **AND** the status tooltip shows the status explanation and any attention flag
- **AND** the recipients tooltip shows original source-row count, expanded recipient-entry count, submitted recipients and recipient entries needing attention
- **AND** each tooltip opens for pointer and keyboard focus and contains no interactive controls

#### Scenario: Two uploads use the same filename
- **WHEN** a campaign contains two tasks uploaded from recipients.xlsx
- **THEN** each row's workbook tooltip shows the distinguishing upload time and task reference without opening the task
- **AND** the workbook name opens that exact task at its Overview, while the next action opens the chosen task section
- **AND** the filename is displayed as text and cannot supply active markup or a storage path

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

### Requirement: Template editing is explicit
The system SHALL show the current subject, revision, recipient-column selection, mappings and formatting-approval scope before offering revision creation. Template preview and revision history SHALL live in Template & preview. Creating or editing a revision SHALL remain explicit and preserve existing variable syntax, draft mapping and immutable revision behavior.

#### Scenario: First template versus existing template
- **WHEN** a campaign has no revision
- **THEN** Tasks explains “Add an email template to validate recipient workbooks” with an “Add template” link as the primary setup action
- **AND** Upload workbook remains a secondary staging action; upload and existing upload links may store a workbook, but validation cannot succeed without a valid template
- **AND** Template & preview opens its initial revision form with the existing mapping draft rules
- **WHEN** a campaign already has a revision
- **THEN** Template & preview shows its current configuration followed by “Create revision”, sample preview and revision history
- **AND** the new-revision form opens only after choosing Create revision and follows the existing empty-form rule for later revisions

#### Scenario: Revision change affects a prepared task
- **WHEN** a newer campaign revision exists for a task that has not started
- **THEN** that task identifies its saved revision and the current revision, explains that revalidation is required, and offers “Revalidate with current template”
- **AND** a running task continues to identify and use its saved revision without being presented as needing an in-place content edit
- **AND** no list, tab selection or preview silently changes a task snapshot

#### Scenario: Prepared task has an enabled schedule while content changes
- **WHEN** the operator requests revalidation of an unstarted task with an enabled future schedule
- **THEN** the workspace first requires “Clear schedule” with an explanation that the old authorization must not authorize revised content
- **AND** a revalidation request is refused while that schedule remains enabled, including from an old form
- **AND** after clearing, the operator explicitly revalidates and obtains applicable formatting approval before choosing Send now or confirming a new schedule
- **AND** no old schedule is silently transferred to the new revision

#### Scenario: Mapping guidance stays usable
- **WHEN** the revision form is open
- **THEN** visible guidance outside placeholders explains the mapping format, exactly one recipient role, and matching variable names
- **AND** a worked example shows a placeholder in a subject/body position, its mapping entry and the rendered value
- **AND** failed submission preserves the supplied text and identifies every invalid variable or mapping; a DOCX that must be selected again is explicitly identified

### Requirement: Campaign activity and settings have distinct purposes
The system SHALL place campaign-wide reports, packages and attempt history in Activity & downloads, and result-column settings and campaign deletion in Settings. It SHALL distinguish campaign exports from a task's result workbook and generated-file removal from campaign/history deletion.

#### Scenario: Campaign activity is opened
- **WHEN** the operator opens Activity & downloads
- **THEN** the section presents campaign export state/actions followed by recorded attempt history with task, revision, kind, recipient, outcome, timestamp and historical-email link
- **AND** tests are labeled separately and excluded from candidate totals
- **AND** revision history is accessed in Template & preview
- **AND** the page explains export scope and as-of time, and shows generation failures without claiming incomplete files are ready

#### Scenario: Destructive action is separated from normal operation
- **WHEN** the operator opens Settings or a generated-file removal confirmation
- **THEN** ordinary label settings, generated-file removal and campaign/history deletion have distinct scopes and confirmations
- **AND** campaign deletion explains removal of duplicate-prevention history and its active-work refusal rules
- **AND** no delete action is promoted as the next workflow action

#### Scenario: No task or no campaign activity exists
- **WHEN** a valid-template campaign has no uploaded tasks
- **THEN** Tasks says “No tasks yet”, explains that each workbook creates a separate task and offers Upload workbook
- **WHEN** Activity & downloads has no recorded attempts or generated exports
- **THEN** it says “No email attempts recorded” and “Downloads not generated”, without implying a failure or generating files merely by opening the section

### Requirement: Workbook upload and selection remain guided
The system SHALL show campaign context throughout workbook upload and worksheet/header selection. It SHALL put common input constraints and required mapped header names before technical details. Upload SHALL require a native Direct/Scheduled preference choice and retain it on error. Upload and validation SHALL remain separate explicit actions, with no candidate sending caused by either action.

#### Scenario: Operator opens Upload workbook
- **WHEN** a campaign opens the upload page
- **THEN** the page shows campaign/task-list breadcrumbs, the campaign name and template revision or “No template”, a labeled file input, “Upload workbook”, and “Cancel”
- **AND** visible guidance gives XLSX format, the effective upload-size limit in readable units and the 1,000-recipient-entry bound after comma splitting, counted before invalid/duplicate exclusions
- **AND** a visible example explains comma-separated recipient cells, copied row details and per-email exclusions
- **AND** when a revision exists it lists all mapped column names as mandatory, including optional-value mappings whose columns are mandatory
- **AND** a required native Direct/Scheduled radio group has no implicit selection and its selected value is retained if upload fails
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
The system SHALL give each task one workspace with Overview, Recipients, Test & approve, Sending, and Results, defaulting to Overview. The header SHALL show the filename and derived status. Overview SHALL integrate status, next-step guidance and blocker remedies into the lifecycle tracker before Task details. Controls and results SHALL each have one canonical location.

#### Scenario: Operator opens and navigates through a task
- **WHEN** the operator opens a task without a section or moves between task sections
- **THEN** Overview is selected by default, the task name is shown without its task ID beside it, and a derived status badge replaces that adjacent ID
- **AND** the short task reference and full ID remain available in Overview details, while task lists retain their short reference
- **AND** the compact task-name/status-badge row, breadcrumbs and section navigation remain in place while the selected section's main content changes
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
- **AND** polling does not override an explicitly selected section
- **AND** Recipients orders its content as validation/setup or snapshot summary, preview, then row reasons/report; Test & approve orders it as approval summary, test submission, then received-format approval when needed
- **AND** Sending orders its content as current progress/wait reason, fresh scoped send/schedule/resume controls, applicable recovery groups, then Sending settings; Results orders it as workbook availability/actions, then task attempt history

#### Scenario: A section cannot yet perform its action
- **WHEN** a staging task opens Test & approve, Sending or Results
- **THEN** navigation remains understandable and the selected section explains “Validate the workbook in Recipients first” with a link
- **AND** unavailable candidate/test/generation controls are not presented as usable actions

### Requirement: Lifecycle strip reflects evidence
The system SHALL show ordered Upload, Recipients, Test & approve, Sending and Results stages. Completion SHALL reflect saved evidence, not visits; validation, approval, execution and artifact availability remain distinct. Each stage SHALL show its name, unchanged state and any blocker remedy. Tooltips SHALL be accessible by keyboard/touch and contain no controls. Only the recommended stage has a `NextLabel`/`NextHref` navigation button; tracker use SHALL never mutate state.

#### Scenario: Overview tracker provides stage guidance and navigation
- **WHEN** Overview displays the lifecycle tracker
- **THEN** each non-current stage has concise generic tooltip guidance; the current stage tooltip shows `StatusExplanation` instead
- **AND** each stage has a 16px info icon in a 24px hit area, and tooltips are at most 240px wide with 11px text
- **AND** each unchanged stage state is followed by its matching `blockerLabel` and `Remedy` navigation link when available
- **AND** only the recommended stage has a navigation button, using `NextLabel`/`NextHref`; all other stages remain navigable through the sidebar
- **AND** an external template or mailbox prerequisite is assigned to Recipients or Sending respectively, and its primary external link has a secondary Open section link
- **AND** the recommended stage name uses the primary color without a Next badge; Upload's section destination is Overview
- **AND** the existing tooltip supports keyboard and touch and contains no interactive controls
- **AND** completion evidence markers remain visible alongside the recommended-stage color emphasis
- **AND** tooltips, emphasis and tracker navigation never submit a form or change task, approval, schedule, sending or result state

#### Scenario: Approved but unstarted task
- **WHEN** a task is validated and has applicable formatting approval but has not been started or scheduled
- **THEN** Upload is complete, Recipients says “Validated”, Test & approve says “Approved”, Sending says “Not started” and Results says “Not available yet”
- **AND** visiting Recipients or Test & approve is not recorded or described as a new human approval
- **AND** the strip permits returning to section content without starting work

#### Scenario: Interrupted sending has a downloadable result
- **WHEN** an interrupted task has uncertain rows and a ready result workbook
- **THEN** Sending says “Interrupted” and Results says “Available”
- **AND** neither the strip nor a completed step implies all recipients were submitted or uncertainty was resolved

### Requirement: Status labels have explicit meanings
The system SHALL display task execution, formatting approval, schedule and artifact status as separate labeled facts. It SHALL use the status vocabulary and precedence below, show blocking/wait reasons beside status, and never use an unexplained “Ready”, “Approved”, “Failed” or “Complete” to describe multiple independent states.

#### Scenario: Derive the task's main status
- **WHEN** a task is rendered
- **THEN** the following table determines its main status; row-specific outcomes and artifact problems remain separately visible:

| Condition, evaluated in this order | Main status | Required explanation |
| --- | --- | --- |
| Workbook awaiting selection with a recorded validation error | Validation failed | Specific worksheet/header/input error |
| Workbook awaiting selection without an error | Needs worksheet selection | Choose worksheet and header row |
| Execution is stopping | Stopping | No new sends; waiting for active requests to settle |
| Execution is running | Sending | Current durable progress |
| Execution is waiting | Waiting | Pacing/cooldown/provider reason and resume time when known |
| An unfired schedule is recorded as missed | Missed scheduled start | Stored time, reason and required operator action |
| An unstarted snapshot differs from the current template | Needs revalidation | Saved and current revision numbers |
| An unfired future schedule is enabled | Scheduled | Stored date/time +05:30; whether any current blocker prevents its start |
| Execution is paused | Paused | Recorded reason; reconnecting alone does not resume |
| Execution was interrupted and recovered | Interrupted | Safe remainder and uncertain rows are distinct |
| Execution is stopped | Stopped | Settled outcome summary and safe remainder |
| Execution is completed | Finished | Submitted, failed, held and remaining breakdown; never “Delivered” |
| No eligible/safe-start recipients remain in an unstarted validated task | No eligible recipients | Exclusions and relevant downloads |
| An unstarted validated task lacks applicable formatting approval | Needs formatting approval | Exact saved content and current mailbox scope |
| Otherwise, a validated unstarted task has eligible recipients and approval | Ready to send | Sending still requires explicit start or schedule authorization |

#### Scenario: A blocker coexists with another status
- **WHEN** restore review, an unusable mailbox, stale content, an invalid approval or a provider cooldown prevents an otherwise relevant action
- **THEN** the primary execution/schedule status is retained and a nearby labeled blocker names the actual cause and remedy
- **AND** restore review is the highest-priority blocking banner, followed by mailbox authorization and content/approval problems, with every applicable blocker still discoverable
- **AND** “Formatting: Approved” always means approval applies to the saved content and current mailbox, not merely that some campaign revision was approved
- **AND** “Already scheduled” on a recipient is explained as claimed by another task, separate from this task's future scheduled start

### Requirement: Recipient review presents understandable totals
The system SHALL put a concise validation summary, fresh eligibility distinction, recipient preview and row-specific exclusions in Recipients. All existing counts and exclusion reports SHALL remain available. Summary totals SHALL not double-count rows or present historical validation counts as a guarantee of present sending eligibility.

#### Scenario: Mixed recipient validation
- **WHEN** validation contains eligible, duplicate, invalid, missing-value, already-submitted, already-scheduled and review-required entries
- **THEN** the section first shows Source rows, Recipient entries, Eligible at validation, and Excluded at validation
- **AND** it shows the current eligible/safe-remaining count separately with “Rechecked before sending” guidance
- **AND** Excluded at validation equals recipient entries minus eligible-at-validation entries; it includes all non-eligible categories, not only invalid/duplicate entries
- **AND** a labeled breakdown retains total, eligible, ineligible/missing-data, duplicate, invalid address, already submitted, already scheduled and review-required counts as a mutually exclusive partition whose categories sum to the recipient-entry total
- **AND** several reason texts can apply to one entry without adding that entry to the summary more than once
- **AND** row tables expose original worksheet row numbers, position within the recipient cell, current state and all recorded reasons; excluded entries remain visible even when they have no candidate identity
- **AND** “Download exclusion report” is available immediately after validation, including when no entry is eligible
- **AND** corrected data is uploaded as a new task; the original snapshot is not edited in place

#### Scenario: Zero attention categories
- **WHEN** validation has one eligible entry and no exclusions
- **THEN** the summary shows one source row, one recipient entry, one eligible at validation and no exclusions
- **AND** zero-valued breakdown categories remain accessible without occupying eight equally prominent metric cards

### Requirement: Preview is recognizable and read only
The system SHALL show task recipient preview within Recipients, with a recognizable address or mapped name alongside the worksheet row. It SHALL use the saved revision and show To, Subject and Body before technical comparison details. Preview SHALL not create attempts, grant approval or start candidate work.

#### Scenario: First previewable row is available
- **WHEN** a validated task opens Recipients with at least one previewable row
- **THEN** the first such row is selected and its preview is rendered without a separate initial “Render preview” submission
- **AND** selecting another previewable row refreshes its content with a visible loading state
- **AND** raw/normalized values, saved revision and full content hash remain available under “Preview details”

#### Scenario: Preview fails or no row is previewable
- **WHEN** a row cannot be rendered or no previewable row exists
- **THEN** the preview area explains the reason and offers a preview-only retry or a link to row validation issues
- **AND** it does not show another recipient's previous preview as the newly selected recipient's email

### Requirement: Formatting approval is scoped and actionable
The system SHALL place diagnostic tests and received-format approval in Test & approve. It SHALL show current sender, saved revision, applicable approval state and qualifying test evidence. Successful test submission alone SHALL not grant approval, and reusable approval SHALL not be misrepresented as missing merely because this task has no test of its own.

#### Scenario: Exact content is not yet approved
- **WHEN** a validated task lacks approval for its saved configuration and current mailbox
- **THEN** the section shows a sample-row selector and labeled test-recipient field followed by “Send formatting test”
- **AND** after a qualifying submitted test it offers explicit selection of that test, the received-format confirmation and “Approve received formatting”
- **AND** pending, failed or uncertain tests cannot be selected as successful evidence
- **AND** a full content hash is available in details rather than the main instruction

#### Scenario: Applicable approval already exists
- **WHEN** another task established valid approval for the same saved configuration and current mailbox scope
- **THEN** this section shows “Formatting approved”, the revision, mailbox and recorded evidence/time, with “Send another test” as a secondary action
- **AND** it does not require another test or approval just to advance this task
- **AND** changing personalized content or replacing the mailbox invalidates the applicable approval; verified same-mailbox application-settings rotation does not by itself invalidate it

#### Scenario: Tests remain independent of candidate work
- **WHEN** the operator explicitly sends a test for a paused/stopped task with a usable mailbox and previewable sample
- **THEN** the test has its own attempt and never resumes or marks a candidate submitted
- **AND** candidate sending remains a separate explicitly authorized action

### Requirement: Sending controls follow the execution state
The system SHALL put fresh action counts, sender, saved revision, approval and exclusions beside start/schedule/recovery controls in Sending. It SHALL use the action matrix below, omit irrelevant operations and explain unmet prerequisites for relevant operations. Every accepted candidate action SHALL retain existing authorization, atomic eligibility checks and repeat-request protection.

#### Scenario: Render the applicable candidate controls
- **WHEN** the operator opens Sending
- **THEN** the available controls follow this matrix, with every sending/retry/resend operation additionally subject to applicable approval, mailbox, restore-review and positive fresh-count checks:

| State/context | Controls to present | Controls to omit |
| --- | --- | --- |
| Staging or unsuccessful validation | Link to Validate workbook | Candidate actions and scheduling |
| Unstarted snapshot needs revalidation with no enabled future schedule | Revalidate with current template | Start, schedule and recovery dispatch |
| Validated unstarted task; no enabled/missed schedule and snapshot current | Show the controls selected by saved `sending_type`; chooser only for older tasks with no saved value or an explicit `choose` query | The unselected first-start form, Resume, Stop and zero-count retries |
| Future schedule enabled | Edit schedule, Clear schedule; secondary Send now instead with explicit supersession warning | Resume and Stop |
| Running or waiting | Stop sending; current wait explanation | Start, schedule and Resume |
| Stopping | Stopping indicator and drain progress | Duplicate stop, start, schedule, resume and recovery dispatch |
| Paused or interrupted | Resume safe remainder when its count is positive; secondary Stop task to settle the task without launching more work | First-start and scheduling controls |
| Stopped with safe remaining work | Resume safe remainder with its count | First-start, Stop and scheduling controls |
| Finished task with resumable safe work from a previous authorized start | Resume safe remainder with its count | First-start and scheduling controls |
| Live scheduled start was refused; task remains validated/unstarted | Review miss; Send now after fresh checks, or Clear schedule then choose a new schedule; resolve any content/mailbox/approval blocker first | Automatic catch-up and Resume |
| Schedule missed during downtime; task is paused | Resume safe remainder with fresh confirmation; Clear schedule remains available but leaves the task paused | First-start, new scheduling and automatic catch-up |
| Retry-eligible definite failures after active work has settled | Retry failed recipients; per-row Retry | Retry for uncertain, submitted, active or correction-required rows |
| Uncertain outcomes after active work has settled | Per-row Review outcome and explicitly confirmed Send again | Bulk retry of uncertain rows |
| Uncertain outcome with externally verified evidence, including while other work is active | Mark already submitted with verification basis, subject to existing non-dispatch prerequisites | Automatic resolution, invented acceptance timestamps and any implication that resolution submits email |
| No currently eligible action scope | Specific reason and relevant report/history links | Zero-recipient send/schedule/resume/retry controls |

#### Scenario: Choose the next action without hiding parallel problems
- **WHEN** task Overview or a list needs one next-action sentence/link
- **THEN** the first matching row below determines its single next-action link; section-local controls still use the complete action matrix:

| Priority | Condition | Next-action link / destination |
| --- | --- | --- |
| 1 | Running, waiting or stopping | View progress / Sending; Stop remains available for running/waiting despite a separate blocker |
| 2 | Staging without a valid template | Add template / campaign Template & preview |
| 3 | Other staging state | Validate workbook / Recipients |
| 4 | Unstarted stale snapshot with an enabled future schedule | Review schedule / Sending, requiring Clear schedule before revalidation |
| 5 | Other unstarted stale snapshot | Revalidate with current template / Recipients; a recorded missed start remains visible |
| 6 | Candidate work remains and restore review prevents dispatch | Review sending restriction / Sending, with the recorded restriction and reconciliation instructions |
| 7 | Candidate work remains and mailbox authorization is unusable | Reconnect mailbox / Mailbox, with a return path to this task |
| 8 | Candidate work remains and exact formatting approval is missing | Test & approve / Test & approve |
| 9 | Any unfired missed schedule remains recorded | Review missed start / Sending, using the appropriate first-start or safe-resume path |
| 10 | An enabled future schedule exists | View schedule / Sending |
| 11 | Unstarted validated task has current eligible recipients | Choose sending time / Sending |
| 12 | Previously started or downtime-paused task has resumable safe work | Resume sending / Sending |
| 13 | Current eligible definite failures exist after active work settles | Retry failed recipients / Sending |
| 14 | Unresolved uncertain rows exist after active work settles | Review outcomes / Sending |
| 15 | Unstarted validated task has no eligible recipients | Review exclusions / Recipients |
| 16 | Paused task with no safe/resumable or retry/review scope remains ineligible for results | Stop task / Sending, explaining this settles the task without more sending |
| 17 | Result-eligible task has ordinary export failure | Retry result generation / Results |
| 18 | Other settled task | View results / Results |

- **AND** “Candidate work remains” means a positive applicable safe-start/resume, definite-retry or uncertain-resend scope, not merely a positive historic input total
- **AND** active progress takes precedence over routine setup prompts, and a safe remainder may be resumed without resolving uncertain rows
- **AND** all independent problems remain visible in the relevant section
- **AND** the link only navigates; it never submits the candidate action

#### Scenario: Confirmation becomes stale or is repeated
- **WHEN** a count, saved configuration, approval, mailbox or schedule relevant to a confirmation changes before acceptance
- **THEN** the action is refused or refreshed according to the existing action contract, no extra candidate work is created, and the UI shows the new scope and asks for fresh confirmation
- **AND** repeating an already accepted action returns its existing result without duplicate dispatch
- **AND** no successful operation is inferred solely from a button click or browser timeout

### Requirement: Saved sending type is authoritative for first-start navigation
The task's saved `sending_type` SHALL be the durable source of truth for Direct/Scheduled selection and pre-execution navigation. GET requests SHALL never change or override the saved value, and a `sending_type=choose` query MAY open the chooser without modifying it. A task sending-type POST SHALL persist the selected value before its controls are shown.

#### Scenario: Legacy task chooser and saved type change
- **WHEN** an older eligible task is ready for its first start with no schedule or saved preference
- **THEN** Sending opens the Direct/Scheduled chooser without rendering either first-start form
- **WHEN** the operator submits Direct or Scheduled to the task sending-type POST
- **THEN** the server saves the value before returning to the corresponding controls, with existing fresh-count, approval, mailbox and restore-review checks intact
- **AND** GET query values `direct` and `scheduled` do not override the saved field
- **AND** a `sending_type=choose` query may open the chooser but does not write the field
- **AND** app navigation, redirects, polling and list actions derive the mode from the saved field
- **AND** for an otherwise eligible first start, the timeline action says “Send” for Direct or “Choose sending time” for Scheduled and only navigates to the matching controls
- **AND** a type-change control submits the new choice and does not perform a candidate action
- **AND** a successful type change resets checked action confirmations and unsaved mode-specific input
- **AND** reload, Back/Forward and progress polling preserve the saved selection
- **AND** a rejected start or schedule submission returns to its selected mode with relevant draft values and a fresh unchecked confirmation

#### Scenario: Scheduled selection and existing schedule state
- **WHEN** a task has an enabled or missed schedule
- **THEN** its saved type continues to select the view, and the schedule's edit/clear or missed-schedule remedy remains visible
- **AND** selecting Scheduled does not clear the existing schedule or resume a paused task

### Requirement: Sending type changes stay inside the pre-start window
Sending-type changes SHALL be allowed only for staging or ready tasks that have not started, and a change to Direct SHALL be refused while an enabled or missed schedule remains. No POST or navigation SHALL clear or cancel that schedule implicitly. Started tasks SHALL retain their existing state-specific controls, and their saved sending type SHALL be immutable.

#### Scenario: Direct change with enabled or missed schedule
- **WHEN** an operator requests a change to Direct while an enabled or missed schedule remains
- **THEN** the change is refused and the workspace directs the operator to explicitly clear the schedule first
- **AND** neither the POST nor any navigation implicitly clears or cancels the schedule
- **AND** the existing “Send now instead” action remains separately confirmed and may supersede an enabled schedule only when accepted
- **AND** a missed live-fire refusal that remains unstarted uses the existing first-start path; a downtime-missed schedule that paused execution retains its safe-resume path

#### Scenario: Type changes and forged query on a task that has already started
- **WHEN** a running, waiting, stopping, paused, interrupted, stopped or completed task is opened with `sending_type=choose|direct|scheduled`
- **THEN** the saved type cannot be changed, the first-start chooser and forms are not shown, and query values cannot change the saved type or expose first-start controls
- **AND** applicable Stop, Resume, recovery, schedule-clear and result controls continue to follow the execution-state matrix

### Requirement: Schedule entry uses native date and time inputs
The system SHALL keep Direct and Scheduled as explicit first-start choices before their respective forms. After Scheduled is selected, schedule entry SHALL use exactly two labeled native inputs for date and time, with minute precision, in India time (+05:30).

#### Scenario: Two-input schedule entry
- **WHEN** the operator prepares or edits a schedule
- **THEN** the form provides only a date input and a time input, with minute precision and a visible India-time label
- **AND** saved schedules prefill both inputs and rejected submissions retain their valid values
- **AND** the server parses the submitted date and HH:MM time in India time and rejects malformed values without scheduling

### Requirement: Schedule entry discloses authorization and timing
Schedule entry SHALL state the 30-minute minimum lead and one-shot behavior, and SHALL disclose that saving the schedule authorizes its future start without another confirmation. Long eligibility, downtime and sending-type explanations SHALL use progressive disclosure, while timezone, minimum lead, one-time authorization and applicable warnings remain visible.

#### Scenario: Schedule is accepted or rejected
- **WHEN** the operator submits a future schedule with explicit confirmation
- **THEN** accepted state displays the full stored date/time +05:30, authorized recipient scope and current eligibility caveat
- **AND** an invalid or too-soon time keeps the entered values, names the problem and creates no schedule
- **AND** editing requires a new explicit confirmation against fresh counts

#### Scenario: Scheduled start becomes due or is missed
- **WHEN** the schedule fires with a running server
- **THEN** current configuration, approval, mailbox and candidate eligibility are rechecked and the UI shows either current sending progress or the concrete reason it did not start
- **WHEN** its time passed during downtime
- **THEN** the UI shows Missed scheduled start, the stored time and an explicit safe-resume action with fresh scope; it never silently catches up
- **AND** clearing a missed schedule never resumes sending or erases the task's recorded pause reason
- **AND** a live-fire refusal that leaves the task unstarted uses Send now after fresh checks; a downtime miss that paused the task uses Resume safe remainder
- **AND** clearing a live-fire miss may remove its obsolete schedule-refusal message without deleting its existing authorization/action records; this does not require a new schedule-audit UI

### Requirement: Recovery differentiates failure from uncertainty
The system SHALL show recovery within Sending when applicable, with separate groups for retry-eligible definite failures, correction-required rows and uncertain outcomes. It SHALL preserve historical attempts, duplicate-risk confirmation, external-verification evidence and the distinction between manual resolution and provider acceptance.

#### Scenario: Definite failure and uncertainty coexist
- **WHEN** a settled task contains both a retryable definite failure and an uncertain submission
- **THEN** Retry failed recipients states its fresh definite-failure count and excludes the uncertain row
- **AND** the uncertain row shows prior attempt time/content/outcome and separate “Send again” and “Mark already submitted” operations
- **AND** Send again requires the explicit possible-duplicate warning/confirmation; manual resolution requires the recorded basis of external verification
- **AND** correction-required rows point to corrected upload rather than an unusable Retry action

#### Scenario: No recovery is needed
- **WHEN** a task has no recovery rows
- **THEN** no empty recovery navigation section or prominent recovery card competes with its normal next action
- **AND** historical manually resolved attempts remain available in Results

#### Scenario: Verified uncertainty is resolved during other active work
- **WHEN** an uncertain row has been externally verified while other task work is still active or restore review is enabled
- **THEN** its explicit Mark already submitted operation remains accessible with the required basis and existing non-dispatch prerequisites
- **AND** it does not require formatting approval, launch/resume mail or erase the previous uncertain attempt
- **AND** Send again remains unavailable while running/stopping or restore-review dispatch restrictions apply

### Requirement: Progress updates preserve operator intent
The system SHALL show a compact current progress summary in Sending, including safe remaining, submitted, failures, review-required, in-flight and reserved counts, with invalid/duplicate/exclusion detail accessible nearby. Updates SHALL preserve the selected section, valid draft inputs and keyboard focus while invalidating stale action confirmations.

#### Scenario: Polling while the operator prepares an action
- **WHEN** grouped progress updates arrive while a form has focus
- **THEN** the workspace remains on the selected section, focus is not stolen and unrelated valid input is retained
- **AND** changed action scope resets its confirmation and visibly explains the updated count/state
- **AND** repeated updates do not continually announce the entire page or shift the form's position
- **AND** mailbox cooldowns and service pauses appear as actionable wait/block reasons, not generic failures

#### Scenario: Mailbox pacing is adjusted
- **WHEN** the operator opens Sending settings within Sending
- **THEN** the existing pacing controls explain their shared mailbox scope, current rate, allowed range and concurrency bound
- **AND** an active cooldown and its known end time remain visible beside progress even when settings are collapsed

- **AND** Sending settings uses an accordion with a full-width header button and disclosure indicator; expansion survives progress refreshes

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
- **AND** its expansion choice survives progress updates, and a legacy history bookmark opens the accordion in Results
- **AND** “Submitted” is explained as accepted by the provider, not confirmed delivery
- **AND** test totals, manual resolutions and uncertain attempts are distinguished from provider-confirmed candidate submissions

### Requirement: Layout and feedback remain accessible
The system SHALL preserve readable content, visible labels, keyboard operation and clear feedback across campaign/task pages. Essential state, scope, examples and next actions SHALL not depend on hover, color, collapsed technical details or placeholders. Technical identifiers SHALL not displace operator-facing explanations.

#### Scenario: Narrow screen or keyboard navigation
- **WHEN** the workspace is used at 360, 768 or 1440 CSS pixels wide, or with keyboard navigation
- **THEN** identity and status wrap, section navigation remains visible and operable, and content/actions fit without whole-page horizontal scrolling or overlap
- **AND** wide row/history tables may scroll within a labeled container while names, status and actions remain understandable
- **AND** current breadcrumb/section, control labels, selected state and focus are programmatically identifiable
- **AND** at 1440 by 900, when Overview is selected by default, the task identity, current status, next-action sentence, lifecycle strip and section navigation are visible without scrolling

#### Scenario: Mutation fails or access is refused
- **WHEN** validation, approval, start, schedule, recovery, pacing or export generation is rejected
- **THEN** the workspace stays on the relevant section and shows a safe, specific reason and remedy near the operation, preserving valid non-secret input where possible
- **AND** it does not show success, expose credentials/private provider responses or direct another operator to private records
- **AND** unauthorized campaign/task/download links reveal no private content or metadata

### Requirement: Approved formatting controls are disclosed explicitly
An approved task SHALL show its approval summary and hide the formatting-test and received-formatting approval forms behind a collapsed, keyboard-operable Change formatting control. An unapproved task SHALL show the existing required forms directly. Reopening the controls SHALL preserve existing approval and SHALL NOT send mail or modify configuration.

#### Scenario: Review already approved formatting
- **WHEN** an operator opens Test & approve with current formatting approval
- **THEN** both forms are collapsed until Change formatting is activated
- **AND** the expanded content links to the existing template revision flow and explains revalidation and new-configuration approval
- **AND** a rejected test or approval submission keeps the disclosure open so the error and controls remain accessible
- **AND** exact-configuration approval, explicit test submission and received-formatting confirmation checks remain in force

### Requirement: Connected mailbox setup is disclosed explicitly
A connected mailbox page SHALL hide provider selection and connection forms on normal navigation and offer Change provider beside Disconnect. Disconnected or reconnect-required mailboxes SHALL retain access to connection setup.

#### Scenario: Change a connected provider
- **WHEN** the operator activates Change provider
- **THEN** the existing provider chooser opens without disconnecting, changing credentials or replacing the mailbox
- **AND** provider selections and rejected connection forms remain visible during setup
- **AND** same-mailbox reconnect and explicit replacement confirmation retain their existing checks
- **AND** an unfinished replacement continues to block provider setup

### Requirement: Expanded recipient review
The system SHALL distinguish original source-row counts from expanded recipient-entry counts throughout validation and task summaries. Recipients SHALL identify each entry by source row, position within its cell and individual address, provide independent eligible-entry previews, and expose entry-specific exclusions and protected report downloads.

#### Scenario: Mixed recipient cell is reviewed
- **WHEN** one source row contains two eligible addresses and one invalid entry
- **THEN** the task shows one source row, three recipient entries, two eligible recipients and one invalid entry
- **AND** each eligible recipient is independently selectable for preview with its own To address and copied details
- **AND** the invalid entry's address, source location and reason remain visible without disabling its eligible siblings

#### Scenario: Duplicate siblings need attention
- **WHEN** two duplicate entries and one eligible entry come from the same source row
- **THEN** two entries need attention and one recipient is eligible
- **AND** both duplicates appear with distinct positions and the eligible sibling remains selectable

#### Scenario: Action scope and result guidance
- **WHEN** the operator reviews Sending or Results for an expanded task
- **THEN** action confirmations and progress use recipient counts rather than original row counts
- **AND** Results explains that original worksheets are preserved and individual outcomes are in the added recipient-results worksheet
- **AND** all start, schedule, approval and recovery prerequisites still apply
