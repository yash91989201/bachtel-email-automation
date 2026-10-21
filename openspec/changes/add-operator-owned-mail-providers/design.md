# Design

## Context

See proposal.md for motivation and agreed scope. The following is the original baseline recorded before the first implementation, retained to explain the original migration:

- `internal/operator/operator.go` refuses a second account; `internal/operator/operator_test.go` explicitly tests that singleton restriction. Preserve the protected provisioning command, password policy and closed PocketBase API while removing the singleton count check.
- `internal/microsoft/microsoft.go` stores one global connection, verifies a fixed environment sender and serializes refresh globally. OAuth attempts already bind the callback to an operator and use state/PKCE.
- `internal/sending/graph.go` already defines `Sender.Send(context.Context, Message) SendResult`, optional preparation, and semantic submitted/failure/retry/auth/uncertain outcomes. Graph transport suppresses request replay and has bounded timeouts.
- `internal/sending/store.go` keys pacing by the singleton `mailbox`; mailbox pauses currently reach all tasks. `worker.go` persists attempts before transmitting. Recovery conservatively holds unfinished intents.
- Campaign loads/lists, file downloads, export generation and retention currently assume one operator. An authenticated route alone is not record authorization.
- The active release-gate removal retains test approval and explicit operator actions; its integration verification remains outstanding. This design must compose with its deltas before spec sync/archive.

Revision state: the first implementation has since added private operators, provider adapters and connection ownership, and its 24 tasks were marked complete. It still reads Microsoft tenant/application settings from shared environment configuration (`internal/config/config.go`, `internal/microsoft/microsoft.go`). That implementation does not yet satisfy this revised operator-managed setup. Prior verification covers the earlier scope only; affected tasks are reopened. This artifact update makes no code or data changes.

## Goals / Non-Goals

**Goals:** Keep one durable submission engine, one persistent store and small consumer-owned interfaces; make operator isolation explicit; keep provider-specific authorization and wire protocols out of campaign logic; support recoverable replacement without preserving obsolete sender history.

**Non-Goals:** Runtime plugins, generic provider SDK/framework, organization membership, cross-operator sharing, multiple concurrent mailboxes per operator, shared senders/aliases, inbox reading, consumer Microsoft accounts or a shared multi-tenant Microsoft application/consent framework, attachment changes, and backward-compatible production data migration.

## Decisions

### 1. Reuse the sender seam; separate connection lifecycle from message transport

Keep `Sender`, `Message` and `SendResult` owned by `internal/sending`. Move their declarations from Graph-specific source into a small provider-neutral source file within that same package. Keep Graph and Titan implementations there initially, so no new package graph or shared `types` package is needed. Constructors return concrete implementations. The existing optional preparing interface remains only where credential acquisition needs to precede a paced start.

Wire an explicit connection-to-sender resolver at the application composition boundary. It takes a trusted, loaded mailbox connection ID and returns the corresponding bound sender; an unknown provider fails closed. Start with an ordinary switch for `microsoft365` and `godaddy-titan`, not runtime registration or reflection. The worker must never resolve the current global account at transmission time.

A future provider adds its connection flow, concrete sender, semantic result mapping, policy defaults, explicit selection branch and provider tests. Existing campaign/intake/recovery behavior stays shared. Authentication differs enough between OAuth and SMTP that a large interface with login/refresh/disconnect/send methods would force irrelevant methods and is rejected.

### 2. Explicit ownership at service and storage boundaries

Add a required operator relation to campaigns and connections. Candidates/revisions/tasks/attempts/uploads/exports inherit the owner through their campaign/task; store a direct owner reference only where needed for efficient queries or standalone operations such as connection work. Any denormalized owner must agree with its parent, including inside worker transactions.

Pass trusted operator IDs explicitly to user-facing service operations and include ownership in the query that loads or mutates a record. Resolve child ownership through its parent; do not load a global record by ID and then assume an authenticated caller owns it. Check authorization before file reads, converter work, export actions or queue mutations. Reject client-selected ownership and cross-owner foreign-key relationships.

Keep public PocketBase collection/file access closed and use protected application routes for files. PocketBase server-side calls and raw SQL do not gain owner checks merely from record rules; audit both. Return the same not-found response for absent and non-owned resources. Login identity and connected mailbox address are separate concepts.

Background jobs use their durable task/connection ownership rather than a browser session. Maintain existing campaign/email uniqueness and atomic reservations; global campaign IDs already distinguish owners, so no application-wide candidate email constraint is introduced.

