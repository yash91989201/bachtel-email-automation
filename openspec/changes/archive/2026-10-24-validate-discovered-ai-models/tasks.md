# Tasks

## 1. Discovery and compatibility validation

- [x] 1.1 Remove compiled model eligibility and format mappings in `internal/ai`; keep provider format capabilities, valid-ID filtering and response-order deduplication. Update discovery tests to prove a never-before-known ID is returned as a selectable, unvalidated choice and discovery writes no connection or draft. Verify with focused `make test PACKAGES=./internal/ai TEST_FLAGS='-run TestDiscoverModels'`.
- [x] 1.2 Implement sequential preset format resolution using the existing synthetic probe and response checks, ignoring caller-supplied preset formats. Test unknown-model success on the first format, continuation to the second format, both-formats-compatible deterministic choice, one-format Xiaomi validation, normalization failure despite HTTP 200, and all-format failure without staging. Assert exact endpoint/model/key and call counts; run the focused AI tests.
- [x] 1.3 Add finite compatibility-failure classification and one overall deadline: continue only for the specified HTTP statuses or output-contract failures; stop for authentication, throttling, service/transport errors, unsafe destinations, oversized responses, cancellation, timeout and other failures. Add table-driven simulated-provider tests asserting no extra requests after a stopping failure or deadline; verify redacted errors and run focused AI tests plus `make lint` after related edits are integrated.
- [x] 1.4 Integrate the resolved configuration into staging, fingerprint/generation and credential sealing. Preserve pre-request encryption-key checks, stored-format-only repair and explicit-format-only Custom validation. Test missing encryption key causes zero requests, retired-model repair without discovery, failed repair without format switching, successful draft identity, explicit confirmation, stale/foreign draft refusal and pending-task switch prevention. Verify with `make test PACKAGES='./internal/ai ./internal/accountlock'` using temporary databases only.
- [x] 1.5 Revise the model discovery, preset format resolution, repair and usage sections in `docs/account-settings.md` alongside the backend change. Verify the documented request order, continuation/stopping behavior and exact staged identity match the backend tests and these delta scenarios.

## 2. Account model selection and validation feedback

- [x] 2.1 Update Account handlers/view models, `internal/web/views/account.templ` and `web/js/ai-setup.js` to remove the compiled fallback catalog and disabled unknown-model options. Render discovered IDs as selectable and awaiting validation; show saved/staged formats only for their exact configurations. Update web regression tests to verify unknown discovery choices, empty fresh setup, safe empty/error states, no secret echo and absence of a manual preset format control; run focused `make test PACKAGES=./internal/web TEST_FLAGS='-run TestAI'`.
- [x] 2.2 Preserve discovery input invalidation and late-response protection, saved-selection repair without fetching, and submitted non-secret IDs after failed validation. Clearly separate failed/edited form values, earlier validated proposals and the active connection. Add focused browser-controller checks for edits/late responses and web tests for failed forms, exact draft confirmation, resolved-format display and up-to-two-request disclosure; verify Custom setup still uses one explicit format.
- [x] 2.3 Format `account.templ` with `go tool templ fmt internal/web/views/account.templ` and regenerate only that file with `go tool templ generate -f internal/web/views/account.templ`. Finish the Account UI documentation in `docs/account-settings.md`; verify a local browser walkthrough with simulated providers covers fetch, unknown-model selection, validation success/failure, confirmation and saved repair without real keys or email. Record the observed outcomes.

## 3. Integrated verification

- [x] 3.1 Run affected AI, Account web and preparation regression tests after integration, confirming runtime calls retain only the stored endpoint/model/format and never negotiate another format. Verify task data, snapshots and prior evidence remain unchanged by discovery, failed validation and settings confirmation; use simulated providers and temporary databases, never local `.env` or live data.
- [x] 3.2 Run `npm ci` if applying on a fresh checkout; stage only intentional generated output before generation consistency checks, then run `make check` and `make vulncheck` once after integration. Run `make verify-converter` if supporting work touches converter, campaign, dependencies, Docker or release files. Record actual results and any blocked or skipped checks without weakening them.
- [x] 3.3 Run `openspec validate --all --strict --json --no-interactive`, inspect the final diff for only this change's work, and record requirement coverage and remaining limitations before marking implementation tasks complete. OpenSpec validation alone is not application verification.

## Workflow follow-up

- After implementation and required verification, sync and archive the completed change through the OpenSpec archive workflow.

## Verification notes

- Local simulated-provider walkthrough through the real HTTP server and a
  Chromium tab (2026-10-24): fresh preset form offered no catalog and no
  manual format control; discovery listed three IDs, all enabled; the unknown
  `brand-new-model` validated after Responses returned HTTP 400 and Chat
  Completions passed, with the resolved format and no key shown; confirmation
  activated it; same-selection repair without fetching made exactly one Chat
  Completions request and confirmed as repaired; a failing model kept the
  submitted ID as an unvalidated choice, left the confirmed connection intact,
  staged no draft and never echoed the key. Recorded provider calls: models
  GET; Responses 400; Chat Completions 200; Chat Completions (repair); models
  GET; Responses 400; Chat Completions 422.
- `go test ./internal/ai ./internal/accountlock ./internal/preparation ./internal/web`: pass.
- `npm run test:js` browser-controller checks: 5 pass.
- `make check`: exit 0 (format, generation consistency, lint, build, race suite).
- `make vulncheck`: exit 0, no vulnerabilities found.
- `make verify-converter`: not run; this change touches no converter, campaign,
  dependency, Docker or release file.
- `openspec validate --all --strict --json --no-interactive`: 1 change and 15
  specs pass.
- Requirement coverage: every modified `ai-connections` requirement (single
  confirmed connection, validation before activation, shared provider
  contract, discovery without activation) and `account-settings` requirement
  (provider-first setup, discovery input invalidation, saved repair, model
  selection, validated proposal review, unsuccessful-validation recovery,
  exact-input evidence) is exercised by the AI and web tests plus the
  walkthrough above.
- Remaining limitation: no real provider was contacted, so entitlement,
  billing and model availability stay operator-verified at setup time, and a
  passing probe proves compatibility only at validation time.
