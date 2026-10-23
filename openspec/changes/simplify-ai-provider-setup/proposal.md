# Proposal

Status: specification-only at the operator's request. Implementation edits were reverted; the change records the requested behavior for a future apply.

## Why

Supported AI connections currently require operators to know exact model IDs and API formats, and the OpenCode Go preset prevents endpoint edits. Provider defaults and model discovery can supply those details while preserving deliberate model selection and exact-configuration validation.

## What Changes

- Offer OpenCode Go and Xiaomi MiMo with an API key, an editable verified default endpoint and a Fetch models action.
- Let the operator choose a discovered compatible model and select its API format automatically on the server.
- Identify models whose protocol is unsupported or unknown rather than guessing a format.
- Keep custom connections fully explicit for the supported protocols: endpoint, API format, exact model ID and API key.
- Bound and secure model discovery exactly like inference; discovery never activates settings or starts work.
- Preserve confirmed configuration, credential confidentiality, synthetic validation, explicit confirmation and pending-task switching locks.
- Synchronize the affected main specs after implementation and verification. Fireworks, new inference protocols, arbitrary authentication/header scripting and live provider calls are outside this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ai-connections`: supported provider discovery, editable defaults, automatic protocol selection and custom manual setup.
- `account-settings`: provider-specific setup controls and discovery reset/error behavior.

## Impact

Changes affect `internal/ai`, Account handlers/templates and focused tests, provider migration constraints if necessary, and setup documentation. Existing HTTP, JSON and UI dependencies are sufficient. No email behavior, production credentials or candidate data are involved. Existing account connection work is implemented; the pending reviewed-snapshot change is not a prerequisite for this setup improvement.
