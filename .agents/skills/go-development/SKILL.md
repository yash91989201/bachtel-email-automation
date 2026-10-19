---
name: go-development
description: Implement, fix, or refactor Go code in this repository using its OpenSpec decisions, pinned tooling, and mandatory quality checks. Use during coding, not for read-only reviews or planning-only requests.
---

# Go development

Before editing, read `AGENTS.md`, the applicable OpenSpec requirements and the
selected change's design/tasks. For an OpenSpec implementation, use its apply
workflow alongside this skill; this skill does not authorize unrelated scope or
turn an exploration into implementation. Check the current Go version in
`go.mod` and lint policy in `.golangci.yml` and `.golangci-version`.

Trace the affected code and callers using CodeGraph when this checkout is
indexed, otherwise source reads. Reuse existing patterns and dependencies.
The shared linter configuration defines mechanical rules; do not maintain a
second list here. Prefer supported modern idioms when semantics are preserved,
including the evaluation frequency of loop bounds. Handle errors at the layer
that can act on them; returning an error or ending a failed response can be more
appropriate than logging it repeatedly. Never expose credentials or candidate
content in diagnostics.

Use `make lint` for feedback while coding. It generates templates/assets and uses
the pinned linter wrapper; no global linter installation is needed. Edit `.templ`
sources and regenerate output. Keep formatting and mechanical modernization
separate from behavioral fixes when reviewing the diff.

Before declaring completion, run `make fmt`, stage intentional generated output,
and run `make check` and `make vulncheck`. Follow `AGENTS.md` for the packaged
converter and OpenSpec checks when applicable. Fix diagnostics at their cause and
rerun affected checks after changes. Do not disable rules, add blanket exclusions,
or assign errors to `_` to obtain a pass. Use a specific, explained suppression
only when the operation's contract justifies it; inspect the diff after autofix.

Add focused regression coverage for changed behavior and failure paths. Tests use
temporary stores and simulated providers, never local `.env`, production data or
real mail. Preserve OpenSpec acceptance scenarios even when a shorter solution
passes lint. Report the checks actually run and their results; if a check cannot
run, disclose the exact limitation instead of declaring a clean result.
