# Reviewed task sending snapshots

## Why

Operators must review the exact values used for sending and approve formatting for each accepted final dataset.

## What Changes

- Remove mapping formats for new authoring while preserving historical content and versions.
- Combine AI findings with backend required/optional, address, duplicate, campaign-history and safety validation.
- Offer Approve as-is or edited final upload; never call AI after final upload. Preserve mixed acceptance, zero-accepted and structural rejection behavior.
- Provide protected prepared/final/exclusion downloads, round-trip labels and original/final provenance.
- Seal immutable final snapshots, use final values directly in preview/test/send, and require a submitted test plus received-format approval per exact task/snapshot/configuration/mailbox generation.
- Integrate complete task UI, migration, started-task freeze and end-to-end verification.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `task-data-preparation`: Add both finalization paths, snapshot identity and started-content freeze.
- `campaign-templates`: Mapping syntax, direct samples and historical revisions.
- `email-personalization`: Direct final-value rendering.
- `excel-intake`: Deterministic validation and recipient expansion.
- `email-dispatch`: Exact-snapshot approval and explicit sending boundaries.
- `campaign-task-workspaces`: Preparation/review/finalization workflow.
- `history-exports`: Sending/exclusion downloads and provenance.

## Impact

`account-connection-settings` and `ai-task-data-preparation` must be implemented and verified first. Activate the new intake/review/rendering/approval path together; do not temporarily allow campaign-only approval or raw AI output to authorize sending.

Planning only. Use existing dependencies and simulated providers; no live credentials, production data reset, real email in automated checks, automatic sending or provider fallback. Native MiMo/Fireworks presets remain future work.
