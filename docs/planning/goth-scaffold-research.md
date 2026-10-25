# Go/templ/htmx scaffold comparison

Research date: 2026-10-25. Scope: project generators and starters comparable to
Better-T-Stack, with particular attention to Bachtel's PocketBase backend,
templ, htmx, Tailwind, and locally owned shadcn-templ components.

## Recommendation

For a new project, shortlist **Gowebly for backend choices**,
**Go-Blueprint for a conventional Go application skeleton**, and
**shadcn-templ/create for UI scaffolding**. They cover different parts of the
experience. None of the inspected candidates supplies a verified drop-in
replacement for Bachtel's complete application and quality workflow.

For this existing repository, use shadcn-templ's current scaffold as a reference
for the component/build contract, retain PocketBase and the existing Make/CI
workflow, and implement the tool-environment improvements from
[the tooling research](development-tooling-research.md). Re-scaffolding the
application would discard working domain behavior without solving executable
selection on its own.

## What is being compared

[Better-T-Stack](https://www.better-t-stack.dev/docs) provides interactive CLI
choices, noninteractive flags, a visual stack builder, and choices for frontend,
backend, database, ORM, auth, and addons. Its listed stack options are
TypeScript-oriented, not Go/templ. The useful comparison is the quality of
project composition and developer setup, not similarity of the names.

A full application generator, a cloneable starter, and a component registry
are different products. A generator's own CI and release process also do not
prove that every generated project has equivalent lint/testing coverage.

## Shortlist

| Candidate | Kind and verified features | Fit for Bachtel | Assessment |
| --- | --- | --- | --- |
| [Gowebly](https://github.com/gowebly/gowebly) | Interactive Go web generator; templ, htmx, Alpine/hyperscript choices, Tailwind/CSS options, multiple routers including PocketBase, Air, optional lint config. | Closest feature menu to the existing stack. | Closest general generator analogy, but the inspected PocketBase and lint templates are older than the APIs/policy in this repository. Do not adopt unchanged. |
| [Go-Blueprint](https://github.com/Melkeydev/go-blueprint) | Configurable Go skeleton with routers, databases, optional HTMX/templ, Tailwind, Docker, and GitHub Actions. | Useful Make/Air/build organization; PocketBase is not a documented backend option. | Useful for a new conventional Go web app, not a PocketBase migration tool. Published and current source templates still install some tools at latest. |
| [shadcn-templ/create](https://shadcn-templ.com/docs/installation) | Visual style/color/font/icon presets, `init -t templ`, copied component ownership, Task-based dev/build commands, optional monorepo. | Same component tool already pinned by Bachtel. | Most relevant current UI scaffold. It does not provide Better-T-Stack's backend/database/auth composition. |
| [templUI quickstart](https://github.com/templui/templui-quickstart) | Cloneable UI starter; Task, templ proxy, Tailwind, Docker. | Adjacent to the component ecosystem already in use. | The current templUI docs identify shadcn-templ as the 2.0 successor. Prefer current shadcn-templ guidance for this repo, rather than moving to the older import workflow. |
| [templ-quickstart](https://github.com/phillip-england/templ-quickstart) | Small cloneable Go/htmx/templ/Tailwind skeleton with separate watcher commands and Air. | Useful introductory example. | Not a broad configurable generator or a stronger quality/tool-pinning baseline. |
| [PocketBase/htmx/templ/Tailwind template](https://github.com/maxpaulus43/go-pocketbase-htmx-templ-tailwind) | Cloneable starter using the same broad stack, Air, and Docker. | Exact technology names match. | Its documented Go 1.21 era setup and latest global installs make it a historical reference, not an upgrade baseline. |

## Maintenance and licensing evidence

Dates below come from GitHub repository/release API responses, not inferred
years from relative dates. Repository push activity does not prove that a
particular template was updated or that the generated output compiles.

| Candidate | Verified release/activity | License evidence |
| --- | --- | --- |
| shadcn-templ | [v2.0.0-beta.15](https://github.com/axadrn/shadcn-templ/releases/tag/v2.0.0-beta.15), 2026-10-15. The name/docs identify it as beta even though the release API's prerelease flag is false. | [MIT](https://github.com/axadrn/shadcn-templ/blob/main/LICENSE). |
| Gowebly | [v3.1.1](https://github.com/gowebly/gowebly/releases/tag/v3.1.1), 2026-04-20; repository last push reported 2026-04-18. | [Apache-2.0](https://github.com/gowebly/gowebly/blob/main/LICENSE). |
| Go-Blueprint | [v0.10.11](https://github.com/Melkeydev/go-blueprint/releases/tag/v0.10.11), 2025-07-10; repository last push reported 2026-04-26. | [MIT](https://github.com/Melkeydev/go-blueprint/blob/main/LICENSE). |
| templ-quickstart | Repository last push reported 2024-08-11. | No license was returned in repository metadata; permission terms need separate review before copying substantial source. |
| PocketBase template | Repository last push reported 2024-01-16; repo page reports no releases. | [Apache-2.0](https://github.com/maxpaulus43/go-pocketbase-htmx-templ-tailwind). |

The two small templates' age is supporting context. The substantive reason not
to use them as the new baseline is the inspected setup, not their star count.

## Source-level findings

### shadcn-templ: the strongest existing connection

Bachtel already pins `github.com/axadrn/shadcn-templ/v2` to
`v2.0.0-beta.15`. Local read-only checks confirmed `go tool shadcn-templ version`
and that `init --help` exposes the templ project and monorepo scaffold flags.

The current [installation guide](https://shadcn-templ.com/docs/installation)
offers a visual preset builder and both new/existing-project workflows. Its
[actual Taskfile template](https://github.com/axadrn/shadcn-templ/blob/191c364eabdfb6e6b75f2733163634db2903a06d/cmd/shadcn-templ/templates/templ-app/Taskfile.yml)
runs three dev watchers: templ/server/proxy, Tailwind, and component script
bundling. Production builds CSS, bundles component scripts, generates templ,
then builds Go. The
[module template](https://github.com/axadrn/shadcn-templ/blob/191c364eabdfb6e6b75f2733163634db2903a06d/cmd/shadcn-templ/templates/templ-app/go.mod.tmpl)
declares templ and shadcn-templ as Go tools.

These support Bachtel's existing generation and bundling choices. The inspected
Taskfile does not define an aggregate lint/test/vulnerability gate, and expects
Task and Tailwind commands to be available. It therefore does not replace the
tool-environment work. It also does not select or migrate PocketBase/auth.
Do not run `init` over the application merely to acquire these conventions.

### Gowebly: broad choices, concrete template drift

Inspected current main commit:
`08d7f8dc1c260eb8f0679b8e90e9fc9512f72380`.

- The [module template](https://github.com/gowebly/gowebly/blob/08d7f8dc1c260eb8f0679b8e90e9fc9512f72380/internal/attachments/templates/backend/go.mod.gotmpl)
  emits Go 1.26.1 and uses `latest` placeholders for selected modules. The
  [package template](https://github.com/gowebly/gowebly/blob/08d7f8dc1c260eb8f0679b8e90e9fc9512f72380/internal/attachments/templates/frontend/package.json.gotmpl)
  also uses `latest` for htmx, Tailwind, Parcel and optional packages.
- The [Make template](https://github.com/gowebly/gowebly/blob/08d7f8dc1c260eb8f0679b8e90e9fc9512f72380/internal/attachments/templates/misc/Makefile.gotmpl)
  provides race tests/build tasks but invokes templ at latest. Its optional
  linter command uses the older non-v2 module path at latest. The
  [lint template](https://github.com/gowebly/gowebly/blob/08d7f8dc1c260eb8f0679b8e90e9fc9512f72380/internal/attachments/templates/misc/golangci.yml.gotmpl)
  uses the older configuration shape and disables analysis of tests. This is
  not equivalent to Bachtel's current v2 policy and needs compatibility review.
- The [PocketBase server template](https://github.com/gowebly/gowebly/blob/08d7f8dc1c260eb8f0679b8e90e9fc9512f72380/internal/attachments/templates/backend/pocketbase/server.go.gotmpl)
  uses `OnBeforeServe().Add` and Echo-era routing/middleware. Current
  [PocketBase Go routing docs](https://pocketbase.io/docs/go-routing/) use
  `OnServe().BindFunc`. A selectable PocketBase option is therefore not proof
  that this output fits the current PocketBase version in Bachtel.

These are source-template observations, not an executed-generator failure.
Post-generation dependency resolution can change module files; it does not
by itself demonstrate that an old hook API or lint policy has been updated.

### Go-Blueprint: conventional skeleton, not a reproducibility upgrade

Inspected current main commit:
`81f56f8c24637d2fd2adb5f37071c6d4cb72b571`.

The [Make template](https://github.com/Melkeydev/go-blueprint/blob/81f56f8c24637d2fd2adb5f37071c6d4cb72b571/cmd/template/framework/files/makefile.tmpl)
generates templ and CSS before compiling and supports Air watch. It also
installs templ/Air at latest and downloads Tailwind from the latest release.
The [Docker template](https://github.com/Melkeydev/go-blueprint/blob/81f56f8c24637d2fd2adb5f37071c6d4cb72b571/cmd/template/advanced/files/docker/dockerfile.tmpl)
pins a Go image but installs templ and Tailwind at latest. These findings
confirm the caveat in the public docs with actual current template source.

Its value is scaffolding familiar Go structure and selectable infrastructure.
Replacing Bachtel's already pinned Go tools, npm lockfile, and quality gate with
these installation defaults would make reproducibility weaker.

## Application to this repository

Borrow conventions selectively: generated assets before compilation, source-only
watching, module-managed templ/component tools, and a clear dev/build interface.
Keep the existing application architecture, local asset packaging, and
simulated-provider/race/converter tests.

Use a project tool environment to make ordinary command discovery consistent.
A scaffold cannot force an existing editor's Mason binary or an already-running
agent to use that environment. Generic editor/PATH integration remains part of
the setup even with an excellent starter.

For a future reusable Bachtel-style starter, first finish and verify the tooling
migration in this app. Only then extract the minimal configuration into a
template. Creating a new generator now would add another tool to maintain
before its output contract is proven.

No candidate scaffold was installed or generated over the repository. This is
a documentation/source audit, not a compile/run/security review of every
generator combination. The concrete implementation recommendation remains the
dedicated project-tooling migration described in the companion research.
