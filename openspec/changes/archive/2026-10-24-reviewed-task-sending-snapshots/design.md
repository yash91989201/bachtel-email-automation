# Design

## Context

See proposal.md for motivation and capability scope. Observed current code uses campaign.Format on Mapping; intake.normalizeValues prepares normalized values, but intake.Preview, sending.QueueTest and sending.workFrom render again from raw values. Sending approval lookup currently matches configuration/content hash and mailbox generation, allowing reuse across tasks. Intake already expands comma-separated addresses, counts the complete expanded input, excludes every duplicate occurrence, and publishes a private manifest and exclusion XLSX.

These existing paths must change together: saving AI output alone would not stop later reformatting, and changing the UI alone would not bind approvals to reviewed data. Current main specs include multi-email expansion; no active prerequisite change exists.

## Goals / Non-Goals

**Goals:** One preparation stage, two operator finalization paths, one authoritative final value set for all task rendering, and an approval boundary tied to the finalization event.

**Non-Goals:** A generic workflow engine, recipe evaluator, provider framework, AI during rendering/final upload, semantic validation of human-reviewed final edits, or changes to mailbox pacing/submission guarantees.

## Decisions

### 1. Preparation is separate from execution state

Add task preparation state and selected run/final snapshot references without overloading running/paused/stopped or Direct/Scheduled state. The UI progression is:

Initial upload -> header preflight -> preparing -> prepared review
  -> Approve as-is -> backend recheck -> final snapshot
  -> Edited upload -> backend validation only -> final snapshot
Final snapshot -> explicit test -> received-format approval -> explicit start/schedule

Missing headers, file corruption and bounds failures are task-level failures. Entry-level problems produce exclusions while accepted entries remain reviewable. Zero accepted entries cannot enable sending. A structurally failed replacement keeps the prior final snapshot explicitly selected; a structurally valid zero-accepted replacement supersedes the old selection with an unsendable result, so old approval cannot accidentally send old data.

Run preparation in a server-owned cancellable worker with bounded requests, durable queued/running/completed/failed state and stable row/field identifiers. Reuse the project's job-lifecycle patterns; no provider network calls inside database transactions. Browser closure does not own the worker lifetime. Interrupted calls are marked incomplete, with explicit retry; do not silently rerun successful calls or publish partial provider responses as complete.

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

### 3. Prompt and run history

Store one editable task-instruction draft, not a campaign-like revision UI for every keystroke. An explicit initial preparation/rerun captures immutable source digest, campaign revision, complete base prompt/version, task extension, AI connection/configuration generation, exact provider/endpoint/API format/model/options, actual outputs, corrections and findings. Persist completed output, because prompts and model ids alone cannot reproduce inference.

Use the header-first tested prompt in Appendix A as the initial base. Add the explicit output distinction: corrections that were successfully resolved are not review findings; unresolved semantic mismatches or uncertainty are findings. Do not place required/optional enforcement in the model. Task instructions may override cleanup defaults but not fabricate recipient identities or bypass application validation.

Changing the draft alone does not change selected results. A successful new run becomes available for explicit review/selection; changing selection clears current finalization and snapshot approval. Show failed and previous runs without mutating historical results. Restrict rerun/selection changes to unstarted tasks; refuse them while a future schedule is enabled until the operator clears it.

### 4. One backend validator for both finalization paths

After AI output validation, expand the normalized recipient list and classify the whole input before writing candidates. Retain the existing 1,000-expanded-entry limit, including invalid/duplicate occurrences, and never admit a preferred duplicate. Combine all applicable reasons while counting entries in mutually exclusive outcome categories.

Apply required/optional checks after preparation. A whitespace-only value is empty; required emptiness excludes, optional emptiness uses the existing literal fallback or empty string. A populated optional field with an AI finding remains excluded; fallback cannot conceal findings or provider failures. Render-time header-control and contextual escaping safeguards remain mandatory.

Approve as-is uses accepted prepared values and rechecks current backend eligibility without AI. Edited final upload uses the same backend validator with AI findings absent: a human may explicitly restore a previously flagged entry. Preserve populated content verbatim, except recipient trim/lowercase and optional fallback substitution. Do not recase, collapse populated whitespace, normalize lists or reapply historic mapping formats. Commas remain the only final-upload email list delimiter; slash lists fail with a clear correction message.

Permit edits, removals, additions and corrected excluded entries. New final-upload row numbers are their own provenance; never assume that final row 8 corresponds to original row 8. Extra unmapped columns can be retained in the final source artifact but cannot influence AI, eligibility or template substitution. Header mapping remains authoritative.

### 5. Private artifacts and operator review

Reuse Excelize and existing owner-authorized download/file storage. Keep original upload, AI run evidence and each final upload separately, with digests and source references.

