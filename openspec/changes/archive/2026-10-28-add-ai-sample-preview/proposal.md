# Proposal

## Why

Campaign sample preview renders supplied values directly, so operators cannot try the AI cleanup used during task preparation before uploading a workbook. An explicit, temporary AI sample action will make those transformations and unresolved issues inspectable without creating task data or sending email.

## What Changes

- Keep **Render preview** as a backend-only action and add **Normalize with AI & preview** for one sample row using the owner's confirmed AI connection.
- Reuse the existing header-guided normalization pipeline, including its versioned base prompt, mapped-field payload, response validation, recipient integrity rules and findings handling.
- Show original, prepared and effective rendered values, corrections, unresolved findings, and the model/prompt identity used. Clearly label the result as an AI-prepared sample, never a final task email or approval.
- Keep samples temporary: no saved inputs/results, preparation runs, jobs, task records, artifacts, persistent caches or browser storage. Disclose external provider processing and possible usage charges before the AI action.
- Bound execution and concurrency, preserve original inputs, reject stale results, and make failures explicit without fallback to raw values or another model.
- Narrow existing blanket prohibitions on AI during preview to allow only this explicit campaign sample action. Final task preview, test, downloads, final upload and sending retain their existing deterministic behavior.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-templates`: Separate direct sample rendering from explicit temporary AI preparation and define comparison, findings, failure and lifecycle behavior.
- `task-data-preparation`: Clarify that automatic AI execution remains forbidden when viewing data, with only the explicit campaign sample action exempted; task runs remain durable.
- `ai-connections`: Extend confirmed-connection and privacy rules to temporary sample normalization without requiring durable run evidence for samples.
- `email-personalization`: Scope the no-AI preview rule to task/final content and preserve single-recipient sample validation.

## Impact

- Reuse `internal/preparation` input/prompt/response/output logic and extract the batch inference operation currently embedded in `Service.execute`; the durable worker remains responsible for task claims, progress and publication.
- Extend the campaign preview HTTP actions and `internal/web/views/campaign.templ` result/form models, with focused service, handler and UI regression coverage and regenerated templ output.
- Reuse `internal/ai` confirmed credentials, safe transport and request bounds. No new dependency, collection or migration is expected.
- Prerequisites are the implemented AI connection, task preparation and versioned campaign rendering capabilities; there is no active prerequisite change at proposal creation.
- Out of scope: implementation in this proposal, workbook/task workflow redesign, campaign-level instruction defaults, custom sample prompts, multi-recipient sample expansion, storing sample history, background sample jobs, email delivery and deployment. No new authorization gate or approval is introduced.
