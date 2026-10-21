# Design

## Context

See `proposal.md` for motivation. Current state that shapes the approach:

- `migrate-shadcn-templ` (archived) chose static primitives only: "add no browser
  runtime when static components suffice" and "keep native disclosure, checkbox
  and select controls native". This change reverses that decision for tabs,
  disclosures, confirmations and dialogs, and keeps it for native selects and
  date/number/file inputs.
- `internal/web/ui` owns 60 primitives; pages import nine (`button`, `card`,
  `alert`, `badge`, `input`, `label`, `textarea`, `table`, `nativeselect`).
- `internal/web/static/shadcn-templ-cecc214bac71383a.js` is loaded on every page
  through `ui.Scripts()`, so `window.templ.lifecycle`, `tabs`, `collapsible`,
  `checkbox` and `alertdialog` runtimes are already delivered. Adopting them adds
  no new request.
- The lifecycle runtime is a single `MutationObserver` that re-initializes
  matching elements after htmx swaps, so no manual re-init hook is needed.
- `internal/web/static/workspace.js` is an untracked, gitignored prototype that
  syncs tabs with the URL hash and restores collapsible state after swaps. It is
  excluded by `.dockerignore` (`internal/web/static/*.js`) and never copied by
  `scripts/copy-assets.mjs`, so production never serves it.
- Three workspaces render vertical sidebars as `role="tablist"` with Alpine
  `tab` state in `views/workspace.templ`; panel ids are `<scope>-panel-<value>`.
- Six `<details>` disclosures: four in the campaign page, one in the task page,
  one inside the sending polling region.
- Eight native confirmation checkboxes (seven in sending, one campaign delete).
- `views/workspace_test.go` and `views/campaign_test.go` assert the current tab
  contract: `hidden` attribute on inactive panels and `aria-controls` on the
  selected trigger. `views/ui_contract_test.go` protects native form contracts.

## Goals / Non-Goals

**Goals:**

- One component system and one owner for interactive state on every page.
- Vertical tablists keep correct ARIA orientation and Up/Down keyboard support.
- Workspace state (active panel, disclosure open state) survives the 2-second
  `outerHTML` swap of `#sending-progress` and hash navigation.
- Confirmation checkboxes keep `required`, `name` and `value` submission
  semantics and survive `hx-preserve` forms.
- The adapter ships through the normal asset pipeline into the production image.

**Non-Goals:**

- No route, view-model, handler, database, authorization or sending-gate change.
- No application-shell redesign, no `ui/sidebar`, no progress bar without a real
  total, no JS-driven form selects.
- No pixel-parity guarantee against the archived Luma baseline; layout, copy and
  control attributes stay, component chrome may differ slightly.

## Decisions

### 1. Patch vertical tabs in `tabs.templ`, not in JavaScript

`tabs.js` already supports vertical lists: `init()` reads
`list.data-orientation` to choose the composite orientation and
`activationDirection()` reads it too. Only `orientation(ctx)` in `tabs.templ`
hardcodes `OrientationHorizontal`, so `List` emits `data-orientation="horizontal"`
and no `aria-orientation`, while the root says vertical. Return the orientation
stored in the tabs context and delete the comment that documents the upstream
limitation as permanent.

Alternative rejected: patch the bundled runtime. The bundle is generated from
the owned `tabs.js`; patching the generated file would be overwritten by
`go tool shadcn-templ bundle` and would diverge from upstream semantics.

Verify with a rendered-attribute test, not a JS test: `List` carries
`aria-orientation="vertical"`, triggers and panels carry
`data-orientation="vertical"`, and the runtime's documented vertical path is the
one exercised.

### 2. Adapter becomes tracked source, loaded once by the layout

Move the prototype to `web/js/workspace.js`, copy it in `scripts/copy-assets.mjs`
next to the other static scripts, and add `<script defer src="/static/workspace.js">`
to `layout.templ` after `ui.Scripts()`. Referencing it from `internal/web/static`
directly would keep it outside git and outside the Docker build context.

