# Design

## Context

See `proposal.md` for the motivation and the two delta specs for behavior. Current implementation evidence:

- `internal/ai/ai.go` defines `Preset.Models`, `ModelContract` and `ModelFormat`. `discovery.go` parses only model IDs, then assigns eligibility and format from that table. The model-list response itself does not establish format compatibility.
- `normalizeProposal` in `internal/ai/store.go` rejects new preset models absent from the table and ignores caller-supplied preset formats. Exact saved selections use their stored format for credential repair.
- `StageValidation` checks encryption availability, seals the submitted key, calls `validateConnection` once, then stages a draft. `Confirm` applies ownership, generation and account-lock checks separately. Configuration identity already includes API format.
- `internal/ai/client.go` already provides the two probe request encoders, response parsers, exact synthetic normalization check and shared safe transport. Requests currently have a 30-second timeout and 512 KiB response limit. HTTP 400-class rejections are currently collapsed into broad errors, so continuation decisions need a small internal classification change.
- `internal/web/account.go`, `internal/web/views/account.templ` and `web/js/ai-setup.js` render a compiled fallback catalog, disable unsupported discovery results and display preassigned formats. Existing AI and web tests explicitly expect unknown models to be disabled and static format labels to exist.

This is a deliberate revision of the main specs' unknown-model refusal rule, already incorporated from archived `simplify-ai-provider-setup`. There are no active prerequisite changes. The behavior above is observed current code; the decisions below are proposed work.

## Goals / Non-Goals

**Goals:** Keep discovery read-only, establish compatibility only at the existing validation boundary, and reuse the draft/confirmation lifecycle and runtime adapters. Remove the model catalog as both a UI fallback and a server eligibility gate.

**Non-Goals:** No new protocol, dependency, provider registry, stored discovery catalog, per-model compatibility cache, background probing or changes to runtime inference selection. Custom setup continues to validate its one explicit format.

## Decisions

### 1. Discovery returns choices, not compatibility assertions

Delete the preset model tables and model-name-based format resolution. Retain provider labels, editable defaults, guidance and supported format lists. Continue trimming and checking model IDs, deduplicating in response order and bounding discovery responses.

Simplify discovery option data and browser rendering so every valid returned ID is selectable and no discovery result claims a verified format. Remove fields whose only purpose was compiled eligibility, including disabled reasons, once their consumers are updated. Keep saved or staged format display separate from unvalidated discovery choices.

The server accepts a syntactically valid submitted model for synthetic validation without requiring a persistent discovery session or a second catalog lookup. This avoids making discovery availability an activation dependency and preserves current direct validation and repair entry points. The normal new-preset UI offers fetched IDs; Custom compatible remains the manual entry route. Invalid IDs still fail locally.

Alternative: expand or periodically refresh the table. Rejected because it retains the same eligibility drift and still cannot prove compatibility at an overridden endpoint. A stored discovery token or membership cache adds state without improving the exact synthetic compatibility proof.

### 2. Resolve a new preset format through a short sequential probe

Add a small resolver in `internal/ai`, called by `StageValidation` after input/endpoint and encryption-key checks and before storing any new draft. Reuse `validateConnection` for each candidate so passing still means correct response-envelope parsing and exact expected synthetic output, rather than HTTP success alone.

- For a new OpenCode Go selection, attempt Responses first and Chat Completions second. For Xiaomi MiMo, attempt only its existing Chat Completions format. Preserve these existing provider format lists; there is no new Anthropic or audio adapter.
- Keep provider, normalized API root, model and supplied API key constant. Ignore browser-submitted preset format fields. Try each candidate once and stop at the first success. If both would work, the first is deterministically retained; do not make the second request after success.
- Continue only for bounded compatibility failures: HTTP 400, 404, 405, 415 or 422, an invalid expected response envelope, or a normalization-contract failure. Preserve enough typed/internal status information to distinguish these cases; never parse provider error prose or expose raw bodies.
- Stop on authentication/authorization refusal, throttling, service failure, transport/TLS/DNS failure, unsafe endpoint/redirect, oversized response, cancellation or deadline exhaustion, and on other unclassified failures. These outcomes do not prove that another format is needed. No retries or backoff are introduced.
- Apply one overall 30-second deadline to the operation as well as the existing per-response 512 KiB bound. At most two probe requests occur. Parent cancellation ends the entire operation, and an exhausted budget cannot launch another request.
- Return the successful configuration including its format. Use that resolved configuration for fingerprints, generation calculation, credential sealing and the staged record; check encryption-key availability before spending provider usage. Do not keep network calls inside database transactions.

