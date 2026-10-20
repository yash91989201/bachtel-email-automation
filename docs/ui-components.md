# UI components

The application uses Tailwind CSS 4 and locally owned shadcn-templ components.
The migration first preserved the existing Bachtel interface and operator workflows.
The subsequently requested preset `b870VEw0in` now supplies the Luma style, Mist/Cyan
colors, small radius, Noto Sans body text and EB Garamond headings. Page structure,
content and operator workflows remain unchanged.

## Structure

- `internal/web/views`: page view models, page templates and persistent Alpine workspace state.
- `internal/web/ui`: component packages copied from shadcn-templ, plus shared class utilities and its MIT license.
- `web/css/theme.css`: resolved preset tokens and application status colors.
- `web/css/vendor`: pinned upstream Luma, Tailwind variants and animation styles.
- `web/css/fonts.css` and `web/fonts`: self-hosted preset fonts and OFL licenses;
  the asset build copies those font files into `internal/web/static/fonts` for
  embedded `/static/fonts/...` serving.
- `web/css/components.css`: presentation of the owned primitives, including native controls.
- `web/css/app.css`: Tailwind sources and application layout/responsive styles.
- `components.json`: component aliases and existing CSS/static paths for maintenance tooling.

The installed primitives render HTML on the server. They require no additional
JavaScript runtime. Alpine handles the existing workspace/disclosure interactions;
htmx handles existing fragments and polling. Native selects, checkboxes, file
inputs and details remain native. Keep workspace state outside polling regions.
Use semantic links styled with `button.Variants`; specify `button.TypeSubmit`
for submission buttons because the component's safe default is `button`.
Keep semantic sections, forms, details, and heading tags where replacing them
with a component's generic div would lose meaning. They can use the owned
`cn-card`/`cn-card-title` presentation while their contents use component APIs.

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
`go tool shadcn-templ bundle` before the npm CSS/font build; the hashed bundle
under `internal/web/static/` stays git-ignored and is rebuilt in Docker too.
Every page renders it once via `@ui.Scripts()` in the layout `<head>`.
Run `templ generate` (via `make generate`) and `go mod tidy` after adding.

Local adaptations: native select and table render their native element without
adding a wrapper or icon; alert respects an explicit role so status fragments
can remain polite; labels retain text selection; the requested Luma preset is active on the body; application
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
