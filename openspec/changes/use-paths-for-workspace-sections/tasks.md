# Tasks

All items below are implementation work for a future apply request. This planning change does not execute them.

## 1. Protected section routes and path helpers

- [ ] 1.1 Add the finite campaign/task section GET routes using existing server view builders and small path helpers; verify all nine destinations and both default roots with focused HTTP tests.
- [ ] 1.2 Keep existing Tasks/Sending routes, action methods, progress and downloads unambiguous; verify unknown-section 404s, unauthenticated requests, foreign ownership and campaign-task mismatch without external providers.

## 2. Native navigation and selected-section rendering

- [ ] 2.1 Replace workspace tab wrappers/triggers with styled ordinary sidebar links and current-page semantics, rendering only the chosen section; update view tests to assert canonical hrefs, breadcrumbs, current state, correct selected content and absence of inactive forms/pollers.
- [ ] 2.2 Remove custom section/hash selection, legacy alias translation, history writes/listeners and tab-restoration-on-swap code; verify section navigation, copy/new-tab, reload and Back/Forward work natively with section JavaScript disabled and no intercepted or boosted section links.
- [ ] 2.3 Retain section-local disclosure preservation and restored-confirmation reset logic alongside required htmx updates; verify valid drafts/focus and accordions survive their local swaps and stale confirmations remain invalidated, without promising persistence across full-page section changes.
- [ ] 2.4 Verify obsolete root fragments open defaults and explicit paths keep their selected section, while legitimate in-page anchors, ARIA IDs and htmx targets remain functional; update the navigation design/tests to remove obsolete bookmark-migration expectations.

## 3. Action feedback and complete link migration

- [ ] 3.1 Update campaign/task redirects, preparation/finalization/export returns and generated section/form links to canonical paths; verify focused HTTP/view tests retain existing operation authorization/idempotency checks and successful POST returns do not perform extra work.
- [ ] 3.2 Keep rejected POSTs server-rendered on the relevant section with safe errors and valid non-secret input, without custom history rewriting; verify representative template, recipient, sending/schedule and result errors plus subsequent native section navigation.
- [ ] 3.3 Audit section hrefs/form hints, remaining workspace adapter callers and the final design inventory; preserve legitimate DOM targets, format/regenerate only changed templates, and verify generated diffs stay confined to those files.

## 4. Integrated verification

- [ ] 4.1 Run focused affected-package tests and `make lint` during implementation, then use isolated browser fixtures at 360, 768 and 1440 CSS pixels to verify native section links/history, focus, no overflow, selected-section-only markup, obsolete fragment behavior and section-local polling draft preservation; record actual results without real email, local `.env` data or external AI calls.
- [ ] 4.2 After integration, run `npm ci` on a fresh checkout, stage only intentional generated output for consistency checking, then run `make check`, `make vulncheck`, `make verify-converter` and `openspec validate --all --strict --json --no-interactive`; record each result and limitation, rerunning only failed or subsequently affected checks. If both routing changes are applied together, use one shared final batch after both are integrated rather than overlapping suites.

## Workflow follow-up

- Review and archive only after a separately authorized implementation satisfies its required checks; planning completion is not implementation completion.
