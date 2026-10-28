## MODIFIED Requirements

### Requirement: Incomplete and failed AI processing
The system SHALL make AI failures and incomplete or invalid responses visible and retryable. Missing, failed or structurally invalid results SHALL NOT become eligible through silent use of raw data or fallback text. Reopening a page, task preview, direct sample rendering, downloading or uploading a final sheet SHALL NOT invoke AI. Only the separate explicit campaign AI sample action SHALL be exempt from this preview prohibition; it SHALL NOT create or modify task preparation history.

#### Scenario: Partial response or timeout
- **WHEN** a provider times out or omits, duplicates or misidentifies a requested row or mapped field
- **THEN** affected results remain unavailable for sending with a processing reason
- **AND** the operator can explicitly retry failed preparation without changing previously saved run evidence

#### Scenario: Server interruption
- **WHEN** preparation is interrupted by restart or browser closure
- **THEN** completed evidence remains durable and reopening shows the actual processing state
- **AND** unresolved work never appears successfully prepared or finalized

#### Scenario: Explicit campaign sample is separate from task preparation
- **WHEN** the owner explicitly requests AI normalization of a campaign sample
- **THEN** it uses the same base normalization policy and response contract without creating a task or saved preparation run
- **AND** merely reopening or rendering an existing sample or task result does not invoke AI
- **AND** task preparation retains durable input, run history, retry and recovery evidence independently of temporary samples

