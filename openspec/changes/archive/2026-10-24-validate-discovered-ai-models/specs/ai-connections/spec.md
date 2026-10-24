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
- **WHEN** required values are missing, the provider or explicit API format is unsupported, or the model identifier is malformed
- **THEN** setup identifies the problem and no new configuration becomes active
- **AND** absence from a compiled model list does not make a valid model identifier unsupported
- **AND** selecting or entering a model identifier alone does not assert compatibility

### Requirement: Validate the selected model before activation
The system SHALL validate the exact proposed endpoint and model against the normalization response contract before staging settings for confirmation. New preset selections SHALL resolve a supported API format through bounded synthetic validation. Validation SHALL disclose possible provider usage charges. Failure, cancellation or stale confirmation SHALL preserve the confirmed configuration and task data.

#### Scenario: Model verification fails
- **WHEN** the endpoint rejects authentication, cannot access the selected model or cannot return the required output through any attempted format
- **THEN** no newly validated proposal is staged or activated and the owner receives a safe actionable error
- **AND** no workbook values, test email or candidate email are used for connection validation
- **AND** the error distinguishes compatibility failure from authentication, throttling or service failure where the provider outcome permits it

#### Scenario: Successful confirmation
- **WHEN** the owner confirms successfully validated settings and no pending-task lock prevents the change
- **THEN** those exact settings, including the successfully validated API format, become the single confirmed AI configuration
- **AND** confirmation does not prepare a workbook, change existing snapshots or authorize sending

#### Scenario: Newly discovered model passes compatibility validation
- **WHEN** the owner validates a valid discovered model ID absent from any previously shipped model catalog
- **THEN** the system tests the exact submitted endpoint and model using the preset's supported API formats
- **AND** it stages settings only when a response passes both API-format parsing and the required normalization output checks
- **AND** a successful HTTP response or a discovery listing alone is insufficient
- **AND** validation stops at the first passing format and shows that format for review

#### Scenario: One API format is incompatible
- **WHEN** a new preset selection's first tested format returns HTTP 400, 404, 405, 415 or 422, an invalid expected response envelope, or an invalid normalization result, and another supported format and time budget remain
- **THEN** validation tests the remaining format with the same endpoint, model and supplied key
- **AND** each supported format is attempted at most once, with at most two synthetic requests and a single overall 30-second deadline
- **AND** no alternate provider, endpoint or model is selected

#### Scenario: Validation must stop
- **WHEN** validation encounters authentication refusal, rate limiting, service or transport failure, cancellation, an exhausted deadline, an unsafe destination, an oversized response or any other failure outside the compatibility continuation cases
- **THEN** it stops without attempting another format or automatically retrying
- **AND** no newly validated proposal is staged and the owner can correct the setup or explicitly retry

#### Scenario: Revalidate a changed selection
- **WHEN** the owner changes provider, endpoint, model, explicit API format or the supplied key after successful validation
- **THEN** the changed proposal requires fresh validation before it can be confirmed
- **AND** a prior successful result cannot authorize the changed values
- **AND** stale confirmation and pending-task checks still apply when activating a staged proposal

#### Scenario: Repair preserves the recorded format
- **WHEN** the owner validates a replacement key for the unchanged confirmed preset selection
- **THEN** validation uses only its recorded API format without requiring discovery or a compiled model entry
- **AND** failure does not trigger automatic format switching
- **AND** a format change is treated as a configuration switch subject to the existing task lock

### Requirement: Supported providers share the connection contract
The system SHALL support OpenCode Go and Xiaomi MiMo presets with editable endpoints, selectable discovered models and API-format selection established by validation. Custom compatible connections SHALL expose endpoint, supported API format, exact model ID and API key explicitly. Every confirmed configuration SHALL record its validated format and retain the same private validation, fixed-selection and pending-task-lock behavior.

#### Scenario: Custom compatible endpoint
- **WHEN** the operator supplies a permitted custom endpoint, supported API format, API key and model identifier
- **THEN** activation requires successful validation of that exact combination
- **AND** no other format is automatically attempted for this manual setup
- **AND** no claim is made that every model at that endpoint supports normalization

#### Scenario: Another provider becomes supported
- **WHEN** a later release adds another verified provider preset
- **THEN** its discovered models and required settings can be configured through the same account workflow
- **AND** existing connections and saved task evidence remain intact
- **AND** native presets beyond OpenCode Go and Xiaomi MiMo are not required in this change

#### Scenario: Built-in model resolves its format
- **WHEN** the owner validates a new preset selection
- **THEN** the system establishes the format through successful compatibility validation independently of caller-supplied format values or model-name heuristics
- **AND** the absence of a model-specific mapping does not prevent validation
- **AND** subsequent preparation and retries use only the confirmed format without protocol probing or provider/model fallback

### Requirement: Discover supported provider models without activation
The system SHALL fetch models for the selected supported provider using the owner's supplied key and editable endpoint. Every syntactically valid returned model ID SHALL be selectable without a compiled eligibility list. Discovery SHALL NOT assert compatibility, validate normalization, stage or activate settings, start task work or send email. Missing discovery support or an invalid response SHALL leave the confirmed connection and task data unchanged.

#### Scenario: Fetch and choose a compatible model
- **WHEN** the owner requests models from OpenCode Go or Xiaomi MiMo
- **THEN** every syntactically valid returned model ID is selectable, including previously unknown IDs
- **AND** invalid entries are omitted and duplicate IDs appear once in provider response order
- **AND** the list does not label an untested model or format as verified
- **AND** selecting a model still requires exact-configuration validation and explicit confirmation before activation

#### Scenario: Discovery failure and retry
- **WHEN** the endpoint rejects the key, times out, returns malformed or oversized data, or has no valid model IDs
- **THEN** the operator receives a safe actionable error or empty-state explanation and can retry or change the configuration
- **AND** no secret-bearing request or raw provider response is disclosed
- **AND** no compiled fallback model list is substituted

#### Scenario: Endpoint override controls discovery
- **WHEN** the operator replaces a preset endpoint
- **THEN** discovery and subsequent validation use the overridden destination
- **AND** credentials are never sent to a different provider destination as a fallback

#### Scenario: Unsafe or foreign discovery
- **WHEN** an unauthenticated request attempts discovery, or a submitted endpoint targets a prohibited service or redirects to another origin
- **THEN** access or the outbound request is refused before credentials reach a prohibited destination
- **AND** discovery responses do not disclose saved credentials or another operator's settings