Rationale: the adapter is the only place that knows the URL hash and the htmx
swap lifecycle. Duplicating that in each page would put per-page script in
templates and break the single-source rule already used for the Alpine and htmx
vendors.

Alternative rejected: keep Alpine tab state and use no adapter. That leaves two
state owners and does not fix disclosure state inside the polling region.

### 3. Tabs root ids stay `<scope>`, panel ids keep their current values

`tabs.triggerID` and `tabs.contentID` derive `<id>-tab-<value>` and
`<id>-panel-<value>`. Setting the root id to `campaign`, `task` and `sending`
preserves every existing panel and trigger id, so handlers, `aria-*` assertions,
hash values and htmx targets keep working unchanged.

### 4. Hash preference and error-selected panels

The current `x-init` applies the URL fragment only when the page has no
validation error, so a failed revision or label submission opens the panel that
shows the error. The adapter applies the fragment unconditionally. Add a
root-level opt-out (`data-workspace-ignore-hash`) that the campaign page sets
when `RevisionError`, `LabelsError` or `DeleteError` is present; `syncRoot` then
keeps the server-rendered active tab.

Alternative rejected: drop hash restore on error pages. The operator would lose
the fragment on every successful navigation because the state is re-derived on
each render.

### 5. One tab list per workspace; descriptions carry grouping

`role="tablist"` may contain only tabs, so the current mid-list headings
("Content & preparation", "Records & management") are invalid and cannot survive
`tabs.List`. `tabs.js` wires the composite on `LIST[0]` only, so splitting into
several lists would silently disable keyboard navigation in the later groups.

Use a single `tabs.List`; the existing two-line trigger descriptions already name
each group, and a decorative `workspace-group-separator` div marks the boundary
where the heading had meaning. It is `aria-hidden` and carries no role, because a
tablist may only contain tabs.
Alternative rejected: patch the runtime for multiple lists; it changes the
component contract for a cosmetic heading and was not requested.

### 5a. The polling fragment renders panels without their root

`#sending-progress` swaps only the four dynamic panels; the tabs root and its
triggers stay mounted outside the swapped region. Rendering `tabs.Content`
without a root cannot derive the panel's id or its `aria-labelledby`, and the
panel counter is nil. `tabs.Panels(ctx, id, orientation)` supplies the root's
identity to a fragment (a no-op inside the real root), and `Content` tolerates a
missing counter. The panels render hidden; the adapter re-applies the active tab
in `htmx:afterSwap` and `htmx:afterSettle`, which is the existing contract.

### 5b. Preserved element ids must be selector-safe

htmx restores an `hx-preserve` element by looking it up with a selector built
from its id. The approval form's id embedded the content hash (`sha256:…`), so
the lookup threw and the whole poll swap failed. `approvalFormID` keeps letters,
digits, `-` and `_`; the form still changes identity when the hash, the latest
test or the approval state changes, so a stale confirmation is still discarded.

### 6. Disclosures: `collapsible` only where state must persist

The sending guidance disclosure lives inside the 2-second polling swap and is
the only disclosure whose state can be lost; it moves to `collapsible` with
`data-workspace-persist`, and the adapter restores it after each swap using the
trigger's `data-templ-controls` id.

The four campaign and one task disclosures stay native `<details class="card-disclosure">`.
`collapsible.Trigger` renders a `<button>`, and the current structure puts an
`<h2>` inside the summary; a heading cannot legally nest in a button, so
converting those would either drop heading semantics or change the visible
affordance. `docs/ui-components.md` keeps that exception explicit.

Alternative rejected: move the heading outside the trigger and add a separate
"show/hide" button. That is a UI change outside this refactor.

### 7. Card roots: `card.Card` with accessible landmark attributes

Replace `<section class="cn-card border border-border bg-card shadow-sm" aria-labelledby=...>`
with `card.Card` and pass `role="region"` plus the existing `aria-labelledby`
through `Attributes`. `.cn-card` already supplies background, border, radius and
shadow, so the duplicated Tailwind utilities are dropped. Real `<h2>` elements
stay inside `card.Content`.

