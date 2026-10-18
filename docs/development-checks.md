# Development checks and reviews

Repository agreements live in `AGENTS.md`; product behavior remains in OpenSpec.
The two project review skills live in `.agents/skills/`. Invoke `$go-review` for
Go correctness or `$automation-reliability-review` for intake/sending/recovery and
export integrity. Each produces findings rather than editing the application.
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
committed templates, builds assets, runs vet and race tests, and builds the
production binary. Stage intentional generated `*_templ.go` changes alongside
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

## Required checks on GitHub

The `CI` workflow runs on every pull request, pushes to `main` and `codex/**`,
and manual dispatch. It has no path filters or secret requirements. Its stable
job/check names are:

- `Go quality`: formatting, generation, vet, race tests, and production build.
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
