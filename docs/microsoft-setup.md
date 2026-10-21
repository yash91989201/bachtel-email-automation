# Microsoft connection setup

Each operator connects their own Microsoft 365 mailbox. The application
registration belongs to the operator, not to the deployment: every operator
creates — or is given — an Entra app registration in their own tenant, saves
its Tenant ID, Client ID and Client Secret on `/connection`, and then signs in.
The deployment supplies only the public origin and the credential-encryption
key; it holds no Microsoft credentials.

The callback URI is fixed and derived from the deployment origin: exactly
`APP_ORIGIN` + `/connection/callback`, for example
`https://mail.example.com/connection/callback`. The connection page displays
the exact URI to register.

## 1. Register the application in Entra ID

The provider or tenant administrator creates the app registration in the
operator's own tenant:

1. Open the Entra ID admin portal (**Microsoft Entra ID > App registrations**)
   and choose **New registration**.
2. Name it (for example `bachtel-email-automation`).
3. Supported account types: **Accounts in this organizational directory only**
   (single tenant) in the operator's own tenant. Note the **Directory (tenant)
   ID** — the operator saves this as **Tenant ID** on `/connection`.
4. Platform: **Web**. Do not use the mobile/SPA platforms: the flow is a
   confidential server-side web client.
5. Redirect URI: exactly `<APP_ORIGIN>/connection/callback`, for example
   `https://mail.example.com/connection/callback`. It must match
   `APP_ORIGIN` plus `/connection/callback` character for character — same
   scheme (`https`), host, port, and path. An `http` callback is rejected when
   `APP_ORIGIN` is `https`.

Record the **Application (client) ID** — the operator saves this as **Client
ID** on `/connection`.

Each operator may use a separate single-tenant registration in their own
tenant. There is no deployment-wide tenant restriction: one operator's tenant
never limits another operator's, and no shared registration is required.

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
   **Value** immediately — it is shown once. The operator saves it as
   **Client Secret** on `/connection`.
