# Email Personalization Delta

## MODIFIED Requirements

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

### Requirement: Stable preview and submission content
The system SHALL render task preview, test and candidate email content from the same immutable final sending snapshot and pinned configuration, with test-recipient override as the only intentional addressing difference. Previewing or sending SHALL NOT call AI or rerun normalization.

#### Scenario: Approved content later sent
- **WHEN** a candidate submission uses an unchanged finalized snapshot
- **THEN** its subject and body match that snapshot's preview and selected-row test content
- **AND** later uploads, prompts, provider settings or model updates cannot alter its stored content

#### Scenario: Prepared preview is not final authorization
- **WHEN** an operator previews data before approve as-is or final-upload acceptance
- **THEN** the preview is identified as prepared data
- **AND** it cannot supply final-snapshot test approval or authorize candidate sending
