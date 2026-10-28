# Account settings: mailbox, AI connection, and task closure

Account settings hold the one sending mailbox and the one private AI connection
for the signed-in operator. Mailbox lives at `/account/mailbox` and the AI
connection at `/account/ai`; there is no separate Account landing page.
Campaign settings stay on the campaign page. No connection change ever starts processing, prepares a
workbook, or sends email.

## Sections and navigation

- **Mailbox** (`/account/mailbox`) keeps the existing provider-first setup,
  Microsoft authorization callback, Titan validation, replacement confirmation,
  and exclusive sender claim. The former `/account` and `/connection` landing
  URLs return 404; there is no compatibility redirect and no second mailbox
  surface.
- **AI connection** (`/account/ai`) configures the operator's single confirmed
  AI provider, exact model, validated API format and API key. Supported presets
  are OpenCode Go and Xiaomi MiMo, both with an editable default endpoint and a
  Fetch models action; Custom compatible keeps the fully manual setup.

Both sections show the shared pending-task warning. It names every blocking
task with campaign, filename, distinguishing short reference, state and a link
to the task workspace, across all campaigns. Nothing is disabled only in the
browser: every mutation rechecks the lock inside its own transaction.

## AI connection model

Each operator owns at most one confirmed AI connection:

| Stored value | Purpose |
| --- | --- |
| provider preset | `opencode-go`, `xiaomi-mimo` or `custom` |
| API root | Exact HTTPS base for the protocol path; preset defaults are starting values, an override is preserved |
| API format | `responses` or `chat-completions`; established by validation for presets, explicit only for `custom` |
| model | Exact provider model identifier; no fallback |
| configuration generation | Bumped only by a configuration switch |
| credential generation | Bumped only when the API key changes |
| sealed credential | AES-256-GCM under `APP_ENCRYPTION_KEY`, bound to the owner and credential generation |

The stored provider field is free text bounded to 32 characters, so adding the
Xiaomi preset needed no migration and never rewrote a saved connection. Older
application versions do not recognize a Xiaomi selection: switch those accounts
deliberately before rolling the application back.

A proposal is validated and stored separately. It becomes the confirmed
connection only when the operator confirms the exact provider, endpoint,
format and model. Failed validation, cancellation, a stale callback or a
competing committed change leaves the previous confirmed configuration and all
task data untouched. The API key is never returned to a browser, logged,
downloaded, or used to prefill a form; the page only says whether a key is
configured. A missing or different encryption key makes the saved connection
unusable without rewriting its ciphertext and does not affect another
operator's connection.

There are no deployment-wide AI credentials. Connection records and drafts are
owner-scoped, and every request is guarded by the operator session.

## Supported providers

Both presets share one contract: an API key, an editable API root that starts
at the documented default, a Fetch models action and API formats established
during validation. Empty roots receive the preset default; any submitted
override is validated and preserved, so no request silently moves to the
official host.

### OpenCode Go preset

