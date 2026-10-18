# Spec Delta

## ADDED Requirements

### Requirement: Mapping help example follows the campaign
The system SHALL present the mapping help's example row and example line with the campaign's own expected recipient header: the variable name and source header SHALL equal the first row the default draft derives for that campaign, and the format SHALL be the draft's `text`.

#### Scenario: Example matches the draft
- **WHEN** a campaign's expected recipient header starts with `Candidate Name`
- **THEN** the example line shows `candidate_name | Candidate Name | required | text`
- **AND** the example variable name is the name the draft uses for that header

#### Scenario: Campaign without a usable header
- **WHEN** no expected recipient header can form a variable name
- **THEN** the example uses a generic `variable_name` placeholder instead of a campaign-specific name

## MODIFIED Requirements

### Requirement: Variable consistency validation
The system SHALL recognize double-brace variables in subject and body, report configured variables absent from both, and report template variables without a mapping. The unmapped-placeholder error SHALL name every offending placeholder and state the remedy: add a mapping row for that name, or rename the placeholder in the subject or body. Invalid configuration SHALL NOT become ready for sending.

#### Scenario: Missing configured variable
- **WHEN** the operator configures location but neither subject nor body contains its placeholder
- **THEN** the configuration displays a location-specific error

#### Scenario: Unknown body or subject variable
- **WHEN** either subject or body references a variable with no mapping
- **THEN** the app reports its name and prevents use of that configuration
- **AND** the report states that a mapping row must be added for that name or the placeholder renamed in the subject or body

#### Scenario: Subject-only variable
- **WHEN** a configured variable appears only in the subject
- **THEN** it satisfies the usage check and is replaced there
