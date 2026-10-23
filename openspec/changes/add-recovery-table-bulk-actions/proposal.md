# Proposal

## Why

Recovery currently renders one card per failed or uncertain row, each carrying its own forms. A settled task with many failures pushes the remaining sending controls and pacing settings out of reach, and the operator repeats the same confirmed action row by row. The rows also read as separate groups even though the operator needs one scannable list keyed by state.

## What Changes

- Present recovery rows in one table whose state column distinguishes retry-eligible definite failures, correction-required rows and uncertain outcomes. Each row keeps its recipient, worksheet position, reason, prior attempt and state-specific action.
- Move each row's action into a dialog that repeats the prior attempt and keeps the explicit confirmation. Retry, Send again and Mark already submitted remain deliberate single-candidate operations.
- Allow a confirmed row selection to submit one bulk request for Retry, Send again or Mark already submitted. The confirmed count is the selected row count, and the existing atomic eligibility recheck refuses a stale selection instead of acting partially.
- Preserve the sending safeguards: no bulk retry of uncertain, submitted, active or correction-required rows; resend keeps the duplicate-risk acknowledgement; manual resolution records the actor, time and basis per candidate and never invents a provider acceptance time.
- Keep recovery usable without the selection layer: the per-row dialogs are native posts, and the table keeps the row evidence visible.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-task-workspaces`: recovery presentation becomes one state-differentiated table, and the sending action matrix lists selection-scoped retry, resend and manual resolution beside the per-row controls.
- `sending-recovery`: retry, uncertain resend and manual resolution each accept a confirmed selection and act on it in one atomic request.

## Impact

- Affects the recovery view, the checkbox primitive (attributes for its native input), the sending action request and handler, and focused sending/view tests.
- No schema, migration, route or provider change. No new operator authorization, no automatic retry, no bulk retry of uncertain rows, and no real email in automated checks.
- Depends on the implemented operator-owned dispatch and approval path; the durable attempt and resolution records stay the audit evidence.
- Selection state is a presentation convenience only: it never authorizes an action that the per-row control could not already perform.
