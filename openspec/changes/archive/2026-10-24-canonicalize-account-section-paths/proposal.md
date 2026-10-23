# Proposal

## Why

Mailbox currently renders at both `/account` and `/connection`, while AI connection already has `/account/ai`. Complete the previously requested account URL structure by making `/account/mailbox` the canonical Mailbox section without requiring a general Account landing page.

## What Changes

- Serve the protected Mailbox section at `/account/mailbox` and retain `/account/ai` for AI connection.
- Leave `/account`, `/account/connections` and the old `/connection` landing URL unserved (404), without compatibility redirects.
- Update Account navigation, mailbox remedies, provider-chooser links and mailbox-operation return destinations to canonical section URLs.
- Keep mailbox OAuth callback and existing mutation/replacement endpoints working, with the same explicit confirmation, isolation and pending-task restrictions.
- Preserve the separate AI discovery, synthetic validation and confirmation flow at its existing endpoints.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `account-settings`: Canonical Mailbox and AI section URLs, unused landing URLs returning 404 and section-specific feedback.

## Impact

Expected implementation touches account/connection route registration and handlers, Account/Mailbox/layout/home templates, task mailbox remedies, their generated template output and associated account/mailbox tests. No provider, model, credential-storage, database or authorization changes are planned. Existing `ai-connections` requirements remain unchanged; editable preset endpoints, discovery and exact-model confirmation are already specified there and are not reimplemented by this routing change.

This change is independently applicable from `use-paths-for-workspace-sections`. If applied after it, update task mailbox remedies against the new section-link helpers rather than restoring fragment links. No live OAuth, real email, external AI validation or deployment is authorized by this proposal.
