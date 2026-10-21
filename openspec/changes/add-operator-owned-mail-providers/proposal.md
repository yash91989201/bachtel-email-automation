# Proposal

## Why

The application currently binds one operator to one fixed Microsoft mailbox. Independent operators need private campaign workspaces and exclusive mailbox connections, initially using Microsoft 365 or GoDaddy Titan, without rebuilding dispatch for each future provider.

## What Changes

- **BREAKING**: Allow multiple administratively provisioned operators, each with private campaigns, templates, candidates, uploads, tasks, approvals, history and exports. Keep public registration disabled.
- **BREAKING**: Replace the singleton fixed HR mailbox with one exclusive mailbox per operator, supporting Microsoft 365 delegated sign-in and GoDaddy Titan authenticated SMTP initially. Reject duplicate mailbox claims across operators.
- Share the existing durable sending workflow across provider implementations; scope credentials, pacing, cooldowns and service pauses to each mailbox, with a bounded overall worker capacity.
- Bind formatting approval and task authorization to the current mailbox connection as well as the exact content revision; reconnecting never resumes work automatically.
- Allow explicit mailbox replacement after stopping launches and draining active submissions. Preserve campaigns, source-header suggestions, result settings, DOCX templates and configuration revisions; delete only the replacing operator's candidates, tasks, uploads, approvals, attempts/history and generated exports. Explain that deleting duplicate-prevention history allows those recipients to be sent again after a new upload and approval.
- Permit a documented destructive local-development data reset for the schema transition. Do not require legacy compatibility or preservation of old sender history. This planning change does not execute a reset.

## Capabilities

### New Capabilities

- `mail-connections`: Exclusive operator-owned mailbox connections, confidential credentials, Titan connection behavior, reconnect and destructive replacement lifecycle.

### Modified Capabilities

- `operator-access`: Administrative multi-operator provisioning and private record/file authorization.
- `microsoft-connection`: Remove the fixed HR identity and retain Microsoft-specific delegated identity, callback, refresh and setup behavior.
- `campaign-candidates`: Preserve campaign recipient identity and reservations within operator ownership, with an explicit mailbox-reset exception.
- `email-dispatch`: Mailbox-bound approval, sender selection, bounded mailbox controls and provider-specific acceptance/throttling.
- `sending-recovery`: Provider-neutral outcomes and mailbox-scoped recovery/pauses, retaining safe retry and uncertainty rules.
- `history-exports`: Owner-only historical downloads, reset deletion and no requirement to retain an obsolete sender identity.
- `coolify-deployment`: Multi-operator persistent service, provider setup and encrypted credential backup/restore.

## Impact

Affected areas include operator provisioning, PocketBase collections and constraints, campaign/intake/sending/export/retention services, web authorization and connection UI, worker wiring, configuration, provider setup and deployment documentation, and simulated acceptance tests. Keep Go/PocketBase, the existing OAuth dependency and MIME-building dependency; prefer standard SMTP/TLS primitives behind the existing small sender boundary over a new provider framework.

The active `remove-candidate-release-gate` change has implementation work complete but integration verification outstanding. Finish its verification and sync/archive its overlapping dispatch/deployment deltas before this change is synced or archived; do not restore its obsolete gate or mark that work complete here. Existing shadcn UI work remains separate.

Out of scope: public registration, organization/team sharing, multiple simultaneous mailboxes per operator, shared-mailbox delegation or alias send-as, provider installation at runtime, receiving/synchronizing inboxes, new attachment support, production data migration, live email, deployment and implementation in this planning request. Future providers require a reviewed adapter and tests, not a new campaign/recovery subsystem.
