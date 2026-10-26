# Spec Delta

## MODIFIED Requirements

### Requirement: Campaign activity and settings have distinct purposes
The system SHALL place campaign-wide reports, packages and attempt history in Activity & downloads, and campaign-name editing, result-column settings and campaign deletion in Settings. It SHALL distinguish campaign exports from a task's result workbook and generated-file removal from campaign/history deletion.

#### Scenario: Campaign activity is opened
- **WHEN** the operator opens Activity & downloads
- **THEN** the section presents campaign export state/actions followed by recorded attempt history with task, revision, kind, recipient, outcome, timestamp and historical-email link
- **AND** tests are labeled separately and excluded from candidate totals
- **AND** revision history is accessed in Template & preview
- **AND** the page explains export scope and as-of time, and shows generation failures without claiming incomplete files are ready

#### Scenario: Destructive action is separated from normal operation
- **WHEN** the operator opens Settings or a generated-file removal confirmation
- **THEN** ordinary name/label settings, generated-file removal and campaign/history deletion have distinct scopes and confirmations
- **AND** campaign deletion explains removal of duplicate-prevention history and its active-work refusal rules
- **AND** no delete action is promoted as the next workflow action

#### Scenario: No task or no campaign activity exists
- **WHEN** a valid-template campaign has no uploaded tasks
- **THEN** Tasks says “No tasks yet”, explains that each workbook creates a separate task and offers Upload workbook
- **WHEN** Activity & downloads has no recorded attempts or generated exports
- **THEN** it says “No email attempts recorded” and “Downloads not generated”, without implying a failure or generating files merely by opening the section

#### Scenario: Campaign settings offers ordinary identity editing
- **WHEN** the owner opens Campaign Settings
- **THEN** a General area offers a labeled Campaign name field prefilled with the saved name and a Save name action
- **AND** its required-name and length guidance is visible
- **AND** the rename form is separate from result-column editing and campaign deletion
- **AND** opening Settings does not save a name or trigger a processing operation

## ADDED Requirements

### Requirement: Owner can rename a campaign
The owning operator SHALL be able to rename a campaign from Campaign Settings. The system SHALL trim surrounding whitespace, require a nonblank name of at most 200 Unicode code points, and allow duplicate names. A successful save SHALL persist the name, return to Settings and show success feedback. Rejected input or storage failure SHALL retain the prior saved name and preserve the submitted text with a safe error and remedy.

#### Scenario: Save a corrected campaign name
- **WHEN** the owner saves “  November outreach  ” in the campaign-name form
- **THEN** the saved campaign name becomes “November outreach”
- **AND** Settings shows success feedback with the saved name prefilled
- **AND** subsequent campaign lists, campaign headings and campaign breadcrumbs, including those on task pages, use the new name
- **AND** existing campaign and task links continue to address the same records
- **AND** refreshing the successful Settings page does not replay the update

#### Scenario: Blank or oversized name is refused
- **WHEN** the owner submits an empty or whitespace-only name, or a name longer than 200 Unicode code points after trimming
- **THEN** the save is refused with an actionable required-name or length error
- **AND** Settings stays selected with the submitted text and the prior name remains saved
- **AND** the app neither supplies a default name nor silently truncates the input

#### Scenario: Duplicate and Unicode names remain usable
- **WHEN** the owner saves a name shared with another campaign or containing Unicode characters
- **THEN** the name is accepted if it meets the nonblank and length rules
- **AND** exactly 200 Unicode code points are accepted while 201 are refused
- **AND** names are displayed as escaped text, remain inspectable, and wrap without whole-page overflow
- **AND** stable identifiers keep same-named campaigns distinct

#### Scenario: Save fails without changing the name
- **WHEN** the name passes validation but storage cannot complete the rename
- **THEN** Settings shows a safe retry message and preserves the submitted name
- **AND** no success is reported and the prior saved name remains authoritative

#### Scenario: Rename access is refused
- **WHEN** an anonymous user, another operator, or a request for a nonexistent campaign attempts to access or submit its rename form
- **THEN** the existing authentication/not-found policy refuses the request without revealing private campaign metadata
- **AND** no campaign name is changed

### Requirement: Campaign renaming preserves operational evidence
Renaming SHALL change only campaign-name metadata and its ordinary update time. It SHALL preserve campaign identity, ownership, tasks and task names, source data, prepared/final snapshots, content revisions, approvals, schedules, recipient history, attempts and generated artifacts. Renaming SHALL be available regardless of task lifecycle state and SHALL neither authorize nor trigger processing or email actions.

#### Scenario: Rename a campaign with active or finished tasks
- **WHEN** the owner renames a campaign containing staging, scheduled, running, completed, closing or closed tasks
- **THEN** the name change succeeds without stopping, reopening, recreating or renaming those tasks
- **AND** current revision identifiers and hashes, selected snapshots, tests, received-format approvals, schedules, candidate memberships, duplicate-prevention history, attempts and manual resolutions remain intact
- **AND** concurrent preparation, revision, dispatch and result updates are not overwritten
- **AND** no new validation, preparation, finalization, revision, test, approval, start, retry, resend, schedule change or export generation occurs

#### Scenario: Recorded content and downloads remain intact
- **WHEN** a renamed campaign has stored historical emails or ready result/report/package artifacts
- **THEN** their recorded contents and existing filenames remain unchanged and available through their existing protected links
- **AND** the rename does not invalidate or regenerate them
- **AND** later explicit processing and export requests continue to follow their existing rules
