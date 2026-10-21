# Verification

No real email was sent and no live mailbox was used. Every test used temporary
PocketBase data directories, sealed throwaway credentials, simulated Microsoft
Graph/authorization responses and local simulated TLS SMTP servers. The local
`pb_data` directory was never read, migrated or reset; no local `.env` was
loaded by any check.

Revision note: this section records the operator-managed provider
settings correction. The earlier table and evidence below remain valid only for
the earlier shared Microsoft environment configuration.

## Commands and actual results

| Command | Result |
| --- | --- |
| `make lint` (via `make check`) | passed, 0 issues (modernize fix applied during the run) |
| `make check` (default `go test -race ./...` timeout) | **failed**: `internal/sending` hit the default 10m per-package test timeout while `TestTwoOperatorWorkflow` (1,000 simulated submissions) was still progressing; no test assertion failed |
| `GOFLAGS=-timeout=30m make check` | passed in 10m07s: formatting, generated-output consistency, shared lint policy, full race suite, production build. Same tests, same flags; only the Go test deadline was raised for this slower local run |
| `make verify-converter` | passed (packaged-Pandoc converter-test stage built and its suite ran) |
| `openspec validate --all --strict --json --no-interactive` | passed: 3/3 changes, 10/10 specs |
| `make vulncheck` | **failed**: the same 4 reachable advisories in unchanged `github.com/xuri/excelize/v2` v2.11.0 (GO-2026-6684/6685/6688/6689) recorded for the prior revision; `go.mod`/`go.sum` are untouched |

`npm ci` was not re-run: this checkout already had `node_modules`, and no
dependency changed. A fresh-clone `npm ci` remains part of CI, not this local
run.

## Focused verification

- `go test ./...` passes (18 packages with tests), including the new coverage:
  - saved provider setup is staged, sealed and owner-scoped; invalid tenants
    (`common`), malformed identifiers and empty secrets are refused with field
    text that never echoes values; another operator cannot read the setup;
  - saved setup and connection credentials survive a store/app restart; a
    wrong deployment key reports `ErrCredentialsUnreadable` and leaves the
    stored ciphertext byte-for-byte unchanged;
  - a staged registration activates together with the tokens of the sign-in
    that validated it; a superseded or cancelled change leaves the active
    registration untouched; replacement activates a staged registration only on
    completion;
  - application-settings rotation for the same mailbox keeps the mailbox
    generation (approvals stay applicable), updates the settings generation,
    and refuses a stale generation without touching credentials;
  - Microsoft authorization uses the operator's saved tenant/client/secret and
    the origin-derived callback; two operators with different registrations
    exchange and refresh independently, each grant carrying only its own
    credentials; a settings edit between begin and callback fails with
    `ErrSettingsStale` and installs nothing; refresh refuses a connection whose
    settings generation was superseded and marks it reconnect-required;
  - a same-mailbox rotation waits for in-flight submissions (bounded drain) and
    reports `ErrRotationInFlight` with settings and credentials unchanged when
    they do not settle;
  - approvals are bound to owner, connection, generation and exact content: a
    record naming another operator's connection authorizes nothing, same-mailbox
    rotation preserves an applicable approval without resuming work, and a
    scheduled fire after a replacement generation bump is recorded as missed
    with no attempt or reservation;
  - the established simulated workflows still pass unchanged: 1,000-row intake,
    duplicate exclusion, independent pacing and approvals, concurrent dispatch,
    unattended schedule, restart and mailbox replacement isolation.

## Browser QA

A production binary was run against a disposable data directory with a local
Python mock of the Entra authorize/token endpoints and Graph `/me`; no live
Microsoft or Titan account was contacted and no email was sent. Chromium
confirmed:

- the connection page renders the operator's own setup form with the exact
  deployment-derived callback URI, `required` inputs, matched labels, empty
  secret field, and no `MS_*` variable names; Microsoft sign-in is unavailable
  until a registration is saved;
- a `common` tenant is refused server-side with field guidance and no echoed
  secret; a valid save confirms encryption, shows the saved Tenant/Client IDs
  and offers sign-in, with the secret never rendered or pre-filled;
- simulated sign-in connects the mailbox (`connected`, tenant-qualified
  identity), and the page leaks neither the client secret nor tokens;
- after restarting the binary on the same data directory the saved registration
  and connected mailbox are still shown;
