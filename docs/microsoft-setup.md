# Microsoft connection setup

First-time setup for the confidential Microsoft connection: register the
application in Entra ID, configure the server, provision each operator, and
have each operator connect their own Microsoft 365 mailbox in the configured
tenant.

## 1. Register the application in Entra ID

1. Open the Entra ID admin portal (**Microsoft Entra ID > App registrations**)
   and choose **New registration**.
2. Name it (for example `bachtel-email-automation`).
3. Supported account types: **Accounts in this organizational directory only**
   (single tenant). Note the **Directory (tenant) ID** — this is
   `MS_TENANT_ID`.
4. Platform: **Web**. Do not use the mobile/SPA platforms: the flow is a
   confidential server-side web client.
5. Redirect URI: exactly `<APP_ORIGIN>/connection/callback`, for example
   `https://mail.example.com/connection/callback`. It must match
   `APP_ORIGIN` plus `/connection/callback` character for character — same
   scheme (`https`), host, port, and path. An `http` callback is rejected when
   `APP_ORIGIN` is `https`.

Record the **Application (client) ID** — this is `MS_CLIENT_ID`.

## 2. Delegated permissions

Under **API permissions > Add a permission > Microsoft Graph > Delegated
permissions**, add exactly:

| Permission | Why |
|---|---|
| `Mail.Send` | Send mail as the signed-in user (the only send path used) |
| `User.Read` | Read the signed-in user's profile to verify the mailbox identity via `/me` |
| `offline_access` | Obtain a refresh token so the connection survives access-token expiry |

Do **not** add:

- **Application**-level `Mail.Send` (daemon-style send is not used; only
  delegated send is requested).
- `Mail.Read` or any other mailbox-reading permission — mail is sent, never
  read.
- Shared-mailbox or `Mail.Send.Shared` delegation — each operator signs in
  with their own mailbox; there is no alias or shared-mailbox send-as.
- Any Google or non-Microsoft permission — none are used.

## 3. Client secret

1. **Certificates & secrets > New client secret**. Record the secret
   **Value** immediately — it is shown once. This is `MS_CLIENT_SECRET`.
2. Set an expiry your operations team can rotate before it lapses. When
   rotation happens, update `MS_CLIENT_SECRET` and restart the server; a
   stale secret surfaces as `reconnect_required` once refresh is rejected.
3. Never commit the secret. It lives only in the server environment.

## 4. Admin consent and Conditional Access

If the tenant requires admin consent for delegated scopes, a tenant
administrator must grant it (**API permissions > Grant admin consent**).
Microsoft's tenant policy decides this; the application cannot bypass
admin-consent or Conditional Access requirements. Conditional Access policies
(for example requiring multi-factor authentication or a compliant device for
`login.microsoftonline.com`) apply to this sign-in like any other — if policy
blocks or later revokes the session, the connection moves to
`reconnect_required` and requires an interactive reconnect that satisfies the
policy. Do not weaken tenant policy to make the connection work.

## 5. Server configuration

Set the environment variables documented in `.env.example` (see the
configuration table in `README.md`):

```sh
# Generate the credential-encryption key; back it up separately from the
# database. Losing it keeps the stored ciphertext but forces a reconnect.
openssl rand -base64 32   # -> APP_ENCRYPTION_KEY

export APP_ORIGIN="https://mail.example.com"
export APP_ENCRYPTION_KEY="..."     # SECRET
export MS_TENANT_ID="00000000-0000-0000-0000-000000000000"
export MS_CLIENT_ID="00000000-0000-0000-0000-000000000000"
export MS_CLIENT_SECRET="..."       # SECRET
# MS_REDIRECT_URL defaults to APP_ORIGIN + /connection/callback; set it only
# to override that default.
```

`APP_ENCRYPTION_KEY` is distinct from PocketBase's own `--encryptionEnv`
settings key and from `MS_CLIENT_SECRET`; each protects different material.
With Docker Compose, put these in `.env` — `compose.yaml` passes them into the
container. `make dev` reads the same `.env` (copy `.env.example` to `.env`); a
plain `go run` needs the variables exported in the shell instead.

