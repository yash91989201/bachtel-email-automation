# Design

## Context

- `preparation.Output` stores `Rows[].Values` (cleaned mapped values), `Rows[].Corrections`, `Rows[].Findings` and `Entries[]` with one occurrence outcome and rendered finding reasons.
- `finalization.ReviewPrepared` classifies cleaned values read-only and returns accepted entries plus exclusions carrying AI and backend reasons; `internal/finalization/report.go` already owns a collision-free column resolver and an inert text cell writer.
- `PreparedSheet` writes only accepted mapped values into a `Sending` worksheet because that file is re-uploaded and parsed by `parseSendingSheet`. `PreparedExclusionSheet` writes held entries with original and proposed mapped values.
- `internal/web/task.go` owns the protected `/excluded.xlsx` download and its precedence; `internal/web/finalization.go` owns the prepared sending sheet download and computes the review for the workspace.

## Goals / Non-Goals

**Goals:** Complete per-occurrence sanitization evidence in one download; identical eligibility to what the operator reviews; no stored artifact; unchanged prepared/final sending sheets.

**Non-Goals:** Changing preparation input/output, prompt and response contract, classification rules, the sending-sheet import contract, retention or deletion, or any sending authorization.

## Decisions

### 1. Separate workbook; sending sheets stay clean

Extra columns in the prepared sending sheet would be ignored by the importer, but that sheet is the artifact an operator edits and re-uploads, and its approved contract is accepted mapped values only. A separate file keeps audit metadata out of a sending artifact and out of the re-upload path.

Alternatives rejected: a second worksheet inside the sending workbook (still puts non-sending data in the sending artifact); adding status columns to the sending sheet (would need a spec change and invites operators to hand-edit status).

### 2. Derive from the selected run plus the existing review

The builder loads the task, its selected completed run, the pinned configuration and the mapped source rows, then reuses `ReviewPrepared`. Accepted entries, held reasons and counts therefore match the screen exactly; no second eligibility rule set exists. Original values come from `intake.MappedRows` (the stored workbook), prepared values from the run output.

### 3. Row granularity is the recipient occurrence

One row per `Output.Entries` occurrence keeps a shared finding on every sibling and a recipient-scoped finding on its address only. Corrections and findings are source-row evidence and repeat on each occurrence of that row, which keeps the sheet filterable by any single row.

### 4. Column set and collision handling

Metadata columns are source row, position, prepared recipient, prepared outcome, exclusion reasons, AI corrections and AI findings. For every configured mapping the sheet writes `<label> (original)` and `<label> (prepared)`, resolved against the metadata labels with the existing resolver so a mapping header that matches a metadata label never produces duplicate columns.

### 5. Read-only, inert, owner-scoped, un-stored

Built on request like the prepared sending sheet and exclusion report: never stored, no AI call, every cell written through the string cell API. The route sits behind `requireOperator` and the service refuses a run or task that does not belong to the operator through the existing loaders. The operator must have selected completed evidence; after `auto-select-prepared-run`, completion selects it, so the download needs no extra action.

## Risks / Trade-offs

- Long correction or finding lists repeat across sibling rows → acceptable for evidence; Excel filtering handles it, and the note bounds already exist per row.
- The generated file can differ from an earlier download if the selected run changes → the file is a snapshot at request time; approval still binds the stored run, and selecting another run clears the final snapshot.
- Large tasks cost one workbook build per download → same model as the existing prepared downloads; no new storage growth.

## Migration Plan

No schema, artifact or migration change. The route and view field ship together; removing them leaves prepared downloads unchanged.

## Open Questions

None.