Alternative: trust a name suffix, discovery metadata that the parser does not currently read, or the operator's hidden format value. Rejected because these do not demonstrate compatibility. Parallel probes would spend usage even when the first format suffices and complicate deterministic choice.

### 3. Keep repair and runtime selection pinned

For an unchanged confirmed preset provider/root/model, validate the supplied replacement key against the stored format only. This path needs neither discovery nor a model table, even for a retired ID. If it fails, return the error without probing another format. Custom connections always validate their explicit supported format once.

After resolution, use the existing exact draft and explicit confirmation checks. Any format difference remains a configuration switch, and pending tasks must still prevent activation of a switch. Existing configuration and credential generations, in-flight settlement and stale-draft refusal remain authoritative.

No format resolution is added to `Complete` or preparation. Runtime calls and retries continue using the recorded configuration. Probe success verifies setup compatibility at that moment, not indefinite model availability or every workbook result.

Alternative: silently repair a broken saved format by negotiating a new one. Rejected because that changes configuration during what the operator intended as credential repair and could bypass pending-task protections.

### 4. Reuse the Account workflow and make validation state explicit

Update `account.go`, the `.templ` source and `web/js/ai-setup.js` together. A fresh preset form has an empty model choice until fetching. All discovered valid IDs are enabled; the format label says it will be established during validation. The draft and confirmed cards show only their own recorded format.

Retain sequence-based rejection of late discovery responses. Provider/root/key edits clear fetched choices and unvalidated format state instead of restoring a compiled list. Preserve the exact saved selection for repair without fetching. After validation failure, keep the submitted non-secret model as an unvalidated form value without asserting it came from discovery; never refill a returned secret. An earlier staged proposal must remain visibly separate from the failed form.

Keep the existing full-page validation POST and confirmation POST. Confirmation submits the immutable staged draft identity, not current editable form values. Edits must not make new inputs appear validated; any prior staged proposal is clearly labeled with its exact values. Update the disclosure from one synthetic request to up to two, with possible provider usage. No AJAX validation framework is needed.

Regenerate only `account_templ.go` from its edited `.templ` source. Update `docs/account-settings.md` to remove claims about static model contracts, disabled unknown models and one-request-only validation.

## Risks / Trade-offs

- More selectable models can still be unusable, including models requiring unsupported protocols → Explain that discovery is unverified and fail compatibility validation safely rather than promising support.
- A Chat Completions model may require two requests and consume additional usage → Disclose the maximum, stop after success and retain the shared deadline; do not validate the whole catalog.
- A slow first request can consume the overall budget before a second format is tried → Report timeout and allow explicit retry; do not extend request time automatically.
- Provider refusals can be ambiguous → Use the finite continuation classification above and safe per-format failure summaries. Never infer entitlement or an alternative endpoint from error text.
- Runtime model behavior can drift after setup → Keep current failure and explicit retry behavior; never negotiate a new format or model during preparation.
- A previous draft may survive a failed validation → Show its exact validated identity separately; no failed or edited values acquire that draft's validation evidence.

## Migration Plan

No database migration or rewriting of connections, drafts or run evidence is expected. Existing records already store the exact format and remain usable. Introduce the resolver and UI changes together, validate with temporary databases and simulated providers, and leave existing records untouched.

Rollback can restore the earlier application without a schema rollback. Its existing saved-selection repair path can retain an already confirmed unknown model's stored format, but creating a new unknown preset selection would again be restricted. Never rewrite connections to fit the old catalog or silently revalidate during deployment.
