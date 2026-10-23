# Design

Status: implemented. Verification evidence is recorded in `verification.md`.

## Context

See proposal.md for motivation. The current `internal/ai` package already owns encrypted owner-scoped keys, two HTTP protocol adapters, exact synthetic validation, immutable configuration fingerprints and account-lock enforcement. Account settings renders server forms using templ/Alpine. The current preset overwrites OpenCode endpoint submissions and supplies a short static suggestion list; there is no discovery route. Xiaomi is not currently a valid provider.

## Goals / Non-Goals

Use those existing boundaries and dependencies to hide supported-provider protocol details. Preserve manual root/format/model/key configuration for Custom compatible. This change does not create a universal provider framework, arbitrary request/response scripting, new authentication schemes, Anthropic/Gemini adapters, fallback routing, or production suitability approval for coding subscriptions.

## Decisions

- Extend provider reference data with Xiaomi MiMo (`xiaomi-mimo`) and model-contract resolution. OpenCode Go's current documented OpenAI-family root is `https://opencode.ai/inference/go/openai/v1`; Xiaomi's standard API root is `https://api.xiaomimimo.com/v1`. Both are defaults only: preserve submitted permitted roots. Empty roots can receive the selected preset default.
- Maintain the small verified model-to-format mapping for OpenCode's two implemented protocols. Xiaomi's documented compatible text model IDs use Chat Completions. Models outside the verified contract remain listed but unavailable, with a reason; no suffix guessing or failed-inference probing. Server-side validation resolves the supported preset format again, so hidden/client fields cannot select another wire contract. Existing confirmed exact configuration remains usable for same-configuration credential repair, including retired catalog entries, without requiring a model-list round trip.
- Add `Store.DiscoverModels(ctx, ownerID, Proposal) ([]ModelOption, error)` using the same bounded client, timeout, response-size, destination, redirect and credential-redaction policy as inference. `ModelOption` carries JSON `id`, `format`, `supported`, `reason`. It authenticates requests with the submitted key, performs no writes and never launches inference. Validate provider, root and key without requiring a chosen model. Parse the documented OpenAI-style `data` list, validate bounded model identifiers, deduplicate and order results. The list may contain unavailable protocols or no compatible models; return those facts rather than inventing model availability.
- Derive Go's model-list URL by replacing a trailing `/openai/v1` with `/v1/models`; generic overridden roots and Xiaomi append `/models`. Thus the default reaches `https://opencode.ai/inference/go/v1/models`, and overrides never silently contact the official host. Custom setup remains manual and requires no discovery.
- Add authenticated `POST /account/ai/models` with the existing form/CSRF boundary, request-body limits and no-store headers. Return only model options or fixed safe error text; never return keys, outbound headers or raw provider errors. The browser keeps the entered password in the existing form. Supported setup uses Alpine fetch/state, clears selection on root/key/provider edits, suppresses late responses, and shows a read-only inferred format. Built-in validation remains followed by explicit confirmation. Restore-default affects only the form.
- Reopen saved configuration from stored provider/root/model/format rather than defaulting to a different provider. Offer a repair path retaining the exact saved selection and new key, independent of discovery; existing task locks still gate activation atomically.
- No new secret fields, custom headers or JSON configuration are needed for the already-supported standard protocols. Existing custom controls remain the complete supported manual contract. Do not broaden this work into a new provider SDK.

## Risks / Trade-offs

- Provider catalogs evolve: unknown contract IDs are disabled with a Custom compatible/manual remedy instead of being silently assigned a format. Update the explicit mapping as official contracts change.
- A public model list may not prove key validity, billing or model entitlement: only the exact synthetic validation establishes setup readiness.
- Edited roots may have different behavior: validate the selected root/model/format, and never reuse stale browser discovery state as proof of validation.
- Go and MiMo Token Plan describe coding-oriented usage restrictions. Testing here is simulated; ordinary production API credentials and appropriate provider permission remain operator responsibilities. The Xiaomi default is the standard API, not a Token Plan host.

## Migration Plan

The existing provider fields are unconstrained text, so Xiaomi requires no database migration. The implementation leaves confirmed configurations and credentials unchanged and does not rewrite existing OpenCode endpoints; defaults affect new forms only. Older application versions do not recognize new Xiaomi selections; switch those accounts deliberately before rolling the application back. The ai-connections and account-settings main specs are synchronized with the implemented behavior.

## Sources

- https://opencode.ai/v2/docs/console/go/ (model list, per-model contracts and client requirements, checked 2026-10-23)
- https://mimo.mi.com/docs/en-US/api/model/list-models (standard API model listing)
- https://mimo.mi.com/docs/api/chat/openai-api (compatible text model contract)
- Cherry Studio v2.1.7 provider registry and model-sync implementation, inspected during this conversation; use the pattern rather than importing its frontend/SDK architecture.
