# Proposal

## Why

Operators need to inspect automatically sent messages in their mailbox's Sent folder, regardless of provider. Titan currently only submits through SMTP, while Microsoft 365 already requests Sent Items saving through Graph's default behavior; the existing provider configuration and sending path need to preserve this requirement consistently.

## What Changes

- Incorporate Sent-copy behavior into the existing provider setup and sending contract: use native provider-managed saving or save the copy after definite acceptance and durable submission recording. Preserve the current provider structure and `Sender` interface.
- Retain Microsoft 365's native Graph Sent Items saving and add Titan's separate post-submission copy step for future candidate and test emails.
- Preserve the submitted recipient, subject and HTML in the owning mailbox's existing Sent folder; Titan's separately saved copy reuses its exact submitted MIME payload.
- Keep copy-saving failures separate from sending outcomes: retain `SUBMITTED`, record a safe warning in existing attempt details, and never resend an email to repair a missing copy.
- Use a bounded, single application-managed copy-saving attempt. Preserve confirmed sending results across interruption without replaying copies on restart. Native provider saving does not receive a second app-generated copy.
- Leave the first run and all earlier messages untouched. Add no backfill, copy-retry button, settings toggle or new operator action.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `email-dispatch`: Establish Sent-copy behavior for every provider, including Microsoft 365 and Titan candidate/test sends, without changing approval or start authorization.
- `sending-recovery`: Persist acceptance before application-managed copy work and preserve it through copy failures and interruption without automatic resend or copy replay.

## Impact

- Affects Titan configuration and its sender, a small extension to send-result/completion handling, a narrow attempt-warning update, composition wiring, and simulated provider tests in `internal/sending`, `internal/config` and `cmd/server`. Microsoft retains its current Graph request/configuration and gains regression coverage for native Sent Items saving.
- Reuses saved owner-scoped Titan credentials and the existing MIME builder. Adds an IMAP client dependency because Go's standard library and current dependencies do not provide the required client; the version and protocol choices are recorded in design.
- Depends on the implemented operator-owned connections and durable dispatch path. No active predecessor change exists; this plan does not treat archived verification as current execution evidence.
- No historical data migration or backfill, new button/route, Microsoft IMAP or mailbox-reading permission, delivery tracking, new release unlock or additional live test authorization. Candidate sending retains exact-configuration received-format approval and explicit operator start requirements.
