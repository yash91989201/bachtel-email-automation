# Tasks

## 1. Vertical tab support

- [x] 1.1 Patch `internal/web/ui/tabs/tabs.templ` so `orientation(ctx)` returns the orientation stored by `Tabs` instead of a constant, and remove the comment that presents the horizontal-only list as permanent; verify with a new rendered-attribute test that a vertical `Tabs` emits `aria-orientation="vertical"` on `List` and `data-orientation="vertical"` on triggers and panels, and that horizontal output is unchanged.
- [x] 1.2 Regenerate templates (`make generate`) and verify `go tool shadcn-templ bundle` reproduces the same runtime without a JS diff, confirming the fix is template-only.
- [x] 1.3 Confirm the vertical path in a browser with the synthetic fixture: Up/Down moves between sidebar tabs, Enter/Space activates, Home/End reach the ends, and the active panel is the only visible one; record the result and the fixture command in the change verification notes.

## 2. Workspace adapter pipeline

- [x] 2.1 Move the prototype adapter to tracked `web/js/workspace.js`, extend `scripts/copy-assets.mjs` to copy it to `internal/web/static/`, and add the deferred script tag to `internal/web/views/layout.templ`; verify `/static/workspace.js` is served and included by an embedded-asset assertion in `internal/web/web_test.go`.
- [x] 2.2 Add the adapter's missing behavior for error-selected panels: support a root-level `data-workspace-ignore-hash` flag that makes `syncRoot` keep the server-rendered active tab; verify with a unit-level DOM fixture or a focused test that a campaign page rendering a revision error keeps its error panel when the URL names another panel.
- [x] 2.3 Verify the file reaches production: build the container `assets` stage or run `make build` and confirm `workspace.js` is embedded and served from the built binary, and remove the gitignored prototype so no stale copy is served in development.
- [x] 2.4 Update `docs/ui-components.md` structure and build sections to describe the tracked adapter, its script tag, its data attributes and its role across htmx swaps; verify the documented paths and commands match the repository.

## 3. Workspace migration

- [x] 3.1 Migrate the campaign workspace to `tabs.Tabs` (`OrientationVertical`, id `campaign`, `DefaultValue: campaignInitialTab(data)`) with `tabs.List`/`tabs.Trigger`/`tabs.Content`, keeping `campaign-tab-*` and `campaign-panel-*` ids, the conditional preview tab, the task link and the hash opt-out on error; verify `views/campaign_test.go` asserts the primitive contract (one `data-active` trigger, one panel without `data-hidden`, labelled panels) and error-selected panels still pass.
- [x] 3.2 Migrate the task workspace the same way and keep the worksheet-setup panel as the staging default; verify `views/workspace_test.go` covers staging (1 tab), validated (4 tabs) and all-held (3 tabs) cases against the new contract, and the sending action still appears only when eligible rows exist.
- [x] 3.3 Migrate the sending workspace, keeping the polling region exactly as bounded today: dynamic panels inside `#sending-progress`, results and pacing panels outside it, and the active tab re-applied after each swap; verify the test that asserts state outlives polling still passes in its new form and that `hx-preserve` forms remain untouched.
- [x] 3.4 Move the sending guidance disclosure to `collapsible` with a stable controls id and `data-workspace-persist`, add `data-workspace-hash-link` to the refresh link, and verify the open state and the checked diagnostic address survive two real polling cycles.
- [x] 3.5 Delete `internal/web/views/workspace.templ` and the CSS the primitives replace (`.workspace-tab`, `.workspace-panel`, `.workspace-nav-heading` if unused, Alpine-only selectors), retarget retained sidebar styling at `[data-slot="tabs-trigger"]`, and verify no template references a deleted class and the sidebar keeps its desktop and mobile layout.

## 4. Static primitive consistency

- [x] 4.1 Replace the 50 hand-written `cn-card` roots with `card.Card` plus `card.Header`/`Action`/`Title`/`Description`/`Content`/`Footer` where the existing structure matches, passing `role="region"` and the existing `aria-labelledby` through `Attributes`, and keep every `<h2>`; verify rendered landmarks and that no page loses a labelled section.
- [x] 4.2 Replace pages that only repeat label + control + help text with `field`/`field.Field`/`field.Description`/`field.Error`, keeping ids, `for` targets, `aria-describedby` ids and constraint attributes; verify `views/ui_contract_test.go` still proves each named control, its label and its constraints.
- [x] 4.3 Replace the two "no records yet" cards with `empty`, the campaign back link with `breadcrumb`, the overview workflow steps and sending outcome rows with `item`, grouped form sections with `separator`, and nested raw alert bodies with `alert.Title`/`alert.Description`; verify each page renders the same text, roles and live-region attributes as before.
- [x] 4.4 Update `docs/ui-components.md` and README UI guidance to describe the adopted primitives, the `<details>` disclosure exception and the remaining native controls; verify the statements match the rendered markup.

## 5. Confirmations and dialogs

- [x] 5.1 Replace the eight native confirmation checkboxes with `checkbox.Checkbox` inside their existing labels, keeping `name`, `value="yes"`, `Required: true` and form ownership; verify a browser submission with an unchecked box blocks the request and shows validation, a checked box submits, and clicking the visible control toggles the hidden input.
- [x] 5.2 Verify checked state survives `hx-preserve` and polling: check a confirmation, wait two progress cycles, confirm it is still checked, and confirm the server-side expected-count and CSRF requirements are unchanged.
- [x] 5.3 Replace the campaign-delete and mailbox-disconnect inline confirmations with `alertdialog` whose confirm control is the submit button of the same POST form; verify cancel submits nothing, confirm submits once, focus returns to the trigger, and Escape closes the dialog.
- [x] 5.4 Verify no confirmation path can submit without its confirmation field by re-running the confirmation contract tests and recording the actual field names the browser posted.

## 6. Integration verification

- [x] 6.1 Run `make fmt`, `make generate assets` and `make lint`; report actual results and any file that had to be regenerated.
- [x] 6.2 Run `make check` (format, generated-output consistency, lint with vet, race tests, production build) and `make vulncheck`; report the existing Excelize advisories separately from any finding introduced by this change.
- [x] 6.3 Run `make verify-converter` because the asset pipeline and container inputs change; report the result.
- [x] 6.4 Run `openspec validate --all --strict --json --no-interactive`.
- [x] 6.5 Smoke the five routes with the synthetic fixture from `tmp/_ui-migration/` at desktop and mobile widths: overview, login, campaign detail across all tabs, task review, sending with a real 2-second poll. Record screenshots or observations for tabs, disclosures, confirmations, dialogs, keyboard order and the absence of console exceptions, using no production database, `.env` or mailbox credentials.
- [x] 6.6 Record the final limitation set in the change's verification notes: remaining Alpine attributes, any JS-disabled degradation, and any visual difference accepted against the archived baseline.

## Workflow follow-up

- Archive the change after implementation, review and verification are complete.
- Re-run strict OpenSpec validation after any artifact update made during implementation.
