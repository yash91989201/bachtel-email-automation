# Spec Delta

## MODIFIED Requirements

### Requirement: Shared bounded concurrency
The system SHALL accept a whole-number concurrency limit from 1 through 10 for each mailbox connection, defaulting to 2. All tasks, tests and retries using that mailbox SHALL share its limit. Each launch SHALL require available capacity under the current limit. A saturated mailbox SHALL NOT delay another mailbox's launches. Tasks SHALL share their mailbox's capacity fairly without bypassing campaign candidate reservations.

#### Scenario: Several tasks and a test
- **WHEN** multiple tasks are ready and the operator requests a test
- **THEN** each new request starts only when that mailbox has fewer in-flight requests than its saved concurrency limit
- **AND** the test obtains a normal paced opportunity without bypassing the limit

#### Scenario: Several independent mailboxes
- **WHEN** multiple operators have ready tasks with different concurrency limits
- **THEN** each mailbox uses its own configured submission capacity regardless of other mailboxes' in-flight work
- **AND** one mailbox's cooldown or saturated slots do not block another mailbox's paced opportunity
- **AND** the dispatcher does not indefinitely starve a ready mailbox

#### Scenario: Concurrency boundaries
- **WHEN** the owner saves concurrency 1 or 10
- **THEN** that boundary value is accepted and used for that mailbox's subsequent launch decisions
- **AND** concurrency 1 permits only one in-flight request at a time, while concurrency 10 permits at most ten subject to pacing and cooldown

#### Scenario: Lower concurrency while requests are in flight
- **WHEN** the owner lowers concurrency from 4 to 2 while four requests are already in flight
- **THEN** the existing requests finish normally and retain their individual recorded outcomes
- **AND** no new request launches until fewer than two remain in flight
- **AND** a request whose preparation began before the save must also satisfy the new limit before transmission begins

#### Scenario: Increase concurrency during authorized sending
- **WHEN** the owner raises a mailbox's limit while authorized task work is running
- **THEN** subsequent work can use the additional slots without restarting the server
- **AND** increased capacity does not bypass pacing, cooldown, candidate eligibility or durable attempt recording

### Requirement: Configurable bounded pacing
The system SHALL default to 15 submission starts per minute and accept only configured integer rates from 1 through 30. Pacing SHALL be independent per mailbox and shared across its tasks, tests, automatic retries and manual actions, evenly spaced without restart or batch bursts.

#### Scenario: Invalid setting
- **WHEN** the operator submits a rate above 30, below 1 or a non-integer
- **THEN** both UI validation and server validation reject it
- **AND** neither saved sending setting changes

#### Scenario: Concurrent dispatch
- **WHEN** ten request slots are available at a configured rate of 30 per minute
- **THEN** new starts remain paced at least two seconds apart rather than launching all ten simultaneously
- **AND** no rolling 60-second interval exceeds that mailbox's configured allowance

#### Scenario: Different mailbox rates
- **WHEN** two operators choose different valid rates
- **THEN** each mailbox observes its own rolling allowance and even spacing
- **AND** tests, retries and schedules cannot bypass that mailbox's controls

#### Scenario: Default and boundary pacing
- **WHEN** sending settings are first initialized for a mailbox
- **THEN** pacing is 15 starts per minute, with starts at least four seconds apart and at most 15 in any rolling 60-second interval
- **WHEN** the owner saves pacing 1 or 30
- **THEN** that boundary value is accepted and subsequent starts observe the saved rate

#### Scenario: Settings change retains pacing history
- **WHEN** the owner changes either sending setting after recent starts or during a mailbox cooldown
- **THEN** subsequent starts honor the saved rate together with recent start history and the active cooldown
- **AND** the save does not clear a wait or create an immediate batch burst

## ADDED Requirements

### Requirement: Owner updates mailbox sending limits together
The owning operator SHALL be able to save concurrency and pacing together for their current mailbox. Both values SHALL be required whole numbers in their allowed ranges. Invalid, unauthorized or failed saves SHALL preserve both previous values. Saving limits SHALL NOT grant approval, start or resume a task, reserve candidates, queue a test, or retry an uncertain submission.

#### Scenario: Valid settings are saved
- **WHEN** the owner saves concurrency 3 and pacing 12 from one of their tasks
- **THEN** both values are persisted together for the current mailbox and govern subsequent launch decisions after the save
- **AND** subsequent views of other tasks using that mailbox show the same saved values
- **AND** other mailboxes' settings are unchanged

#### Scenario: Invalid concurrency or incomplete input
- **WHEN** concurrency is below 1, above 10, fractional, blank or nonnumeric, or either required setting is missing
- **THEN** UI validation and server validation refuse the save
- **AND** no valid companion value is partially saved and no success is reported

#### Scenario: Storage failure
- **WHEN** both submitted values are valid but persistence fails
- **THEN** the previous pair remains authoritative for display and sending
- **AND** the operator receives a safe error and a retry remedy without a success message

#### Scenario: Unauthorized or unusable target
- **WHEN** an anonymous user, another operator, or an owner using a task under the wrong campaign attempts to change limits
- **THEN** the existing authentication and ownership policy refuses the request without exposing or changing foreign settings
- **WHEN** the owner has no current mailbox connection
- **THEN** the save is refused with a mailbox setup remedy and no settings are assigned to another connection

#### Scenario: Settings save is not sending authorization
- **WHEN** the owner saves limits while a task is unapproved, paused, interrupted, stopped, closing or closed
- **THEN** the save alone does not authorize candidate sends, reopen or resume the task, or create a submission attempt
- **AND** existing exact-snapshot formatting approval and explicit start, schedule, retry and resend requirements still apply
- **AND** pacing and concurrency changes alone do not invalidate otherwise applicable formatting approval

### Requirement: Sending limits persist through upgrades and restarts
New mailbox settings SHALL initialize concurrency to 2 and pacing to 15. Upgrading existing mailbox settings SHALL initialize missing concurrency to 2 and preserve saved valid pacing values. Saved limits SHALL survive restart without resetting pacing history, cooldowns or audit evidence, and without automatically resuming interrupted work.

#### Scenario: Existing mailbox is upgraded
- **WHEN** an existing mailbox with saved pacing 25 and no concurrency setting is upgraded
- **THEN** its concurrency becomes 2 and its pacing remains 25
- **AND** other saved valid pacing values are likewise retained
- **AND** recorded attempts, approvals, schedules, recent start history and cooldowns are preserved

#### Scenario: Existing setting is preserved on repeated initialization
- **WHEN** initialization encounters an already saved valid concurrency and pacing pair
- **THEN** it preserves both values rather than reapplying defaults

#### Scenario: Saved settings survive restart
- **WHEN** the server restarts after the owner saved concurrency 7 and pacing 9
- **THEN** those values remain the mailbox's settings
- **AND** normal interruption recovery and explicit resume requirements remain in force
- **AND** recent starts and persisted cooldowns still constrain future authorized work