Missing or incorrect settings never crash the server: the connection page
names the missing variables, or (for a wrong/missing encryption key) reports
an actionable credential error. While the connection is unusable, no email
can be sent and the stored ciphertext is never overwritten or exposed. Missing
Microsoft settings disable only Microsoft readiness; GoDaddy Titan mailboxes
still connect (see [titan-setup.md](titan-setup.md)). Reconnecting the same
mailbox or replacing it with a different one is documented in
[mailbox-replacement.md](mailbox-replacement.md).

## 6. Provision the operator

One-time command, run with the **same `--dir` as the server** so it writes to
the same database:

```sh
./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
# or: go run ./cmd/server provision-operator --email operator@example.com --dir=./pb_data
```

The password is read from the terminal without echo, or piped on stdin:

```sh
echo "$PASSWORD" | ./bin/bachtel provision-operator --email operator@example.com --dir=./pb_data
```

No credentials are seeded anywhere. Reusing an existing normalized email is
refused with `an operator with that email already exists` and never changes
that account's password.

The PocketBase superuser (dashboard `/_/`, created from the one-time
installer link on a fresh database) is an **administrative** account for
migrations, settings, and backups. The operator is the **application-level**
account for day-to-day use. The `operators` collection is locked to
administrators (record rules and auth rule), so it is unreachable through the
public PocketBase API; sign-in happens only through `/login`, which adds rate
limiting and cookie protection.

## 7. Sign in and connect the mailbox

1. Start the server and open `<APP_ORIGIN>/login`.
2. Sign in with the operator credentials.
3. Open **`/connection`** and choose **Connect**. You are redirected to
   Microsoft; sign in with **that operator's own mailbox address** in the
   configured tenant.
4. Approve the delegated permissions. You are redirected back to
   `/connection/callback`.
5. The connection page now shows **connected** and the verified mailbox
   address. The identity is verified against Microsoft's `/me` response
   (stable account id and verified address), and that address becomes the
   operator's exclusive mailbox claim. Every operator repeats this flow for
   their own mailbox; a mailbox already claimed by any other operator (even
   while disconnected) is refused.

## 8. Failure table

| Operator-visible state | Cause | Fix |
|---|---|---|
| **disconnected** | Never connected, or the operator disconnected the mailbox. | Connect from `/connection`. |
| **connected** | Working. | None. |
| **reconnect_required** | Microsoft rejected refresh (revoked consent, rotated/expired client secret, Conditional Access change), or authorization definitively failed. | Rotate/verify `MS_CLIENT_SECRET` and tenant policy if needed, then reconnect from `/connection`. |
| **missing configuration** (names `MS_TENANT_ID`, `MS_CLIENT_ID`, `MS_CLIENT_SECRET`, ...) | Required environment variables absent or empty. | Set the named variables (`.env` for Compose), restart, reconnect. |
| **sign-in attempt expired, already used, or cleared by a restart** | Sign-in attempts last 10 minutes, are single-use even after a failed token exchange, and are held in server memory. Restarting after an environment change clears pending attempts. | Open `/connection` and click **Connect** or **Reconnect** for a fresh sign-in. Do not refresh or reuse an old `/connection/callback?...` URL. Keep the server running until sign-in finishes. |
| **unreadable credentials / encryption key error** | `APP_ENCRYPTION_KEY` missing or not base64 for 32 bytes; or the key changed. Ciphertext is preserved. | Restore the original key (`APP_ENCRYPTION_KEY`), or reconnect the mailbox under the new key. |
| **disconnect refused while submissions are in flight** | Submissions are in progress; disconnect is blocked so active work can settle. | Wait for the in-flight submissions to settle, then disconnect again. |

## 9. Session and lifecycle semantics

- **Reconnect or disconnect never resumes interrupted candidate tasks.**
  Tasks interrupted by a connection change stay interrupted; history and
  attempts are unchanged.
- **Sign-out ends only the browser session.** It does not cancel any
  server-side work already running.
- **Disconnect is refused while submissions are in flight**; dispatch stops
  launching new requests and settles or classifies the active ones first.