- a second operator sees an empty setup form (no first operator identifiers),
  receives the "save your provider settings" guidance on connect, and a
  different registration cannot claim the already-connected mailbox
  (`notice=claimed`);
- overview, campaigns, campaign detail, task, sending and connection pages all
  render from the simulated fixture harness, and the connection and sending
  pages have no horizontal overflow at 375 px.

Limitations: browser QA did not exercise a real Titan SMTP validation (the
fixture would dial a live endpoint), so Titan validation coverage stays with
the simulated TLS SMTP tests; the mock signs in a single mailbox, so the
destructive replacement confirmation page was verified by automated two-session
and reset tests rather than in the browser; provider-side consent and real
delivery remain unverified by design.

## Prior revision: shared Microsoft environment settings

## Commands and actual results

| Command | Result |
| --- | --- |
| `npm ci` | passed (39 packages) |
| `make lint` | passed, 0 issues |
| `make check` | passed: formatting, generated-output consistency, shared lint policy, full race suite, production build |
| `make verify-converter` | passed (packaged-Pandoc converter-test stage built and its tests ran) |
| `openspec validate --all --strict --json --no-interactive` | passed: 3/3 changes, 10/10 specs |
| `make vulncheck` | **failed**: 4 reachable advisories in unchanged `github.com/xuri/excelize/v2` v2.11.0 (GO-2026-6684/6685/6688/6689). `go.mod`/`go.sum` are unchanged by this change; this is the same outstanding repository blocker recorded in `openspec/changes/remove-candidate-release-gate/verification.md`. No check was weakened and no dependency was changed. |

## Focused verification

- `go test ./internal/... ./cmd/server/` passes, including:
  - schema invariants on a fresh database (owner required, one current
    connection per operator, unique sender and provider identity claims,
    campaign-scoped candidate uniqueness);
  - two-operator isolation for campaigns, intake, sends, exports and downloads,
    with foreign ids returning the same refusal as absent ids;
  - Microsoft OAuth state/PKCE expiry, replay, operator binding, duplicate
    identity refusal, same-identity reconnect preserving generation, foreign
    identity staging a replacement, and per-connection refresh serialization;
  - Titan validation without a message transaction, acceptance-before-DATA
    classification, permanent/temporary rejection, auth failure, lost final
    response as uncertain, and certificate verification failure;
  - per-mailbox pacing/cooldown independence, approval binding to connection
    generation, owner-scoped pauses, and shared-storage shutdown;
  - mailbox replacement deletion matrix, write/launch blocking while a reset is
    confirmed, idempotent recovery after an interrupted reset, and no
    publication of late exports;
  - a complete simulated two-operator workflow: 1,000-row intake, duplicate
    exclusion, independent pacing and approvals, concurrent dispatch, a
    scheduled start, a restart, and a replacement that leaves the other
    operator's data intact;
  - backup/restore of a data directory preserving both operators, their
    connections and credentials, with restore review blocking dispatch.

## Browser QA

Isolated simulated fixtures, driven in Chromium:

- sign-in, overview, campaign list, campaign detail, task review snapshot and
  the sending workspace for the fixture operator;
- diagnostic test → explicit receive-format confirmation → start → 12/12
  candidates recorded SUBMITTED, then a restored task page showing completed
  progress;
- `results.xlsx` and `excluded.xlsx` downloads returned the spreadsheet content
  type, and a stored attempt HTML returned static HTML with no script;
- mailbox page rendered both providers, the connected Titan sender and status,
  empty credential inputs and no secret value; `/connection/replace` without a
  staged proposal redirected to `/connection`;
- at a 480 px viewport the mailbox and sending pages had no horizontal overflow.

Limitations: the harness has one signed-in fixture operator, so cross-account
denials and the replacement confirmation page were verified by the automated
two-session web tests rather than in the browser; no live Microsoft or Titan
account was connected, so provider-side consent and real SMTP delivery remain
unverified by design.

## Remaining blocker

`make vulncheck` fails on the pre-existing Excelize advisories. Resolving them
requires a reviewed dependency upgrade, which is outside this change. This
work does not claim a hosted CI run, a deployment, or release acceptance. Local
data written by the previous singleton schema is not migrated: the documented
`reset-local-data` path must be run explicitly before using this build locally.
