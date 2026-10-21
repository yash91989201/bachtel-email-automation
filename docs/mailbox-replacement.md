# Mailbox reconnection and replacement

Each operator has exactly one current mailbox. Its address is an exclusive
claim across all operators: a mailbox stays claimed while connected,
disconnected or awaiting reconnection, so no other operator can take it.

## Routes

All routes require the operator session and affect only that operator's
workspace.

| Route | Purpose |
| --- | --- |
| `GET /connection` | Connection page: status, provider, mailbox address, connect forms, disconnect |
| `POST /connection/microsoft/connect` | Start a Microsoft 365 authorization |
| `GET /connection/callback` | Microsoft redirect; must match an in-flight authorization attempt |
| `POST /connection/titan` | Validate and connect a GoDaddy Titan mailbox |
| `POST /connection/disconnect` | Remove usable credentials, keep the claim |
| `GET /connection/replace` | Staged replacement confirmation page |
| `POST /connection/replace/confirm` | Confirm the destructive reset and switch mailboxes |
| `POST /connection/replace/cancel` | Cancel the staged replacement and release the target claim |

## Reconnect the same mailbox

Connecting the same mailbox again — the verified identity or address matches
the current connection — is a reconnect, not a replacement:

- Campaigns, history and still-applicable test approvals are kept.
- Paused or interrupted work is **never** resumed automatically; the operator
  resumes the safe remainder explicitly on the sending page.
- A disconnected mailbox keeps its claim, so no other operator can connect it.

Disconnect is refused while that operator's submissions are in flight:
dispatch stops launching new requests and settles the active ones first.

## Rotate the application settings for the same mailbox

Rotating the Microsoft client secret, or moving to a new client id, for the
**same** verified mailbox is a settings change, not a replacement. The
operator does it on `/connection`:

1. Save the new Tenant ID, Client ID and Client Secret on `/connection`. The
   new registration is staged; the active registration and its credentials
   stay untouched.
2. Sign in again from `/connection` under the staged registration.

What that means:

- **No data is lost.** History, candidates, campaigns, templates and a
  still-applicable received-format approval are preserved.
- **Active submissions drain first**, so nothing is sent under half-changed
  credentials.
- **Work never resumes automatically.** Paused or interrupted tasks stay
  paused until the operator resumes the safe remainder explicitly on the
  sending page.
- The mailbox claim and the sender identity do not change.

If the sign-in under the new settings verifies a **different** mailbox, it is
not a rotation: the application requires the explicit replacement flow below
before anything changes. Provider setup and rotation are documented in
[microsoft-setup.md](microsoft-setup.md).

## Replace with a different mailbox

Connecting a different mailbox (either provider) always goes through an
explicit staged replacement. Unsupported providers are refused.

1. Submit the new mailbox on `/connection` (Microsoft sign-in or Titan
   credentials). The application validates the mailbox and **claims its
   address** for the staged target; nothing existing is changed yet.
2. `GET /connection/replace` shows the current and proposed mailbox and the
   exact scope of the reset, in two lists: what is kept and what is
   permanently deleted.
3. **Cancel** (`POST /connection/replace/cancel`) abandons the replacement and
   releases the staged claim. **Replace mailbox and delete sending data**
   (`POST /connection/replace/confirm`) runs the destructive reset and makes
   the new mailbox current.

### What the reset deletes

For the replacing operator only:

- Candidates, task snapshots and reservations
- Tasks and pending schedules
- Uploaded candidate workbooks and exclusion/duplicate reports
- Test approvals, submission attempts and manual resolutions
- Submission history and generated downloads

### What the reset keeps

- The operator account, campaigns and result-column settings
- Source-header mapping suggestions
- Immutable configuration revisions and original template DOCX files

Other operators' records are untouched.

### Consequences

- **Duplicate-prevention history is deleted.** Re-uploading those recipients
  after a new upload, a new successful test and a new explicit approval can
  send to them again. The reset does not recall emails already sent and does
  not rewrite existing backups.
- **After replacement there is no historical sender.** The new mailbox is the
  only sender; nothing is retained or synthesized from the old one. The
  operator must upload candidates again, run a new test, and approve it before
  any candidate start.

### Interrupted reset

The reset is durable. It first drains that operator's in-flight submissions,
then deletes the disclosed records and files, then switches the connection. An
interruption leaves the workspace **blocked from sending and publishing** until
recovery finishes the reset on the next start. A reset that already began
deleting can no longer be cancelled; it must complete through recovery.

## Related setup

First-time provider setup: [microsoft-setup.md](microsoft-setup.md) and
[titan-setup.md](titan-setup.md). What deletion means for backups:
[coolify-deployment.md](coolify-deployment.md).
