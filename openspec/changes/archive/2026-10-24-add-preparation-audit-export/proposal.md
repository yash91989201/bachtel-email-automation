# Proposal

## Why

A completed preparation run stores per-row corrections, unresolved findings and both the uploaded and cleaned mapped values, but the workspace renders only the first 100 notes and the prepared sending sheet deliberately carries accepted mapped values only. With 150 source rows and more than 200 corrections, the operator cannot audit a row's sanitization or hand that evidence to someone else.

## What Changes

- Add a protected **sanitization report** XLSX download for the selected completed preparation run, beside the existing prepared sending sheet and prepared exclusion downloads.
- One row per prepared recipient occurrence, accepted and held: source row, position, prepared recipient, backend outcome, every exclusion reason, the source row's AI corrections and AI findings, and each mapped value as an original/prepared column pair.
- Keep it a separate workbook. The prepared sending sheet stays accepted-values-only because it is re-uploaded; audit columns never enter a sending artifact.
- Generation reads stored run evidence and the pinned workbook only: no AI call, no value rewrite, no candidate, membership, attempt, approval or schedule change. Cells stay inert text and the download stays owner-authorized.
- Link it while the selected run's backend review is displayed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `history-exports`: add the prepared sanitization report: contents, provenance, inert text, refusal and no-side-effect behavior.
- `campaign-task-workspaces`: expose the download beside the prepared review evidence.

## Impact

- New read-only builder in `internal/finalization` reusing `ReviewPrepared`, the collision-free column resolver and the inert text writer of the exclusion report.
- New owner-authorized route in `internal/web`, one view field and a link in `internal/web/views/task.templ` with regenerated output.
- No new dependency, collection, migration or stored artifact. Downloads resolve the selected run, so this change assumes prepared evidence can already be selected; removing that selection click is scoped to `auto-select-prepared-run`.
- Out of scope: preparation, prompt/response validation, sending sheets, exclusion reports, retention, and any test, start or schedule authorization.
