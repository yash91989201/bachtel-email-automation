# campaign-templates Specification

## Purpose

Define reusable campaign email configurations with a separate subject, DOCX body and explicit mappings that can be validated, previewed and approved by revision.

## Requirements

### Requirement: Campaign configuration
The system SHALL let the operator create a named campaign with a separate non-empty subject, a DOCX body, source-header suggestions, configurable variable-to-header mappings and configurable result-column names. Recipient routing SHALL be selected by exactly one mapping in each valid revision, not by a campaign-level header.

#### Scenario: Create configuration without candidate upload
- **WHEN** the operator uploads a DOCX and supplies subject, source headers and mappings
- **THEN** the app validates the configuration without requiring candidate data or Google authorization
- **AND** the default result columns are Email Status, Submitted At and Email Error

#### Scenario: Configurable header labels
- **WHEN** a campaign maps candidate_name to Applicant Full Name
- **THEN** the variable need not have the same name as the source header

### Requirement: Subject is separate from body
The system SHALL use the campaign subject field as the email subject and the uploaded DOCX as the body, without interpreting or stripping a Subject marker in the document.

#### Scenario: Document contains a Subject paragraph
- **WHEN** the DOCX contains literal text labeled Subject
- **THEN** that text remains body content and does not override the configured subject

### Requirement: Variable consistency validation
The system SHALL recognize double-brace variables in subject and body, report configured content variables absent from both, and report template variables without a mapping. A recipient mapping MAY appear in an explicit subject or body placeholder but SHALL NOT be injected automatically and SHALL be exempt from the unused-mapping check. Each error SHALL name every offending variable and state its remedy. Invalid configuration SHALL NOT become ready for sending.

#### Scenario: Missing configured variable
- **WHEN** the operator configures location but neither subject nor body contains its placeholder
- **THEN** the configuration displays a location-specific error
- **AND** the error states that the row is removed or the placeholder added

#### Scenario: Unknown body or subject variable
- **WHEN** either subject or body references a variable with no mapping
- **THEN** the app reports its name and prevents use of that configuration
- **AND** the error says to add a mapping row for that name or rename the placeholder in the subject or body

#### Scenario: Subject-only variable
- **WHEN** a configured variable appears only in the subject
- **THEN** it satisfies the usage check and is replaced there

#### Scenario: Unused ordinary content mapping
- **WHEN** a configured content mapping is absent from both subject and body
- **THEN** the error names the variable and says to remove its row or add its placeholder to the subject or body

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

### Requirement: Semantic DOCX formatting
The system SHALL preserve paragraphs, headings, ordered and unordered lists, bold, italics and safe links through conversion and personalization. It SHALL support placeholders split across Word text runs within a paragraph and report unsupported or malformed placeholder structures.

#### Scenario: Formatting survives conversion
- **WHEN** a valid DOCX contains the supported formatting and a placeholder split across adjacent runs
- **THEN** the preview retains the semantic formatting and replaces the complete placeholder

#### Scenario: Unreadable document
- **WHEN** the upload is corrupt, not a supported DOCX or exceeds documented processing limits
- **THEN** the app reports a conversion error and does not mark the configuration ready

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

### Requirement: Bounded rendering scope
The system SHALL document that Word page layout, macros, attachments and embedded-image delivery are outside the initial rendering contract, and visibly report unsupported document content rather than claiming exact Word reproduction.

#### Scenario: Unsupported content
- **WHEN** conversion encounters an embedded image or page-layout feature outside the supported contract
- **THEN** the operator sees a warning or actionable conversion error before approval
- **AND** no attachment is added automatically

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
