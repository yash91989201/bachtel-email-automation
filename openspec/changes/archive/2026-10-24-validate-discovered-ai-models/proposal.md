# Proposal

## Why

Provider discovery returns models that the application disables solely because their IDs are absent from a compiled model-to-format table. This makes new models unusable until the application is updated, even when an existing API adapter could serve them.

## What Changes

- Remove the hardcoded preset model eligibility and model-to-format tables. Make every syntactically valid discovered model selectable, without claiming compatibility from discovery alone.
- Resolve a new preset configuration's API format through bounded synthetic validation of the exact submitted endpoint and model, using the preset's supported API formats. Stage only a configuration that passes both the format response parser and the normalization contract.
- Show the successfully validated format before explicit confirmation. Explain compatibility failures and possible usage from multiple synthetic requests.
- Preserve saved connections and same-configuration credential repair without discovery, using their recorded API format. Preserve owner isolation, endpoint protections, stale-confirmation checks and pending-task switching locks.
- Keep Custom compatible setup manual and retain fixed endpoint, model and format use after confirmation; runtime preparation does not probe or fall back.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ai-connections`: Replace compiled model eligibility and format assignment with selectable discovery results and exact-configuration compatibility validation before staging and activation.
- `account-settings`: Present all discovered valid model IDs as unvalidated choices, display the format established by validation, and preserve discovery input invalidation and repair flows.

## Impact

- Affects `internal/ai` preset reference data, discovery, proposal normalization and synthetic validation; Account AI handlers/view models, `internal/web/views/account.templ`, the existing browser discovery controller, their tests, and `docs/account-settings.md`.
- Reuses the existing Responses and Chat Completions adapters, encrypted draft/confirmation storage, authenticated settings actions and request safety policy. No new dependency or database schema is expected.
- Builds on the implemented and archived `simplify-ai-provider-setup` workflow. This deliberately supersedes its rule that unknown model contracts are refused and discovered unknown models are disabled; it does not expand the supported wire protocols.
- Out of scope: new providers or API adapters, automatic background model refresh, mass validation of discovered models, runtime protocol/model fallback, real-provider evaluation, email sending, deployment and changes to task preparation or sending approvals.
- Prerequisites for activation remain successful exact-configuration validation, explicit owner confirmation and the existing account lock checks. Planning and automated verification require no real API key or email authorization.
