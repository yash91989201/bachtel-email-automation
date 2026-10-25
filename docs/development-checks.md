# Development checks and reviews

Repository agreements live in `AGENTS.md`; product behavior remains in OpenSpec.
Project skills live in `.agents/skills/`. Use `$go-development` while coding,
alongside `$openspec-apply-change` when implementing a spec change. Invoke
`$go-review` for Go correctness or `$automation-reliability-review` for intake,
sending/recovery, and export integrity. These review skills report findings
without editing the application.
Oh My Pi discovers the same skills through `.omp/skills/` symlinks.

## Local checks

Use the Go version in `go.mod`, Bun 1.4.4 (from `.bun-version` and matching the
Docker asset stage), Git, Make, and a C compiler for Go's race detector.
`make setup` installs tools and dependencies and generates templates and assets.
It runs `make tools`, `bun install --frozen-lockfile`, `go mod download`, and
generation. The pinned `golangci-lint` executable is installed in
`$(go env GOPATH)/bin`; ensure the
effective `PATH` used by shells and GUI-launched editor clients includes this
directory before any other linter installation. After changing tool versions,
rerun `make tools` and restart clients. If another binary still shadows the
installed one, `make tools` fails its final version check and prints the active
path; correct the normal PATH ordering rather than adding a runner wrapper.
This setup does not automatically switch versions when entering a project.

Start a fresh checkout with `make setup`, then run:

```sh
make check
make vulncheck
make verify-converter
```

`make check` verifies Go/templ formatting without rewriting source, regenerates
committed templates, builds assets, checks Tailwind diagnostics, runs `bun test
./scripts`, runs the shared linter and race tests, and builds the production
binary. Tailwind warnings remain failures; the check does not apply fixes. Stage
intentional generated `*_templ.go` changes alongside
`.templ` edits before checking: generated output is compared with the Git index,
and new untracked generated files fail the check. Generated CSS/JS and copied fonts stay ignored.
The asset build copies Alpine and htmx from `node_modules` installed by Bun,
plus licensed fonts from `web/fonts`, into `internal/web/static`. It then
compiles `web/css/app.css` to the embedded CSS.
Air watches `.templ`, CSS, JS and WOFF2 source changes, along with `bun.lock`,
`bunfig.toml` and `.bun-version`, and reruns generation, asset copying and the
server build; generated files under `internal/web/static` are excluded from its
watch list.

`make vulncheck` uses a pinned govulncheck version and the live Go vulnerability
database. Findings fail the check; assess and update affected code/dependencies
rather than suppressing the command. The tool is separate from application
module requirements. Network access is needed for dependency/tool downloads and
the vulnerability database.

`make verify-converter` needs Docker and runs the existing converter-test stage
against the runtime image's pinned Pandoc. Local tests can skip conversion cases
when Pandoc is unavailable; this container check closes that coverage gap. It is
required in CI on every PR. For a local converter/authoring/release change, run
it too. No automated check needs `.env`, live mailbox credentials, or real email.

If OpenSpec artifacts change, also run:

```sh
openspec validate --all --strict --json --no-interactive
```

Checks do not replace simulated end-to-end, redeploy, backup/restore, and
browser acceptance. Release records document what was verified; they do not
unlock candidate sending. Candidate actions remain protected by explicit
operator authorization and received-format approval for the exact submitted
test configuration. Automated checks must never send real email.

## Shared Go policy

`.golangci.yml` is the single executable policy for Go source and tests. It
selects govet, staticcheck, unused, ineffassign, errcheck, modernize, and nolintlint.
Generated output is excluded using Go's strict generated-file convention; it is
still compiled and checked for consistency with its `.templ` sources.

Run `make lint` while coding. It first checks that the ordinary `golangci-lint`
on `PATH` matches `.golangci-lint-version`, then runs it with the shared config.
If it is missing or mismatched, the check prints guidance to run `make tools`.
That target installs the upstream binary at the exact pinned version in
`$(go env GOPATH)/bin` when that installed binary does not match the pin. The linter does not affect application
module dependencies.

For editor integration, use a native linter integration such as nvim-lint and
run `golangci-lint run --config .golangci.yml ./...` from the project root.
Keep the selected Go tools and OMP on the effective `PATH`; where Mason offers
`PATH='append'`, use it so project tools on `PATH` take precedence. Keep gopls
for interactive feedback; its extra suggestions are not automatically CI
policy. `gofmt` is authoritative for Go formatting; disable extra gofumpt
formatting in editor settings. Use `go tool templ fmt` for templates and
`go tool templ lsp` for their language server, from the module directory.
Do not maintain another enabled-rule list in editor settings or skills.

Errors assigned to `_` and unchecked type assertions are checked too. The only
function exclusions are four concrete strings.Builder/bytes.Buffer write methods
whose contracts always return nil errors; generic writers and cleanup are
checked. Fix real failures at their cause. A necessary suppression must name the linter and explain
the safe contract; generic error swallowing, blanket test exclusions, and
hand-editing generated files are not acceptable fixes. Modernization must
preserve behavior, especially loop-bound evaluation and serialization formats.

Upgrade `.golangci-lint-version` and inspect the new diagnostics in a reviewed
change; keep local/editor/CI versions aligned instead of following `latest` on
every run. CI reads this pin and uses the same `make check` as local development.

## Required checks on GitHub

The `CI` workflow runs on every pull request, pushes to `main` and `codex/**`,
and manual dispatch. It has no path filters or secret requirements. Its stable
job/check names are:

- `Go quality`: formatting, generation, shared lint policy, race tests, and
  production build.
- `Go vulnerabilities`: govulncheck.
- `Packaged converter`: tests using the packaged Pandoc version.

After this branch is pushed and these checks have run, configure a ruleset or
branch protection for `main` requiring a pull request and all three checks before
merge. Require the branch to be up to date before merging and prevent bypasses
where repository permissions allow it. Do not rename these jobs without updating
that protection. Keep the workflow running on every PR so required statuses are
always produced.

Workflow YAML makes failures visible; repository rules make them merge-blocking.
Protection is a GitHub setting, not something enabled by committing this file.
Activate it with this setup's rollout after the current OpenSpec work is ready;
this branch must not change protection or merge the in-progress implementation.