Current verified contract (checked 2026-10-23; see
[OpenCode inference docs](https://opencode.ai/v2/docs/console/go) and the
[Go docs](https://opencode.ai/v2/docs/console/api/inference)):

- Default API root: `https://opencode.ai/inference/go/openai/v1`
- Default model list: `https://opencode.ai/inference/go/v1/models`
- Responses: `POST /responses`; Chat Completions: `POST /chat/completions`
- Authentication: `Authorization: Bearer <api key>`
- Client identity: a product user agent, plus a stable `x-opencode-session`
  per conversation. The adapter sends both; there is no operator-controlled
  header editor.
- Both formats are candidates for any listed model: validation establishes
  which one the exact endpoint and model accept. There is no per-model format
  mapping, and a model absent from any shipped list is not refused for that
  reason.

The Go plan is documented for single-member workspaces and coding-agent
traffic. This project's validation sends up to two small synthetic
normalization requests (Responses first, then Chat Completions only if the
first fails with a continuation-case error), which may consume provider usage.
Deployment access, plan suitability
and the current model list must be confirmed with the operator's own Console
credentials: this change deliberately uses no live credentials, so it records
that readiness limitation rather than claiming verified production access.

### Xiaomi MiMo preset

Current verified contract (checked 2026-10-23; see the
[model-list docs](https://mimo.mi.com/docs/en-US/api/model/list-models) and the
[OpenAI-compatible chat docs](https://mimo.mi.com/docs/api/chat/openai-api)):

- Default API root: `https://api.xiaomimimo.com/v1`
- Default model list: `https://api.xiaomimimo.com/v1/models`
- Chat Completions: `POST /chat/completions`
- Authentication: `Authorization: Bearer <api key>`
- The preset attempts only Chat Completions. The Anthropic-compatible
  and audio endpoints are not supported here.

The preset uses the standard API, not the Token Plan host or its
coding-oriented usage limits. Use ordinary production API credentials with
provider permission appropriate to the work.

### Model discovery

`POST /account/ai/models` fetches the selected preset's model list with the
submitted key and endpoint. It is operator-authenticated, form-bounded,
no-store, and shares the inference endpoint policy: HTTPS, public destination
checks on the resolved address, same-origin redirects only, timeout and
response-size bounds, and fixed redacted failures. The browser keeps the typed
key; the response contains only model options or fixed safe error text, never
the key, outbound headers or a raw provider body.

Discovery is read-only: it never asserts compatibility, stages a draft,
activates a configuration, validates normalization, prepares work or sends
email. A public list may prove neither key entitlement nor billing; only the
exact synthetic validation establishes setup readiness. Every syntactically
valid listed model ID is selectable as an unvalidated choice, including IDs
the application has never seen: there is no compiled model catalog or
fallback list, no disabled unknown models and no model-to-format mapping.
Invalid IDs are omitted, and duplicate IDs appear once in provider response
order. Changing the provider, endpoint or key clears the fetched selection and
late responses cannot replace models for newer inputs.

### Provider data handling

Normalization sends only mapped workbook values, mapping/header metadata and
necessary source references to the confirmed endpoint. Account settings states
the external destination. OpenCode documents traffic monitoring for abuse, and
its Muse Contributor option may allow training on prompts and completions; it
does not promise zero retention. Review Xiaomi's data-handling terms too. Treat
both as deployment-suitability decisions for real workbook data, not as assumed
coding-subscription contracts.

## Task preparation

A validated task can prepare its mapped values with the confirmed connection
from its Recipients section. Initial preparation, retry and rerun each create an
immutable run capturing the mapped input, base prompt version and text, task
instructions, provider/endpoint/API format/model, configuration and credential
generations, inference options and the published output. Every batch of a run
uses the captured configuration; a provider failure never calls another model.
Runs execute in a server-owned durable job, so closing the browser does not stop
them, and a restart marks unfinished work incomplete until an explicit retry.
Prepared values and findings are review evidence only: no candidate, attempt or
schedule record is written, and sending keeps using the validated task snapshot
until the later review/finalization workflow is installed.

### Temporary campaign samples

The campaign Template tab also offers **Normalize with AI & preview** for one
sample row. It uses this same confirmed selection and base policy without task
instructions, captures the configuration and its credential from one read, and
sends exactly one bounded request under a 30-second deadline with no retry and
no fallback model. A connection or credential change during the request
discards the result and asks for a new explicit action; it never combines a
captured endpoint with another configuration's key. The sample creates no run
record, so it has no permanent history, and merely reopening or rendering a
sample never calls AI. See `docs/campaign-templates.md` for the operator view.

## Validation, activation and repair

1. Submit the provider credentials. A preset asks for API root and API key,
   optionally fetches models and then requires one model choice; its API
   format is resolved during validation (step 3), never from the model name or
   a compiled mapping. Custom compatible asks for endpoint, supported API
   format, exact model and API key explicitly, and that one explicit format is
   validated once without ever probing another.
2. The server checks the endpoint policy: HTTPS only, no loopback, private,
   link-local, multicast, unspecified or metadata target; the resolved dial
   address is checked again; redirects may not leave the validated origin; the
   request is time- and size-bounded; errors are fixed redacted text.
   Encryption-key availability is checked before any provider usage is spent,
   and no network call is made inside a database transaction.
3. A new preset selection resolves its API format with a short sequential
   synthetic probe over the preset's supported formats: OpenCode Go tries
   Responses first, then Chat Completions; Xiaomi MiMo tries only Chat
   Completions. Each request keeps the provider, endpoint, model and supplied
   key constant, uses no workbook, recipient or candidate value, and must pass
   both response-envelope parsing and the exact normalization output contract:
   one known id, one expected normalized value; unknown, omitted or duplicated
   records fail. The first format that passes wins, and no further request is
   made after success; if both would pass, the result is deterministically the
   first. Each format is attempted at most once, so at most two requests total,
   under a single overall 30-second deadline and the existing 512 KiB response
   bound; there are no retries and no backoff.
4. Validation continues to the next format only on HTTP 400, 404, 405, 415 or
   422, an invalid response envelope, or a normalization-contract failure. It
   stops without trying another format on authentication or authorization
   refusal, throttling, service or transport failure, an unsafe destination,
   an oversized response, cancellation or deadline exhaustion, and any other
   failure. Only fixed redacted errors reach the operator; provider prose and
   response bodies are never exposed or parsed for routing.
5. The successfully validated API format, with the exact provider, endpoint and
   model, is shown for review before explicit confirmation. The validated
   configuration is staged with fingerprints, configuration and credential
   generations and the sealed credential; confirmation installs that exact
   staged proposal with the expected configuration generation and the account
   lock.
6. A same-configuration key replacement is allowed while tasks are pending and
   takes effect for future calls only. It does not resume, retry or regenerate
   anything; failed work still needs an explicit retry. The saved model and
   root remain selectable without a discovery round trip, including a retired
   model the provider no longer lists: the replacement key is validated
   against the recorded API format only, with no discovery and no catalog
   lookup. A repair failure never switches format automatically; a format
   change is a configuration switch refused with the named blocking tasks
   while tasks are pending. A repair that changes provider, endpoint, model or
   output options is refused the same way.

After confirmation there is no probing anywhere: runtime preparation and
retries use only the stored endpoint, model and format, with no protocol
probing and no provider or model fallback.

### Setup feedback on the page

The Account page keeps each setup state distinct:

- A fresh preset form offers no model list until Fetch models succeeds; there
  is no compiled fallback. Every fetched ID is enabled as an unvalidated
  choice, with no format or compatibility claim.
- Changing the provider, root or key clears the fetched selection and ignores
  late discovery responses. A saved selection stays available for repair
  without another fetch.
- After a failed validation the page keeps the submitted non-secret values,
  including the model ID, as an unvalidated form choice and shows a fixed safe
  error. The entered key is never returned.
- The validated proposal is a separate card showing its exact provider,
  endpoint, model and resolved API format. Edited or failed inputs cannot
  acquire that evidence, and confirmation submits only the staged proposal's
  identity.

## Pending-task lock

A task is pending from durable upload or task creation until execution is
completed or an explicit closure is final. Pending states include staging,
ready, running, waiting, stopping, paused, interrupted, stopped, closing, and
completed tasks with queued or in-flight operations. Export generation alone is
not a connection lock.

While any owning-account task is pending, both sections refuse:

- AI provider, model, API root, API format or output-option switching/removal;
- mailbox switching, replacement or disconnect.

They still allow:

- initial setup of a connection that has never been confirmed;
- same-configuration AI key repair;
- same-mailbox Microsoft reauthorization or Titan reconnect, and verified
  application-credential rotation under the existing settlement guards;
- finishing or explicitly closing the blocking tasks.

Task activation (start/resume/retry/resend) makes the task pending in the same
transaction that reserves work, so a competing connection change cannot slip
between the lock check and dispatch. Confirmation rechecks the lock and the
expected generations inside a brief transaction; no provider call is made while
a transaction is open.

Mailbox replacement still performs its separately confirmed destructive reset
only after the lock clears, and it preserves the operator's AI connection.
AI switching never deletes tasks, history, campaign revisions, outputs or
approvals.

## Close task: completion vs stop vs close

| Action | Meaning | Can it send again? |
| --- | --- | --- |
| **Stop** | Stop new launches and settle active requests; the stored outcome remains a live task state. | Yes: resume the safe remainder after fresh checks. |
| **Complete** | Execution reached its natural end status (`completed`); remaining recipients may still be retryable/resumable. | Yes: an explicit resume/retry may reactivate it. |
| **Close** | Terminal closure marker with durable actor and time. Queued work and enabled schedules are cancelled, in-flight requests settle first, and uncertain submissions stay uncertain. | No: no preparation, finalize, schedule, test, resume, retry or resend. |

Closing is explicit and idempotent. It works for staged/unvalidated,
failed-preparation, review-pending, paused, stopped, scheduled and active
tasks. The underlying execution status is preserved separately from the
closure marker, so lists and the task page can explain why no next send action
exists. Attempts, approvals, manual resolutions, original inputs, final values,
duplicate-prevention history and downloads remain available; manual outcome
classification stays available without reopening the task or authorizing
email. A closed task keeps blocking connection changes until in-flight work
settles, then releases the lock. Future processing requires a new task.

## Future presets and adapters

Add a provider as a `Preset` (label, default root, supported API formats and
guidance) when a documented compatible endpoint exists. The preset supplies its
supported API formats; models are discovered at setup and the format is
resolved by validation of the exact configuration; there is no
model-to-format table to extend. Reuse an adapter for a supported protocol;
add one small adapter when a provider speaks a different protocol. Fireworks,
a live model marketplace, automatic routing/fallback, arbitrary headers and
plugin scripts are deliberately outside this change.
