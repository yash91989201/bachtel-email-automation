# AI task data preparation

## Why

Operators need mapped-field normalization and actionable uncertainty evidence before reviewing sending data.

## What Changes

- Normalize mapped values only, guided by headers, a visible base prompt and task-specific extensions.
- Preserve comma-space lists and recipient occurrences; distinguish corrections from unresolved findings.
- Persist immutable run/source/prompt/provider/model/output evidence and durable explicit retry/rerun.
- Integrate preparation with task closure and fixed confirmed account configuration; keep output unavailable to sending until the reviewed-snapshot change is installed.

## Capabilities

### New Capabilities

- `task-data-preparation`: Mapped-field AI preparation, findings and durable evidence.

### Modified Capabilities

None.

## Impact

`account-connection-settings` must be implemented and verified first. This backend stage is independently testable but does not replace legacy intake or expose candidate sending from AI output. Operator review/download/finalization UI and sending integration belong to `reviewed-task-sending-snapshots`.

Planning only. Use existing dependencies and simulated providers; no live credentials, production data reset, real email in automated checks, automatic sending or provider fallback. Native MiMo/Fireworks presets remain future work.
