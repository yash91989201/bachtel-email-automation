# Proposal

## Why

Fixed mapping formats cannot adequately clean inconsistent workbook values or identify semantic mismatches such as compensation in a department column. Operators need AI-prepared data, actionable exclusions, and control over the exact reviewed values used for sending.

## What Changes

- **BREAKING:** Remove text/name/designation/email formatting choices from new mapping authoring; retain explicit recipient selection, required/optional settings and fallbacks.
- Normalize only campaign-mapped fields during initial task preparation, using a visible application base prompt and optional task-specific instructions. Use headers as semantic guidance and separate multiple distinct values with comma followed by one space, including multi-address email cells.
- Separate successful corrections from unresolved AI findings. Backend validation combines mapped-field findings with required/optional rules, address validation, expanded-recipient duplicate detection and existing campaign-history restrictions.
- Provide protected exclusion and prepared-sending XLSX downloads. Unmapped-column problems do not exclude entries; shared-field problems affect all recipient siblings while address-specific problems affect only the relevant entry.
- Let the operator either **Approve as-is** or upload an edited sheet. The latter receives backend validation only: no AI calls or content rewriting. Both paths create an immutable final sending snapshot with a downloadable accepted sheet and email preview.
- **BREAKING:** Require a submitted test and explicit received-format approval for each exact final snapshot, campaign revision and mailbox generation. Dataset approval never authorizes a test, candidate start or schedule. Each replacement final upload needs fresh test approval, even when the campaign revision is unchanged.
- Add owner-scoped Account settings with Mailbox and AI connection sections. Keep one confirmed AI connection per operator, with provider-first setup, model, API key and endpoint configuration, verified before activation. Use extensible provider presets and supported API adapters rather than binding normalization to one vendor.
- Use only the confirmed AI provider/model for preparation, without automatic fallback. Block AI configuration switching and mailbox replacement/disconnect while any account task remains pending; completed or explicitly closed tasks release the lock after outstanding operations settle. Show named blocking tasks and retain same-configuration credential repair.
- Save instruction drafts separately from actual normalization-run history. Show failures and permit explicit retry/rerun without silently changing reviewed results. Freeze input and final content after sending starts.

## Capabilities

### New Capabilities

- `task-data-preparation`: AI normalization, mapped-field findings, preparation history, operator finalization and final sending snapshots.
- `account-settings`: Account-level connection sections, shared pending-task lock and blocking-task remedies.
- `ai-connections`: One private verified AI configuration per operator, fixed model selection, provider setup, credential repair and future provider support.

### Modified Capabilities

- `campaign-templates`: Mapping syntax without formats, direct sample rendering and preservation of historical content.
- `email-personalization`: Header-guided preparation instead of render-time format transforms; saved final values drive preview/test/send.
- `excel-intake`: Separate preparation and final-upload validation, recipient expansion and reportable exclusions.
- `email-dispatch`: Test approval and sending authorization bound to the exact final snapshot.
- `campaign-task-workspaces`: Preparation/review controls, both finalization paths, downloads, distinct approval states and explicit task closure.
- `mail-connections`: Account-wide pending-task lock for switching/disconnecting, with same-mailbox repair and existing replacement/reset safeguards preserved.
- `history-exports`: Prepared/final sending downloads and enhanced exclusion provenance without changing historical result semantics.

## Impact

Campaign mapping validation/hash/version handling; intake planning, private artifacts and task memberships; personalization rendering; sending tests, approvals, starts/schedules/recovery guards; task/account UI, owner-scoped connection storage, provider adapters, migrations and documentation are affected. Existing multi-email expansion, exclusions, campaign/email identity, mailbox isolation, pacing and submission history remain prerequisites and must be preserved. No active change prerequisites were found; multi-email expansion is already present in main specs and code.

The initial setup preset is OpenCode Go with Muse Spark 1.3; activation requires the operator to validate and confirm the exact configuration in Account settings. Include a custom compatible connection for supported Responses and Chat Completions APIs. Future presets such as Xiaomi MiMo or Fireworks reuse a compatible adapter or add a small adapter when necessary. Pi remains a local evaluation harness. Verify actual provider HTTP contracts, deployment access and data policies without importing local Pi credentials. No additional dependency is approved by this proposal.

Out of scope: recipe builders, AI after final upload, automatic sending, automatic factual enrichment, an inline spreadsheet editor, automatic provider/model routing or a provider/model marketplace, edits to started tasks, retroactive rewriting of historical snapshots or attempts, and production email or provider calls in automated checks. AI readiness is not proof of factual correctness; operators own semantic review of final uploaded values.
