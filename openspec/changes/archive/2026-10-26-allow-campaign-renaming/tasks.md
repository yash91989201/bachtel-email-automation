# Tasks

## 1. Campaign name update service

- [x] 1.1 Add campaign rename validation and an owner-scoped, cancellable service operation that trims input and updates only `name` and `updated` in a short transaction. Add focused campaign tests for required names, whitespace trimming, duplicates, Unicode 200/201-code-point boundaries, missing/foreign campaigns and failed saves retaining the old name; verify with focused `make test PACKAGES=./internal/campaign` selections.
- [x] 1.2 Add representative preservation tests around renaming a campaign with active, scheduled and terminal task evidence. Verify task IDs/names/state, revision pointer/hash, snapshots, approval/test evidence, schedules, candidate/duplicate-prevention history, attempts, resolutions and ready artifacts remain unchanged, and concurrent revision/settings updates are not clobbered. Keep fixtures isolated and avoid provider/converter calls for the rename operation.

## 2. Campaign Settings form and handler

- [x] 2.1 Add the bounded authenticated campaign-name POST, owner checks and POST/Redirect/GET success feedback, plus separate name-draft/error fields for rejected saves. Add focused handler tests for success, unchanged ID-based URLs, validation/storage failures, draft retention, anonymous/foreign/missing access refusal, existing request-origin protection and read-only Settings GETs; verify no processing operation or replay occurs.
- [x] 2.2 Add the General name-editing card before result-column settings using existing components, visible required/length guidance and Save name. Update focused view tests to verify prefilled and rejected drafts, escaped text, save feedback and separation from label editing/deletion. Format only changed Templ sources and regenerate each with `go tool templ generate -f <file>`; verify generated output matches the sources.
- [x] 2.3 Verify fresh campaign list, campaign section and task page responses use the renamed campaign in headings/breadcrumbs while task names, protected historical/download links and existing IDs remain intact. Add focused response regression coverage using the existing web fixtures; verify duplicate campaign names still address distinct campaigns.

## 3. Integrated verification

- [x] 3.1 With isolated data, exercise successful rename, same-name save, invalid input and error recovery from Campaign Settings using keyboard navigation and at 360, 768 and 1440 CSS pixels. Verify long names wrap, labels/errors remain visible, the current saved name remains the page identity after a rejected save, and active/finished task records remain usable without any email/provider request.
- [x] 3.2 Run `make lint` during development. Once integrated, stage only intentional generated output and run one non-overlapping full `make check`, then `make vulncheck` and `make verify-converter`; on a fresh checkout first run `bun install --frozen-lockfile`. Record actual results, skipped coverage and limitations; rerun failed checks or checks affected by subsequent fixes rather than repeating passed full suites.
- [x] 3.3 Reconcile planning artifacts with implemented behavior and run `openspec validate --all --strict --json --no-interactive`. Verify acceptance-scenario coverage and mark tasks complete only after their implementation and stated checks pass, preserving unrelated work.