Prepared and final sending XLSX files have a dedicated sending sheet with only accepted mapped columns, recognizable source-header labels, one row per recipient, stable order, and no status/exclusion rows or original-source sheet that might be imported accidentally. No AI rewrite occurs to create a download. Use the source header when unique; when several mappings share that header, label their separate output columns with the source header and variable name. The final-upload importer resolves those deterministic labels to the pinned mapping, rejecting missing or ambiguous labels rather than overwriting values or changing the campaign mapping.

For shared source headers, the candidate label is exactly Source Header (variable_name), preserving the stored header text and identifier. Resolve any remaining collisions with the existing ResolveResultLabels policy in pinned mapping order, including collisions with literal headers that already contain parentheses. Export and final import use the same computed label table; no fuzzy matching or parsing of parentheses is needed. Missing or renamed expected labels receive a structural error identifying the exact required labels.

Exclusion XLSX files preserve source columns plus source artifact/sheet/row/recipient position, individual address, affected variable/header, original and proposed values, reason source and all reasons. Distinguish AI review findings, required-value/address/duplicate/history/safety failures and AI processing errors. Unmapped data may be copied for operator context but cannot create an exclusion. Corrected uploads are new evidence; keep earlier reports inspectable.

Historical results retain original worksheets and provide final-source provenance and accepted mapped values alongside per-recipient outcomes. Preparation downloads are available before sending; existing automatic post-execution result generation remains separate. Formula-like input must be written as inert text.

### 6. Immutable finalization and exact-snapshot test approval

Persist a final snapshot containing task/owner, pinned campaign revision, source/run or edited-upload reference, accepted row identities and final values, exclusion evidence, finalization actor/time and digest. Store the selected snapshot id on the task. Publication is atomic with the selection update and approval invalidation; network calls and large file processing occur before the brief transaction.

Use action IDs for idempotency and compare the expected selected run/snapshot to reject stale actions. Repeating one action returns the same snapshot. A new accepted final upload creates a new snapshot id even when byte-identical: a content-only hash must not reuse earlier approval.

Bind test attempts and received-format approvals to owner + task + snapshot id/digest + campaign revision/content hash + mailbox connection/generation. Snapshot-specific tests select an accepted final row, overriding only the destination with the designated test address. An arbitrary sample or campaign diagnostic cannot approve a final dataset.

Recheck these identities on approval, start, schedule creation/fire, resume, retry and resend. Reject a late test for an older snapshot. Replacing content never transfers schedule/start authorization. Clear enabled schedules explicitly before changing final data. Existing candidate reservations/history still determine which approved recipients are free at action time; the snapshot download describes reviewed content, not a promise that every recipient remains sendable forever.

Already-started tasks cannot rerun preparation or replace data, including after pause/stop/completion. Recovery continues from their pinned content and existing explicit action contracts; no new snapshot is substituted beneath stored attempts.

### 7. Rendering and mapping versions

New mapping authoring uses variable | source header | required/optional/recipient | fallback, no format choice. Keep exactly one required recipient and existing placeholder-validation semantics. Use an explicit mapping/configuration version so historical serialized format values cannot be misread as new fallback text. Reject legacy-shaped new submissions with a clear remedy.

Add a rendering path that consumes already accepted final values without calling Normalize. Route task preview, snapshot-specific tests and candidate work creation through it. Campaign sample preview is a direct-value diagnostic with recipient/safety checks, not an AI call or snapshot approval. Optional fallback resolution occurs before snapshot sealing; preview/download/test/send read the same resolved values.

Existing campaign revisions and started tasks keep their recorded mapping semantics and content; do not rewrite their hashes or history. A new revision of an existing campaign uses the new syntax, displaying the old formats as historical rather than silently reinterpreting them. Keep the smallest explicit versioned legacy read path needed for existing evidence, not a second normalization product.

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

## Risks / Trade-offs

- AI false negatives or inconsistent corrections -> preserve before/after evidence and require human data review. The sample's unflagged REBIT-to-Revit correction is a real limitation, not proof of correct credentials.
- AI false positives and provider latency/cost -> only unresolved findings exclude; use bounded batching and explicit retries. Do not promise identical outputs on rerun or extrapolate evaluation timings into a service guarantee.
- Human final edits can introduce semantic errors -> no AI after final upload by explicit user decision; make operator responsibility clear while retaining deterministic and rendering-safety checks.
- List delimiters can belong inside one value -> header/context guide preparation, preserve compounds and flag ambiguity. Never generalize content-list formatting into multiple To recipients per message.
- Stronger approval increases operator work -> clearly separate dataset approval from test approval and require new approval only for a newly selected final snapshot/configuration/mailbox scope.
- Pending-task locks can prevent switching away from a broken model -> permit same-configuration credential repair and explicit terminal closure; a new task can be created after a deliberate switch, without assuming unsent recipients are safe to resend.
- Provider API compatibility and policies can change -> verify actual format/model support and deployment suitability; keep model/endpoint selection explicit, with no silent fallback.
- Cross-operator leaks or in-flight stale selection -> authorize all artifacts/actions, scope worker jobs to owner/task/run, verify expected revisions at publish time and discard stale late responses.

