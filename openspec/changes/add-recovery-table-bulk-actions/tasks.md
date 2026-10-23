# Tasks

## 1. Recovery table and row actions

- [x] 1.1 Replace the recovery cards with one table that keeps recipient, worksheet position, state, reason and prior-attempt evidence per row, with correction-required and manually resolved rows pointing at their remedy instead of an unusable action. Verify the rendered table keeps a state badge per row and offers no action for those states.
- [x] 1.2 Move Retry, Send again and Mark already submitted into per-row dialogs that repeat the prior attempt and keep the required confirmations, duplicate-risk acknowledgement and resolution basis. Verify each dialog form still posts to its sending route with `hx-preserve` and an explicit submit button.
- [x] 1.3 Forward input attributes to the checkbox native input so a row selection can bind to the value the browser emits. Verify the selection checkbox carries the model binding on the input, not on the visual span.

## 2. Selection-scoped bulk actions

- [x] 2.1 Accept a selected member set on the sending action request and filter the existing atomic eligibility recheck by it, keeping the confirmed count equal to the selected row count. Verify a narrowed selection claims only its members and a stale count is refused without changing claims.
- [x] 2.2 Render the bulk bar with the selected count, one form per applicable action, and the selected ids and count bound at submit time. Verify the retry and uncertain forms appear only for their own selection, and that a selected uncertain set offers resend and resolution but never bulk retry.
- [x] 2.3 Keep the per-row dialogs as the no-selection path. Verify recovery actions still render and post without the selection layer.

## 3. Verification

- [x] 3.1 Cover the selected-set filtering, stale-count refusal, bulk resolution audit rows, posted member ids, and the table and bulk-form contract with focused tests.
- [x] 3.2 Verify the rendered page in a browser: selection count, submitted ids and count, each dialog's prior attempt and confirmation, and the required-confirmation refusal.
- [x] 3.3 Run make check and make vulncheck, and strict OpenSpec validation after the artifact changes. Record actual outcomes; `make lint-tailwind` still reports the pre-existing `break-words` diagnostic at `task.templ`, which this change does not touch.
