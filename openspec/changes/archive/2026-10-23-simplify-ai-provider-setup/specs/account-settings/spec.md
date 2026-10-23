# Spec Delta

## ADDED Requirements

### Requirement: Provider-first AI setup
Account settings SHALL offer OpenCode Go, Xiaomi MiMo and Custom compatible setup. Supported presets SHALL ask for API key and editable endpoint before fetching models, then offer model selection without a manual API-format control. Custom setup SHALL retain explicit endpoint, supported API format, exact model ID and API key controls. Discovery SHALL preserve the setup inputs without returning the secret in a server response.

#### Scenario: Edit a verified default
- **WHEN** the owner selects OpenCode Go or Xiaomi MiMo
- **THEN** the verified default endpoint is editable and can be restored
- **AND** the page explains model fetching and the separate synthetic validation action

#### Scenario: Inputs change during discovery
- **WHEN** provider, endpoint or API key changes after fetching or while a request is pending
- **THEN** the previous discovered selection is cleared or made unavailable
- **AND** a late response cannot replace the models for the current inputs

#### Scenario: Saved connection repair
- **WHEN** an owner reopens settings for a confirmed connection
- **THEN** its non-secret configuration remains visible and the saved secret is not prefilled
- **AND** same-configuration key repair remains available even if model discovery is unavailable
- **AND** discovery cannot bypass the pending-task switching lock or activate a different configuration

#### Scenario: Custom manual configuration
- **WHEN** the owner chooses Custom compatible endpoint
- **THEN** manual API-format and exact-model controls remain available without requiring model discovery
- **AND** the exact submitted configuration still requires validation and confirmation
