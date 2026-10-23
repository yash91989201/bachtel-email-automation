# Testing

Inventory audited on 2026-10-24: **466 top-level Go tests in 92 files across
24 packages, plus 3 JavaScript tests in one file**. The Go count includes the
new fixture-isolation test. Table-driven subtests are additional cases, not
separate top-level tests. There are no benchmark or fuzz entry points.

[Complete inventory: every test name and file](test-inventory.tsv).
Regenerate an executable Go inventory with `go test ./... -list .`; list the
JavaScript tests with `rg "^test\(" scripts/*.test.mjs`. The TSV is an audit
snapshot, not another CI artifact that needs updating after every test.

| Package | Test files | Top-level tests |
| --- | ---: | ---: |
| `cmd/server` | 5 | 10 |
| `internal/accountlock` | 1 | 2 |
| `internal/ai` | 1 | 20 |
| `internal/campaign` | 6 | 33 |
| `internal/config` | 1 | 10 |
| `internal/convert` | 2 | 5 |
| `internal/database` | 1 | 1 |
| `internal/exports` | 6 | 28 |
| `internal/finalizationtest` | 4 | 14 |
| `internal/intake` | 4 | 44 |
| `internal/mailbox` | 3 | 13 |
| `internal/microsoft` | 1 | 15 |
| `internal/migrations` | 6 | 7 |
| `internal/operator` | 1 | 4 |
| `internal/preparation` | 3 | 20 |
| `internal/presentation` | 1 | 12 |
| `internal/reset` | 1 | 3 |
| `internal/retention` | 1 | 3 |
| `internal/sending` | 17 | 101 |
| `internal/testutil` | 1 | 1 |
| `internal/web` | 16 | 76 |
| `internal/web/ui/tabs` | 1 | 1 |
| `internal/web/ui/utils` | 1 | 1 |
| `internal/web/views` | 6 | 42 |
| `scripts` | 1 | 3 |

## Development loop

Batch related edits, then run the relevant tests. For example:

```sh
make test PACKAGES=./internal/intake TEST_FLAGS='-run TestRecipientCellExpansion'
make test-race PACKAGES=./internal/sending
```

`PACKAGES` and `TEST_FLAGS` apply to `test` and `test-race` only. They cannot
narrow the full race suite in `make check`. For Go-only edits with generated
assets already available, `go test ./internal/intake -run TestRecipientCellExpansion`
avoids frontend generation entirely. Include affected callers when a shared
package changes. Keep Go's normal test cache; use `-count=1` for a deliberate
fresh timing run or suspected cache issue, not routinely.

Run `make check` and `make vulncheck` once after integration. Run
`make verify-converter` for converter, campaign, dependency, Docker or release
changes. After fixing a failure, rerun that check; repeat passed checks only
when later edits affect them. Do not have multiple agents run the full suite.
Run `npm run test:tailwind` when changing the Tailwind tooling. Strict OpenSpec
validation is also required when OpenSpec artifacts change.

## What changed

- Service and HTTP fixtures copy pristine migrated database bytes into each
  test's own temporary directory. The seed is created once per test process,
  closed before copying, and never contains operators or task data. Tests
  retain their own records, files and SQLite connections, including in parallel.
- Generic fixtures use bcrypt's minimum cost while still exercising real
  hashing, correct/incorrect password checks and authentication. Production
  configuration is unchanged. Migration, operator provisioning, backup/restore
  and specialized database configuration tests still use fresh setup where
  needed; operator/migration tests retain production password defaults.
- `make check` uses one Make dependency graph, so templates and assets are
  generated once instead of regenerating during lint and again during build.
  Its complete formatting, generation, lint/vet, build and race checks remain.
- The packaged-Pandoc Docker stage runs all converter and campaign tests plus
  `TestAcceptanceCampaignAuthoringFlow` through the HTTP routes. It no longer
  repeats unrelated packages or the full vet pass already run by `make check`.
- No behavioral tests were deleted. Review found no clear redundant test that
  justified losing its acceptance coverage. Short sleeps and polling windows
  were left alone: they were not the measured bottleneck.

## Timing and limits

An isolated race-instrumented fixture took **8.36 seconds replaying migrations**,
compared with **47 milliseconds provisioning an operator** at the test-only
bcrypt cost. After migration caching, the account-lock package's first test
(including seed creation) took 8.97 seconds; its next test took **0.38 seconds**.
These are local observations, not a promise for every machine or package.

The 1,000-row export and sending acceptance scenarios remain deliberately
comprehensive; focused development runs avoid paying for them after unrelated
edits.

The initial full fresh race run was stopped after collecting package/test
samples, so there is no complete before/after suite percentage. The original
broad run included many 15–50 second tests and resource contention. Final
validation after optimization passed:

| Check | Result |
| --- | --- |
| `make fmt` | Passed |
| `make lint` | Passed, zero issues |
| `make check` | Passed in 3m 29.374s, including generation, lint/vet, build and the full race suite |
| `make vulncheck` | Passed, no reachable vulnerabilities; six module-level advisories in uncalled code |
| `make verify-converter` | Passed with pinned packaged Pandoc |
| `npm run test:tailwind` | All three passed in 21.7s |
| `git diff --check` | Passed |

Changed Go packages ran without cached results; unchanged packages used Go's
normal cache. The slowest packages were exports (178.2s), sending (169.7s),
intake (90.4s), server (78.3s), and web (77.2s), running concurrently. This was
local verification, not hosted CI. `npm ci` was not repeated because this was
an existing checkout with installed dependencies and unchanged lockfiles.
No OpenSpec artifacts changed, so strict OpenSpec validation was not required.

The opt-in `TestAcceptanceBrowserHarness` remains manual and requires
`BACHTEL_BROWSER_HARNESS`. Pandoc-dependent conversion and HTTP authoring tests
can skip locally without Pandoc; the Docker check supplies its pinned version.
The missing-converter test skips when Pandoc is available. Automated tests use
temporary data and simulated providers; none send real email.
