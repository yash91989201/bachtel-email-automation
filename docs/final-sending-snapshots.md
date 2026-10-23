# Final sending snapshots

A task does not send the raw uploaded workbook and does not send AI output
directly. The operator reviews prepared values and accepts one immutable final
dataset. Preview, test content and candidate work all read that same dataset.

## Operator sequence

1. Upload and validate the workbook. The campaign revision pins the mappings.
2. Prepare mapped values with the confirmed AI connection, review corrections
   and unresolved findings, and select the completed run.
3. Finalize the sending data by one of two paths:
   - **Approve as-is** seals the accepted prepared values after fresh backend
     required/optional, address, duplicate, campaign-history and rendering
     safety checks.
   - **Edited final upload** accepts a downloaded sending sheet with edits,
     removals, additions or restored entries. It runs deterministic backend
     validation only: no AI, no recasing and no list normalization. Commas are
     the only email-list delimiter.
4. Download the accepted final sheet or its exclusion report. A replacement
   upload always creates a new snapshot identity, even when its bytes match.
5. Send a formatting test from a row of that exact snapshot and approve the
   received formatting. Dataset approval is not formatting approval and is not
   sending authorization.
6. Start or schedule explicitly. An enabled schedule freezes the dataset until
   it is cleared.

## Historical transition

- Revisions written before mapping versions keep their recorded format
  behavior. Their stored hashes and rendered evidence are not rewritten.
- New revisions use `variable | source header | role | fallback` and no format
  choice. A legacy-shaped submission is rejected with a remedy.
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