### 3. Exclusive mailbox claims and encrypted credential state

Replace the singleton Microsoft record with operator-owned mailbox connections containing owner, provider key, canonical sender/address key, provider identity when available, connection status/generation and encrypted provider-specific credential payload. Enforce one current connection per operator and unique mailbox claims in the database, not with a check-then-insert sequence.

Canonicalize mailbox addresses consistently using the app's existing trimmed lowercase email convention, without Gmail-style dot/plus rewriting. For Microsoft retain a verified stable account identifier qualified by that connection's concrete tenant. Require an explicit tenant UUID, not the shared `common`/`organizations` aliases; obtain identity through that tenant's token exchange and authenticated profile. Different operators may select different tenants. Claim both canonical address across providers and provider identity where available. Never include Client ID in the mailbox uniqueness key: a different application registration must not create a second claim on the same mailbox. For Titan require the full mailbox address as the SMTP authentication username and sender; successful authenticated TLS establishes control of that mailbox. Do not accept a separately supplied From address, aliases or shared-mailbox delegation. SMTP cannot enumerate every alias; document that boundary rather than claiming universal alias detection.

A disconnected or reconnect-required connection keeps its ownership claim. Refresh, credentials and disconnect barriers are per connection, preventing cross-account token use and unnecessary global refresh serialization. Keep AES-GCM encryption with the deployment key and version/tag the payload for its provider; bind owner/connection/provider as authenticated encryption metadata where practical. Store provider setup alongside the connection: for Microsoft, tenant/client identifiers plus an encrypted payload containing the Client Secret and tokens; for Titan, the mailbox identity and encrypted password. The owner may view their non-secret identifiers, but no saved secret is returned to pre-fill a browser field. Do not expose any connection settings to another operator.

### 4. Provider connection behavior and configuration

The operator-managed setup contract applies to every supported provider. Each provider exposes only its actual setup fields and validation; the shared connection store enforces owner scope, encryption, exclusive sender claims and lifecycle rules. Adding an operator requires no server provider credential change. A future provider contributes its setup validation/UI and sender adapter; it does not require duplicating campaign, history or recovery code. Do not build a generic arbitrary-host/credential schema to anticipate unknown providers.

For Microsoft, collect Tenant ID, Client ID and Client Secret on the operator's protected connection form before authorization. Validate tenant/client identifier syntax locally, encrypt staged secrets immediately, and persist setup for that owner rather than requiring environment configuration. Build authorization and refresh configs from the exact connection's saved settings. Remove shared `MS_TENANT_ID`, `MS_CLIENT_ID` and `MS_CLIENT_SECRET` loading/readiness/fallbacks, along with the obsolete fixed sender setting. Microsoft authentication validates secret/consent; local field checks alone do not make a mailbox connected.

Keep `APP_ORIGIN` and `APP_ENCRYPTION_KEY` as deployment settings. Derive the Microsoft callback URI from the trusted origin and fixed application callback path; display it for each operator to register exactly. Do not collect arbitrary redirect, token, authority or Graph URLs from operators, and remove the ordinary `MS_REDIRECT_URL` override. Provider endpoint overrides remain available only through injected simulated test configuration. Each operator can register a separate single-tenant application in their own tenant, so supporting different operators' tenants does not require one shared multi-tenant app.

Keep state/PKCE, operator binding, single-use expiry, minimal delegated scopes and consent policy enforcement. Bind each pending OAuth attempt to its owner and an immutable staged-settings snapshot/revision; the callback must not use settings from another operator or a later edit. Store only encrypted pending secret material and clear expired/consumed attempts. Persist validated settings and their newly issued tokens together. Refresh must use the app registration that issued the stored refresh token, not another operator's registration or process-wide config.

For same-mailbox client-secret rotation or a new Client ID, stage and validate a fresh authorization against the same verified tenant-qualified account and canonical sender. Drain active submissions before an atomic settings/token swap. Preserve identity generation, campaigns, history, candidate state and applicable approvals for this same-mailbox change; serialize refresh with updates and reject stale attempts so old refresh work cannot overwrite new credentials. Failure leaves the prior saved settings/tokens unchanged. A changed verified identity follows the explicit mailbox-replacement/reset flow, and neither action resumes paused work automatically.

