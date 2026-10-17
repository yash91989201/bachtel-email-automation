# Tasks

Prerequisites: add-operator-and-microsoft-auth and add-campaign-template-authoring. Implement third. Dispatch is supplied by change 4.

## 1. Workbook intake and validation artifacts

- [ ] 1.1 Add pinned Excelize and a private XLSX upload/worksheet/header selection flow; verify a multi-sheet fixture, chosen header row and 1,000-row workbook with no Google calls.
- [ ] 1.2 Enforce file/expanded-resource bounds and campaign header validation; verify corrupt, encrypted, unsupported and 1,001-row files plus missing/ambiguous mapped columns fail before candidate admission.
- [ ] 1.3 Preserve source bytes/digest and atomically publish a row-validation manifest; verify original cells/row coordinates remain unchanged and interrupted publication cannot mark a task ready.
- [ ] 1.4 Document XLSX support, source-header matching, formula cached-value behavior and configurable resource limits; verify the upload screen shows those constraints and actionable errors.

## 2. Immediate duplicate UX

- [ ] 2.1 Normalize all recipient keys and classify complete duplicate groups before row filtering or database upserts; verify case/space variants, identical copies and duplicate-plus-missing-value cases hold every member.
- [ ] 2.2 Generate an immediate duplicates XLSX using original columns plus row/reason metadata; verify it is downloadable before sending and every duplicate row is included.
- [ ] 2.3 Keep duplicate details file-only and protect the report endpoint; verify candidate/task-candidate/attempt counts remain unchanged even when a duplicate address already exists in the campaign.
- [ ] 2.4 Document correction by new upload and show counts/grouped row numbers; verify unique rows remain usable while a duplicate report is downloaded.

## 3. Campaign identity and snapshots

- [ ] 3.1 Add task, campaign-candidate and task-candidate migrations with a unique campaign/email index; verify same-campaign reuse, cross-campaign independence and no input ID requirement.
- [ ] 3.2 Persist raw/normalized immutable membership snapshots for admitted valid-address rows and file-only errors for invalid addresses; verify missing required values exclude only affected rows.
- [ ] 3.3 Implement transactional eligibility/reservation used by later start/resume/retry actions; verify simultaneous claims for one campaign/email admit only one owner.
- [ ] 3.4 Preserve submitted/uncertain/active history precedence and safe claim release; verify corrected uploads cannot clear holds or change old snapshots and imported status cells are ignored.
- [ ] 3.5 Document email-key matching and changed-address behavior; verify the UI explains Already submitted, Already scheduled and Review required exclusions.

## 4. Task review

- [ ] 4.1 Add protected review/preview views with candidate totals and row-specific reasons; verify selecting any eligible candidate renders the pinned subject/body and raw/normalized values.
- [ ] 4.2 Resolve result-label collisions without overwriting source columns; verify the chosen labels appear in the task snapshot and preview.
- [ ] 4.3 Add current-configuration revalidation and a reusable final-start preflight contract for change 4; verify edits invalidate an unstarted task's stale preview while running/past snapshots remain unchanged.
- [ ] 4.4 Permit immediate corrected-task creation and zero-eligible validation downloads; verify no zero-email start action is offered and regenerated templ sources are committed.

## 5. Integration acceptance

- [ ] 5.1 Run make test and SQLite-backed intake/claim checks; exercise a 1,000-row mixed fixture and verify unique candidate counts, duplicate non-persistence, corrected-upload history and private downloads.
