# Proposal

## Why

The operator explicitly requests candidate sending without the development release gate. Retain the successfully submitted test and received-formatting approval for the exact configuration revision.

## What Changes

- Remove the global release gate from candidate actions, scheduled starts and dispatch.
- Keep exact-configuration test approval, explicit batch confirmation, recovery and restore-review protections.
- Remove obsolete UI warnings and update active operating guidance.
- Perform verification only with temporary data and simulated providers; send no real emails.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `email-dispatch`: allow explicitly authorized candidate batches without a release gate while preserving approval.
- `coolify-deployment`: retain repeatable release verification as guidance without a runtime release lock.

## Impact

Sending store, server comments, sending/task templates, simulated tests and active documentation. No dependencies, database migrations, deployments or real email actions. The user's instruction supersedes the previous repository requirement to wait for change 5 release acceptance; this change does not claim that acceptance occurred.