GoDaddy Titan uses `smtpout.secureserver.net:465` with implicit TLS and certificate/hostname verification. Operators enter their full mailbox address and password through the CSRF-protected connection UI. Validate TLS/authentication without issuing a message transaction; no validation email is sent. Missing or invalid Microsoft settings affect only that operator's Microsoft setup; they do not block another Microsoft operator or any valid Titan connection. A missing/incorrect global encryption key blocks every connection requiring it.

Reuse pinned `github.com/domodwyer/mailyak/v3 v3.6.2` for MIME construction where needed; promote it to a direct requirement only if imported directly. Prefer standard `net/smtp` over a certificate-validated TLS connection established with context-aware dialing. Set operation deadlines and close the connection on cancellation so blocked reads/writes terminate. Explicit SMTP command stages expose acceptance boundaries, unlike one opaque Send call. The stdlib package is frozen but sufficient for this bounded SMTP/auth use; no new dependency is planned. Revisit only on a demonstrated protocol need, documenting and pinning any replacement rather than implementing a custom SMTP/MIME stack.

Transport-specific endpoint overrides stay test-only; the Titan UI does not accept arbitrary SMTP hosts. Secrets never enter logs or provider-error text saved for operators.

### 5. Durable dispatch per connection

Resolve task owner and immutable mailbox connection/generation when authorizing work. Bind approval to configuration content/revision plus that connection generation. A same-identity credential refresh/reconnect does not change identity generation, while replacement always invalidates approval. Recheck owner, connection, approval and candidate claims at manual start/resume/retry/resend, schedule authorization and fire, and before a queued send acquires its launch permission.

Retain one dispatcher owner for the datastore, with a total four-request ceiling and at most four per mailbox. Use round-robin selection among ready mailboxes so a blocked mailbox does not hold the dispatcher or consume all opportunities. Keep evenly spaced starts, existing rolling-window checks and persisted cooldowns per connection at the existing 25/min default and 1-30 setting bounds. These application ceilings do not override provider quotas. Every test, schedule and retry uses the same controls. Refresh preparation must not hold a global paced-launch barrier.

A provider auth/throttle/service failure pauses or delays only that connection. Shared inability to persist results stops all affected launches. Workers are server-owned, have bounded exit conditions and honor process shutdown without inheriting the originating browser request's lifetime.

### 6. Preserve semantic outcomes without hidden retries

Adapters perform a single message submission and map wire results into the existing outcomes. Only the durable dispatcher schedules retries. Preserve Graph acceptance/replay protections and Retry-After parsing.

For SMTP, use explicit authentication, MAIL, RCPT, DATA and final acceptance stages. Definite temporary rejection or proven non-submission can be retried under existing backoff/limits; permanent recipient rejection records a safe failure; credential rejection requires reconnect. A disconnect/timeout after possible acceptance is uncertain, never blindly retryable. Once final acceptance after DATA is confirmed, later QUIT/close failure cannot reverse submission. Cancellation is classified by submission phase, not automatically marked safe. No claim of exactly-once delivery or Message-ID-based deduplication is made.

Persist intent before transmission and retain the existing review-required/manual-resend path. Submitted means provider accepted, not delivered. Tests use the same result contract as candidates.

### 7. Replacement is a recoverable owner-only reset

Separate first connect, same-mailbox reconnect, disconnect and replace. Validate proposed replacement credentials and identity before deleting anything. Show old/new identity, retained templates and exact deletion scope; require a dedicated CSRF-protected confirmation bound to that validated proposal and current connection generation.

After confirmation, block launches and new workspace sending-data writes for this owner; invalidate pending old OAuth attempts/actions. Drain active submissions and quiesce that owner's export generation. In a short transaction, recheck identity/ownership and reserve the target mailbox claim under uniqueness constraints while retaining the old claim until completion. Persist a replacement/reset operation and the encrypted staged provider settings/credentials and cleanup manifest. Never hold a database transaction over network validation, drain waits or file I/O.

Delete candidates, task memberships, reservations, tasks/schedules, uploaded candidate workbooks, duplicate reports, test approvals, attempts, resolutions, history and generated exports. Preserve the operator account, campaign definitions/settings, source-header suggestions, original template DOCX files and immutable revisions. Derive file deletion lists from owned records before deleting them; never delete a shared campaign/template directory indiscriminately. Reject or cancel stale generation and publication by connection/reset generation checks.

