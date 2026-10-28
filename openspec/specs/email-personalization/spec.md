# email-personalization Specification

## Purpose

Turn mapped candidate values into predictable personalized subjects and safe HTML bodies while preserving original data and distinguishing required from optional values.

## Requirements

### Requirement: Required and optional variables
The system SHALL default each mapped variable to required. Empty required values SHALL make only the affected candidate row ineligible. Optional values SHALL use a configured fallback or an empty string.

#### Scenario: Missing required location
- **WHEN** one candidate has a whitespace-only required location and other candidates have valid values
- **THEN** that row is held with the missing variable named
- **AND** other valid candidates remain eligible

#### Scenario: Optional fallback
- **WHEN** an optional mapped field is empty
- **THEN** its fallback is substituted if configured, otherwise an empty string is used

### Requirement: Mapped columns remain mandatory
The system SHALL distinguish absent mapped columns from empty values: every configured source column SHALL be present in the selected header row even when its variable is optional.

#### Scenario: Optional column missing
- **WHEN** an optional location mapping references a header absent from the uploaded worksheet
- **THEN** task-level validation reports the missing header before sending is allowed

### Requirement: Field-specific normalization
The system SHALL prepare new task values through header-guided AI normalization and operator review rather than mapping format selections. Final accepted values SHALL be used directly for rendering without AI or name/designation/case transforms. Historical snapshots SHALL retain their original rendering behavior.

#### Scenario: Names and designations
- **WHEN** initial preparation receives a name of john doe and a designation of head of hr
- **THEN** header-guided cleanup can produce John Doe and Head of HR, retaining the acronym
- **AND** no name or designation formatting choice is required in the mapping

#### Scenario: Preserved text
- **WHEN** a mapped value contains iOS Engineer
- **THEN** its intentional spelling is preserved by preparation
- **AND** operator-reviewed final text is not recased at render time

#### Scenario: Human-edited values
- **WHEN** the final upload contains an operator-selected spelling or capitalization
- **THEN** accepted text appears unchanged in subject and body, subject to contextual escaping
- **AND** neither preview nor sending normalizes it again

### Requirement: Whitespace and original values
The system SHALL preserve original uploaded values independently of prepared and final values. Initial AI preparation SHALL clean single-line whitespace before backend validation. Final-upload content SHALL remain operator-authored; whitespace-only values SHALL count as empty without silently trimming or collapsing populated content.

#### Scenario: Irregular spacing
- **WHEN** initial preparation receives a name with surrounding spaces and repeated internal spaces
- **THEN** its prepared replacement has clean spacing
- **AND** the original value remains available in source artifacts and comparison preview

#### Scenario: Final whitespace and fallback
- **WHEN** a final mapped cell contains only whitespace
- **THEN** required/optional rules treat it as empty and apply the configured optional fallback if any
- **AND** a non-empty final content value is not rewritten to impose preparation whitespace conventions

### Requirement: Single recipient validation
The system SHALL obtain each recipient from the single mapping marked recipient, expand comma-separated workbook cells during intake, and require one non-empty valid bare address per resulting entry. Individual rendering and test-recipient input SHALL reject address lists, display-name wrappers and header-control characters, and use each address's trimmed lowercase form for delivery and campaign identity. It SHALL NOT claim to verify mailbox existence or delivery.

#### Scenario: Multiple addresses in a cell
- **WHEN** the selected recipient mapping's cell contains two comma-separated addresses
- **THEN** intake produces two independently validated recipient entries
- **AND** each eligible entry renders and sends only to its own normalized address

#### Scenario: Case and surrounding whitespace
- **WHEN** the selected recipient mapping's cell contains surrounding spaces and mixed letter case
- **THEN** normalization produces the lowercase address used for matching

#### Scenario: Recipient placeholder for expanded entries
- **WHEN** the recipient variable is explicitly used in a subject or body and a workbook cell contains two valid unique addresses
- **THEN** each recipient's preview and stored email substitute only that recipient's normalized address
- **AND** the full original cell remains available in source comparison without becoming the To address or recipient placeholder

#### Scenario: Single-recipient diagnostic and sample inputs
- **WHEN** an operator supplies a comma-separated list as a diagnostic test destination or a campaign sample-preview recipient
- **THEN** the single-recipient input is refused with an actionable error
- **AND** workbook expansion does not enable multiple To recipients or send several diagnostic emails

#### Scenario: Invalid sibling does not poison rendering
- **WHEN** an eligible saved recipient came from a cell that also contained an invalid entry
- **THEN** its task preview, test content and candidate submission use the same pinned single-recipient rendering rules
- **AND** the invalid sibling does not become part of its recipient value

### Requirement: Safe data substitution
The system SHALL replace variables in both subject and body without evaluating uploaded values as template code. HTML replacements SHALL be escaped for their context, unsafe links or markup SHALL be removed or rejected, and subject header-control characters SHALL be rejected.

#### Scenario: Candidate contains markup
- **WHEN** a mapped value contains an HTML tag or template expression
- **THEN** it is rendered as inert data rather than executed markup or a second template evaluation

#### Scenario: Header injection
- **WHEN** a subject replacement introduces carriage-return or newline header characters
- **THEN** that row is ineligible with a clear validation error

### Requirement: Stable preview and submission content
The system SHALL render task preview, test and candidate email content from the same immutable final sending snapshot and pinned configuration, with test-recipient override as the only intentional addressing difference. Task previewing, testing and sending SHALL NOT call AI or rerun normalization. The separate explicit campaign AI sample action SHALL NOT change final snapshots or supply test approval or sending authorization.

#### Scenario: Approved content later sent
- **WHEN** a candidate submission uses an unchanged finalized snapshot
- **THEN** its subject and body match that snapshot's preview and selected-row test content
- **AND** later uploads, prompts, provider settings or model updates cannot alter its stored content

#### Scenario: Prepared preview is not final authorization
- **WHEN** an operator previews data before approve as-is or final-upload acceptance
- **THEN** the preview is identified as prepared data
- **AND** it cannot supply final-snapshot test approval or authorize candidate sending

#### Scenario: AI sample does not change final content
- **WHEN** an operator prepares and renders a temporary campaign sample using AI
- **THEN** the sample is identified as illustrative prepared data and cannot become a task final snapshot or test selection
- **AND** task preview, test and sending continue to use the accepted final values without invoking AI
- **AND** the sample provides no received-format approval or start/schedule authorization
