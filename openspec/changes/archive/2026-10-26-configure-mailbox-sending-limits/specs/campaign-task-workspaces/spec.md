# Spec Delta

## MODIFIED Requirements

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
- **THEN** the controls explain that concurrency and pacing are shared by all tasks, formatting tests and retries using this mailbox
- **AND** an editable Concurrent emails field shows the saved value, the whole-number range 1–10 and default 2
- **AND** an editable Pacing field shows the saved value, the whole-number range 1–30 and default 15 with the explicit unit emails per minute
- **AND** concise guidance explains that concurrency limits emails in flight while pacing limits how often emails start
- **AND** the current concurrency summary reflects the saved limit rather than a fixed four
- **AND** an active cooldown and its known end time remain visible beside progress even when settings are collapsed
- **AND** Sending settings uses an accordion with a full-width header button and disclosure indicator; expansion survives progress refreshes

#### Scenario: Save both sending settings
- **WHEN** the owner edits concurrency and pacing and submits Update sending settings
- **THEN** both values are submitted together and a successful save returns to Sending with visible success feedback and the saved values
- **AND** subsequent page loads show the saved pair for every task using that mailbox
- **AND** refreshing the page does not replay the update

#### Scenario: Refused or failed save preserves the form
- **WHEN** either value is invalid, missing or cannot be saved
- **THEN** Sending remains selected, the settings accordion is open, and the form retains readable submitted values with an actionable field or save error
- **AND** the current-settings summary continues to show the previously saved pair
- **AND** no success feedback is displayed and no partial update is represented as saved

#### Scenario: Progress refresh while editing limits
- **WHEN** the operator has unsaved concurrency or pacing input and a live progress refresh arrives
- **THEN** both drafts, keyboard focus and the accordion's expansion state are preserved
- **AND** displayed saved settings remain distinguishable from unsaved form input

#### Scenario: Settings remain accessible on narrow screens
- **WHEN** the operator edits sending settings by keyboard or at 360 CSS pixels wide
- **THEN** both fields have associated labels, range/default help and accessible validation feedback
- **AND** the save control is reachable without a pointer and the form does not cause whole-page horizontal overflow

#### Scenario: Saving limits does not approve or start a task
- **WHEN** the owner saves valid sending limits
- **THEN** the workspace continues to show the task's actual approval, authorization and lifecycle state
- **AND** the save does not display a task as newly approved, started, resumed or reopened
