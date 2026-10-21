# Proposal

## Why

The earlier shadcn-templ migration installed 60 owned primitives but stopped at
static ones: 50 page cards still use hand-written `cn-card` class strings, the
three operator workspaces still run a second tab implementation in Alpine, eight
confirmations remain native checkboxes, and disclosures stay plain `<details>`.
The interactive primitives, their runtime bundle and an untracked
`workspace.js` adapter already exist, so the app maintains two component systems
and two sources of workspace state. Finish the adoption so pages use the owned
components consistently.

## What Changes

- Patch the owned `internal/web/ui/tabs` component so a vertical tab list renders
  `aria-orientation="vertical"` and drives Up/Down keyboard navigation; the
  bundled `tabs.js` already reads the list orientation.
- Add the workspace adapter to the real asset pipeline: move it to a tracked
  source, copy it in `scripts/copy-assets.mjs`, load it from the layout, and wire
  `data-workspace-tabs`, `data-workspace-persist` and hash links. Today it is
  gitignored and excluded from the Docker build context, so it never reaches
  production.
- Replace the three Alpine tabbed workspaces (campaign, task, sending) with the
  owned `tabs` and `collapsible` primitives while keeping every panel, hash
  preference, polling region, form id and error-selected panel.
- Replace hand-written card roots, manual empty states, form groups, back links,
  workflow/outcome rows and alert bodies with `card`, `empty`, `field`,
  `breadcrumb`, `item`, `separator` and `alert` subcomponents.
- Replace the eight native confirmation checkboxes with the owned `checkbox`
  primitive, keeping name, value and required submission semantics, and use
  `alertdialog` for the campaign-delete and mailbox-disconnect confirmations.
- Update view contract tests, remove the superseded workspace helpers and dead
  CSS, and correct `docs/ui-components.md`, which currently states that the
  primitives need no JavaScript runtime and that checkboxes and disclosures stay
  native.
- **BREAKING** (internal only): the rendered markup of tabs, disclosures,
  confirmations and card roots changes. No route, form field, authorization
  check, sending gate or persisted record changes.

## Capabilities

### New Capabilities

None. This is a UI implementation refactor with no spec-level behavior change;
`skip_specs: true` applies. Keyboard, focus and confirmation behavior remain
within the acceptance scenarios already owned by `operator-access`,
`campaign-templates`, `excel-intake`, `email-dispatch`, `sending-recovery` and
`history-exports`.

### Modified Capabilities

None. Every requirement and scenario in the existing specs keeps its current
outcome; only the markup that satisfies it changes.

## Impact

- `internal/web/ui/tabs/tabs.templ` and the owned interactive components used for
  the first time: `tabs`, `collapsible`, `checkbox`, `alertdialog`, `empty`,
  `field`, `breadcrumb`, `item`, `separator`, `card` subcomponents.
- `internal/web/views`: all eight templates plus `workspace.templ` removal.
- `internal/web/views/workspace_test.go`, `campaign_test.go`,
  `ui_contract_test.go` and any sending/task tests that assert tab or checkbox
  markup. Assertions move to the primitive contract; the behaviors they protect
  (one visible panel, labelled panels, retained state across polling, required
  confirmations, native form fields) must remain enforced.
- Assets: `web/js/`, `scripts/copy-assets.mjs`, `internal/web/views/layout.templ`,
  `Dockerfile` inputs if the new source is not covered by the existing `web/`
  copy.
- CSS: `web/css/app.css` and `web/css/components.css` lose `.workspace-tab`,
  `.workspace-panel`, `app-checkbox`, `card-disclosure` and related rules that
  the primitives replace; retained selectors are retargeted at
  `data-slot="tabs-trigger"`.
- Documentation: `docs/ui-components.md`, plus README UI notes if they describe
  the tab or checkbox implementation.

Out of scope: the application shell (`ui/sidebar`), a progress bar where the six
sending counters have no honest total, `select`/`combobox`/`command` replacing
native selects, avatar/chart/calendar/carousel/command/drawer/tooltip adoption,
route or data-model changes, and any change to candidate-sending authorization.
Candidate sending stays gated by the delivery change's release verification; this
change cannot enable it and sends no real email.
