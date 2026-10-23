# Account connection settings

## Why

Operators need private, switchable AI setup and safe mailbox management without changing connections beneath pending work.

## What Changes

- Add Account settings with Mailbox and AI connection sections, preserving existing mailbox authorization and callbacks.
- Configure one verified AI connection per operator, encrypted keys, exact model selection, supported Responses/Chat Completions adapters, OpenCode Go and custom compatible setup, and no automatic fallback.
- Enforce account-wide pending-task locks, named blockers, initial setup and same-configuration repair exceptions.
- Add terminal Close task, bounded settlement, preserved uncertainty/history, and atomic lock reacquisition for completed-task recovery.

## Capabilities

### New Capabilities

- `account-settings`: Account sections and shared connection locks.
- `ai-connections`: Private verified AI configuration and extensible provider support.

### Modified Capabilities

- `mail-connections`: Pending-task change restrictions and preserved replacement/repair contracts.
- `campaign-task-workspaces`: Account mailbox controls and terminal task closure.

## Impact

None. This is independently deployable with the existing intake/sending workflow. It does not enable workbook AI preparation.

Planning only. Use existing dependencies and simulated providers; no live credentials, production data reset, real email in automated checks, automatic sending or provider fallback. Native MiMo/Fireworks presets remain future work.
