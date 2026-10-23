# Tasks

## 1. Membership provenance and immutable artifacts

- [x] 1.1 Add a forward migration for recipient position/original-cell provenance and replace unique task/source-row membership with unique task/candidate membership; verify on temporary databases that two different recipients share a source row, a repeated candidate membership is rejected, and existing member/candidate/attempt IDs and claims survive migration.
- [x] 1.2 Extend intake snapshots, manifest/report entries and duplicate references with per-entry source coordinates and raw recipient tokens; implement version 2 writes and singleton version 1 reads at one boundary. Verify round trips, repeated source rows and old saved manifests without resplitting or mutating history.

## 2. Expansion, classification and publication

- [x] 2.1 Expand only the selected recipient mapping before duplicate grouping using independent mapped-value copies; preserve the full original cell for comparison. Add focused intake tests covering optional comma spacing, copied non-recipient values, single-address inputs, empty tokens/empty cells, ignored blank rows and unsupported separators.
- [x] 2.2 Enforce the 1,000-entry cap before exclusions and any publication, retaining file/XML/worksheet safeguards and replacing the earlier physical candidate-row gate. Extend the existing workbook/intake limit coverage for exactly 1,000 and 1,001 expanded entries, one cell with excessive entries, duplicate/invalid-heavy uploads and more than 1,000 rows with only unmapped data but at most 1,000 contributing entries; verify refused imports publish no partial candidate memberships or snapshot.
- [x] 2.3 Run existing trimmed-lowercase duplicate grouping over all expanded entries before validity/required-value checks; verify same-cell and cross-row duplicates all remain file-only, missing-required values cannot bypass grouping, existing campaign candidates are untouched, and valid siblings save independently of invalid entries.
- [x] 2.4 Replace source-row-only publication reasons and duplicate/exclusion report lookups with occurrence references; extend `TestValidatePublishesSnapshotAndCounts` and `TestExclusionReportDownloadsEveryHeldRow` to verify category totals, original-source-row totals, independent invalid/duplicate entries and copied source columns with collision-safe metadata labels. Document recipient syntax/limit/correction rules in the applicable input guidance alongside this work.

## 3. Rendering and task review

- [x] 3.1 Pass each saved entry's single raw recipient token through the existing renderer in task preview, selected-row test content and sending preparation. Add tests proving distinct To values and recipient-placeholder substitutions for siblings, original full-cell comparison visibility and immutable snapshots after corrected uploads; preserve single-address campaign sample-preview and diagnostic destination rejection.
- [x] 3.2 Update task upload guidance, Recipients tables/reasons/preview selectors, summary counts and campaign list tooltips to distinguish source rows from expanded entries. Update associated view/server tests for one row with two eligible and one invalid entry, same-row duplicates, protected report access and independent preview selection. Format/regenerate only changed `.templ` files.
- [x] 3.3 Verify Sending confirmations/progress count recipients independently while keeping all existing approval/start/schedule/recovery controls. Add a focused simulated-provider scenario sending two admitted siblings to separate recipients with correct content and distinct attempts; assert import/preview alone never dispatch and existing submitted/uncertain/reserved candidate rules still apply per email.

## 4. Recipient-level results workbooks

- [x] 4.1 Replace source-row-only member/result association in `internal/exports/task_xlsx.go` with per-entry identity and generate the separate `Recipient results` worksheet. Extend `TestTaskResultsWorkbookPreservesSourceAndReportsOutcomes` for two same-row recipients with different outcomes, file-only invalid/duplicate occurrences, stable source/position ordering and original worksheet/formula/value/order preservation.
- [x] 4.2 Add collision tests for case-insensitive existing worksheet names and source/result/metadata header conflicts, plus inert formula-like candidate text. Extend `TestTaskResultsKeepSourceColumnsAndCollisionFreeLabels`; verify regenerated singleton version 1 tasks and new single-address tasks use accurate recipient results without rewriting existing artifacts or attempts.
- [x] 4.3 Update Results guidance and relevant export/input documentation to describe preserved originals and the added recipient-results sheet; verify existing ownership, as-of snapshots, atomic publication, retry/recovery and regeneration tests remain valid and export generation never changes sending state.

## 5. Integration and required checks

- [x] 5.1 Run focused intake/campaign/web/export/sending tests as groups land using temporary data and simulated providers; perform a synthetic desktop/mobile task workflow covering mixed-validity cells, duplicate download, independent previews and mixed-outcome result download. Record actual results without real email, local `.env` or live database use.
- [x] 5.2 Run `make lint` during development, then the repository-required `make check`, `make vulncheck`, `make verify-converter` and `openspec validate --all --strict --json --no-interactive` after implementation. Run `npm ci` first on a fresh checkout and stage only intentional regenerated output before generation-consistency checks; record command outcomes and limitations, keeping tasks incomplete until behavior and checks pass.
