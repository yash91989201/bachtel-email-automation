# ai-connections Specification

## Purpose
Let each operator configure and verify one private AI connection for normalization, with a deliberate model selection and a consistent setup contract as supported providers evolve.

## Requirements

### Requirement: One confirmed AI connection per operator
The system SHALL maintain at most one confirmed AI connection per operator. Operators SHALL configure their provider, exact model identifier, API credentials and required endpoint settings through Account settings. Proposed settings SHALL remain separate from the active configuration until successfully validated and explicitly confirmed.

#### Scenario: Configure the first connection
- **WHEN** the operator selects a supported provider
- **THEN** only that provider's applicable fields, endpoint guidance and next action appear
- **AND** OpenCode Go is offered as the initial preset with Muse Spark 1.3 as a suggested model, without being activated implicitly
- **AND** the operator reviews the exact provider, endpoint and model before confirmation

#### Scenario: Unsupported or incomplete setup
- **WHEN** required values are missing or the provider, API format or model is unsupported by the selected connection
- **THEN** setup identifies the problem and no new configuration becomes active
- **AND** merely entering another model name does not assert support

### Requirement: Validate the selected model before activation
The system SHALL validate proposed settings using the exact selected model and supported normalization response contract before activation. Validation SHALL use synthetic data and disclose possible provider usage charges. Failed validation, cancellation or a stale confirmation SHALL preserve the existing confirmed configuration and all task data.

#### Scenario: Model verification fails
- **WHEN** the endpoint rejects authentication, cannot access the selected model or cannot return the required output
- **THEN** the proposed connection is not activated and the owner receives a safe actionable error
- **AND** no workbook values, test email or candidate email are used for connection validation

#### Scenario: Successful confirmation
- **WHEN** the owner confirms successfully validated settings and no pending-task lock prevents the change
- **THEN** those exact settings become the single confirmed AI configuration
- **AND** confirmation does not prepare a workbook, change existing snapshots or authorize sending

### Requirement: Confirmed model is the only runtime model
Each normalization run SHALL record and use the operator's confirmed provider, endpoint, model and inference settings. Retries SHALL retain that selection. The system SHALL NOT silently choose a fallback provider/model or use deployment-wide credentials. Unavailable configuration SHALL block AI preparation with a setup or repair remedy.

#### Scenario: Model is unavailable during preparation
- **WHEN** the confirmed model fails, is unavailable or is removed by its provider
- **THEN** the run remains failed or incomplete and no other model is called
- **AND** completing or closing pending tasks is required before deliberately switching configuration

#### Scenario: Settings change after earlier work completes
- **WHEN** the operator confirms another model after the account lock is released
- **THEN** future runs use the newly confirmed selection
- **AND** earlier outputs, run provenance, final snapshots and applicable formatting approvals remain unchanged
- **AND** final-sheet upload, preview, download and sending still do not call AI

### Requirement: Supported providers share the connection contract
The system SHALL support an OpenCode Go preset and a custom compatible connection for supported Responses or Chat Completions APIs. Each configuration SHALL identify its API format explicitly. Later supported providers SHALL follow the same private setup, validation, fixed-selection and pending-task-lock behavior without changing task preparation or finalization rules.

#### Scenario: Custom compatible endpoint
- **WHEN** the operator supplies a permitted custom endpoint, supported API format, API key and model identifier
- **THEN** activation requires successful validation of that exact combination
- **AND** no claim is made that every model at that endpoint supports normalization

#### Scenario: Another provider becomes supported
- **WHEN** a later release adds a verified Xiaomi MiMo or Fireworks preset
- **THEN** its supported models and required settings can be configured through the same account workflow
- **AND** existing connections and saved task evidence remain intact
- **AND** adding native presets for those providers is not required in this change

### Requirement: AI credentials are confidential and owner-scoped
The system SHALL encrypt AI secrets at rest under the deployment-managed key, use them only for the owning operator's confirmed connection, and exclude them from HTML, logs, downloads and application responses. Saved secrets SHALL NOT be returned to prefill forms. Missing or incorrect encryption keys SHALL block affected use without overwriting ciphertext.

#### Scenario: Restart and reopen
- **WHEN** the server restarts with its original key and the operator reopens AI settings
- **THEN** their saved connection remains available and non-secret configuration is visible
- **AND** the secret is indicated as configured without revealing it

#### Scenario: Missing encryption key or foreign access
- **WHEN** decryption fails or another operator requests the connection
- **THEN** no secret or private connection data is disclosed and affected processing cannot use those credentials
- **AND** another operator's usable connection is unaffected

### Requirement: Same-configuration AI credential repair
The owner SHALL be able to validate and rotate the API key for an unchanged provider, endpoint, model and inference configuration while tasks are pending. Repair SHALL preserve run evidence and final content, settle in-flight calls before credential activation, and require an explicit retry for failed work. It SHALL NOT bypass switching restrictions.

#### Scenario: Expired API key
- **WHEN** the owner validates a replacement key for the same confirmed configuration
- **THEN** the new key can become active without changing the selected model or deleting tasks
- **AND** failed preparation is not automatically rerun and completed output is not regenerated

#### Scenario: Repair attempts to switch configuration
- **WHEN** a repair request changes the model, provider, endpoint, API format or inference settings while tasks remain pending
- **THEN** it is refused with the named blocking-task warning and the old configuration remains unchanged

### Requirement: Safe endpoints and bounded AI requests
The system SHALL use certificate-verified encrypted connections to permitted endpoints, bound request time and response size, and prevent operator-supplied endpoints or redirects from exposing credentials or reaching private deployment services. Provider errors SHALL be actionable without exposing secret-bearing requests or private raw responses.

#### Scenario: Unsafe endpoint or redirect
- **WHEN** proposed settings target a prohibited local/private service, use plaintext transport or redirect credentials to another origin
- **THEN** validation and activation are refused without transmitting the secret to the prohibited destination

#### Scenario: Preparation data boundary
- **WHEN** a confirmed connection processes a workbook
- **THEN** only the mapped values and necessary preparation metadata are sent to that configured provider
- **AND** Account settings identifies the external processing destination and known provider data-handling limitations
