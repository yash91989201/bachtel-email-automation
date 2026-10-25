# Development tooling research

Research date: 2026-10-25. Status: recommendation, not an implemented migration.

## Conclusion

Keep the existing Go/templ/htmx application and Make-based workflow. Adopt a
project-owned tool environment for commands that currently depend on a global
installation, and make the same environment available to CI, editors, and coding
agents. For this repository, mise is the best fit: it is already installed on
the development machine, supports project configuration and release binaries,
and has documented CI and editor integration.

This is an engineering recommendation for this repository, not a claim that
mise is a universal GOTH standard. The examined projects agree on generating
templ output before building, simple project commands, and a source watcher;
they differ in version management, layout, and optional tools.

Do not replace Make with a second task runner, move the linter into the
application module, or change the router/database/application architecture to
solve development-environment drift.

## Evidence from upstream projects

| Source | Observed setup | Implication for Bachtel |
| --- | --- | --- |
| [templ installation](https://templ.guide/quick-start/installation/) and [Go tool directive](https://go.dev/doc/modules/gomod-ref#tool) | templ documents project installation with `go get -tool` and invocation through `go tool templ`. Tool dependencies use the module graph. | Keep the existing module-managed templ CLI and runtime together. Keep shadcn-templ module-managed too. |
| [templ live reload](https://templ.guide/developer-tools/live-reload/) | Supports its own watcher/proxy and documents Air as an alternative. Air's example generates templates before compiling and excludes generated output from watching. | The current Air/Make arrangement is a supported pattern. There is no reason to add another watcher just to standardize tooling. |
| [Go-Blueprint HTMX/templ documentation](https://docs.go-blueprint.dev/advanced-flag/htmx-templ/) | Uses Make and generates templates before building; illustrates embedded assets. Its installation examples use `templ@latest`. | Reuse the workflow pattern, not its unpinned installation example. |
| [gohtmxapp Makefile](https://github.com/josephspurrier/gohtmxapp/blob/main/Makefile) and [Air configuration](https://github.com/josephspurrier/gohtmxapp/blob/main/.air.toml) | Make owns generation/assets/build; Air invokes the build and excludes generated Go files. Tool installation follows latest releases, and setup uses `npm install`. | This sample supports the build/watch structure, but is a weaker reproducibility baseline than Bachtel's existing pins and `npm ci`. |
| [Gowebly](https://github.com/gowebly/gowebly) | A configurable generator offering templ, Air, CSS/runtime choices and lint configuration. Its emitted template sources were subsequently inspected; see the scaffold comparison linked below. | Useful evidence of optional conventions, but its latest-version installs and older PocketBase/lint templates need review before adoption. |
| [golangci-lint installation](https://golangci-lint.run/docs/welcome/install/local/) | Recommends released binaries. Documents mise's aqua backend as installing GitHub release assets. Discourages source/tool-directive installs because build and dependency selection can differ from tested releases. | Keep golangci-lint independent of application dependencies; replacing the custom downloader with a standard binary manager is reasonable. |
| [mise configuration](https://mise.jdx.dev/configuration.html) and [CI integration](https://mise.jdx.dev/continuous-integration.html) | Project tool versions live in repository configuration; CI can use the same configuration and a committed lockfile. | Tool selection should be repository data, shared by local and CI processes. |
| [mise editor integration](https://mise.jdx.dev/ide-integration.html), [shims](https://mise.jdx.dev/dev-tools/shims.html), and [Mason configuration](https://github.com/mason-org/mason.nvim#configuration) | Shims select tools from the process working directory; `mise exec` supplies an environment to a command and its children. Mason prepends its own binaries by default, with append/skip alternatives. | A project config alone cannot override an editor's private executable selection. Use generic environment integration and verify PATH precedence. |

The starter comparison is qualitative, not a statistical survey of production
Go applications. Older `@latest` examples do not establish current best practice.
No external starter was installed or executed against this application.

## Current repository audit

### What already works well

- `go.mod` declares Go 1.27.2 and pins templ 0.3.1070. Its `tool` block declares
  templ and shadcn-templ. `Makefile` uses `go tool` for these tools.
- `package.json` pins the asset dependencies exactly; `package-lock.json` and
  `npm ci` govern installation. htmx and Alpine assets are copied locally for
  the application binary rather than requiring a runtime CDN.
- `Makefile` already provides setup, development, generation, assets, formatting,
  lint, tests, build, vulnerability analysis, and converter verification.
- `.air.toml` watches source, invokes Make generation/assets before building,
  and excludes generated output and data directories.
- `.golangci.yml` is one shared Go lint policy. The enabled rules are explicit;
  generated source is excluded from lint but remains compiled.
- `.github/workflows/ci.yml` invokes the same `make check` and `make vulncheck`
  commands documented for developers. Converter verification has its own job.
- Docker separates asset/build/runtime stages and pins Node, Go, and Pandoc
  versions. Production does not need the developer tool manager.

Sources: `go.mod`, `package.json`, `Makefile`, `.air.toml`, `.golangci.yml`,
`Dockerfile`, `.github/workflows/ci.yml`, and `docs/development-checks.md`.

### Gaps that the migration should address

1. **Executable selection is outside the project contract.** Make reads
   `.golangci-version` through `scripts/golangci-lint.sh`; OMP's installed
   auto-discovery resolves `golangci-lint` through PATH; Neovim initially picked
   its Mason installation. The same lint config was therefore used by different
   checker versions. The direct reproduction was 8 `errors.AsType` warnings
   with Mason's 2.14.0 versus zero with the repository's 2.15.0 on
   `./internal/web`. The earlier Neovim workaround routes through the wrapper,
   but adds a project-specific rule outside the repository.
2. **Go version declarations are not automatically exact environment pins.**
   This shell ran Go 1.27.3 while `go.mod`, CI setup, and Docker reference
   1.27.2. This does not itself prove incompatibility. Go permits newer
   toolchains; a reproducible development environment needs explicit selection.
   [mise's Go support](https://mise.jdx.dev/lang/go.html) can read an exact
   patch/toolchain request from `go.mod`, with idiomatic version-file support
   enabled locally. Go's own toolchain switching must also be accounted for.
3. **Format-on-save and the repository formatter differ.** Installed LazyVim
   defaults use goimports/gofumpt; `make fmt` and the formatting gate use gofmt
   plus `go tool templ fmt`. Gofumpt output may still pass gofmt, so this is a
   policy difference rather than proof of a failing check. Choose one intended
   formatting policy and make editor guidance match it.
4. **The aggregate check does not cover every existing frontend check.**
   `make check` runs `npm run test:js`, which selects
   `scripts/ai-setup.test.mjs`. It does not run the separate
   `test:tailwind` script or `lint:tailwind`. Decide whether canonical-class
   diagnostics are required or advisory before adding them to the gate; do not
   silently reclassify warnings or weaken checks.
5. **Tool pins have several owners.** The linter version is in a custom file,
   Air and govulncheck versions are embedded in Make recipes, and the Node
   runtime is in CI/Docker documentation. This can work, but the ownership and
   upgrade path should be explicit.

The repo's existing `docs/development-checks.md` already recognizes the need for
editor/linter consistency. The missing part is making that easy through normal
tool discovery, rather than relying on every caller knowing a custom wrapper.

## Recommended ownership model

| Concern | Authority | Expected invocation |
| --- | --- | --- |
| Go language/module dependencies | `go.mod` and `go.sum` | Normal Go commands; select the development toolchain from the module's exact version through mise. |
| templ and shadcn-templ | Existing Go tool directives and module versions | `go tool templ`, `go tool shadcn-templ`; templ LSP should use the module-managed CLI. |
| Node runtime and independent developer CLIs | Project `mise.toml` and `mise.lock` | Normal commands in the project environment; use released golangci-lint binaries. Consolidate Air/govulncheck pins here during migration rather than leave duplicate pin owners. |
| Asset packages | `package.json` and `package-lock.json` | `npm ci`, then npm scripts; do not reinstall them as global tools. |
| Go lint rules | `.golangci.yml` | `golangci-lint run --config .golangci.yml ./...`. |
| Formatting, generation, tests, build, aggregate quality gate | `Makefile` | Keep existing public Make commands; do not duplicate their recipes as mise tasks. |
| Packaged production dependencies | `Dockerfile` | Keep the separate runtime image and converter test; verify alignment with the tested development versions. |

The exact tested Go and Node versions in Docker must remain aligned with the
development/CI contract. Avoid introducing a script that rewrites Docker or CI
files at runtime; use simple explicit values plus a consistency check where
cross-file values must remain.

Use `mise exec -- make check` in noninteractive environments to make tool
selection explicit. Once installed/activated, normal `make` commands remain
the user-facing interface. Starting an assistant with `mise exec -- omp`
provides the same tool environment without patching its binary or launcher.

For Neovim, replace the project-specific wrapper override only after generic
mise/PATH integration is verified. Mason must not shadow project-owned linters;
its documented PATH append option is one possible generic integration. Verify
effects on other editor tools before changing it. Restart processes that
inherited an old environment. A GUI editor or a process operating across
multiple project directories needs working-directory-aware integration.

`gopls` remains useful for completion and interactive diagnostics. Extra gopls
analyses are not automatically the CI lint policy. Likewise, OMP cleanse is a
diagnostic-repair command: even with identical tool versions, its discovered
Go/ShellCheck checks are not equivalent to the application's complete quality
gate, browser acceptance, race suite, or packaged converter test.

## Alternatives considered

- **Retain the existing binary wrapper and document it everywhere.** Smallest
  change, and already reliable for Make/CI, but leaves command-discovery
  integrations dependent on adapters. Reasonable if adding a tool manager is
  undesirable; it does not meet this user's goal as cleanly.
- **Native Go tools for every CLI.** Good for templ, whose runtime and generator
  already share a version. Rejected for golangci-lint because upstream recommends
  release binaries and warns about dependency graph/build differences.
- **Dedicated golangci-lint module.** Upstream documents it as the safer option
  when insisting on `go tool`, while still discouraging that installation mode.
  It adds module files without fixing editor/PATH discovery on its own.
- **A dev container or Nix environment.** Can supply a stronger isolated
  environment, but adds infrastructure and editor workflows. No evidence from
  this issue requires that extra layer; preserve Docker for packaged checks.
- **Replace Make with Task/Just/mise tasks.** Task-runner choice is independent
  of the version mismatch. Rewriting working recipes would increase migration
  scope without resolving the core issue by itself.

## Migration and acceptance criteria

Implement as a dedicated tooling change, preserving the existing dirty
campaign/task templates and keeping application behavior unchanged.

1. Add a project mise manifest, pin independent CLI versions, preserve current
   versions during migration, and commit a lockfile covering the supported
   developer/CI platforms. Verify the installed mise version supports the
   selected configuration; pin the CI mise version too.
2. Retain module ownership of templ/shadcn-templ. Align the editor's templ
   language server with `go tool templ lsp`; resolve the formatter policy
   deliberately without an unrelated repository-wide formatting change.
3. Update Make to use the selected ordinary commands. Remove the custom linter
   downloader and `.golangci-version` after all references have migrated. Update
   project agreements, skill guidance, and development documentation together.
4. Configure CI to install the project environment and run the same Make
   commands. Preserve stable required-check names, dependency caches, race
   testing, vulnerability analysis, and converter verification.
5. Include existing frontend test suites in the aggregate test command. Make
   the required/advisory status of Tailwind diagnostics explicit.
6. Replace the earlier project-specific Neovim override with verified generic
   environment selection. Do not add an OMP-specific PATH shim or edit OMP's
   installed source.
7. Verify from both the project root and a nested package: selected executable,
   version, working directory, config, package scope, and parsed diagnostics.
   Compare terminal, Neovim, OMP checker invocation, and CI configuration.
8. Check a controlled temporary unchecked-error example: terminal/editor/OMP
   must all report the real finding using the same linter version, and become
   clean after it is fixed. A clean existing package alone is insufficient to
   verify diagnostic propagation. Do not start automatic repair agents against
   unrelated dirty application work as part of this test.
9. Verify fresh installation with no cached linter, then run the repository's
   required integrated checks: `make check`, `make vulncheck`, and
   `make verify-converter` for the dependency/CI tooling migration. Run focused
   shell/config checks first. Distinguish local results from actual hosted CI.

## State of this research

No project tooling migration, tool upgrade, deployment, live data operation,
or email send was performed. No new full test suite was run for this report.
The previously authorized Neovim override remains in place until replacement
is implemented and verified. The OMP launcher is unchanged.

This document records inspected upstream documentation and current repository
configuration. It does not claim that a proposed mise configuration has already
been installed, locked, or validated across editors and CI.

See [the scaffold comparison](goth-scaffold-research.md) for the separate
evaluation of Better-T-Stack-like generators and starter repositories.
