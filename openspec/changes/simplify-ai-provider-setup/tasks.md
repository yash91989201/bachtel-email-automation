# Tasks

Implementation has not been retained. The code edits from the initial apply attempt were reverted at the operator's request; the requirements below remain planned work.

## 1. Provider contracts and discovery

- [ ] 1.1 Add Xiaomi, editable provider defaults and authoritative model-format resolution; verify focused AI tests cover both formats, unknown protocols, endpoint preservation and same-configuration repair.
- [ ] 1.2 Implement bounded credential-safe discovery with simulated tests for Go/Xiaomi URL derivation, malformed/oversized/empty lists, auth failures, cancellation and unsafe redirects; verify discovery never activates settings.
- [ ] 1.3 Extend any persisted provider constraints without rewriting saved settings; verify Xiaomi save/restart and existing connection tests.

## 2. Account setup workflow

- [ ] 2.1 Add protected discovery route and provider-specific setup with editable defaults, fetched model choice, automatic format, stale-response suppression and custom manual controls; verify handler and rendered-view tests.
- [ ] 2.2 Preserve saved non-secret configuration and discovery-independent key repair; verify owner isolation, secret-free responses, overridden endpoints and pending-task lock regressions.
- [ ] 2.3 Update provider setup documentation with testing/production credential boundaries, supported protocols and discovery limitations; verify documentation against implemented forms and routes.

## 3. Integration and specifications

- [ ] 3.1 Verify the integrated setup in a browser using temporary data and simulated provider endpoints, including editable inputs, stale discovery and mobile layout; record results and stop temporary services.
- [ ] 3.2 Synchronize ai-connections and account-settings main specs and verify strict OpenSpec validation.
- [ ] 3.3 Run required formatting, generated consistency, lint, race tests, build and vulnerability checks; record actual results and any remaining limitations.

## Workflow follow-up

- Archive this change after review when requested.
