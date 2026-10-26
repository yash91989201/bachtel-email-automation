# Tasks

## 1. Persist task names and backfill existing tasks

- [x] 1.1 Add the additive `send_tasks.name` field and transactional backfill from `source_name`, preserving populated names and using `task.xlsx` for missing/whitespace-only source names. Add isolated migration tests for staging, active, completed and closed records, repeated backfill, and unchanged IDs, upload timestamps and workflow/history fields; verify with focused migration tests.
- [x] 1.2 Add the name to the task model and shared record mapping; extend the existing staging entry point and its callers to resolve and persist the name atomically at creation. Add focused intake tests for explicit/trimmed names, omitted/blank defaults, duplicate names, source filename preservation, Unicode/255-versus-256-code-point boundaries and no partial task on validation failure; verify loading/listing retains the saved name.

## 2. Name tasks during upload and display their identity

- [x] 2.1 Add Task name (optional) and visible default/length guidance to the upload form, pass the submitted text through the handler, and preserve it with the sending preference on errors. Add handler/view tests for successful named and unnamed uploads, invalid names, workbook/preference errors and escaped text; verify the ordinary upload/validation redirect remains correct.
- [x] 2.2 Carry name and source filename separately into campaign/task views. Use the Task column and saved-name link in the campaign list, name in each task heading/breadcrumb, source filename in the identity tooltip and Overview Task details, and retain reference/upload-time distinctions. Update focused view/handler contracts to verify duplicate names, direct section visits, escaped markup, original filename visibility and unchanged ID-based destinations.
- [x] 2.3 Format and regenerate only the Templ sources changed by this group with `go tool templ fmt <file>` and `go tool templ generate -f <file>`. Verify the committed generated output corresponds to those sources and run focused web/view tests before continuing.

## 3. Rename tasks through task Settings

- [x] 3.1 Add an owner-checked intake name-update operation using the shared creation rules and a short transaction that updates only name metadata. Add focused tests for custom/blank/duplicate/invalid names, unauthorized access, storage failure with prior-name retention, and allowed updates across active, scheduled, completed, closing and closed states; verify concurrent workflow changes and approvals/schedules/history are preserved.
- [x] 3.2 Add the Settings section after Results and its direct GET route, plus the bounded owner-only name POST and POST/Redirect/GET success feedback. Reuse existing authentication, request-origin protections and campaign-task membership checks. Add route tests for prefilled names, successful rename/reset, rejected input/storage failure with draft retention, anonymous/foreign/wrong-campaign refusal, and read-only GET navigation.
- [x] 3.3 Build the Settings name form using existing components and shared shell, update section-selection/navigation contracts, and format/regenerate only affected Templ sources. Verify Settings works in staging and terminal states, remains outside the lifecycle tracker, keeps invalid drafts with safe feedback, and successful renaming is reflected in subsequent campaign/task page responses without changing processing controls.

## 4. Integration and required checks

- [x] 4.1 Exercise creation, blank-name fallback, backfilled-task display, rename and reset using isolated workbook/task data. Inspect upload, task list, task sections and Settings at 360, 768 and 1440 CSS pixels and with keyboard navigation; verify long names wrap, identity tooltips remain accessible, errors/success are visible, and no real email or provider request is triggered.
- [x] 4.2 Run `make lint` while iterating. After integration, stage only intentional regenerated output and run one non-overlapping final `make check`, plus `make vulncheck` and `make verify-converter`; run `bun install --frozen-lockfile` first if using a fresh checkout. Record actual outcomes and limitations; rerun failed checks or checks affected by subsequent fixes rather than repeating already-passed full suites.
- [x] 4.3 Reconcile artifacts with the implemented behavior and run `openspec validate --all --strict --json --no-interactive`. Verify every acceptance scenario has implementation/test or manual evidence before checking off the corresponding work, and retain any unrelated changes.

## Verification record

- `make lint`: passed, 0 issues (golangci-lint with the pinned version).
- `make check`: passed, exit 0 (format, generation consistency, lint/vet, race tests, production build).
- `make vulncheck`: passed, no called vulnerabilities; 6 module-only advisories are unreachable from this code.
- `make verify-converter`: passed in the packaged-Pandoc Docker `converter-test` stage (`internal/convert`, `internal/campaign`, acceptance campaign-authoring flow).
- `openspec validate --all --strict --json --no-interactive`: passed, 17 of 17 items.
- Isolated browser walkthrough: temporary PocketBase data dir and local server, headless Chromium at 360, 768 and 1440 CSS pixels. Verified named upload trimming, blank-name fallback, duplicate and long names, pointer- and keyboard-open identity tooltips, Settings rename/reset, save feedback without replay, 256-code-point refusal with draft retention, Settings on staging and closed tasks, and task navigation order. All 200 page requests stayed on `127.0.0.1:18090`; no mailbox or provider host was contacted.
- Limitation: the walkthrough campaign had no content revision, so validation and dispatch were not exercised through the browser; package, handler and view tests cover those sections, and no automated or manual check used a real mailbox.
