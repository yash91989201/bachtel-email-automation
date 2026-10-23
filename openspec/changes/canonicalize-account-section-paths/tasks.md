# Tasks

All items below are implementation work for a future apply request. This planning change does not execute them.

## 1. Canonical Account GET routes

- [x] 1.1 Add protected `/account/mailbox`, retain `/account/ai`, and remove exact GET `/account` and `/connection` landing entries, leaving `/account/connections` unserved; verify all three unused URLs return 404 without redirects, plus direct section visits, reload, selected states and authentication in focused account HTTP tests.
- [x] 1.2 Keep supported non-secret provider, chooser and notice state on `/account/mailbox`; verify valid/invalid values and provider-setup anchors, and verify old landing queries still return 404 without provider calls or credential propagation.

## 2. Navigation and operation returns

- [x] 2.1 Update Mailbox navigation, global/home Account links, provider choices, repair links and task mailbox remedies to `/account/mailbox`, retaining `/account/ai` setup links; update view/helper tests and verify the final design's route inventory against generated links.
- [x] 2.2 Update ordinary mailbox success/failure return destinations while leaving callback, POST and replacement endpoints supported; verify settings save, connect, callback success/error/replacement-required, disconnect and replacement confirm/cancel routes through focused existing tests with simulated providers, including no OAuth code/state in landing URLs.
- [x] 2.3 Preserve Mailbox rejected-form feedback, non-secret drafts, named task locks and same-mailbox repair access; verify rejected forms select Mailbox and stale/locked actions cannot switch configuration or disclose secrets.
- [x] 2.4 Pin AI section routing as a regression boundary: verify model-list responses remain POST-only, validation/confirmation feedback and redirects stay in AI, Account navigation causes no provider calls, and existing lock/credential rules remain enforced; do not expand this into provider-feature implementation.
- [x] 2.5 Format and regenerate only changed templates and audit remaining `/account` and `/connection` landing references; verify only intentional 404 regression tests, current-state documentation and retained operational endpoints remain, with no change to the configured OAuth callback URI.

## 3. Integrated verification

- [x] 3.1 Run focused account/mailbox/view tests and `make lint` during implementation; use isolated browser fixtures to check direct Account paths, unused landing 404s, Back/Forward, provider disclosure anchors, errors and keyboard-selected navigation, recording actual results without live OAuth, email, local `.env` data or external AI calls. Result: `go test ./internal/web/ ./internal/web/views/` passed; `make lint` reported 0 issues after simplifying the nav test helper. Browser run on an isolated temporary data directory confirmed `/account/mailbox` and `/account/ai` selected states, reload, Back/Forward, `/account`, `/account/connections` and `/connection` 404s with old queries, five Tab presses to the Account link then Enter, and `#provider-setup` scrolled into view; the page requested only its own origin.
- [x] 3.2 After integration, run `npm ci` on a fresh checkout, stage only intentional generated output, then run `make check`, `make vulncheck` and `openspec validate --all --strict --json --no-interactive`; record results and skipped coverage. Result: `npm ci` added 39 packages; `make check` passed (format, generation consistency, lint, race tests, production build); `make vulncheck` found no called vulnerabilities; `openspec validate --all --strict` passed 2/2 changes and 15/15 specs. `make verify-converter` skipped: this change does not touch converter, campaign, dependency, Docker or release code. No hosted CI run, deployment or release acceptance is claimed. If integrated with the workspace change, share its final validation batch and packaged converter check; otherwise run `make verify-converter` if implementation also changes converter, campaign, dependency, Docker or release code. Rerun only failed or subsequently affected checks.

## Workflow follow-up

- Review and archive only after a separately authorized implementation satisfies its required checks; planning completion is not implementation completion.
