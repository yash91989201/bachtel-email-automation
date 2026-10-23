# Tasks

Implementation is present and under final verification. Provider presets,
server-resolved formats, bounded discovery, the account setup workflow and
documentation are implemented; see `verification.md` for exercised evidence.

## 1. Provider contracts and discovery

- [x] 1.1 Add Xiaomi, editable provider defaults and authoritative model-format resolution; verify focused AI tests cover both formats, unknown protocols, endpoint preservation and same-configuration repair.
- [x] 1.2 Implement bounded credential-safe discovery with simulated tests for Go/Xiaomi URL derivation, malformed/oversized/empty lists, auth failures, cancellation and unsafe redirects; verify discovery never activates settings.
- [x] 1.3 Extend any persisted provider constraints without rewriting saved settings; verify Xiaomi save/restart and existing connection tests.

## 2. Account setup workflow

- [x] 2.1 Add protected discovery route and provider-specific setup with editable defaults, fetched model choice, automatic format, stale-response suppression and custom manual controls; verify handler and rendered-view tests.
- [x] 2.2 Preserve saved non-secret configuration and discovery-independent key repair; verify owner isolation, secret-free responses, overridden endpoints and pending-task lock regressions.
- [x] 2.3 Update provider setup documentation with testing/production credential boundaries, supported protocols and discovery limitations; verify documentation against implemented forms and routes.

## 3. Integration and specifications

- [x] 3.1 Verify the integrated setup in a browser using temporary data and simulated provider endpoints, including editable inputs, stale discovery and mobile layout; record results and stop temporary services.
- [x] 3.2 Synchronize ai-connections and account-settings main specs and verify strict OpenSpec validation.
- [x] 3.3 Run required formatting, generated consistency, lint, race tests, build and vulnerability checks; record actual results and any remaining limitations.

## Workflow follow-up

- Archive this change after review when requested.
