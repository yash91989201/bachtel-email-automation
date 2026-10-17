# Proposal

## Why

The existing Go/PocketBase starter has public demonstration routes but no protected operator workflow or Microsoft connection. Candidate data and background sending need a single authenticated operator and durable authorization for the fixed HR mailbox.

## What Changes

- Add one protected operator account with controlled bootstrap, login/logout, and no public registration.
- Protect application routes, uploaded files, downloads, and state-changing actions; preserve minimal unauthenticated health checks.
- Connect Microsoft directly as `hr@bachtelcorporation.com` using delegated authorization, verify the mailbox identity, refresh credentials, and expose connection/reconnection status.
- Encrypt stored Microsoft tokens using a deployment secret and keep secrets out of browser state, logs, and exports.
- Document Microsoft app registration and local/server configuration. This foundation does not expose a candidate-send action.

## Capabilities

### New Capabilities

- `operator-access`: Single-operator authentication and authorization for private application data and actions.
- `microsoft-connection`: Secure, refreshable delegated authorization for the fixed sender mailbox.

### Modified Capabilities

None.

## Impact

Implementation order: **1 of 5**; no other feature change is required. Changes 2 and 3 use operator protection; change 4 consumes the Microsoft connection.

Reuse PocketBase auth, SQLite, Go migrations, existing templ/htmx views and standard HTTP routing. Reuse the already installed `golang.org/x/oauth2` rather than implementing OAuth. Target `cmd/server/main.go`, `internal/web/`, feature packages under `internal/`, `internal/migrations/`, tests, configuration examples and setup documentation.

No Google authorization, TypeScript service, multiple operator roles, shared mailboxes, application-wide Microsoft permissions, or delivery tracking is introduced.
