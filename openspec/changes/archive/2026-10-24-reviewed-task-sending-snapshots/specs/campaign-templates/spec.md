# Spec Delta

## MODIFIED Requirements

### Requirement: Explicit recipient mapping
Each valid revision SHALL contain exactly one recipient mapping. Mapping lines SHALL use `variable | source header | required|optional|recipient | fallback`. Mappings SHALL NOT offer a formatting choice. A recipient mapping SHALL be required and reject any non-empty fallback. The selected mapping supplies the recipient address.

#### Scenario: Recipient mapping is independent of placeholders
- **WHEN** exactly one mapping is marked recipient and its variable is absent from subject and body
- **THEN** the revision is valid with respect to placeholder usage and routing still uses that mapping

#### Scenario: Recipient placeholder is explicit
- **WHEN** a subject or body contains the recipient variable placeholder
- **THEN** it is replaced with the mapped address

#### Scenario: Invalid recipient mapping configuration
- **WHEN** no mapping or more than one mapping is marked recipient, or a recipient mapping has a fallback
- **THEN** the revision is rejected with an actionable mapping error

### Requirement: Default mapping draft
The system SHALL retain source-header suggestions and use them only to seed the first-revision mapping draft. It SHALL pre-fill one required mapping per source header, leave the table empty once a revision exists, and require the operator to explicitly mark exactly one mapping as recipient. The draft SHALL NOT guess routing or create, modify or approve any revision.

#### Scenario: New campaign with expected headers
- **WHEN** a campaign whose source headers are `Candidate Name, Candidate Email, Expected Profile` has no content revision
- **THEN** the mapping form is pre-filled with `candidate_name | Candidate Name | required`, `candidate_email | Candidate Email | required` and `expected_profile | Expected Profile | required`
- **AND** none is marked recipient until the operator chooses one
- **AND** no revision exists until the operator submits the form

#### Scenario: Variable name derived from the header
- **WHEN** an expected header is `Expected Department/Division`
- **THEN** its drafted variable name is `expected_department_division`
- **AND** a header that cannot form a variable name is omitted from the draft

#### Scenario: Campaign already has a revision
- **WHEN** the campaign has a content revision
- **THEN** the mapping form starts empty and no draft is offered

#### Scenario: Failed revision submission
- **WHEN** the operator submits a mapping table that fails validation
- **THEN** the form is re-rendered with the submitted mapping text unchanged

### Requirement: Mapping help example follows the campaign
The system SHALL present a mapping help example based on the campaign's first source header suggestion, with the derived variable and source header; the example SHALL explain the explicit recipient role choice and show no formatting choice.

#### Scenario: Example matches the draft
- **WHEN** a campaign's first source header is `Candidate Name`
- **THEN** the example line shows `candidate_name | Candidate Name | required`
- **AND** the example variable name is the name the draft uses for that header

#### Scenario: Campaign without a usable header
- **WHEN** no source header can form a variable name
- **THEN** the example uses a generic `variable_name` placeholder instead of a campaign-specific name

### Requirement: Immutable configuration revisions
The system SHALL preserve campaign revisions of the subject, original DOCX, rendered body, mappings, required flags and fallbacks, including historical formatting rules. Changes to campaign content SHALL create a new unapproved campaign revision. Task instructions and prepared/final values SHALL be preserved in task run/snapshot history rather than rewriting campaign revisions.

#### Scenario: Edit after formatting approval
- **WHEN** the operator changes the subject, template, mappings or fallback
- **THEN** the new revision requires its own test-email approval
- **AND** previously stored email content and running task snapshots remain unchanged

#### Scenario: Change recipient selection
- **WHEN** the operator changes which mapping is marked recipient
- **THEN** the change creates a new revision that requires its own test-email approval

#### Scenario: Presentation settings do not alter email content
- **WHEN** only downloadable result-column labels change
- **THEN** task exports retain their chosen label snapshot
- **AND** the app does not pretend the personalized subject or body changed

#### Scenario: New final snapshot does not inherit earlier approval
- **WHEN** a task creates a final sending snapshot, including from a replacement final upload, under a campaign revision that was previously approved
- **THEN** the new snapshot has no inherited test or received-format approval
- **AND** it requires a successful test and explicit received-format approval for that exact snapshot, campaign revision and mailbox generation
- **AND** existing started task snapshots and stored historical email content remain unchanged

#### Scenario: Task instruction changes are task-scoped
- **WHEN** an operator edits task normalization instructions and explicitly reruns preparation
- **THEN** the campaign revision remains unchanged and the task retains separate run evidence
- **AND** selecting and finalizing the revised data requires approval for its own final snapshot

### Requirement: Preview and source visibility
The system SHALL show the To recipient, subject and rendered body for supplied sample values and allow previewing a finalized task recipient using the same rendering rules as sending. Sample values SHALL be used directly without AI normalization or formatting transformations; backend recipient and safety validation still applies.

#### Scenario: Sample preview
- **WHEN** the operator previews the campaign with supplied sample values
- **THEN** the app shows the mapped To recipient, subject and body using those values directly
- **AND** it shows the supplied mapped values without AI normalization or format-based transformation
- **AND** backend recipient and safety validation errors remain visible

#### Scenario: Candidate preview
- **WHEN** the operator selects an eligible recipient after creating a final sending snapshot
- **THEN** the app shows the To recipient, subject and body rendered from the accepted final mapped values
- **AND** it identifies that the preview uses the task's final snapshot
- **AND** accepted mapped values and source details remain available for review
- **AND** previewing does not create a sending attempt

#### Scenario: Prepared values are not presented as final email
- **WHEN** the operator previews prepared values before finalization
- **THEN** the workspace labels them as prepared values and does not present them as the final email preview
- **AND** only the accepted final snapshot drives the email preview, test and sending
