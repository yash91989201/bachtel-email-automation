# Design

## Selected-set action

The bulk request carries one `member_id` form value per selected row, and the action request carries the selected set. The store filters its existing atomic recheck by that set instead of loading rows one at a time, so a bulk retry, resend or resolution uses the same authorization, approval, mailbox, restore-review, eligibility and count checks as the single-candidate path. `ExpectedCount` is the selected row count: when a selected row is no longer eligible, the claim count differs and the whole request is refused with the changed-count message. Nothing is applied partially.

Fanning out one request per row was rejected. It would apply some rows before another row's refusal, need one action identity per row to stay idempotent, and turn a single confirmed count into an unverifiable sequence.

## Selection state

Selection lives in the view (two client-side sets: retry-eligible rows and uncertain rows) because it is presentation state with no durable meaning. The bulk forms stay native posts and are hidden until their set is non-empty; the selected ids and the confirmed count are written into the form at submit time. The visible checkbox span cannot carry the binding, so the checkbox primitive forwards input attributes to the hidden native input that owns the value and change event.

Recovery therefore degrades without the selection layer: every row's own dialog is a native post with its own confirmation, and only the bulk bar disappears.

## Uncertain rows

Resend and manual resolution share one selection because an uncertain row is either resent or resolved, never both in the same confirmed action. A batch resend acknowledges duplicate risk once for the confirmed selection, and a batch resolution records one basis per candidate, matching the per-candidate audit rows the store already writes. Uncertain rows are still never retried in bulk, and a selection containing mixed states only offers the actions applicable to its own state.

## Known limitation

While the task is running, the Sending section polls and replaces its markup, which discards a selection made just before the swap. Recovery actions normally happen after work settles, and the per-row dialogs keep their stable ids so an open dialog is not re-created mid-action; a lost selection only requires reselecting.
