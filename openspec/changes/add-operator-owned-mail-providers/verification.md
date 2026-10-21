# Verification

No real email was sent and no live mailbox was used. Every test used temporary
PocketBase data directories, sealed throwaway credentials, simulated Microsoft
Graph/authorization responses and local simulated TLS SMTP servers. The local
`pb_data` directory was never read, migrated or reset; no local `.env` was
loaded by any check.

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
