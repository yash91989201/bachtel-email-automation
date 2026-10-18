# Spec Delta

## MODIFIED Requirements

### Requirement: Variable consistency validation
The system SHALL recognize double-brace variables in subject and body, report configured variables absent from both, and report template variables without a mapping. Each error SHALL name every offending variable and state its remedy: for an unmapped placeholder, add a mapping row for that name or rename the placeholder in the subject or body; for a configured variable absent from both, remove its mapping row or add its placeholder to the subject or body. Invalid configuration SHALL NOT become ready for sending.

#### Scenario: Missing configured variable
- **WHEN** the operator configures location but neither subject nor body contains its placeholder
- **THEN** the configuration displays a location-specific error
- **AND** the error states that the row is removed or the placeholder added

#### Scenario: Unknown body or subject variable
- **WHEN** either subject or body references a variable with no mapping
- **THEN** the app reports its name and prevents use of that configuration

#### Scenario: Subject-only variable
- **WHEN** a configured variable appears only in the subject
- **THEN** it satisfies the usage check and is replaced there
