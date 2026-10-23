# History, exports and manual deletion

Every download is generated from recorded state and served through the
operator session. Generating, retrying or deleting a download never sends or
retries an email and never changes candidate eligibility.

## Task results workbook

A completed, stopped or recovered task carries the durable
`result_generation_needed` marker. A committed terminal-task update notifies the
shared, process-owned export worker, which consumes the marker and publishes
the workbook without a browser or recurring idle database polling. A stopping
task waits for final in-flight outcomes and its stopped state. At startup,
sending recovery finishes before the worker takes any export snapshot; the
worker also scans durable markers to recover a crash before notification.
The task page also offers **Generate results
workbook** when no ready file exists and **Regenerate** afterward. A failed
generation stores a fixed retry message and hides the file: an incomplete file
is never offered as complete, and retrying rebuilds from the retained records.
Ordinary failures, including failures during automatic recovery, wait for an
explicit **Generate results workbook** retry; idle periods and restarts do not
retry them. Temporary discovery or recovery-storage errors use capped backoff,
which stops after success and is cancelled during shutdown.

A crash or shutdown cancellation during generation is different: the durable
claim is requeued and rebuilt automatically on the next startup. This includes
previously requested campaign reports and packages. Recovery preserves ready
outputs and intentionally removed files, and does not create campaign exports
that were never requested. A recovered report/package pair uses one new
consistent snapshot; a completed sibling stays unchanged. Late result markers
committed while the dispatcher drains are discovered at the next startup.

The workbook is the retained original upload plus one added `Recipient results`
worksheet (or the first free `Recipient results (N)` when an input worksheet
already uses that name, compared case-insensitively). Every original worksheet,
row, cell value, formula and column order is preserved and untouched. The added
sheet copies the selected source columns and appends source worksheet, original
row, position in the recipient cell, individual recipient and the
task-snapshotted result labels, one row per expanded entry in source-row and
position order. Two recipients from one workbook cell therefore get two rows
with their own outcome; neither overwrites the other. Result and metadata labels
are resolved against both the source headers and each other, so a column is
never duplicated or overwritten. New values use the string cell API, so
candidate text that looks like a formula stays text.

With the default labels the columns after `Recipient` are:

| Column | Content |
| --- | --- |
| First result label | Recorded entry outcome |
| Second result label | Timestamp in `Asia/Kolkata` (`+05:30`) when one is recorded |
| Third and later labels | Error/exclusion reason; extra configured labels stay empty |

The first column can read `Submitted`, `Submitted (manual resolution)`,
`Definite failure`, `Authorization failure`, `Uncertain - review required`,
`Retry scheduled`, `Scheduled`, `In flight`, `Queued`, `Eligible - not sent`,
`Already submitted in this campaign`, `Already scheduled in another task`,
`Excluded - duplicate`, `Excluded - invalid address` or `Ineligible`. A manual
resolution shows the operator's recorded basis. Excluded entries come from the
private manifest, so duplicate and invalid entries appear with their source row,
position and raw recipient token without candidate records being created for
them. A saved version 1 manifest is read as singleton entries at position 1
without re-splitting its stored cells, so regenerating an older task keeps the
same outcomes.

## Campaign report

The campaign page provides two generated downloads, both built from one
consistent snapshot with an as-of time:

- **Campaign report (XLSX)** with four worksheets:
  - `Report` — campaign identity, as-of time and totals. Test attempts are
    counted separately and never inflate candidate totals.
  - `Candidates` — one row per campaign candidate: recipient, current
    submission state, claim, candidate-attempt count, submitted time, last
    outcome and last attempt time.
  - `Attempts` — every attempt, one row each: attempt id, kind (`candidate` or
    `test`), task, configuration, candidate, recipient, subject, state, reason,
    start/finish times, retry count, retry time, content hash and the latest
    manual resolution.
  - `Resolutions` — member, actor, verification basis, resolution time and
    action id.
- **Campaign package (ZIP)** containing:
  - `report.xlsx` — the report above from the same snapshot;
  - `emails/<attempt-id>.html` — one static HTML file per recorded attempt;
  - `index.csv` — attempt id, kind, task, candidate, recipient, subject, state,
    reason, sender, resolution, file path.

Archive entries are built from internal record ids only, so no uploaded value
can introduce a path separator or escape the package. Individual attempt HTML
is also linked from the campaign history table and served with a restrictive
content policy. The subject, sender, recipient and outcome metadata are
escaped; the body is the stored, already sanitized rendered body. A later
template, mapping or candidate change cannot alter it.

Generated files are private PocketBase file records with no public collection
rules. Every download route requires the operator session, answers 404 while
the file is not ready, and sends `Cache-Control: no-store`.

## Retention and deletion

Nothing expires automatically. Original uploads, revisions, manifests,
candidate snapshots, attempts and generated downloads stay until an explicit
operator action. Two scopes are separate:

- **Remove generated file(s)** deletes only the generated workbook, report or
  package. History, uploads and eligibility are unchanged, and the download can
  be generated again.
- **Delete campaign** removes the campaign, revisions and uploaded DOCX files,
  tasks and retained workbooks, candidate identities, attempts, approvals,
  manual resolutions and generated downloads. This includes the campaign's
  duplicate-prevention history, so a future campaign can send to the same
  recipients again. Deletion is refused while a task is running or stopping,
  while a candidate is reserved or in flight, or while an attempt is queued or
  in flight.

A third, separate deletion path is the mailbox replacement reset, which
deletes an operator's sending data across all campaigns when a different
mailbox is connected; its exact scope is documented in
[mailbox-replacement.md](mailbox-replacement.md).

The confirmation UI states both consequences before submission. Sources
referenced by retained tasks are not independently deletable: removing them
would break audit and regeneration, so they are only removed with the campaign.

Deleting live records never rewrites backups that already exist. Any
PocketBase or volume backup keeps its own copy until the operator removes that
backup separately. See [coolify-deployment.md](coolify-deployment.md).
