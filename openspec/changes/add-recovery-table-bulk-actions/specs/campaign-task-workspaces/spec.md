# Spec Delta

## MODIFIED Requirements

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
| Retry-eligible definite failures after active work has settled | Retry failed recipients; per-row Retry; Retry selected for a confirmed row selection | Retry for uncertain, submitted, active or correction-required rows |
| Uncertain outcomes after active work has settled | Per-row Review outcome and explicitly confirmed Send again; Send again selected for a confirmed row selection | Bulk retry of uncertain rows |
| Uncertain outcome with externally verified evidence, including while other work is active | Mark already submitted with verification basis; Mark selected already submitted with one recorded basis, subject to existing non-dispatch prerequisites | Automatic resolution, invented acceptance timestamps and any implication that resolution submits email |
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


### Requirement: Recovery differentiates failure from uncertainty
The system SHALL show recovery within Sending when applicable as one table whose state column distinguishes retry-eligible definite failures, correction-required rows and uncertain outcomes, and whose row actions differ by state. It SHALL preserve historical attempts, duplicate-risk confirmation, external-verification evidence and the distinction between manual resolution and provider acceptance.

#### Scenario: Definite failure and uncertainty coexist
- **WHEN** a settled task contains both a retryable definite failure and an uncertain submission
- **THEN** Retry failed recipients states its fresh definite-failure count and excludes the uncertain row
- **AND** the uncertain row shows prior attempt time/content/outcome and separate “Send again” and “Mark already submitted” operations
- **AND** Send again requires the explicit possible-duplicate warning/confirmation; manual resolution requires the recorded basis of external verification
- **AND** correction-required rows point to corrected upload rather than an unusable Retry action

#### Scenario: Confirmed selection acts once
- **WHEN** the operator selects several recovery rows and confirms that selection's action
- **THEN** the selection submits one request whose confirmed count is the selected row count
- **AND** a row that is no longer eligible refuses the whole request with the changed count instead of acting partially
- **AND** uncertain rows still require the duplicate-risk warning for Send again and one recorded external-verification basis for manual resolution

#### Scenario: A row's action is unavailable
- **WHEN** a recovery row's action is withheld by active work, restore review or a changed count
- **THEN** the row explains why the action is unavailable instead of rendering an empty action cell
- **AND** the row keeps its state, reason and prior attempt visible

#### Scenario: Recovery rows stay readable without the selection layer
- **WHEN** the selection layer is unavailable
- **THEN** each row's own dialog still performs Retry, Send again or Mark already submitted with its explicit confirmation
- **AND** the table keeps the row's recipient, worksheet position, state, reason and prior attempt visible

#### Scenario: No recovery is needed
- **WHEN** a task has no recovery rows
- **THEN** no empty recovery navigation section or prominent recovery card competes with its normal next action
- **AND** historical manually resolved attempts remain available in Results

#### Scenario: Verified uncertainty is resolved during other active work
- **WHEN** an uncertain row has been externally verified while other task work is still active or restore review is enabled
- **THEN** its explicit Mark already submitted operation remains accessible with the required basis and existing non-dispatch prerequisites
- **AND** it does not require formatting approval, launch/resume mail or erase the previous uncertain attempt
- **AND** Send again remains unavailable while running/stopping or restore-review dispatch restrictions apply

