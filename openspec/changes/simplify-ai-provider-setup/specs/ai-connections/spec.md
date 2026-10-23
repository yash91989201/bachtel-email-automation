# Spec Delta

## MODIFIED Requirements

### Requirement: One confirmed AI connection per operator
The system SHALL maintain at most one confirmed AI connection per operator. Operators SHALL configure their provider, exact model identifier, API credentials and required endpoint settings through Account settings. Proposed settings SHALL remain separate from the active configuration until successfully validated and explicitly confirmed.

#### Scenario: Configure the first connection
- **WHEN** the operator selects a supported provider
- **THEN** only that provider's applicable fields, endpoint guidance and next action appear
- **AND** OpenCode Go and Xiaomi MiMo are offered with editable default endpoints, without being activated implicitly
- **AND** the operator reviews the exact provider, endpoint and selected model before confirmation

#### Scenario: Unsupported or incomplete setup
- **WHEN** required values are missing or the provider, API format or model is unsupported by the selected connection
- **THEN** setup identifies the problem and no new configuration becomes active
- **AND** merely entering another model name does not assert support

### Requirement: Supported providers share the connection contract
The system SHALL support OpenCode Go and Xiaomi MiMo presets with editable endpoints, model discovery and automatic API-format selection. Custom compatible connections SHALL expose endpoint, supported API format, exact model ID and API key explicitly. Every confirmed configuration SHALL record its resolved format and retain the same private validation, fixed-selection and pending-task-lock behavior.

#### Scenario: Custom compatible endpoint
- **WHEN** the operator supplies a permitted custom endpoint, supported API format, API key and model identifier
- **THEN** activation requires successful validation of that exact combination
- **AND** no claim is made that every model at that endpoint supports normalization

#### Scenario: Another provider becomes supported
- **WHEN** a later release adds another verified provider preset
- **THEN** its supported models and required settings can be configured through the same account workflow
- **AND** existing connections and saved task evidence remain intact
- **AND** native presets beyond OpenCode Go and Xiaomi MiMo are not required in this change

#### Scenario: Built-in model resolves its format
- **WHEN** the owner validates a supported preset model
- **THEN** the system resolves its API format independently of caller-supplied format values
- **AND** an unsupported or unknown model contract is refused without guessing or calling another model

## ADDED Requirements

### Requirement: Discover supported provider models without activation
The system SHALL fetch models for the selected supported provider using the owner's supplied key and editable endpoint. Discovery SHALL identify compatible models and explain unavailable protocols. It SHALL NOT activate settings, validate normalization, start task work or send email. Missing discovery support or an invalid response SHALL leave the confirmed connection and task data unchanged.

#### Scenario: Fetch and choose a compatible model
- **WHEN** the owner requests models from OpenCode Go or Xiaomi MiMo
- **THEN** compatible models returned by that endpoint can be selected with their API format supplied automatically
- **AND** listed models requiring an unsupported or unknown protocol are unavailable for selection with an explanation
- **AND** fetching a list is distinguished from successful exact-model validation

#### Scenario: Discovery failure and retry
- **WHEN** the endpoint rejects the key, times out, returns malformed or oversized data, or has no compatible models
- **THEN** the operator receives a safe actionable error or empty-state explanation and can retry or change the configuration
- **AND** no secret-bearing request or raw provider response is disclosed

#### Scenario: Endpoint override controls discovery
- **WHEN** the operator replaces a preset endpoint
- **THEN** discovery and subsequent validation use the overridden destination
- **AND** credentials are never sent to a different provider destination as a fallback

#### Scenario: Unsafe or foreign discovery
- **WHEN** an unauthenticated request attempts discovery, or a submitted endpoint targets a prohibited service or redirects to another origin
- **THEN** access or the outbound request is refused before credentials reach a prohibited destination
- **AND** discovery responses do not disclose saved credentials or another operator's settings
