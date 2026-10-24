# Tasks

## 1. Sanitization workbook builder

- [ ] 1.1 Add a read-only sanitization workbook builder and service method in `internal/finalization`: load the selected completed run, reuse `ReviewPrepared` for occurrence outcomes and reasons, read original mapped values from `intake.MappedRows`, and write one row per prepared occurrence with source row, position, prepared recipient, outcome, exclusion reasons, source-row corrections and findings, and an original/prepared pair per mapping. Refuse a missing, incomplete, foreign or stale run without invoking AI and without writing any record.
- [ ] 1.2 Verify with focused `internal/finalizationtest` coverage that an accepted and a held occurrence of one row each appear once, a shared finding reaches every sibling while a recipient-scoped finding reaches only its address, a correction stays separate from findings, an original value differs from its prepared value in the report, formula-like input stays text, and the prepared sending sheet and exclusion report are byte-identical before and after the download. Run `make test PACKAGES=./internal/finalizationtest`.

## 2. Operator download surface

- [ ] 2.1 Add the protected `GET /campaigns/{id}/tasks/{task}/preparation/sanitization.xlsx` route and handler behind `requireOperator`, mapping a missing selected run to the existing download refusal path without leaking private task metadata.
- [ ] 2.2 Add the download link to the prepared review evidence in `internal/web/views/task.templ` with a one-line description of its contents, populated only while the review is available; format and regenerate only that file with the pinned templ tools. Verify with focused `internal/web` coverage that the link appears with a completed selected run, the download returns an XLSX containing the report sheet, an anonymous or foreign operator request returns no content, and the link is absent without a completed run. Run `make test PACKAGES=./internal/web TEST_FLAGS='-run TestPreparation'`.

## 3. Documentation and required checks

- [ ] 3.1 Document the sanitization report beside the prepared sending sheet and prepared exclusion report in `docs/final-sending-snapshots.md` and `docs/task-intake.md`: row granularity, provenance, inert text, and that it changes no sending state.
- [ ] 3.2 Run `make lint`, then `openspec validate --all --strict --json --no-interactive` after staging intentional generated output.
- [ ] 3.3 Run `make check` and `make vulncheck` once on the integrated change and record actual results. Do not weaken checks to pass.
