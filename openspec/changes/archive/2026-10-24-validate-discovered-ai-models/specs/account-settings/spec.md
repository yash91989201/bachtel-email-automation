# Spec Delta

## MODIFIED Requirements

### Requirement: Provider-first AI setup
Account settings SHALL offer OpenCode Go, Xiaomi MiMo and Custom compatible setup. Presets SHALL accept a key and editable endpoint before fetching models, make every valid discovered model selectable, and establish its API format during validation without a manual format control. Custom setup SHALL retain explicit endpoint, supported API format, model ID and API key controls. Discovery SHALL preserve setup inputs without returning the secret.

#### Scenario: Edit a verified default
- **WHEN** the owner selects OpenCode Go or Xiaomi MiMo
- **THEN** the verified default endpoint is editable and can be restored
- **AND** the page explains model fetching and the separate synthetic validation action
- **AND** before fetching, a new preset setup does not offer a compiled list of supposedly compatible models

#### Scenario: Inputs change during discovery
- **WHEN** provider, endpoint or API key changes after fetching or while a request is pending
- **THEN** the previous discovered selection is cleared or made unavailable
- **AND** a late response cannot replace the models for the current inputs

#### Scenario: Saved connection repair
- **WHEN** an owner reopens settings for a confirmed connection
- **THEN** its non-secret configuration and recorded API format remain visible and the saved secret is not prefilled
- **AND** same-configuration key repair remains available even if model discovery is unavailable or the saved model is no longer listed
- **AND** repair validation uses the recorded format and cannot switch it automatically
- **AND** discovery cannot bypass the pending-task switching lock or activate a different configuration

#### Scenario: Custom manual configuration
- **WHEN** the owner chooses Custom compatible endpoint
- **THEN** manual API-format and exact-model controls remain available without requiring model discovery
- **AND** the exact submitted configuration still requires validation and confirmation

#### Scenario: Choose a newly discovered model
- **WHEN** a valid model ID is returned that the application has never previously recognized
- **THEN** the owner can select it and invoke Validate exact configuration
- **AND** its API format is presented as awaiting validation rather than inferred from its name or claimed as verified
- **AND** the model is not disabled because it lacks a hardcoded mapping

#### Scenario: Review the validated proposal
- **WHEN** compatibility validation succeeds
- **THEN** the page shows the exact proposed provider, endpoint, model and successfully validated API format before explicit confirmation
- **AND** it clearly distinguishes the staged proposal from any active connection
- **AND** the usage disclosure explains that automatic format validation can issue up to two synthetic requests and consume provider usage
- **AND** only synthetic values are used and validation alone starts no workbook preparation or email

#### Scenario: Correct or retry an unsuccessful validation
- **WHEN** no attempted format validates or a stopping failure occurs
- **THEN** the AI section shows a safe actionable problem and preserves valid non-secret inputs, including the selected model ID
- **AND** an unvalidated submitted model ID remains available for correction or retry without being represented as provider-discovered or compatible
- **AND** the entered API key is never returned in HTML, URLs or discovery responses
- **AND** any earlier validated proposal is identified separately and cannot be mistaken for success on the failed selection

#### Scenario: Validation evidence belongs to exact inputs
- **WHEN** the owner edits a provider, endpoint, model, explicit format or supplied key after validation or while validation is pending
- **THEN** the changed setup is not presented as validated or confirmable using the previous result
- **AND** delayed results cannot be attributed to the newer inputs
- **AND** any confirmation still identifies and activates only the exact server-staged proposal subject to current ownership, generation and task-lock checks
