# Design

## Context

Existing mailbox and task lifecycle evidence is recorded in the retained source plan. No implementation has occurred.

## Dependencies and scope

Independent of the other two changes. Provider adapters include a synthetic verification path only; task preparation consumers arrive later. Task closure must already block all existing execution actions.

## Decisions

### 2. Provider boundary and response validation

Use one confirmed owner-scoped AI connection stored alongside the existing mailbox settings pattern, with encrypted credentials under the deployment key. Proposed settings are staged separately; validation uses a tiny synthetic normalization request against the exact model and output contract, followed by explicit confirmation. Connection validation can consume provider usage and must say so. Failed validation/cancellation leaves the prior active connection intact. Do not import Pi keys or run Pi subprocesses in production.

Use a narrow consumer-owned normalization boundary and small HTTP protocol adapters, using existing net/http and encoding/json. Keep provider presets (label, default API root, supported API format, suggested model IDs, required settings and guidance) separate from protocol handling. Initial support is OpenCode Go plus Custom compatible with Responses and Chat Completions adapters. Future Xiaomi MiMo/Fireworks presets reuse verified compatible adapters; a provider with another protocol gets a small adapter, not a speculative provider framework. Native MiMo/Fireworks presets, a live model marketplace, automatic routing/fallback, arbitrary headers and plugin scripts are outside this change. A manually entered exact model ID is sufficient initially; validate it rather than depending on a model-catalog UI.

For the initial OpenCode Go preset, the documented API root is https://opencode.ai/inference/go/openai/v1. Muse Spark 1.3 Contributor (muse-spark-1.3-contributor) uses Responses (/responses), while DeepSeek v4.1 Flash (deepseek-v4.1-flash) uses Chat Completions (/chat/completions). API format is explicit and preset guidance must prevent treating those model IDs as interchangeable request contracts. Keep required client identity/session headers in the adapter, including a stable OpenCode session per run; never expose an arbitrary header editor as initial product scope. Verify model support using the current official contract before implementation. See [OpenCode Go documentation](https://opencode.ai/v2/docs/console/go/).

Persist a connection identity/configuration generation for provider, API root/format, exact model and output-affecting options; separately version credential repairs. Every run captures that immutable non-secret configuration and uses only it for every batch/retry. Provider errors do not trigger another model. A successful settings change affects future runs only; it neither regenerates old output nor invalidates an already sealed dataset or its formatting approval. Same-configuration key rotation can repair a failed run, but retry remains explicit. Account settings shows the selected model and external processing destination. Missing/unusable configuration blocks AI preparation with a setup/repair remedy; final uploads and saved final snapshots remain usable without AI.

Bound server-controlled timeouts, response/batch sizes and retries. Validate HTTPS endpoint roots and resolved addresses, reject private/loopback/link-local/metadata targets, check every dial/redirect destination and do not forward authentication across origins. Use certificate verification and redacted errors. Restrict keys, drafts and connection records to their owner; never prefill secrets or include them in logs/downloads/run provenance. Reuse the existing encrypted-secret facility rather than a new secrets service.

The Pi evaluations proved local access and sample behavior, not permission or suitability for deployed workbook processing. Before shipping OpenCode Go support, verify deployment access, applicable usage terms and the synthetic/real normalization contract independently of Pi. Its current documentation describes coding-agent use, and the Muse Contributor option allows training on prompts/completions rather than promising zero retention. Document current provider data handling in setup; do not silently transmit operator workbook data under an assumed coding subscription contract. This is a deployment suitability prerequisite, not permission to use local credentials or an assertion that every OpenAI-compatible model works.

Send only mapped fields, mapping/header metadata and necessary source references. Separate trusted application output rules, overridable normalization defaults/task instructions, and untrusted cell data. Return normalized strings, ordinary correction notes, and unresolved findings with field ids and scope (shared field or individual recipient occurrence). Do not infer eligibility from a model-generated list of surviving rows: every input must have a corresponding validated output.

Validate unique row/field IDs, complete coverage, strings, finding scope, response bounds and recipient identity. Unknown, omitted or duplicated records invalidate the affected batch; never interpret omitted output as exclusion or success. Backend checks, ownership and sending permission cannot be overridden by prompts. No model confidence percentage determines eligibility.

For initial multiple-address normalization, permit recognized separator cleanup while preserving the ordered original address occurrences, including repetitions and invalid/empty occurrences relevant to exclusions and limits. Verify that each output token maps to an original token apart from trim/lowercase; changed, fabricated or dropped addresses become preparation failures or explicit review findings. Preserve ambiguous source tokens rather than guessing their address spelling. This protects the existing all-occurrences duplicate rule and prevents AI cleaning away an invalid sibling.