2. Set an expiry your operations team can rotate before it lapses. When
   rotation happens, save the new secret on `/connection` and sign in again;
   see [Rotate the application settings](#8-rotate-the-application-settings-for-the-same-mailbox).
   A stale secret surfaces as `reconnect_required` once refresh is rejected.
3. Never commit the secret. It is stored encrypted under the deployment's
   `APP_ENCRYPTION_KEY`, is never displayed again, and is never pre-filled.

## 4. Admin consent and Conditional Access

If the tenant requires admin consent for delegated scopes, a tenant
administrator must grant it (**API permissions > Grant admin consent**).
Microsoft's tenant policy decides this; the application cannot bypass
admin-consent or Conditional Access requirements. Conditional Access policies
(for example requiring multi-factor authentication or a compliant device for
`login.microsoftonline.com`) apply to this sign-in like any other — if policy
blocks or later revokes the session, the connection moves to
`reconnect_required` and requires an interactive reconnect that satisfies the
policy. Do not weaken tenant policy to make the connection work. Consent and
policy are per tenant, so one operator's policy never blocks another
operator's sign-in.

## 5. Save the settings on the connection page

1. Sign in at `<APP_ORIGIN>/login` with the operator account.
2. Open **`/connection`**, choose **Microsoft 365**, then use its setup form.
3. Enter the **Tenant ID** — the directory UUID of the operator's tenant.
   Shared aliases such as `common` or `organizations` are rejected: the tenant
   must be explicit, so the verified identity stays tenant-qualified.
4. Enter the **Client ID** — the application UUID.
5. Enter the **Client Secret** — write-only. It is never shown again and
   never pre-filled into the form.
6. Save. The page confirms the saved registration and displays the callback
   URI to register in Entra ID.

The Tenant ID and Client ID must both be UUIDs and the Client Secret must not
be empty; the form refuses anything else before it is stored.

### Staged registration

Saving stages the registration. The staged settings become **active only after
a successful sign-in under them**. Until then the previously active
registration and its credentials stay untouched, and a failed sign-in changes
nothing. A sign-in attempt that started before the save cannot install tokens
under the changed registration either: its callback is refused and the
operator starts a fresh attempt.

The secret is verified by Microsoft at sign-in, not locally. A rejected secret
therefore appears as a failed sign-in, and the previous registration remains
active.

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
3. Open **`/connection`**, choose **Microsoft 365**, save your registration
   settings if needed, then choose **Continue with Microsoft 365**. You are
   redirected to Microsoft; sign in with **that operator's own mailbox address** in their
   own tenant.
4. Approve the delegated permissions. You are redirected back to
   `/connection/callback`.
5. The connection page now shows **connected** and the verified mailbox
   address. The identity is verified against Microsoft's `/me` response
   (stable account id and verified address) and is qualified by the operator's
   tenant, and that address becomes the operator's exclusive mailbox claim.
   Every operator repeats this flow for their own mailbox; a mailbox already
   claimed by any other operator (even while disconnected) is refused.

## 8. Rotate the application settings for the same mailbox

Rotating the client secret, or moving to a new client id, for the **same**
verified mailbox:

1. Create the new client secret in Entra ID, or record the new application
   id.
2. Save the new **Tenant ID**, **Client ID** and **Client Secret** on
   `/connection`. The staged registration waits; the active registration and
   its credentials are untouched.
3. Sign in again from `/connection` under the staged registration.

The result:

- The new registration becomes active only with that successful sign-in.
- Active submissions drain first, so nothing is sent under half-changed
  credentials.
- History, candidates, campaigns, templates and a still-applicable
  received-format approval are preserved.
- Work never resumes automatically: paused or interrupted tasks stay paused
  until the operator resumes the safe remainder explicitly on the sending
  page.
- A sign-in that verifies a **different** mailbox is not a rotation. It goes
  through the explicit replacement flow in
  [mailbox-replacement.md](mailbox-replacement.md).

## 9. Failure table

| Operator-visible state | Cause | Fix |
|---|---|---|
| **no saved Microsoft settings** | The operator has not saved a Tenant ID, Client ID and Client Secret on `/connection`. | Enter the three values on `/connection`, save, then sign in. |
| **invalid settings** | The Tenant ID or Client ID is not a UUID (shared aliases such as `common` or `organizations` are rejected), or the Client Secret is empty. | Correct the field on `/connection` and save again. |
| **settings changed / stale** | The registration changed after the sign-in attempt started, so the callback cannot install tokens under it. | Start a fresh sign-in from `/connection`. Nothing was changed. |
| **disconnected** | Never connected, or the operator disconnected the mailbox. | Connect from `/connection`. |
| **connected** | Working. | None. |
| **reconnect_required** | Microsoft rejected refresh (revoked consent, rotated or expired client secret, Conditional Access change), or authorization definitively failed. | Save fresh settings if the secret expired, then sign in again from `/connection`. |
| **sign-in attempt expired, already used, or cleared by a restart** | Sign-in attempts last 10 minutes, are single-use even after a failed token exchange, and are held in server memory. Restarting the server clears pending attempts. | Open `/connection` and click **Connect** or **Reconnect** for a fresh sign-in. Do not refresh or reuse an old `/connection/callback?...` URL. Keep the server running until sign-in finishes. |
| **unreadable credentials / encryption key error** | `APP_ENCRYPTION_KEY` missing or not base64 for 32 bytes; or the key changed. Ciphertext is preserved. | Restore the original key (`APP_ENCRYPTION_KEY`), or reconnect the mailbox under the new key. |
| **disconnect refused while submissions are in flight** | Submissions are in progress; disconnect is blocked so active work can settle. | Wait for the in-flight submissions to settle, then disconnect again. |

## 10. Session and lifecycle semantics

- **Reconnect, rotation or disconnect never resumes interrupted candidate
  tasks.** Tasks interrupted by a connection change stay interrupted; history
  and attempts are unchanged.
- **Sign-out ends only the browser session.** It does not cancel any
  server-side work already running.
- **Disconnect is refused while submissions are in flight**; dispatch stops
  launching new requests and settles or classifies the active ones first.
- **Settings are private to their owner.** Another operator cannot read or use
  them, and one operator's missing, invalid or expired registration never
  disables another operator's Microsoft or Titan connection.
