# Verification

Verification for `account-connection-settings`. All checks use temporary
PocketBase data directories and simulated endpoints. No live provider key,
mailbox credential, production data or real email was used.

## Automated coverage added

| Area | Test |
| --- | --- |
| AI connection sealing, restart, missing/wrong key, no secret disclosure | `internal/ai/ai_test.go` (`TestRestartKeepsConnectionAndRevealsNoSecret`, `TestMissingEncryptionKeyBlocksSetup`) |
| Responses and Chat Completions adapters, malformed output, timeout, auth, unknown model, throttling, size bound | `internal/ai/ai_test.go` (`TestValidateAndConfirmBothFormats`, `TestValidationOutcomes`) |
| Endpoint policy, private/metadata targets, cross-origin redirect never receives credentials | `internal/ai/ai_test.go` (`TestUnsafeEndpointsAreRefusedBeforeSendingSecrets`, `TestCrossOriginRedirectNeverReceivesCredentials`) |
| Switch lock, same-configuration key repair, stale proposal, ownership | `internal/ai/ai_test.go` (`TestSwitchLockAndSameConfigurationRepair`, `TestStaleProposalNeverOverwritesNewerConfiguration`, `TestOwnershipIsolation`) |
| Task appearing after validation blocks activation | `internal/ai/ai_test.go` (`TestTaskAppearingDuringValidationBlocksActivation`) |
| Pending predicate across campaigns/states, closed exclusion, outstanding queued work, settled release | `internal/accountlock/lock_test.go` |
| Terminal Close: ready and active tasks, idempotence, foreign refusal, schedule cancellation, settlement, retained evidence | `internal/sending/closure_test.go` |
| Closed/closing status, controls, lifecycle, attention predicate | `internal/presentation/presentation_test.go` |
| Mailbox switch/replacement/disconnect guard, same-mailbox repair allowed | `internal/mailbox/lock_test.go` |
| Account sections, AI validate/confirm over both protocols, lock warning, repair, owner isolation, disconnect refusal, named blockers | `internal/web/account_test.go` |
| Close task confirmation, closed state, foreign refusal, retained routes | `internal/web/close_test.go` |
| Account view structure and locked mailbox repair-only surface | `internal/web/views/workspace_test.go` |

## Commands and results

- `npm ci` — passed.
- `make check` — passed (format, generation consistency, golangci-lint, race
  tests, production build).
- `make vulncheck` — passed; no vulnerabilities found in called code
  (0 called, 6 module-level advisories not reachable).
- `make verify-converter` — passed (packaged Pandoc converter image test).
- `openspec validate --all --strict --json --no-interactive` — 15/15 items
  passed.

## Limitations and boundaries

- OpenCode Go deployment access and current model availability cannot be
  verified without the operator's own credentials. The adapter, headers,
  endpoint families and model/format split follow the documentation checked on
  2026-10-23; the docs record this as a deployment prerequisite.
- Task preparation/publication is the next change. This change stores the
  confirmed configuration and generation but does not run normalization, so
  "in-flight AI repair settlement" has no runtime consumer yet.
- Keyboard and responsive behavior is exercised through the shared component
  contracts and the structural view tests; no browser automation was run.
- Local checks are not a hosted CI run, deployment or release acceptance.
