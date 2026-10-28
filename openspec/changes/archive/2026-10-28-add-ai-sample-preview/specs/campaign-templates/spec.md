## MODIFIED Requirements

### Requirement: Preview and source visibility
The system SHALL show the To recipient, subject and rendered body for supplied sample values and allow previewing a finalized task recipient using the same rendering rules as sending. The direct sample action SHALL use supplied values without AI or formatting transformations. A separate explicit AI sample action SHALL prepare values before rendering. Backend recipient and safety validation SHALL apply to both actions.

#### Scenario: Sample preview
- **WHEN** the operator chooses Render preview with supplied sample values
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

## ADDED Requirements

### Requirement: Explicit AI sample normalization
The owning operator SHALL be able to explicitly normalize one sample row using the same header-guided base policy and validation contract as task preparation. The action SHALL use campaign mappings and the current base prompt without task-specific instructions, then render validated prepared values using the selected revision's rendering rules. Normal viewing and direct rendering SHALL NOT invoke AI.

#### Scenario: Normalize noisy sample values
- **WHEN** the owner chooses Normalize with AI & preview for valid mapped sample values
- **THEN** the result identifies itself as an AI-prepared sample and shows the To recipient, subject and body from validated prepared values
- **AND** the result shows original, prepared and effective rendered values, corrections, model identity and base prompt version, with the base policy available for inspection
- **AND** the original form values remain unchanged
- **AND** the app explains that workbook context and task-specific instructions can produce different results

#### Scenario: Findings need review
- **WHEN** a valid AI response contains an unresolved mapped-field finding
- **THEN** the result displays the finding and marks the sample Needs review, even for an optional field
- **AND** any safe render remains labeled illustrative and does not imply task eligibility or approval
- **AND** successful corrections alone do not produce Needs review

#### Scenario: Backend rendering refuses prepared values
- **WHEN** prepared values omit a required value, contain an invalid recipient or introduce unsafe subject content
- **THEN** the app displays the backend validation error and available validated comparisons/findings
- **AND** it does not present a successful email preview or silently substitute original values

#### Scenario: One sample recipient only
- **WHEN** either sample action receives a recipient list, display-name wrapper or recipient header-control character
- **THEN** the action refuses the input with an actionable single-recipient error
- **AND** the AI action makes no provider request and neither action expands the sample into several recipients

### Requirement: Temporary sample processing
Sample inputs, prepared values, corrections and findings SHALL remain temporary request/page data. The app SHALL NOT persist them in records, files, logs, caches or browser storage, or create task/run/job/snapshot/attempt/approval state. Before the AI action, the UI SHALL disclose provider processing, possible usage charges and that app non-retention does not guarantee provider non-retention.

#### Scenario: Process without saving
- **WHEN** a sample action succeeds, fails or is cancelled
- **THEN** no sample input, provider payload or output is persisted by the app and no task or approval state changes
- **AND** responses are marked no-store and operational logs contain no sample contents or private raw provider response
- **AND** the current result can be dismissed and is not restored by the app after navigation or reload

#### Scenario: No authorization from a sample
- **WHEN** the operator reviews an AI-prepared sample
- **THEN** no test email is sent and no received-format approval or start/schedule authorization is granted
- **AND** the label explains that final task preview, test and sending use the accepted final snapshot

### Requirement: Bounded and current AI sample execution
Each explicit AI sample action SHALL make at most one bounded provider inference request without automatic retry. Concurrent sample work SHALL be bounded per operator and across the server. Invalid input, unavailable capacity or unusable setup SHALL be refused visibly. Stale results SHALL NOT be presented as current.

#### Scenario: Provider failure or invalid response
- **WHEN** sample inference times out, is throttled or returns incomplete, malformed or recipient-altering output
- **THEN** the action displays a safe error with an explicit retry or repair remedy
- **AND** no partial output is rendered as successful preparation and no raw-data or provider/model fallback occurs
- **AND** original inputs remain available for correction or deliberate direct rendering

#### Scenario: Repeated action and cancellation
- **WHEN** another sample request is active for the same owner or the server sample capacity is full
- **THEN** the new request is refused as busy without an outbound request or durable queue
- **WHEN** the browser request is cancelled or its deadline expires
- **THEN** processing is cancelled and capacity is released without background continuation

#### Scenario: Sample or revision changes during inference
- **WHEN** inputs change, another action replaces the result, or the campaign revision changes while inference is pending
- **THEN** the earlier result is discarded or visibly invalidated and cannot overwrite a newer result
- **AND** a new explicit action is required to prepare the changed sample
