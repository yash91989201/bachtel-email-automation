## MODIFIED Requirements

### Requirement: Recipient review presents understandable totals
The system SHALL put a concise validation summary, fresh eligibility distinction, recipient preview and row-specific exclusions in Recipients. All recorded counts and exclusion reports SHALL remain available for the states that record them. Summary totals SHALL not double-count rows or present historical validation counts as a guarantee of present sending eligibility. Current role-mapping tasks SHALL present eligibility as pending until a prepared review exists, and SHALL NOT present raw-row validation totals or pre-AI exclusion rows.

#### Scenario: Mixed recipient validation
- **WHEN** a legacy format-mapping task's validation contains eligible, duplicate, invalid, missing-value, already-submitted, already-scheduled and review-required entries
- **THEN** the section first shows Source rows, Recipient entries, Eligible at validation, and Excluded at validation
- **AND** it shows the current eligible/safe-remaining count separately with “Rechecked before sending” guidance
- **AND** Excluded at validation equals recipient entries minus eligible-at-validation entries; it includes all non-eligible categories, not only invalid/duplicate entries
- **AND** a labeled breakdown retains total, eligible, ineligible/missing-data, duplicate, invalid address, already submitted, already scheduled and review-required counts as a mutually exclusive partition whose categories sum to the recipient-entry total
- **AND** several reason texts can apply to one entry without adding that entry to the summary more than once
- **AND** row tables expose original worksheet row numbers, position within the recipient cell, current state and all recorded reasons; excluded entries remain visible even when they have no candidate identity
- **AND** “Download exclusion report” is available immediately after validation, including when no entry is eligible
- **AND** corrected data is uploaded as a new task; the original snapshot is not edited in place

#### Scenario: Current role-mapping task before review
- **WHEN** a current role-mapping task has been validated and no prepared review or final snapshot exists
- **THEN** the section shows its source-row count and states that entry counts, accepted recipients and exclusions appear after a preparation run is selected and backend validation completes
- **AND** it shows no validation-time entry totals, no excluded-at-validation count and no pre-AI row table
- **AND** the main status reports pending eligibility rather than no eligible recipients

#### Scenario: Current role-mapping task after review
- **WHEN** a completed preparation run is selected for review
- **THEN** the section shows reviewed source rows, recipient entries, accepted and held counts, the accepted entries and the held entries with reasons
- **AND** the exclusion download resolves the selected final snapshot first, then the selected prepared review, and is unavailable when neither exists
- **AND** the original values remain available as evidence without being presented as current eligibility

#### Scenario: Zero attention categories
- **WHEN** a recorded validation or review has one accepted entry and no exclusions
- **THEN** the summary shows one source row, one recipient entry, one accepted entry and no exclusions
- **AND** zero-valued breakdown categories remain accessible without occupying eight equally prominent metric cards

### Requirement: Task preparation review separates corrections and findings
The task workspace SHALL present successful AI corrections separately from unresolved findings, and expose the visible application instructions with optional task-specific instructions during preparation. For current role-mapping tasks it SHALL present backend review counts only after a completed run is selected, and SHALL identify that backend validation is still pending before then.

#### Scenario: Review initial preparation
- **WHEN** the operator opens a task after initial preparation
- **THEN** the workspace shows successful corrections and unresolved findings in distinct groups
- **AND** the visible base instructions and saved task-specific instructions are available for review
- **AND** the operator can review mapped values and findings without authorizing any email action

#### Scenario: Backend outcome replaces provisional counts
- **WHEN** a completed preparation run is selected for a current role-mapping task
- **THEN** accepted and held recipient counts come from the backend review of the cleaned values
- **AND** the preparation history continues to report corrections and AI findings without presenting them as admission
- **WHEN** the dataset is already finalized
- **THEN** the selected final snapshot supplies accepted and excluded rows and the preparation history remains labeled as history

#### Scenario: Preparation fails
- **WHEN** preparation fails or cannot complete
- **THEN** the workspace identifies the failure, preserves the task and instruction draft, and offers an explicit retry
- **AND** it does not present partial output as reviewed or final

### Requirement: Task finalization offers both review paths
The task workspace SHALL let the operator finalize prepared data by approving it as-is or uploading an edited final sheet. Both paths SHALL show the backend validation outcome and the accepted values before finalization is reported as successful. Dataset approval SHALL remain unavailable until the backend review of the selected run is available.

#### Scenario: Approve prepared data as-is
- **WHEN** the operator chooses Approve as-is for prepared data whose backend review is available
- **THEN** the workspace shows validation results and an email preview for the resulting final snapshot
- **AND** it offers download of the accepted sheet without requiring a reupload
- **WHEN** the backend review is unavailable or refused
- **THEN** approval stays unavailable with the review remedy

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

### Requirement: Status labels have explicit meanings
The system SHALL display task execution, formatting approval, schedule and artifact status as separate labeled facts. It SHALL use the status vocabulary and precedence below, show blocking/wait reasons beside status, and never use an unexplained “Ready”, “Approved”, “Failed” or “Complete” to describe multiple independent states. Pending eligibility SHALL be distinguishable from having no eligible recipients.

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
| A current role-mapping task has no selected preparation review and no final snapshot | Eligibility pending | Run data preparation, select its output, and review accepted and held entries |
| No eligible/safe-start recipients remain in an unstarted validated task with recorded admission evidence | No eligible recipients | Exclusions and relevant downloads |
| An unstarted validated task lacks applicable formatting approval | Needs formatting approval | Exact saved content and current mailbox scope |
| Otherwise, a validated unstarted task has eligible recipients and approval | Ready to send | Sending still requires explicit start or schedule authorization |

#### Scenario: A blocker coexists with another status
- **WHEN** restore review, an unusable mailbox, stale content, an invalid approval or a provider cooldown prevents an otherwise relevant action
- **THEN** the primary execution/schedule status is retained and a nearby labeled blocker names the actual cause and remedy
- **AND** restore review is the highest-priority blocking banner, followed by mailbox authorization and content/approval problems, with every applicable blocker still discoverable
- **AND** “Formatting: Approved” always means approval applies to the saved content and current mailbox, not merely that some campaign revision was approved
- **AND** “Already scheduled” on a recipient is explained as claimed by another task, separate from this task's future scheduled start
