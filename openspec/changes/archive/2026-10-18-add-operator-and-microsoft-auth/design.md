# Design

## Context

See proposal.md for motivation. The starter uses PocketBase v0.40.5 in-process with SQLite, standard HTTP handlers adapted into its router, templ views and a non-root Docker image. Current custom routes and demonstration tests are public. No auth collection or Microsoft connection exists. This is change 1; implement before the other four changes.

## Goals / Non-Goals

**Goals:** Reuse existing account, migration and HTTP facilities; provide one authorization boundary and a reusable Microsoft token source.

**Non-Goals:** A second identity service, custom password hashing/session protocol, Microsoft mailbox reading, sender switching, or any candidate-send route.

## Decisions

### Reuse PocketBase operator authentication

Create one dedicated operator auth collection with server-enforced single-account provisioning and locked-down public collection rules. Provision through a documented one-time administrative command or protected bootstrap, without seeding credentials. Use PocketBase password verification and token validation, carried in an HttpOnly cookie for the custom UI. Keep authentication and authorization in shared HTTP middleware, including file and progress endpoints. Existing server-side record access bypasses collection API rules, so handlers must enforce the same boundary explicitly.

Use built-in HTTP/framework origin/CSRF protection and secure cookie settings rather than a bespoke CSRF protocol. Login is rate limited. A public health route is the only required private-app exception; operator access and PocketBase administrator access are separate.

Alternative: public PocketBase sign-up or Microsoft sign-in as the app login. Both create unnecessary account or permission coupling.

### Standard OAuth library and minimal Graph identity check

Promote the already installed golang.org/x/oauth2 dependency to direct use. Use a tenant-specific confidential web authorization-code flow with library PKCE helpers, short-lived single-use state bound to the operator session, an exact configured HTTPS callback and server-side client credentials.

Request delegated Mail.Send, offline_access and User.Read; verify /me using its stable account ID and verified mailbox address. Do not trust an entered address or an unverified decoded token claim. Accept only hr@bachtelcorporation.com; do not install a different mailbox's credentials over a valid connection. No Mail.Read or application Mail.Send permission is needed.

Persist connection account/tenant metadata, encrypted token material, expiry and reconnect status. Serialize refresh operations and durably save rotated refresh credentials before treating a refresh as complete. Expose a connection service to later sending code, without exposing raw tokens to routes or templates. Use net/http for the small Graph surface rather than the full generated Graph SDK.

### Standard encryption and deployment secret

Encrypt Microsoft credentials with Go's standard authenticated encryption and a randomly generated deployment key supplied through environment/secret configuration. Store a ciphertext version and nonce; never invent a cryptographic primitive. Persist files under the private PocketBase data directory with restrictive access. Fail closed without destroying ciphertext on a missing/wrong key.

Document the Microsoft client secret and token-encryption key as separate settings. Logs contain safe codes and correlation IDs, never tokens or authorization headers.

### Connection and lifecycle contract

Connection states are disconnected, connected and reconnect_required; readiness is checked by change 4 before submissions. Browser logout affects operator access only. Changing/disconnecting a mailbox must first stop new submissions and drain current work; implement the guard now and bind it to dispatch when change 4 arrives.

## Risks / Trade-offs

- Tenant consent policy or Conditional Access can require administrator action -> show actionable connection errors; do not promise permanent unattended authorization.
- Cookie authentication can expose public record APIs if only pages are protected -> test custom routes, PocketBase collection rules and direct file requests together.
- Lost encryption key makes tokens unreadable -> retain ciphertext, document separate key backup and allow explicit reconnection.
- A valid connection does not prove sending or rendering works -> change 4 still requires an actual operator-approved test email.

## Migration Plan

Add forward/backward migrations for the operator collection and connection metadata without modifying the initial settings migration. Update route integration tests, setup instructions and configuration examples. Verify fresh setup and restart with a temporary persistent directory. Rollback stops the app, restores its pre-migration backup and retains secrets separately; never silently delete connection data on startup.

## References

- [Microsoft delegated sendMail permissions and acceptance semantics](https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0)
- [Microsoft scopes and offline access](https://learn.microsoft.com/en-us/entra/identity-platform/scopes-oidc)
