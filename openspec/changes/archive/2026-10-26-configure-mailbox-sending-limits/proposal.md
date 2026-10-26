# Proposal

## Why

Sending settings exposes pacing but fixes concurrent submissions at four, preventing operators from choosing how many emails their mailbox may have in flight. Operators need editable concurrency and lower defaults: two concurrent emails and 15 email starts per minute.

## What Changes

- Add a saved **Concurrent emails** setting accepting whole numbers from 1 through 10, defaulting to 2.
- Keep **Pacing** configurable from 1 through 30 email starts per minute and change its default from 25 to 15.
- Preserve the existing mailbox-wide scope: all tasks, formatting tests and retries using that connection share both limits, even when the controls are edited from one task's Sending section.
- Save both settings together and show their current values, ranges, defaults and mailbox scope in the existing Sending settings accordion.
- Apply saved limits to subsequent launch decisions. Reducing concurrency lets existing requests finish and holds new requests until capacity is available; increasing it still respects pacing and cooldown.
- Initialize existing mailbox state with concurrency 2 and preserve its saved pacing rate. New mailbox state uses concurrency 2 and pacing 15. Persist settings across restarts without resetting pacing history or authorizing sending.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `email-dispatch`: Replace the fixed concurrency bound with a mailbox-owned 1–10 setting, default 2; change the pacing default to 15; define validation, live updates, persistence and upgrade behavior.
- `campaign-task-workspaces`: Expose both sending controls with accurate scope, defaults and ranges, atomic save feedback and preservation of drafts during progress updates.

## Impact

- Extend existing dispatcher state and its migration; update the sending store, dispatcher launch checks, web mutation handler and task view. Reuse existing numeric inputs, accordion and dispatcher synchronization.
- Revise the current `email-dispatch` requirements that mandate four slots and default pacing 25. The existing per-mailbox scheduling and durable-attempt implementation is the prerequisite; no other active change is required.
- Existing saved rates, including 25, remain intentional stored values. The new default applies when mailbox settings are first initialized, rather than overwriting an operator's choice.
- Keep existing owner authorization, exact-snapshot formatting approval, explicit task start/retry/resend authorization, provider cooldowns and durable recovery behavior. Saving settings grants no sending authorization.
- Out of scope: per-task limit overrides, fixed-size sending batches, provider-specific quota discovery, automatic tuning, changes to message content or approval identity, and live email or deployment.
- No new dependency or external service is required.
