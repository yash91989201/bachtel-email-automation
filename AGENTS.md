# Project working agreements

## Scope and OpenSpec

- Read the applicable main specs in `openspec/specs/` and the selected change's
  proposal, design, delta specs, and tasks before implementation. Check current
  status rather than assuming planned features already exist.
- Implement the selected change and necessary supporting work only. Keep the
  established change order and prerequisites; do not enable candidate sending
  before the release verification in the delivery change.
- Keep behavior and acceptance scenarios in specs, technical decisions in
  design, and implementation/verification in tasks. Refine them in small loops
  as new facts emerge; surface conflicts before changing approved behavior.
- Preserve unrelated edits. Do not mark tasks complete until their behavior is
  implemented and verified. OpenSpec validation alone is not application testing.

## Code and dependencies

- For Go implementation, fixes, and refactoring, use the `go-development` skill
  alongside the selected OpenSpec workflow. Its canonical instructions live in
  `.agents/skills/go-development/SKILL.md`.
- `.golangci.yml` is the shared executable Go policy; `.golangci-version` pins
  its runner. Use the same wrapper/config in the editor, local checks, and CI.
  Upgrade tools and review new rules in a dedicated change, not during a fix.

- Follow existing package boundaries, naming, and error handling. Reuse existing
  code, standard packages, platform features, and approved dependencies before
  adding code or dependencies. Explain any new dependency's concrete need and
  pin its version; consult documentation matching the version in use.
- Add abstractions only for a current need. Small consumer-owned interfaces for
  external boundaries or meaningful tests are valid; speculative framework or
  repository layers are not.
- Simplicity must preserve security, data integrity, durability, accessibility,
  and acceptance scenarios. Ponytail governs simplicity; Caveman governs chat
  brevity. Neither removes required behavior or compresses persisted code/docs.
- Follow Go Code Review Comments (https://go.dev/wiki/CodeReviewComments).
  Format with gofmt and the pinned templ formatter. Edit `.templ` sources and
  regenerate committed `*_templ.go` output; never hand-edit generated output.
- When only some `.templ` files change, regenerate just those files with
  `go tool templ generate -f <file>` (after `go tool templ fmt <file>`), so
  unrelated `*_templ.go` output is not rewritten in the same change.
- Keep analyzer diagnostics clean in `.templ` sources: use tagged `switch` for
  multi-branch comparisons on one value, `maps.Copy` for attribute merges, and
  `slices.Contains`/`slices.ContainsFunc` for membership scans.
- Handle returned errors; explain intentional exceptions. Propagate cancellable
  contexts, bound outbound request time, close resources, and give each goroutine
  an owner and exit condition. Server-owned background jobs must not inherit a
  browser request's lifetime.
- Use database constraints and brief transactions for atomic invariants. Do not
  hold a transaction while waiting on network calls or pacing.
- Add focused behavioral tests for changed logic and regressions, including
  relevant failure paths. Use temporary databases/files and simulated providers;
  never load the local `.env`, production data, or real mailbox credentials in
  automated checks. Real email requires an explicit operator-designated test.

## Required verification

- Run `npm ci` on a fresh checkout, then `make check` and `make vulncheck`.
  `make check` covers formatting, generation consistency, lint (including vet),
  race tests, and the production binary build. Stage intentional regenerated
  output before checking generation consistency; do not stage unrelated work.
- Run `make verify-converter` for converter, campaign, dependency, Docker, or
  release changes. CI always runs this packaged-Pandoc check so a missing local
  converter cannot silently skip its coverage.
- When OpenSpec artifacts change, also run
  `openspec validate --all --strict --json --no-interactive`.
- Use `make lint` during development. Do not bypass error checking with `_` or
  blanket exclusions; any necessary `nolint` must name the rule and explain the
  operation contract that makes the exception safe.
- Never weaken a check to get a pass. Report commands actually run, results,
  skipped coverage, and remaining limitations. A local pass is not a hosted CI
  run or evidence of delivery, deployment, or completed release acceptance.

## Review workflows

- Use `$go-review` for requested Go correctness reviews and
  `$automation-reliability-review` for dispatch, intake/claim, recovery, or export
  reliability reviews. Their canonical instructions live in `.agents/skills/`.
- Reviews must distinguish implemented regressions from future OpenSpec tasks.
  Cite concrete failure paths and relevant acceptance scenarios; do not invent
  findings or mandate another architecture.

<!-- CODEGRAPH_START -->
## CodeGraph

When a `.codegraph/` directory exists at the repository root, use
`codegraph_explore` (or `codegraph explore`) before grep or file reads to locate
or understand indexed code. Use direct reads for unindexed docs/configuration
or explicitly stale files. Without `.codegraph/`, skip CodeGraph; do not index
this repository without a request.
<!-- CODEGRAPH_END -->