Alternative rejected: add a polymorphic `Tag` prop to the owned card. It changes
a stable upstream API for one call pattern; attributes are enough.

### 8. Checkbox keeps native submission semantics

`checkbox.Checkbox` renders a `role="checkbox"` span plus a clip-path-hidden
native input that owns `id`, `name`, `value`, `required` and `form`. The existing
forms rely on browser `required` validation, so each confirmation keeps the
native input in the form, stays inside its `<label>` (implicit association and
`input.labels` both resolve), and is rendered with `Required: true`.

Risk to verify in a browser, not by inspection: submitting with an unchecked box
must show the browser's validation message and send no request, clicking the
visible span must toggle the hidden input, and `hx-preserve` forms must keep
their checked state across swaps.

Alternative rejected: native restyled checkboxes. Already the current state; the
proposal asks for the owned component.

### 9. Destructive confirmations use `alertdialog`

Campaign deletion and mailbox disconnect currently expand an inline Alpine
confirmation inside the form. Replace both with `alertdialog` trigger, title,
description and a confirm control that is the submit button of the same POST
form. The dialog cancels without submitting and preserves the existing
confirmation copy. If focus management or the portal cannot keep the submit
button inside its form, keep the inline confirmation and record the exception
rather than duplicating the form.

The portal moves the dialog to `<body>`, so the confirm control reaches its form
through the HTML `form` attribute rather than DOM containment; the campaign
delete keeps its required confirmation checkbox inside the dialog with the same
`form` association. A browser check confirmed the unchecked box blocks the
submit, the checked box submits once, Escape closes, focus returns to the
trigger, and cancel submits nothing.

### 10. Static primitives adopted where they replace duplicated markup

`empty` for the two "no records yet" cards, `field`/`field.Field` for form groups
that repeat label + control + help text, `breadcrumb` for the campaign back link,
`item` for the overview workflow steps and sending outcome rows, `separator`
between grouped form sections, and `alert.Title`/`alert.Description` where
alerts currently nest raw `<div>`/`<p>`.

Native `nativeselect`, date, number and file inputs stay. `ui/select` and
`ui/combobox` require runtime-driven value submission; replacing working native
controls with them would add risk to scheduling and worksheet selection for no
operator gain.

### 11. Tests move to the primitive contract, not around it

Rewrite the tab assertions to the primitive's contract: exactly one trigger with
`data-active` and `aria-selected="true"`, exactly one panel without `data-hidden`
and without the `hidden` class, panels labelled by their trigger, `aria-controls`
present on the active trigger. Keep the existing higher-level protections: one
visible panel per workspace, error-selected panels, dynamic panels inside the
polling region, management panels outside it, and no submit control without a
confirmation field.

`ui_contract_test.go` keeps asserting route, method, `enctype`, names, types,
`required`, `maxlength`, `accept`, selected options and submit-button types; only
selectors that depend on the replaced markup change (for example, a checkbox
assertion that previously matched `input.app-checkbox`).

## Risks / Trade-offs

- **State loss across polling** → the adapter restores tabs and disclosures in
  `htmx:afterSwap` and `htmx:afterSettle`; verify with the real 2-second poll and
  a checked diagnostic-address input, which is the exact case the archived
  verification recorded as failing in both baselines.
- **Required hidden input not focusable** → verify the browser blocks submission
  and displays its validation message; if the clip-path input cannot receive
  focus, wrap the confirm control and surface the message through `field.Error`
  instead of dropping `required`.
- **JS-disabled operator** → tabs and dialogs degrade to the server-rendered
  active panel and to no confirmation dialog. The active panel and every
  management panel remain reachable without JS only if the non-active panels are
  not permanently hidden; verify and record the actual degradation.
- **Visual drift** → component chrome differs from the hand-written classes.
  Compare representative desktop/mobile routes; accept small differences, keep
  text, structure, focus order and control behavior.
- **Two runtimes remain** → Alpine still owns the inline confirmations not
  converted (hint disclosures, `x-show` bits) and htmx owns swaps. Each piece of
  state must have exactly one owner after migration; list the remaining Alpine
  attributes in the verification record.
