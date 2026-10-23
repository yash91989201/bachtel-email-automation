# Account settings: mailbox, AI connection, and task closure

Account settings (`/account`) holds the one sending mailbox and the one private
AI connection for the signed-in operator. Campaign settings stay on the
campaign page. No connection change ever starts processing, prepares a
workbook, or sends email.

## Sections and navigation

- **Mailbox** (`/account`) keeps the existing provider-first setup, Microsoft
  authorization callback, Titan validation, replacement confirmation, and
  exclusive sender claim. The former `/connection` links render the same
  Account mailbox section, so bookmarks keep working without a second mailbox
  surface.
- **AI connection** (`/account/ai`) configures the operator's single confirmed
  AI provider, exact model, API format and API key.

Both sections show the shared pending-task warning. It names every blocking
task with campaign, filename, distinguishing short reference, state and a link
to the task workspace, across all campaigns. Nothing is disabled only in the
browser: every mutation rechecks the lock inside its own transaction.

## AI connection model

Each operator owns at most one confirmed AI connection:

| Stored value | Purpose |
| --- | --- |
| provider preset | `opencode-go` or `custom` |
| API root | Exact HTTPS base for the protocol path |
| API format | `responses` or `chat-completions`; never inferred |
| model | Exact provider model identifier; no fallback |
| configuration generation | Bumped only by a configuration switch |
| credential generation | Bumped only when the API key changes |
| sealed credential | AES-256-GCM under `APP_ENCRYPTION_KEY`, bound to the owner and credential generation |

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

## OpenCode Go preset

Current verified contract (checked 2026-10-23; see
[OpenCode inference docs](https://opencode.ai/v2/docs/console/api/inference) and
[Go docs](https://opencode.ai/v2/docs/console/go)):

- API root: `https://opencode.ai/inference/openai/v1`
- Responses: `POST /responses`; Chat Completions: `POST /chat/completions`
- Authentication: `Authorization: Bearer <api key>`
- Client identity: a product user agent, plus a stable `x-opencode-session`
  per conversation. The adapter sends both; there is no operator-controlled
  header editor.
- Model families are fixed per model, not per endpoint: Muse Spark 1.3
  (`muse-spark-1.3`) uses Responses; DeepSeek V4.1 Flash
  (`deepseek-v4.1-flash`) uses Chat Completions. The setup form suggests IDs
  but requires the exact one and an explicit format; treating them as
  interchangeable is refused by validation.

The Go plan is documented for single-member workspaces and coding-agent
traffic. This project's validation sends one small synthetic normalization
request, which may consume provider usage. Deployment access, plan suitability
and the current model list must be confirmed with the operator's own Console
credentials: this change deliberately uses no live credentials, so it records
that readiness limitation rather than claiming verified production access.

### Provider data handling

Normalization sends only mapped workbook values, mapping/header metadata and
necessary source references to the confirmed endpoint. Account settings states
the external destination. OpenCode documents traffic monitoring for abuse, and
its Muse Contributor option may allow training on prompts and completions; it
does not promise zero retention. Treat that as a deployment-suitability
decision for real workbook data, not as an assumed coding-subscription
contract.

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

## Validation, activation and repair

1. Submit provider, API root, API format, exact model and API key.
2. The server checks the endpoint policy: HTTPS only, no loopback, private,
   link-local, multicast, unspecified or metadata target; the resolved dial
   address is checked again; redirects may not leave the validated origin; the
   request is time- and size-bounded; errors are fixed redacted text.
3. One synthetic request uses no workbook, recipient or candidate value and
   verifies the exact normalization output contract: one known id, one expected
   normalized value. Unknown, omitted or duplicated records fail.
4. The validated proposal shows its exact configuration. Confirmation installs
   it with the expected configuration generation and the account lock.
5. A same-configuration key replacement is allowed while tasks are pending and
   takes effect for future calls only. It does not resume, retry or regenerate
   anything; failed work still needs an explicit retry. A repair that changes
   provider, endpoint, format, model or output options is refused with the
   named blocking tasks.

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

Add a provider as a `Preset` (label, default root, supported API formats,
suggested exact model IDs, guidance) when a verified compatible endpoint
exists. Reuse an adapter for a supported protocol; add one small adapter when a
provider speaks a different protocol. Native Xiaomi MiMo and Fireworks presets,
a live model marketplace, automatic routing/fallback, arbitrary headers and
plugin scripts are deliberately outside this change.
