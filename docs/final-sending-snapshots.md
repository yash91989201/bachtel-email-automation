# Final sending snapshots

A task does not send the raw uploaded workbook and does not send AI output
directly. The operator reviews prepared values and accepts one immutable final
dataset. Preview, test content and candidate work all read that same dataset.

## Validation is a preflight

For a revision that uses the current role mapping, validation pins the worksheet,
header row and campaign revision and records the source-row count. It creates no
candidate, membership, validation manifest, exclusion report, category count or
selected dataset, and it does not judge row eligibility: duplicate, address,
required/optional, campaign-history and safety outcomes belong to the reviewed
cleaned dataset. A missing or ambiguous mapped header still blocks the task, and
the task then stays in staging with no pinned revision.

A task in that state has no memberships, so nothing is claimable and an explicit
start or schedule is refused with the finalization remedy. The workspace reports
**Eligibility pending** with the source-row count — never "No eligible
recipients", which describes a dataset whose entries were all held.

Legacy format-mapping revisions keep their historical validation snapshot:
memberships, duplicate and invalid-address exclusions, category counts, manifest
and exclusion report are still written at validation and stay downloadable.

## Operator sequence

1. Upload and validate the workbook. The campaign revision pins the mappings.
2. Prepare mapped values with the confirmed AI connection and review
   corrections and unresolved findings. A qualifying completed run becomes the
   selected prepared evidence automatically (see **Accepted values and
   provenance**); explicit selection stays available for older runs and for
   finalized datasets.
3. Review the selected run. The backend review classifies the cleaned values
   with the same rule set approval uses, returns accepted entries and held
   entries with every applicable reason, and writes nothing: no candidate,
   membership, attempt, approval or schedule changes.
4. Finalize the sending data by one of two paths:
   - **Approve as-is** reclassifies the accepted prepared values inside its
     publication transaction after fresh backend required/optional, address,
     duplicate, campaign-history and rendering safety checks. It refuses a run
     whose source digest no longer matches the task's pinned digest, so a
     re-uploaded or tampered workbook cannot be approved through old evidence.
   - **Edited final upload** accepts a downloaded sending sheet with edits,
     removals, additions or restored entries. It runs deterministic backend
     validation only: no AI, no recasing and no list normalization. Commas are
     the only email-list delimiter.
5. Download the accepted final sheet or its exclusion report. A replacement
   upload always creates a new snapshot identity, even when its bytes match.
   The exclusion download resolves the selected final snapshot first, then the
   selected prepared review, then the legacy validation report.
6. Send a formatting test from a row of that exact snapshot and approve the
   received formatting. Dataset approval is not formatting approval and is not
   sending authorization.
7. Start or schedule explicitly. An enabled schedule freezes the dataset until
   it is cleared.

## Accepted values and provenance

Approval publishes one membership per accepted entry, and each membership stores
the accepted cleaned values that preview, test and sending render. The uploaded
original cell is never rewritten: it stays in the stored workbook and, for a held
entry, in the review evidence beside the cleaned proposal. Held entries create no
membership and remain visible only in the report and the review.

Selecting a run for review is not approval, and a repeated approval action ID
returns the same snapshot instead of creating a second one.

Publishing a completed preparation run stores it as the task's selected
prepared evidence in the same transaction, whenever the task is ready,
unstarted, unscheduled, open and has no selected final snapshot. Automatic
selection changes no candidate, membership, attempt, approval or schedule
state and invokes no AI. On the workspace, the backend review counts,
approve/upload controls and prepared downloads then refer to that run on the
next page load without a select request; explicit selection remains available
for older runs and for finalized datasets.

A finalized dataset keeps its evidence: while a final snapshot is selected, a
completing run changes neither the selected prepared evidence nor the snapshot
with its digest. Explicitly selecting another run still replaces the evidence
and clears the selected final snapshot, its digest and the finalization error,
so a fresh approval is required. Failed, incomplete or cancelled runs select
nothing; a task that is staging, started, scheduled, closing or closed selects
nothing from a completing run, and a late publication for a closing task is
cancelled and publishes nothing.

## Historical transition

- Revisions written before mapping versions keep their recorded format
  behavior. Their stored hashes and rendered evidence are not rewritten, and
  their validation-time memberships, counts, rows and exclusion report stay
  exactly as published.
- New revisions use `variable | source header | role | fallback` and no format
  choice. A legacy-shaped submission is rejected with a remedy.
- A current-revision task that was already validated before this workflow keeps
  its existing evidence until an explicit revalidation. Revalidation repeats the
  preflight and clears the previously selected preparation run, final snapshot,
  finalization error and memberships; navigation alone changes nothing.
- Unstarted legacy tasks can enter the snapshot workflow by preparing data or
  uploading an edited final sheet. Nothing calls AI and no approval is
  manufactured during migration.
- Tasks that already started keep their pinned content, attempts and existing
  authorization scope. They cannot rerun preparation, replace the final sheet
  or acquire a new snapshot. Different content requires a new task.
- The snapshot workflow is required for revisions created with mapping version
  2. A task under such a revision cannot start or schedule before finalization,
  and its test approval must name the selected snapshot.

## Deployment and rollback guards

1. Take a normal database and private-files backup before deploying.
2. Stop the dispatcher, export worker and preparation worker before changing
   binaries. The application already drains them on termination.
3. Deploy the new binary and migrations together. New collections retain
   snapshot entries, final sheets, exclusion reports and exact approvals as
   audit evidence.
4. For rollback, stop all workers and disable new-workflow dispatch first.
   Restore a compatible backup; do not drop snapshot or approval fields in
   place. An older binary does not understand snapshot-bound approval and must
   never dispatch new-workflow tasks.
5. Never treat a rollback or a missing test record as proof that an email was
   not submitted. Persisted attempts remain the audit record.
6. Real provider evaluation requires explicit operator authorization. Automated
   checks use temporary databases and simulated providers only.
