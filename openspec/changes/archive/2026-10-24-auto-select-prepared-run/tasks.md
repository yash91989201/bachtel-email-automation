# Tasks

## 1. Automatic selection at publication

- [x] 1.1 Extract the selection rule shared by `SelectRun` and worker publication in `internal/preparation`: select the completed run when the task has no selected final snapshot and the task is ready, unstarted, unscheduled and open, clearing snapshot fields only when the selected run actually changes. Keep publication settling the run even when selection is skipped.
- [x] 1.2 Select the run inside the `publish` transaction after the output and state are written. Verify with focused preparation tests that a completed run is stored as `selected_preparation_run`, that a completed run for a task with a selected final snapshot leaves the selection and snapshot digest unchanged, that a late publication for a closing task selects nothing, and that explicit selection of an older run still clears the final snapshot. Run `make test PACKAGES=./internal/preparation`.

## 2. Workspace follows the selected run

- [x] 2.1 Verify that the review counts, approve/upload availability and both prepared downloads appear on the first page load after completion without a `preparation/select` request, and that older runs still offer explicit selection with the existing snapshot-clearing effect. Extend focused `internal/web` preparation and finalization tests and run `make test PACKAGES=./internal/web TEST_FLAGS='-run TestPreparation'`.
- [x] 2.2 Update the preparation copy that treats selection as a prerequisite (run table, review heading, preparation help text) in `internal/web/views/task.templ`; format and regenerate only that file with the pinned templ tools. Verify the rendered page still names the selected run and keeps explicit selection for history.

## 3. Documentation and required checks

- [x] 3.1 Document the automatic selection rule, the finalized-dataset exception and the lifecycle refusals in `docs/final-sending-snapshots.md` and `docs/task-intake.md`.
- [x] 3.2 Run `make lint`, then `openspec validate --all --strict --json --no-interactive` after staging intentional generated output.
- [x] 3.3 Run `make check` and `make vulncheck` once on the integrated change and record actual results. Do not weaken checks to pass.
