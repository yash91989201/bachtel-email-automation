# Spec Delta

## ADDED Requirements

### Requirement: Default mapping draft
The system SHALL pre-fill the mapping table of a campaign that has no content revision yet with one required `text` mapping per comma-separated expected recipient header, and SHALL leave the table empty once a revision exists. The draft SHALL be editable text in the mapping form and SHALL NOT create, modify or approve any revision.

#### Scenario: New campaign with expected headers
- **WHEN** a campaign whose expected recipient header is `Candidate Name, Candidate Email, Expected Profile` has no content revision
- **THEN** the mapping form is pre-filled with `candidate_name | Candidate Name | required | text`, `candidate_email | Candidate Email | required | text` and `expected_profile | Expected Profile | required | text`
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
