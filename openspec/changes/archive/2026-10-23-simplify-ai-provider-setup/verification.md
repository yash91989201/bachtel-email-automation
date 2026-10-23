# Verification

Verification for `simplify-ai-provider-setup`. All checks used temporary
PocketBase data directories and simulated provider endpoints. No live provider
key, real credential, production data or email was used.

## Browser verification

A throwaway harness (outside the tracked build, since the shipped application
never enables the private-endpoint allowance) served the real handlers over a
temporary data directory with a simulated provider exposing a fast root, a slow
root and both inference formats. Chromium exercised the integrated page.

| Check | Result |
| --- | --- |
| Preset form shows an editable documented default root, `Restore default`, `Fetch models`, a model select and a read-only inferred format; no manual API format control | Passed |
| Fetched list marks `mimo-v2.6-flash`/`mimo-v2.6-pro` as Chat Completions and disables an unknown-contract model with its reason | Passed |
| A slow first discovery response for an edited root cannot replace the models fetched for the newer root; the selection clears on input changes | Passed |
| Exact validation with no `api_format` field stores Chat Completions for the Xiaomi preset over the overridden root | Passed |
| Validated proposal reopens its provider, root and model with the key blank; confirmation installs the connection and shows the saved configuration | Passed |
| Same-configuration repair re-validates without discovery and reports "Replace API key"; the repaired key is active and never rendered | Passed |
| Mobile viewport (390×844): provider cards collapse to one column, no horizontal overflow | Passed |
| Browser console warnings/errors | None |

Temporary services and the harness were stopped and removed after the run.

## Automated coverage

| Area | Test |
| --- | --- |
| Xiaomi provider data, editable defaults, verified model formats, unknown contract refusal before any provider request | `internal/ai/ai_test.go` (`TestPresetReferenceData`, `TestPresetResolvesFormatAndPreservesSubmittedRoot`, `TestPresetRefusesUnknownModelContractBeforeContactingProvider`) |
| Retired catalog model keeps its stored format for same-configuration repair | `internal/ai/ai_test.go` (`TestRepairKeepsStoredFormatForRetiredModel`) |
| Xiaomi save and restart with the sealed credential | `internal/ai/ai_test.go` (`TestXiaomiPresetSurvivesRestart`) |
| Discovery URL derivation (OpenCode `/openai/v1` → `/v1/models`, Xiaomi append), deduplication, unsupported reason, read-only | `internal/ai/ai_test.go` (`TestDiscoverModelsDerivesURLsAndClassifies`) |
| Discovery malformed/missing envelope, oversized, auth, throttling, provider failure, empty list, custom/invalid inputs, timeout, cross-origin redirect | `internal/ai/ai_test.go` (`TestDiscoverModelsOutcomes`, `TestDiscoverModelsEmptyListAndUnsupportedInputs`, `TestDiscoverModelsCancellationAndRedirectAreRefused`) |
| Preset page structure, discovery JSON and secret-free response, custom/invalid refusal, preset format resolution without discovery, repair and reopen | `internal/web/account_test.go` (`TestAIPresetSetupPage`, `TestAIModelDiscovery`, `TestAIModelDiscoveryRejectsInvalidRequests`, `TestAIPresetValidationResolvesFormatWithoutDiscovery`) |
| Existing owner isolation, validation outcomes, switch lock, stale proposal and endpoint policy | `internal/ai/ai_test.go`, `internal/web/account_test.go` |

## Commands and results

- `go build ./...` — passed.
- `go test ./internal/ai`, `go test ./internal/web -run 'TestAI|TestAccount'` — passed
  during implementation.
- `make check` — passed: formatting, generated-output consistency,
  golangci-lint (including vet), race tests over all packages, production
  binary build.
- `make vulncheck` — passed; no called vulnerabilities. Six module-level
  advisories are not reachable from this code.
- `openspec validate --specs` and
  `openspec validate --all --strict --json --no-interactive` — 15/15 specs and
  3/3 changes passed.
- `make verify-converter` — not run: this change touches no converter,
  campaign, dependency, Docker or release packaging path.

## Limitations

- No live provider key or real mailbox credential was used. Discovery and
  validation ran against simulated endpoints; documented provider contracts
  and current model availability remain deployment prerequisites for the
  operator's own Console credentials.
- The browser run used a temporary harness that enables the private-endpoint
  allowance for loopback simulation. The shipped application keeps HTTPS-only
  public destinations and was not otherwise modified for verification.
- Local checks are not a hosted CI run, deployment or release acceptance.
