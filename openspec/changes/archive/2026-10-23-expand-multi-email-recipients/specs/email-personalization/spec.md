# Email Personalization Delta

## MODIFIED Requirements

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

