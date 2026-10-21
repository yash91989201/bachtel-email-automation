# GoDaddy Titan connection setup

Titan mailboxes need no provider registration and no Microsoft settings. The
operator connects their own mailbox from `/connection`; the deployment needs
only `APP_ENCRYPTION_KEY` to store the password encrypted.

## Provider endpoint

The endpoint is fixed in the application:

| Setting | Value |
| --- | --- |
| Host | `smtpout.secureserver.net` |
| Port | `465` |
| TLS | Implicit TLS; the provider certificate and hostname are verified and the connection never downgrades to plaintext |

`TITAN_SMTP_ADDRESS` overrides the endpoint for simulated servers in tests
only. Leave it unset in production.

## Server configuration

`APP_ENCRYPTION_KEY` is required: it encrypts every operator's stored mailbox
credentials, including Titan passwords. Generate and back it up as described
in the configuration table in `README.md`. Missing Microsoft settings
(`MS_TENANT_ID`, `MS_CLIENT_ID`, `MS_CLIENT_SECRET`, `MS_REDIRECT_URL`) do not
affect Titan; they disable only Microsoft readiness.

## Connect the mailbox

1. Sign in and open `/connection`.
2. Under **GoDaddy Titan**, enter the **full mailbox address** and the
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