## Migration Plan

1. Add owner-scoped AI settings/configuration generations, task closure markers, snapshot/run records, indexes, ownership and version markers before enabling new authoring. Preserve originals, normalized values, attempt content and existing records; do not reset the live database.
2. Preserve existing mailbox identities, encrypted credentials, authorization callbacks and generation semantics when moving the UI. Do not auto-provision AI from a local CLI or deployment-wide key; existing operators complete verified Account setup. Apply the lock to existing unfinished tasks as well, with Close task and same-identity repair remedies. New tasks use the new preparation/review flow. Existing unstarted tasks require explicit transition/review/finalization; do not call AI or manufacture operator approval during migration. An enabled schedule must be explicitly cleared before transition.
3. Existing already-authorized started-task work retains its pinned legacy content and authorization scope. It does not acquire AI runs or new final uploads; no old approval can authorize a new workflow snapshot. Preserve stored email attempts and recovery evidence.
4. Gate new-workflow sending on snapshot-specific approval server-side before exposing new UI controls; no deployment interval may let a campaign-only approval authorize new final data.
5. Verify using temporary files/databases and simulated AI/mail providers. Optional live provider evaluation is separately operator-authorized and never requires sending a real email.
6. Take a normal deployment backup. Rollback must stop workers and disable new-workflow dispatch before deploying an older binary that lacks these guards; preserve new artifacts and restore a compatible backup rather than dropping new approval fields in place.

## Appendix A: Initial visible base prompt

The following is the prompt tested during exploration, with header-first guidance, name-spacing repairs and comma-space lists. The final application output contract additionally separates resolved corrections from unresolved findings and enforces recipient occurrence preservation. Prompt updates create a new prompt version and affect only explicit future runs.

```text
Normalize the supplied mapped values for use in personalized email content.

Before normalizing each field, identify its semantic purpose from its
source-column header. Use the mapped variable name and supplied row context
as supporting context. Apply normalization appropriate to that field’s
purpose, rather than applying one generic transformation to every column.
Respect distinctions in the header such as current versus expected values.
If a cell does not fit its header, flag the mismatch rather than redefining
the field or inventing a value that would fit.

For personal names, clear accidental internal-spacing and word-boundary
repairs are allowed when they preserve the same letters and their order.
Preserve initials and do not substitute a more familiar name spelling.

Apply task-specific instructions where provided. For anything they do not
address, use the defaults below.

Normalization defaults

1. Produce clean, readable text. Trim surrounding whitespace, collapse
   unnecessary whitespace, and correct punctuation spacing. Remove stray
   separators only when they clearly represent empty content.

2. Normalize noisy capitalization appropriately for the field. Preserve
   initials, recognizable acronyms, technical terms, and intentional spelling.
   Avoid mechanically title-casing every word.

3. Correct clear spelling and typing errors when the intended wording is
   unambiguous. Be especially careful with personal names, uncommon terms,
   and identifiers. If a correction would require guessing, retain the
   supplied wording and report the uncertainty.

4. Preserve the supplied meaning, specificity, and qualifications. When
   any column contains multiple distinct values, keep every value in its
   original order and separate the values with a comma followed by one
   space. Use the column header and context to distinguish a list from a
   compound expression or an internal part of one value; preserve compound
   technical terms, initials, meaningful parenthetical details, and punctuation
   within individual values. Apply the same list-separator rule to email
   address fields, preserving every supplied address. Do not select,
   summarize, expand abbreviations, or substitute preferred terminology
   unless task instructions request it.

5. Preserve factual values, units, identifiers, and date meanings. Improve
   presentation only where the interpretation is clear. Do not infer a
   missing currency, date interpretation, qualification, or other fact.

6. Leave missing values empty. Recognize missing-value markers only when
   their meaning is clear in context. Preserve meaningful values such as
   zero or “No”. The application handles required fields and fallbacks.

7. When a value appears misplaced, corrupted, ambiguous, or inconsistent
   with its field, retain the supplied value and report a short issue.
   Do not silently move information between fields or invent a repair.

8. Apply the same interpretation and formatting to equivalent inputs
   throughout the task.

Task-specific instructions may extend or override these normalization
defaults for particular fields or for the whole task. Apply those
instructions directly; do not generate a normalization recipe.

Return normalized text and any separate issues using the application’s
required output structure.
```

## Split ownership and activation boundary

Prerequisites: account-connection-settings owns Decisions 2, 8 and 9; ai-task-data-preparation owns Decision 3, the base prompt and durable AI jobs/output validation. Those sections above are dependency context, not duplicate implementation assignments. This change owns final backend validation, both review paths, downloads, mapping migration, immutable snapshots, direct rendering and approval integration. Activate those together. Existing unstarted tasks transition explicitly; started historical content and recovery remain pinned.
