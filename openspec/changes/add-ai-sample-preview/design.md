# Design

## Context

See `proposal.md` for motivation and the capability deltas for behavior. Current code observations:

- `internal/web/campaign.go:handleCampaignPreview` loads the current owned revision and calls `Compiled.Render(sample)` directly. It creates no task or sending attempt.
- `internal/campaign/render.go:ResolveValues` preserves populated current-version content, applies optional fallbacks and reports missing required values. Recipient normalization and validation remain in `Template.Render`; historical revisions retain legacy format behavior.
- `internal/preparation/input.go` already captures mapped fields and source headers. `prompt.go` owns the versioned policy, `response.go` validates exact coverage and recipient occurrence integrity, and `output.go` separates corrections from unresolved findings.
- `internal/preparation/worker.go:execute` combines reusable batch inference with durable job lifecycle. It builds the payload, calls `ai.Store.CompleteFor`, parses the response, updates progress and publishes only complete results.
- `internal/ai` already provides confirmed owner connections, encrypted credentials and safe bounded provider transport. `CompleteFor` accepts captured configuration but separately loads credentials; a temporary sample cannot rely on a pending task to prevent configuration switching.
- Existing specs explicitly prohibit AI sample rendering and broadly prohibit AI during preview. The deltas introduce one explicit exception; they do not change the task finalization or sending contract.

## Goals / Non-Goals

**Goals:** Share the real normalization operation between a one-row sample and durable batches; keep inference outside rendering; bound resource use and prevent stale UI results; leave no app-persisted sample data.

**Non-Goals:** A new normalization framework, persisted sample sessions, background processing, sample instruction editing, changes to workbook eligibility/finalization, or a guarantee that a one-row inference matches a future workbook batch exactly.

## Decisions

### 1. Extract the smallest shared batch operation

Extract the payload/inference/parse portion of `Service.execute` into one operation within `internal/preparation`. It accepts captured mapped input, base prompt/version, instructions, inference options and the selected AI configuration/call boundary, then returns validated row results or an error. Reuse `MarshalPayload` and `ParseResponse`; do not copy their contract into a sample handler or add another prompt.

The worker retains its stored prompt/instructions/options, stable session across batches, job claims, cancellation checks, progress, failure evidence and atomic publication. The sample adapter uses `BuildInput` with one transient `intake.MappedRow` (synthetic row number 1), the current base prompt, empty task instructions and default preparation inference settings. Synthetic input metadata is temporary and never creates a task.

Both callers can use `Derive` to retain the same findings interpretation. No run is required to call the shared operation. Duplicating the worker logic would drift; creating a fake task/job would violate the temporary lifecycle.

### 2. Explicit HTTP action with one-row validation

Keep `POST /campaigns/{id}/preview` backend-only. Add `POST /campaigns/{id}/preview/normalize` behind the existing operator/ownership and CSRF protections. Load the owned revision and require the form's revision identity to match; do not silently use a newly edited revision.

Read only declared mapping values. Bound the form body to 1 MiB and individual values to the existing preparation cell limit; reject overflow before inference. Validate that required sample values are populated and that the recipient is one valid bare address before spending provider usage. Preserve supplied strings for the AI payload/comparison, including original recipient casing; validation does not rewrite the submitted form or pre-render fallback text into the AI input.

After normalization, run the existing compiled renderer on prepared values. This preserves required/fallback/recipient/safety and historical rendering behavior. Display original, prepared and effective rendered values separately so optional fallback or legacy formatting is visible. If backend rendering fails, show the validated comparison/findings plus an inline error rather than claiming a successful email. Unresolved findings label any otherwise safe render Needs review, including findings in optional fields.

### 3. Request-owned execution with small admission bounds

Use the HTTP request context with a single overall 30-second deadline; provider transport may impose tighter existing bounds. One explicit click makes at most one inference request. No retries, polling, streaming, persistent queue or background goroutine is introduced.

Use a server-owned in-memory admission guard: one active AI sample request per owner and four active AI sample requests across the server, with immediate busy refusal and guaranteed release on every exit. Remove inactive owner entries so the map cannot grow with historical users. Direct rendering does not acquire these slots; durable task preparation retains its worker behavior. This is a per-process deployment bound, not a distributed account quota; replicas can each admit up to four requests. A distributed limiter is deferred until a supported multi-replica requirement exists.

### 4. Capture connection and credential consistently

Reuse the AI store's owner access, decryption, destination checks and transport. Capture confirmed configuration, configuration/credential generations and the matching decrypted credential as one consistent read before dispatch; no network call or pacing occurs inside a database transaction. Keep the credential private to the AI call boundary. Do not call `CompleteFor` with an old endpoint and a newly fetched unrelated key.

Recheck generations before dispatch and before returning a successful result. If settings changed, discard the result and require an explicit new action. Preserve existing task-switch locks and credential-repair settlement; a sample must not fabricate pending-task records or alter durable lock behavior. Any small AI-store entry point needed for this consistent capture must serve the existing connection contract rather than introduce another credential store. No provider/format/model discovery or fallback occurs during sample use.

### 5. Temporary UI state and honest processing disclosure

Extend the existing campaign sample form/result components instead of introducing a new screen. Keep Render preview and add Normalize with AI & preview. Show the configured destination/model, provider usage disclosure, current base-policy access and temporary-data explanation before AI submission. Missing AI setup offers the Account settings remedy while direct rendering stays usable.

Use the existing HTMX/Alpine stack for loading, disabling duplicate submits and inline outcomes. Editing an input clears/invalidates the result and aborts pending sample work; associate responses with the submitted form generation so a late result cannot replace newer input or another action. Check revision identity again server-side before returning. Keep original inputs intact on success, failure and busy refusal. Use accessible status/error announcements.

Results exist only in request memory and the current page DOM. No database writes, artifact files, persistent caches, cookies containing samples, browser storage or sample content logs. Responses use `Cache-Control: no-store`; page navigation/reload does not restore sample results through app code. The app cannot promise erasure from browser internals or the external provider: disclosure distinguishes app non-retention from provider policy, and cancellation cannot undo provider processing already received.

Result metadata identifies the actual revision/content hash, provider/model, prompt version and inference settings used. It is temporary provenance, never a saved run or received-format approval. Label it AI-prepared sample and explain that workbook context/task instructions may differ; task preview/test/send remain bound to final accepted snapshots.

## Risks / Trade-offs

- Shared-operation extraction could change durable batch behavior → preserve recorded prompt/options/session, keep lifecycle code in the worker, and add fake-provider parity and failure regressions before integrating sample UI.
- AI may return nondeterministic or context-dependent text → describe the sample as illustrative; never imply that it predicts exact workbook output or approves sending.
- Busy limits trade throughput for predictable provider/resource use → immediate actionable refusal, fixed small per-process bounds, no durable queue.
- Generation changes can consume provider usage before being detected → discard stale output and explain the need to retry; no automatic second charge-producing call.
- Sample data could leak through logs or page caching → no payload/raw-response logging, no-store responses, safe existing provider-error mapping and explicit persistence assertions.

## Migration Plan

No schema or data migration. Implement and verify the shared-operation extraction first, then the request adapter and UI. Existing direct preview remains available throughout. Rollback removes the new action and restores the former worker call wiring if necessary; no sample records or artifacts require cleanup. Deployment and real-email acceptance are outside this planning change.