- **Docker asset gap** → if `web/js` is not copied by the existing `COPY web/ web/`
  stage, the production image would serve a 404 for the adapter. Verify with a
  container build or by asserting the embedded asset in a test.
- **Upstream drift** → the vertical patch is local and must be listed with the
  other documented adaptations that `add --overwrite` would discard.

## Migration Plan

1. Patch `tabs.templ`, regenerate, add the rendered-attribute test.
2. Move the adapter into `web/js`, wire `copy-assets.mjs`, the layout script tag
   and an embedded-asset assertion; confirm the file exists in the built image.
3. Migrate the campaign workspace, then task, then sending; each step keeps its
   tests passing before the next page changes.
4. Convert static markup (cards, empty, field, breadcrumb, item, separator,
   alerts), then checkboxes, then the two alert dialogs.
5. Delete `views/workspace.templ` and the dead CSS, update docs, run the full
   required checks and browser smoke tests.

Rollback is reverting this cohesive source change and rebuilding assets; there is
no database migration, no persisted format change and no endpoint change.

## Verification record

Environment: synthetic fixture (`tmp/_ui-migration/main.go`, `http://127.0.0.1:8768`),
built assets, no `.env`, no production database, no real mailbox credentials.

- Template-only tabs fix: `go tool shadcn-templ bundle` reproduced the same
  runtime hash; only `tabs.templ` and its generated Go changed.
- Vertical tabs (browser): `aria-orientation="vertical"`, Up/Down moves focus,
  Enter/Space activates, Home/End reach the ends, exactly one visible panel;
  hash follows the selection.
- Error pages: `data-workspace-ignore-hash` keeps the server-selected panel when
  the URL names another tab.
- Polling: two real 2-second cycles keep the selected recovery panel, the URL
  fragment and an open guidance disclosure; an unchanged control identity keeps
  a checked confirmation. Diagnostics showed and fixed a failing swap caused by
  a `:` in a preserved form id.
- Confirmations (browser): unchecked submission posts nothing and shows
  validation; checked submission posts once. Posted fields observed:
  `action_id`, `expected_count`, `confirmed=yes` (candidate start);
  `confirm=delete` (campaign delete); empty body (mailbox disconnect).
- Dialogs: campaign delete and mailbox disconnect open, cancel without posting,
  confirm once, close on Escape and return focus to the trigger.
- Layout: desktop and mobile captures of the fixture routes; sidebar grid,
  wrapped trigger labels, workflow items and card spacing kept; no horizontal
  overflow at 390px and no console exceptions (the fixture's own
  `/favicon.ico` 404 excluded).
- Required checks: `make fmt` (0 files changed), `make generate assets`,
  `make lint` (one `modernize` finding fixed in a test), `make check` (format,
  generated-output consistency, lint with vet, race tests, production build) and
  `make verify-converter` (packaged Pandoc stage, full suite) pass.
  `make vulncheck` fails on four pre-existing `github.com/xuri/excelize/v2`
  advisories (GO-2026-6673 and three others traced to `internal/intake` and
  `internal/exports`); this change adds no dependency and does not touch those
  call paths. `openspec validate --all --strict` passes.

Remaining limitations:

- Alpine still owns the sample-preview required-value gate and the
  dismissible preview result (`x-data`/`x-show`/`x-bind`), because neither has a
  primitive that models it without changing the interaction.
- The campaign and task disclosures remain native `<details>`: their summary
  contains an `<h2>`, which cannot nest in the collapsible trigger's button.
- With JavaScript disabled, every tab panel renders hidden except the active
  server-rendered one; `tabs.js` initializes the list on load, so non-active
  panels are reachable only with the runtime. Dialogs do not open without it,
  and their forms still submit directly to the same POST routes.
- Visual drift accepted: card chrome comes from `cn-card` plus primitive
  spacing; the sidebar group headings became separators, and the workflow steps
  became `item` rows.
- `internal/web/ui/tabs` now carries three local adaptations (orientation,
  fragment panels, missing-counter tolerance) that `add --overwrite` would
  discard; they are listed in `docs/ui-components.md`.
