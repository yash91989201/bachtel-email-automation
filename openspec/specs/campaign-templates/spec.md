# campaign-templates Specification

## Purpose

Define reusable campaign email configurations with a separate subject, DOCX body and explicit mappings that can be validated, previewed and approved by revision.

## Requirements

### Requirement: Campaign configuration
The system SHALL let the operator create a named campaign with a separate non-empty subject, a DOCX body, an expected recipient header, configurable variable-to-header mappings and configurable result-column names.

#### Scenario: Create configuration without candidate upload
- **WHEN** the operator uploads a DOCX and supplies subject, expected headers and mappings
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
The system SHALL recognize double-brace variables in subject and body, report configured variables absent from both, and report template variables without a mapping. Invalid configuration SHALL NOT become ready for sending.

#### Scenario: Missing configured variable
- **WHEN** the operator configures location but neither subject nor body contains its placeholder
- **THEN** the configuration displays a location-specific error

#### Scenario: Unknown body or subject variable
- **WHEN** either subject or body references a variable with no mapping
- **THEN** the app reports its name and prevents use of that configuration

#### Scenario: Subject-only variable
- **WHEN** a configured variable appears only in the subject
- **THEN** it satisfies the usage check and is replaced there

### Requirement: Semantic DOCX formatting
The system SHALL preserve paragraphs, headings, ordered and unordered lists, bold, italics and safe links through conversion and personalization. It SHALL support placeholders split across Word text runs within a paragraph and report unsupported or malformed placeholder structures.

#### Scenario: Formatting survives conversion
- **WHEN** a valid DOCX contains the supported formatting and a placeholder split across adjacent runs
- **THEN** the preview retains the semantic formatting and replaces the complete placeholder

#### Scenario: Unreadable document
- **WHEN** the upload is corrupt, not a supported DOCX or exceeds documented processing limits
- **THEN** the app reports a conversion error and does not mark the configuration ready

### Requirement: Immutable configuration revisions
The system SHALL preserve revisions of the subject, original DOCX, rendered body, mappings, required flags, fallbacks and normalization rules. Any change affecting personalized content SHALL create a new unapproved revision.

#### Scenario: Edit after formatting approval
- **WHEN** the operator changes the subject, template, mappings, fallback or normalization settings
- **THEN** the new revision requires its own test-email approval
- **AND** previously stored email content and running task snapshots remain unchanged

#### Scenario: Presentation settings do not alter email content
- **WHEN** only downloadable result-column labels change
- **THEN** task exports retain their chosen label snapshot
- **AND** the app does not pretend the personalized subject or body changed

### Requirement: Preview and source visibility
The system SHALL show the subject and rendered body for supplied sample values and, after task upload, allow previewing any eligible candidate using the same rendering rules as sending.

#### Scenario: Candidate preview
- **WHEN** the operator selects a candidate from a validated task
- **THEN** the app shows the personalized recipient, subject and body plus raw and normalized mapped values
- **AND** previewing does not create a sending attempt

### Requirement: Bounded rendering scope
The system SHALL document that Word page layout, macros, attachments and embedded-image delivery are outside the initial rendering contract, and visibly report unsupported document content rather than claiming exact Word reproduction.

#### Scenario: Unsupported content
- **WHEN** conversion encounters an embedded image or page-layout feature outside the supported contract
- **THEN** the operator sees a warning or actionable conversion error before approval
- **AND** no attachment is added automatically
