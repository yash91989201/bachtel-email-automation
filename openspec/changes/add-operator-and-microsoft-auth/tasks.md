# Tasks

Implement first. No prerequisite feature change. Keep real candidate sending unavailable until changes 4 and 5 are complete.

## 1. Operator model and provisioning

- [ ] 1.1 Add a dedicated single-operator auth collection and locked-down API rules through a new migration; verify fresh migration and a rejected second-account/public-create attempt with temporary PocketBase data.
- [ ] 1.2 Add a protected one-time provisioning command using PocketBase password handling; verify no default credentials are seeded and repeated provisioning cannot add another operator.
- [ ] 1.3 Document operator provisioning and administrator-versus-operator access; verify the documented setup works against a fresh temporary data directory.

## 2. Protected browser access

- [ ] 2.1 Add PocketBase-backed login/logout and server-validated secure cookies using shared middleware in internal/web; verify successful login, invalid login, expiry and logout with HTTP integration checks.
- [ ] 2.2 Apply authorization, built-in origin/CSRF protection and login rate limiting; verify anonymous/forged requests cannot access private routes or mutate state while the minimal health endpoint remains available.
- [ ] 2.3 Add login/connection navigation with existing templ/htmx components and document session behavior; regenerate committed templ Go sources and verify rendered pages contain no browser-stored tokens or private health details.

## 3. Microsoft authorization

- [ ] 3.1 Configure golang.org/x/oauth2 for the tenant-specific confidential authorization-code flow and PKCE; verify expected scopes, callback origin and state with a simulated authorization server.
- [ ] 3.2 Add operator-bound, expiring, single-use authorization state and callback handling; verify mismatched, expired and replayed callbacks cannot install credentials.
- [ ] 3.3 Verify /me identity and accept only hr@bachtelcorporation.com; use simulated responses to verify that a wrong account neither enables sending nor overwrites a valid connection.
- [ ] 3.4 Document app registration, delegated Mail.Send/offline_access/User.Read consent, redirect URI and client settings; verify the configuration example has no real secrets and matches the callback routes.

## 4. Confidential connection lifecycle

- [ ] 4.1 Encrypt versioned token material with standard authenticated encryption and a deployment key; verify restart recovery, wrong/missing-key failure and ciphertext preservation using temporary persistent data.
- [ ] 4.2 Serialize refresh and persist rotated credentials; verify concurrent refresh callers, rejected refresh and failed token persistence with simulated services.
- [ ] 4.3 Add connected/reconnect-required/disconnected UI and guarded disconnect/reconnect actions with a later-dispatch integration point; verify protected access, safe error messages and no secret values in HTML/logs.
- [ ] 4.4 Document secret generation, separate key backup and reconnection behavior; verify the supplied local/server configuration examples can be validated without exposing their values.

## 5. Integration acceptance

- [ ] 5.1 Run make test and the auth/OAuth integration checks against temporary data; verify restart retains the connection, private record/file boundaries remain closed and no candidate-send endpoint has been introduced.
