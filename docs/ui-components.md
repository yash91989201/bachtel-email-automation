# UI components

The application uses Tailwind CSS 4 and locally owned shadcn-templ components.
The migration first preserved the existing Bachtel interface and operator workflows.
The subsequently requested preset `b870VEw0in` now supplies the Luma style, Mist/Cyan
colors, small radius, Noto Sans body text and EB Garamond headings. Page structure,
content and operator workflows remain unchanged.

## Structure

- `internal/web/views`: page view models and page templates. A campaign or task
  section is an ordinary page at its own path; `workspaceNav` renders those
  destinations as links and only the selected section is rendered.
- `web/js/workspace.js`: the tracked section-local adapter. `scripts/copy-assets.mjs`
  copies it to `internal/web/static/`, and `layout.templ` loads it once with a
  deferred `<script>` after `@ui.Scripts()`. It holds no URL or history state:
  it restores disclosure state across htmx swaps and invalidates a
  history-restored sending confirmation.
- `internal/web/ui`: component packages copied from shadcn-templ, plus shared class utilities and its MIT license.
- `web/css/theme.css`: resolved preset tokens and application status colors.
- `web/css/vendor`: pinned upstream Luma, Tailwind variants and animation styles.
- `web/css/fonts.css` and `web/fonts`: self-hosted preset fonts and OFL licenses;
  the asset build copies those font files into `internal/web/static/fonts` for
  embedded `/static/fonts/...` serving.
- `web/css/components.css`: presentation of the owned primitives, including native controls.
- `web/css/app.css`: Tailwind sources and application layout/responsive styles.
- `components.json`: component aliases and existing CSS/static paths for maintenance tooling.

The installed primitives render HTML on the server, and the interactive ones
(`tabs`, `collapsible`, `checkbox`, `alertdialog`, `dialog`) rely on the shared
runtime bundle that every page loads once via `@ui.Scripts()`. The lifecycle
runtime re-initializes matching elements after htmx swaps, so a swapped fragment
needs no manual re-init. htmx still owns fragments and polling; Alpine remains
only for the sample-preview gating and the dismissible preview result.

Workspace markup:

- Campaign sections live at `/campaigns/{id}/{tasks,template,activity,settings}`
  and task sections at `/campaigns/{id}/tasks/{task}/{overview,recipients,test,sending,results}`.
  `views.CampaignSectionPath` and `views.TaskSectionPath` build those URLs for
  links and redirects; the campaign and task roots render their default section.
- `views.workspaceNav` renders the sidebar as ordinary links with
  `aria-current="page"` on the selected destination. Click, new tab, reload and
  Back/Forward are native browser behaviour: no route interception, `hx-boost`,
  `pushState` or `popstate` handler exists for section navigation, and the
  inactive sections' forms and pollers are absent from the response.
- A persisted disclosure is a `collapsible`/`accordion` with a stable id and
  `data-workspace-persist`; the adapter restores it after htmx swaps. Disclosure
  and draft state are section-local: a full-page section change does not carry
  them.
- `hx-preserve` element ids must be selector-safe (letters, digits, `-`, `_`):
  htmx restores them with a selector built from the id, so a content hash with
  `:` is sanitized (`approvalFormID`).

Remaining native controls: `nativeselect`, date, number and file inputs stay
native because `ui/select`/`ui/combobox` need runtime-driven value submission.
The campaign and task disclosures stay `<details class="card-disclosure">`: their
summary contains an `<h2>`, and a heading cannot nest in a button. The campaign
delete and mailbox disconnect confirmations use `alertdialog`; the confirm
control is the submit button of the same POST form (via its `form` attribute),
and the delete form keeps its required `checkbox` confirmation.

Use `card.Card` with `role="region"` and the existing `aria-labelledby` instead
of a hand-written `cn-card` section, and keep real `<h2>` headings inside.
`field.Field` groups a label, control and help text. `empty`, `breadcrumb`,
`item` and `separator` replace repeated list, link and grouping markup.
Use semantic links styled with `button.Variants`; specify `button.TypeSubmit`
for submission buttons because the component's safe default is `button`.

## Provenance and updates

Component source: [shadcn-templ v2.0.0-beta.15](https://github.com/axadrn/shadcn-templ/tree/v2.0.0-beta.15),
commit `191c364eabdfb6e6b75f2733163634db2903a06d`, MIT licensed.
The release is a beta. Sources are owned here deliberately; the upstream
package-import workflow is experimental and is not used.

All upstream components are installed with the official CLI, pinned as a Go
tool dependency (`go tool shadcn-templ`):

```sh
shadcn-templ add --all
```

The CLI resolves registry dependencies, rewrites imports to this module, and
rebuilds the component JavaScript bundle. `make assets` reruns
`go tool shadcn-templ bundle` before the Bun CSS/font build; the hashed bundle
under `internal/web/static/` stays git-ignored and is rebuilt in Docker too.
Every page renders it once via `@ui.Scripts()` in the layout `<head>`.
Run `templ generate` (via `make generate`) and `go mod tidy` after adding.

Local adaptations: native select and table render their native element without
adding a wrapper or icon; alert respects an explicit role so status fragments
can remain polite; labels retain text selection; `tabs` returns the orientation
its root stored (upstream keeps list, panels and arrow keys horizontal),
tolerates a panel rendered without its root, and exposes `tabs.Panels` for a
fragment that swaps only panels; the requested Luma preset is active on the body; application
component CSS supplies structural compatibility and operator-status styling. Input/textarea sizing and label layout are kept compatible
with existing forms. Re-running `add --overwrite` would discard these
adaptations, so review its diff and re-apply them. The shared `utils` package
additionally keeps this project's deterministic class ordering and Tailwind 4
syntax adaptation over the upstream file.

For a new component that needs JavaScript, first inspect its scripts and lifecycle,
then rely on the existing bundle pipeline and embedded `/static/` serving.
Do not add a second Taskfile build system or CDN scripts.
Test initialization, teardown, keyboard access and form state after htmx swaps.

## Verification

Run `make fmt`, `make generate assets`, and `make lint` while editing. Generated
Go for both views and UI packages is committed. Stage intentional generated
output before `make check`; it checks both directories for drift. Run the full
required checks in [development-checks.md](development-checks.md), including the
packaged converter for dependency changes. Compare representative desktop and
mobile screens and exercise keyboard tabs, confirmation controls, validation,
file uploads and progress polling with synthetic data.

## Active preset

The pinned CLI resolved `b870VEw0in` in an isolated temporary module before its
output was applied, avoiding `init` overwriting the application's existing files.
The preset settings are recorded in `preset.json`; aliases and asset paths in
`components.json` retain this application's structure. The preset chooses
Remix Icon, lime charts, bold menu accent and inverted menus. Those preferences
are recorded without adding unused icon/chart/menu runtimes. Local font sources
and licenses are documented in `web/fonts`.
