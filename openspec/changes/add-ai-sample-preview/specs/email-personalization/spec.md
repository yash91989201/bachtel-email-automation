## MODIFIED Requirements

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