Complete file cleanup idempotently before final activation. Atomically make the new mailbox current, release the old claim and unblock the retained workspace with no approved sending state. Interrupted cleanup remains durably blocked and resumes on startup; it must not leak old downloads or publish a late exporter. Failures before validation/claim reservation leave old data unchanged; failures after reset begins remain blocked for recovery rather than claiming a rollback restored deleted files. Other operators continue throughout.

The removed mailbox may later be claimed by another operator after release. No previous sender identity/history needs preserving. Ordinary history must never display the current sender as if it were historical evidence. Explicit reset is the only new exception to retained history; ordinary upload correction and reconnect preserve it.

### 8. Administrative provisioning and local-development schema transition

Extend the existing provision-operator command to create additional unique operator identities using the current password policy, without public registration or new in-app administrator roles. Keep ordinary operators unable to create or manage peers.

Use the user-approved local-development reset instead of a compatibility layer for singleton data. During implementation, stop the local instance and use a documented explicit reset/reinitialize path against the designated local data directory, recreating the new schema and reprovisioning operators. Do not run this destructive reset from automated tests or silently at ordinary startup, and do not make it a production migration policy. Fresh-install migrations must create the final constraints without relying on hand-created records. Reset approval in this conversation is permission to plan this transition; this change creation performs no deletion.

## Risks / Trade-offs

- Cross-owner access in raw SQL or file helpers -> enforce trusted scope before side effects and test every route/action with two operators and foreign IDs.
- Competing connection claims or callback replay -> database uniqueness, bound single-use state, transaction rechecks and per-connection generation barriers.
- SMTP acceptance ambiguity -> phase-aware tests around DATA and final response; retain manual review and prohibit adapter retries.
- Shared-capacity starvation -> select fairly across ready connections and avoid holding slots during cooldown/refresh waits.
- Interrupted destructive replacement -> durable reset state, staged target claim, exact cleanup manifest, idempotent recovery and blocked file publication until completion.
- Clearing duplicate-prevention history -> disclose it before replacement and require new uploads/approval/authorization; do not imply deletion recalls previously sent mail or rewrites backups.
- Tenant/admin policy may prevent a Microsoft sign-in -> surface consent/setup errors for the selected operator tenant; never bypass its policy or apply a deployment-wide tenant restriction.
- Titan quotas may be lower than app settings -> provider rejection/backoff remains authoritative; do not invent unverified daily quotas or delivery guarantees.

## Migration Plan

The operator-managed settings correction reopens the affected implementation tasks. Do not claim that existing code or the earlier verification report implements it. Deployment environment Microsoft credentials are not migrated into an arbitrary operator or used as fallback; operators explicitly enter and validate their settings. Local-development reset/reprovisioning remains allowed under the existing procedure, with no production compatibility migration.

1. Finish the active release-gate change's verification; sync/archive its overlapping specs before syncing this change. Preserve its no-global-gate behavior.
2. Implement isolated schema/service/provider tests on temporary stores, then the owner-scoped application and UI. Keep new sending paths inaccessible until their ownership/approval checks are integrated.
3. Document the local-only destructive reset/reinitialize procedure. On the explicitly selected local directory, stop workers first, reset using that procedure, reprovision accounts and reconnect through the new UI. No production compatibility migration is required.
4. Verify different Microsoft tenant/application registrations for two operators without Microsoft environment credentials, a Titan connection, same-mailbox app-settings rotation and replacement crash recovery. No automated real mailbox traffic; a real test requires a separately designated recipient and operator approval.
5. A code rollback after schema reset requires reinitializing the old local schema; it cannot restore deliberately deleted data. Do not advertise an automatic data rollback. Normal deployment backup/restore remains consistent and preserves ownership/claims.

## References

- Go interface ownership, concrete return types, contexts and synchronous functions: https://go.dev/wiki/CodeReviewComments
- Low-level/frozen standard SMTP API: https://pkg.go.dev/net/smtp
- Existing MIME builder and MimeBuf: https://pkg.go.dev/github.com/domodwyer/mailyak/v3@v3.6.2
- GoDaddy's third-party SMTP endpoint: https://www.godaddy.com/help/set-up-third-party-plugins-or-websites-using-smtp-settings-42788
- Titan mailbox credentials and encrypted client settings: https://www.godaddy.com/help/use-imap-settings-to-add-my-professional-email-powered-by-titan-to-a-client-32204

References informed this plan; provider behavior still requires simulated protocol verification during implementation. No real email or library integration has been tested in this planning run.
