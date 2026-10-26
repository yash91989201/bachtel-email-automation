# Design

## Context

See `proposal.md` for motivation and the two delta specs for behavior. Current implementation evidence:

- `internal/sending/store.go` keeps one `dispatcher_state` row per mailbox connection. `stateRecord` initializes the rate and `SetRate` updates the current owner's mailbox in a transaction.
- `internal/sending/model.go` exposes rate, recent starts and cooldowns through `Settings`. `internal/sending/pacing.go` sets `DefaultRate = 25` and enforces rates 1–30, spacing and a rolling allowance.
- `internal/sending/worker.go` hardcodes four in both `nextWork`'s saturation selection and `launchPrepared`'s final capacity check. Authorization preparation can release the dispatcher lock; the launch path checks again afterward.
- `internal/migrations/1792320010_sending.go` defines the dispatcher state schema with a required whole-number rate, pacing history and a unique connection index.
- `internal/web/sending.go` handles the task-scoped `/sending/rate` form by calling the mailbox-scoped setter under `Dispatcher.Synchronize`. `internal/web/task.go` loads mailbox settings into the view. `internal/web/views/task.templ` shows a fixed “Up to 4” summary and a single rate input.

These are existing mechanisms, not evidence that configurable concurrency has been implemented. The approved specs currently mandate four slots and default pacing 25; this change deliberately revises those requirements.

## Goals / Non-Goals

**Goals:** Extend the existing persisted mailbox settings and launch checks, keep both settings coherent during a save, and make runtime changes work with existing pacing history and in-flight request ownership.

**Non-Goals:** Add another scheduler, concurrency cache, per-task settings table, provider adapter changes, or a new dependency. Existing task authorization and request/copy lifetime ownership remain authoritative.

## Decisions

### 1. Store concurrency alongside the existing mailbox rate

Add a required whole-number `concurrency` field with bounds 1–10 to `dispatcher_state`, expose it in `sending.Settings`, and define `DefaultConcurrency = 2` beside `DefaultRate = 15`. New rows initialize both explicitly. Validate both persisted fields and submitted settings at the existing boundaries.

Use a new migration to backfill existing rows to concurrency 2 while preserving every saved valid rate and all history/cooldown fields. A missing settings row receives the defaults; invalid stored values are errors, not implicit permission to send with fallback limits. Zero-valued synthetic settings in tests must be made explicit or exercise the same documented initialization path.

Alternative: per-task overrides would require aggregate mailbox arbitration and introduce different behavior from today's controls. Keep the current mailbox scope. A separate settings table would duplicate the existing connection-owned state.

### 2. Update both values in one transaction behind the launch barrier

Extend the existing sending-settings mutation and replace the rate-only store operation with a pair update, reusing the current `/sending/rate` route to avoid adding a parallel settings API. Require both `rate` and `concurrency`; an old rate-only form is refused with a reload/correction remedy and cannot reset concurrency silently. Adjust internal callers and tests to supply the complete pair.

Keep existing task/campaign ownership checks, CSRF protection and current-mailbox lookup. Parse and validate both values before mutation. Resolve the connection and update its settings within the transaction; preserve recent starts, `next_eligible`, cooldown and reason. Execute through `Dispatcher.Synchronize`, so a successful save and later starts have a clear ordering, and wake the existing coordinator after success.

Alternative: separate saves allow partial settings changes and require more forms and error paths. One pair update fits the requested UI and provides an unambiguous saved state. Do not create a candidate action or change formatting-approval identity for an operational limit edit.

### 3. Apply the configured limit at both capacity checks

Have `nextWork` read and validate the offered work's mailbox settings before deciding whether its connection is saturated. Keep its existing exhausted-connection selection so a full mailbox yields opportunities to another one. Retain the final settings read and capacity check in `launchPrepared`, including after sender preparation returns through the launch barrier.

Compare `inflightByConnection` to the current configured limit at each decision. Continue using the existing in-flight accounting and release path, including any existing Sent-copy lifetime. When the new limit is below the current count, allow those owned requests to settle and launch nothing until the count is strictly below the new limit. No cancellation or resizing of active workers is needed.

Keep `nextStart`, rolling start history, cooldown and the existing rate limiter in control of launch timing. Additional capacity never means a batch burst. A settings save does not resume a persisted paused task or release a fatal storage barrier.

Alternative: caching concurrency in the dispatcher adds invalidation and restart concerns for a small setting. Read it through the existing store and test both selection and final-launch behavior instead.

### 4. Extend the current form and view state

Add concurrency to `TaskPageData` and populate it with rate from the same mailbox settings read. Replace the fixed concurrency summary with the saved value. Reuse the existing numeric inputs and accordion for **Concurrent emails** and **Pacing**, with their specified bounds/defaults and explicit emails-per-minute units. Submit both through **Update sending settings**.

Keep submitted drafts separate from saved summary values when rendering invalid input or storage errors. Show the settings accordion open on error, use the existing success/redirect behavior, and preserve both inputs through the existing `hx-preserve` and accordion state mechanisms. Explain mailbox-wide scope in the form. Format and regenerate only the affected `.templ` source and committed generated output.

Alternative: a new account page or custom JavaScript state layer is unnecessary for extending the current Sending section.

## Risks / Trade-offs

- A lower concurrency limit can temporarily be below the already in-flight count. Let those requests finish and block additional starts; test the drain boundary and saves racing with sender preparation.
- Raising capacity can increase outstanding provider requests. Keep pacing and provider cooldown behavior intact; the 1–10 range is an application setting, not a promise of provider throughput.
- An existing rate of 25 may have been explicitly chosen or inherited from the old default. Preserve it because the stored row does not distinguish those cases; display the saved value alongside the new default guidance.
- Updating only one hardcoded capacity check would cause incorrect mailbox selection or launches. Cover both paths and independent-mailbox progress with simulated providers.

## Migration Plan

1. Add a new forward migration using the existing migration conventions; introduce and backfill concurrency 2 and enforce its integer bounds. Do not rewrite the historical migration or stored rates.
2. Initialize new dispatcher rows with 2/15 and deploy the store, dispatcher and form changes together. Preserve already-populated valid concurrency on repeated initialization.
3. Verify existing settings, fresh settings and restart recovery using temporary application databases. Preserve durable attempt and pacing evidence during upgrade.
4. For rollback, stop/drain the newer process using the existing shutdown path before returning to a version with a fixed four-slot limit. A schema rollback removes only the new concurrency field; retained rates and history remain valid in the previous schema. Reverting code restores its former fixed-four behavior, so rollback is not a way to preserve an operator-selected concurrency limit.
