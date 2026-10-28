## MODIFIED Requirements

### Requirement: Confirmed model is the only runtime model
Each task normalization run SHALL record and use the operator's confirmed provider, endpoint, model and inference settings. Explicit AI sample normalization SHALL use the same owner-scoped confirmed selection, showing its identity in the temporary result without persisting sample run evidence. Retries SHALL retain that selection. The system SHALL NOT silently choose a fallback provider/model or use deployment-wide credentials. Unavailable configuration SHALL block AI preparation with a setup or repair remedy.

#### Scenario: Model is unavailable during preparation
- **WHEN** the confirmed model fails, is unavailable or is removed by its provider
- **THEN** the run remains failed or incomplete and no other model is called
- **AND** completing or closing pending tasks is required before deliberately switching configuration

#### Scenario: Settings change after earlier work completes
- **WHEN** the operator confirms another model after the account lock is released
- **THEN** future runs use the newly confirmed selection
- **AND** earlier outputs, run provenance, final snapshots and applicable formatting approvals remain unchanged
- **AND** final-sheet upload, task preview, direct sample rendering, download and sending still do not call AI

#### Scenario: Temporary sample uses the confirmed selection
- **WHEN** the owner explicitly normalizes a sample
- **THEN** the call uses their confirmed provider, endpoint, API format, exact model and application inference settings
- **AND** no discovery, compatibility probing, alternate model or deployment-wide credential is used
- **AND** absent or unreadable credentials block only the AI sample action with an Account settings remedy

#### Scenario: Connection changes during sample processing
- **WHEN** the connection configuration or credential generation changes before the sample result is returned
- **THEN** the action reports that settings changed and discards the result
- **AND** it does not combine a captured endpoint with credentials from a different configuration or automatically rerun the sample
- **AND** existing task-switch locks and credential-repair rules remain in force


### Requirement: Safe endpoints and bounded AI requests
The system SHALL use certificate-verified encrypted connections to permitted endpoints, bound request time and response size, and prevent operator-supplied endpoints or redirects from exposing credentials or reaching private deployment services. Provider errors SHALL be actionable without exposing secret-bearing requests or private raw responses.

#### Scenario: Unsafe endpoint or redirect
- **WHEN** proposed settings target a prohibited local/private service, use plaintext transport or redirect credentials to another origin
- **THEN** validation and activation are refused without transmitting the secret to the prohibited destination

#### Scenario: Preparation data boundary
- **WHEN** a confirmed connection processes a workbook
- **THEN** only the mapped values and necessary preparation metadata are sent to that configured provider
- **AND** Account settings identifies the external processing destination and known provider data-handling limitations

#### Scenario: Sample data boundary
- **WHEN** the owner explicitly requests AI sample preparation
- **THEN** only mapped sample values, mapped field metadata and necessary normalization instructions are sent to the confirmed provider
- **AND** the template subject/body, uploaded document and unrelated task/workbook data are excluded
- **AND** endpoint restrictions, transport security, request/response bounds and safe error handling apply to the sample request
- **AND** the sample form identifies the external processing destination and possible provider usage charges before submission

