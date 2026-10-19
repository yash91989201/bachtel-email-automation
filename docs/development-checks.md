# Development checks and reviews

Repository agreements live in `AGENTS.md`; product behavior remains in OpenSpec.
Project skills live in `.agents/skills/`. Use `$go-development` while coding,
alongside `$openspec-apply-change` when implementing a spec change. Invoke
`$go-review` for Go correctness or `$automation-reliability-review` for intake,
sending/recovery, and export integrity. These review skills report findings
without editing the application.
Oh My Pi discovers the same skills through `.omp/skills/` symlinks.

## Local checks

Use the Go version in `go.mod`, Node 26.10.0 (matching the Docker asset stage),
npm, Git, Make, and a C compiler for Go's race detector. Start a fresh checkout
with `npm ci`, then run:

```sh
make check
make vulncheck
make verify-converter
```

`make check` verifies Go/templ formatting without rewriting source, regenerates
committed templates, builds assets, runs the shared linter and race tests, and
builds the production binary. Stage intentional generated `*_templ.go` changes alongside
`.templ` edits before checking: generated output is compared with the Git index,
and new untracked generated files fail the check. Generated CSS/JS stay ignored.

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

Checks do not replace the delivery change's simulated end-to-end, redeploy,
backup/restore, browser, and explicitly authorized real-test release acceptance.
Keep candidate sending gated until that acceptance is complete.

## Shared Go policy

`.golangci.yml` is the single executable policy for Go source and tests. It
selects govet, staticcheck, unused, ineffassign, errcheck, modernize, and nolintlint.
Generated output is excluded using Go's strict generated-file convention; it is
still compiled and checked for consistency with its `.templ` sources.

Run `make lint` while coding. Both it and `make check` use
`bash scripts/golangci-lint.sh`, which reads `.golangci-version` and installs the
upstream release binary into the ignored `bin/` directory on first use. The
linter has no effect on application module dependencies. Installation needs curl
and network access; subsequent runs reuse the pinned binary.

For editor integration, configure your lint runner to execute
`bash scripts/golangci-lint.sh run --config .golangci.yml ./...` from the project
root, or point it at the pinned binary under `bin/golangci-lint/`. Keep gopls for
interactive feedback; its extra suggestions are not automatically CI policy.
Do not maintain another enabled-rule list in editor settings or skills.

Errors assigned to `_` and unchecked type assertions are checked too. The only
function exclusions are four concrete strings.Builder/bytes.Buffer write methods
whose contracts always return nil errors; generic writers and cleanup are
checked. Fix real failures at their cause. A necessary suppression must name the linter and explain
the safe contract; generic error swallowing, blanket test exclusions, and
hand-editing generated files are not acceptable fixes. Modernization must
preserve behavior, especially loop-bound evaluation and serialization formats.

Upgrade `.golangci-version` and inspect the new diagnostics in a reviewed change;
keep local/editor/CI versions aligned instead of following `latest` on every run.

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