### 8. Account settings and the shared connection lock

Move the mailbox entry point into Account settings with Mailbox and AI connection sections, retaining provider-first setup, existing verified sender/replacement rules and valid OAuth callback routes. Keep compatibility redirects for old UI bookmarks as needed; do not create a second mailbox management surface. The account link is distinct from campaign Settings. Follow existing page structure, accessible labels and provider-specific conditional forms.

Define an account task as pending from durable upload/task creation until execution is completed or an explicit close is finalized. Include staged/unvalidated uploads, preparation failures, review/approval waits, schedules, running/waiting/stopping, paused/interrupted and stopped states, across every campaign. Completed tasks with outstanding queued/in-flight AI or test/send operations continue to block until those operations settle. Export generation is not itself a model-switch lock, but mailbox replacement retains its existing export/reset coordination. Do not equate an idle dispatcher or zero currently eligible recipients with task completion.

While any task is pending, refuse AI provider/model/API-root/API-format/output-option switching or removal, and mailbox provider/sender replacement or disconnect. Both sections show the current blockers with campaign, filename, short reference, state and task link, using wording such as: “recipients.xlsx is pending. Mailbox and AI connection changes are locked until all pending tasks are completed or explicitly closed.” List/count multiple blockers without leaking other operators' work. Recompute on submission and confirmation; stale UI is never authority.

Allow initial setup for a connection that has never been confirmed even when staged tasks exist. Allow same-configuration AI key rotation, same-mailbox reauthorization and verified mailbox application-credential rotation under existing request-settlement guards. Automatic token refresh is maintenance, not switching. A repair cannot change provider/model/endpoint/options or the verified sender, and never automatically resumes sending or reruns failed normalization. This avoids deadlocks when a key expires while pending work must retain its configuration.

Use the existing owner-scoped synchronization/database transaction patterns to serialize connection activation with task creation and reactivation. Stage network validation outside transactions; activate with expected connection generation and a fresh authoritative blocker query in a brief transaction. OAuth callbacks and reset/disconnect paths use the same guard. Protect initial setup and credential repair from stale generation overwrites too. A competing upload either binds after a committed change or causes the change to refuse. Do not hold a database transaction during provider validation or network settlement.

A permitted retry/resend/resume of a completed task must atomically make it pending before enqueueing work, reacquiring the lock and verifying the pinned mailbox/snapshot/approval contract. A later AI switch cannot rewrite its final values; recovery does not invoke AI. Mailbox replacement still performs its separately confirmed destructive reset only after the lock clears, and preserves the account AI connection. AI switching never deletes tasks, history, campaign revisions, outputs or approvals. Closing/completing tasks merely unlocks settings; no connection changes automatically.


### 9. Explicit terminal task closure

Add an owner-authorized Close task action with a clear confirmation in the task workspace. Preserve the underlying execution status and a durable terminal closure marker/time/actor, rather than interpreting Stop as closure or losing outcomes. A closed task cannot prepare, finalize, schedule, test, resume, retry or resend; different future work uses a new task and existing duplicate/history protections. Historical downloads and manual outcome classification remain available under existing evidence rules, without transmitting email.

Close prevents further job claims, clears enabled schedules without deleting their recorded authorization, cancels queued preparation/tests/sends and settles bounded in-flight operations before marking closure final. Reuse durable submission classification: unknown provider outcome remains uncertain, not canceled or proven unsent. An AI response arriving after close cannot publish a selected result. Hold the account lock during closing, and preserve existing reservations/history protections for submitted or uncertain recipients; release only reservations that the existing safe-stop rules prove unsubmitted. Closure never erases uncertainty or asserts successful completion.

Close is explicit and idempotent, including for staged uploads, zero-eligible/failed preparation and stopped/paused tasks. Reject stale or foreign close requests. Race checks cover scheduled fire, retries, worker publication and manual start. Record closure separately from completion so list/Overview/Sending/Results can explain why there is no next send action, while retaining audit and export access.


## Migration and verification

Preserve mailbox identities, generations, callbacks and encrypted settings. Apply locks to existing unfinished tasks with initial setup/repair/closure remedies. Do not provision AI from local Pi credentials. Provider deployment suitability and data policy verification remain prerequisites. Use temporary stores and simulated endpoints, including races and legacy mailbox regression coverage.
