# Titan connection setup

Titan mailboxes need no provider registration. The operator connects their own
mailbox from `/connection`; the deployment needs only `APP_ENCRYPTION_KEY` to
store the password encrypted.

## Provider endpoint

The endpoint is fixed in the application:

| Setting | Value |
| --- | --- |
| Host | `smtpout.secureserver.net` |
| Port | `465` |
| TLS | Implicit TLS; the provider certificate and hostname are verified and the connection never downgrades to plaintext |

Titan's sending endpoint does not save a Sent copy, so the application saves one
itself over the mailbox endpoint:

| Setting | Value |
| --- | --- |
| Host | `imap.secureserver.net` |
| Port | `993` |
| TLS | Implicit TLS; certificate and hostname are verified, never downgraded |
| Credentials | The same mailbox address and encrypted password |

`TITAN_SMTP_ADDRESS` and `TITAN_IMAP_ADDRESS` override these endpoints for
simulated servers in tests only. Leave both unset in production. No operator
form field configures a host, port, folder or copy switch.

## Server configuration

`APP_ENCRYPTION_KEY` is required: it encrypts every operator's stored mailbox
credentials, including Titan passwords. Generate and back it up as described
in the configuration table in `README.md`.

## Connect the mailbox

1. Sign in and open `/connection`.
2. Choose **Titan**, then enter the **full mailbox address** and the
   mailbox's **email password**.
3. Submit. The application connects to the provider endpoint over implicit
   TLS, authenticates, and quits. **Validation sends no message**: no MAIL,
   RCPT or DATA transaction is issued, so nothing is delivered.
4. On success the connection page shows **connected** and the mailbox address.

The full address is both the SMTP username and the sending identity. The
trimmed, lowercased address becomes the operator's exclusive mailbox claim:
it is unique across all operators, including while disconnected or requiring
reconnection. Each operator has exactly one current mailbox. Connecting a
different mailbox always goes through the explicit replacement flow documented
in [mailbox-replacement.md](mailbox-replacement.md); unsupported providers are
refused.

## Sent copies

After the application durably records a definite acceptance, it makes **one**
bounded attempt to append the exact submitted message to the mailbox's existing
Sent folder. The copy keeps the submitted recipient, subject, headers and HTML,
is marked read, and uses the acceptance time as its mailbox timestamp. The
advertised `\Sent` folder is preferred; when the provider advertises none, the
existing folder named exactly `Sent` is used. The application never creates a
folder and never writes to Inbox.

Copy saving is best effort and never changes the sending outcome:

- Titan accepting the message stays **SUBMITTED** even when the copy fails, and
  the attempt details record that sending was accepted but the copy could not
  be confirmed. Sending is not paused and the message is never resent.
- An unconfirmed copy is not retried. A lost APPEND response is reported as
  unconfirmed, never as a missing copy.
- Connection validation sends no message and creates no copy.
- Only future sends are covered. Earlier messages are not backfilled, and there
  is no copy-retry button, toggle or new operator action.
- If the server stops between recording the acceptance and confirming the copy,
  restart keeps SUBMITTED and does not replay the copy. A copy missing after
  such an interruption is not repaired automatically.

## Failure and lifecycle behavior

| Operator-visible state | Cause | Fix |
| --- | --- | --- |
| **Authentication rejected** | Wrong address or password. | Correct the credentials and submit again. |
| **Configuration failure** (TLS/hostname) | Certificate or hostname verification failed. | Investigate network interception or endpoint override (`TITAN_SMTP_ADDRESS` must be unset in production). The mailbox's tasks are paused, not retried. |
| **Reconnect required** | The stored password was rejected or the credential is unreadable (missing or changed `APP_ENCRYPTION_KEY`). | Re-enter the mailbox credentials from `/connection`. |
| **Unreadable credentials / encryption key error** | `APP_ENCRYPTION_KEY` missing, not base64 for 32 bytes, or changed. Ciphertext is preserved. | Restore the original key, or reconnect the mailbox under the new key. |
| **Disconnect refused while submissions are in flight** | Submissions are in progress; dispatch stops launching new requests and settles first. | Wait for the in-flight submissions to settle, then disconnect again. |

During sending, a definitive rejection before or at DATA is a definite
failure. A lost final response after the complete message is **uncertain**:
it requires manual review and is never resent automatically. SUBMITTED means
the provider accepted the message for processing, not that it was delivered.

Reconnecting the same mailbox keeps campaigns, history and still-applicable
approvals and never auto-resumes paused work. Disconnecting keeps the mailbox
claim: no other operator can claim that address until it is replaced or
released through the replacement flow.
