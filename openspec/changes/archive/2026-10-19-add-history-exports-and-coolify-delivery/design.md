# Design

## Context

See proposal.md. Changes 1-4 provide private files, immutable campaign/task data, duplicate manifests, exact attempt content and terminal-task export markers. Dockerfile already builds one non-root Go service; compose.yaml already mounts /app/pb_data and has a 30-second stop grace. This is change 5 and the production release gate.

## Goals / Non-Goals

**Goals:** Reuse stored data to create faithful downloads; retain history until explicit deletion; make Coolify redeploy/restore behavior verifiable.

**Non-Goals:** New export services, PDF/EML, scheduled retention, distributed storage, automatic external backups or automatic real candidate tests.

## Decisions

### Existing libraries and snapshot exports

Reuse Excelize from change 3 for task/campaign XLSX, html/template for safe static HTML wrappers and archive/zip for packages. No custom spreadsheet writer, ZIP format implementation or document service.

Task XLSX is based on a retained original workbook copy: preserve original cells, worksheets and ordering, and append status columns only to the selected sheet. Combine admitted-row snapshots and duplicate/invalid-row file manifests by original row coordinate. Use collision-free labels already resolved by intake. Do not normalize the original workbook in place.

Campaign XLSX contains Candidates and Attempts sheets and a small metadata/as-of section, keeping tests explicitly separate. Include task/configuration IDs and manual-resolution basis. Store new text cells with string APIs so formula-like candidate text is not reinterpreted. Full email HTML belongs in its own file, not a potentially truncated spreadsheet cell.

Each attempt HTML uses its stored personalized subject/body and real outcome, not the current renderer or candidate values. Escape metadata and allow only the already sanitized stored body, never arbitrary uploaded HTML. ZIP entries use internal candidate/attempt IDs and a report index; avoid unsafe path segments. Include recorded failed/uncertain attempts with accurate labels, distinguishing them from submitted candidate emails and tests.

Read export source data under a consistent database snapshot, capture as-of metadata and write outside the transaction. Build temporary files in the private persistent artifact directory, then atomically publish and mark an export ready only after successful completion. Store artifact generation state/digest; retries regenerate only files. Reuse the immediate duplicate-report route from change 3 rather than delaying that UX until this change.

### Event-driven exports and startup recovery

The original implementation scanned task markers every five seconds, used separate worker/handler services, and marked interrupted exports failed. The follow-up replaces those behaviors with the shared service and recovery lifecycle below.

Use one shared, process-owned export service and one serial background worker. Retain durable task-result markers and artifact generation state as the source of truth. A buffered, nonblocking Go channel is only a coalesced wake-up signal: each activation scans eligible pending work, so multiple notifications may safely collapse into one. No external queue, broker, database service or recurring idle timer is needed.

Register a collection-scoped PocketBase OnRecordAfterUpdateSuccess hook before dispatcher recovery can save task state. Notify only for committed tasks with result_generation_needed set and a completed, stopped or interrupted status. Do not rely solely on the marker changing from false to true: Stop can set it while the task is still stopping, before final settlement makes generation eligible. The hook signals the shared worker and returns; it performs no workbook generation and must not block sending or open a new export transaction. Rolled-back changes do not signal work. Export generation runs outside the task transaction and browser request lifetime.

At startup, construct the shared service and register notifications before starting the dispatcher, but launch the export worker only after dispatcher startup has successfully completed sending recovery. Recovery notifications can accumulate in the buffered channel; they must not start export snapshots early. The worker then recovers interrupted artifact generation and scans durable pending task markers once. Notifications registered before recovery close the startup race, and the startup scan closes the crash window between a committed marker and its notification. Once work is drained, wait for a notification or shutdown rather than polling. If discovery or recovery persistence fails temporarily, retain the pending state and schedule an error-triggered retry with capped backoff; cancel that retry after success so a healthy idle server performs no recurring discovery queries.

Use the existing text state field's `pending` value to durably requeue interrupted claims in one startup transaction; ordinary failed states are never requeued. Recovery claims only pending campaign roles, leaving absent, ready and failed siblings untouched. Both recovered roles share one snapshot when both are pending. Claim report/package roles atomically, using one captured timestamp, so a browser action and recovery cannot split ownership. A shared generation read/write lock lets startup recovery wait for any active browser generation before reclaiming its claims; it also protects recovery retries after temporary storage errors.

Distinguish generation interrupted by crash or shutdown cancellation from an ordinary generation failure. Preserve or durably requeue interrupted work so the next startup can claim and rebuild it automatically; do not turn shutdown cancellation into an ordinary failed state that requires operator action. Recovery includes task results and campaign report/package generation that was already requested and interrupted. It does not initiate new campaign exports, rebuild already ready outputs, or recreate intentionally deleted files. Preserve the existing consistent-snapshot contract for a recovered campaign report/package pair and do not overwrite an unrelated completed output just to recover its interrupted sibling.

Use the existing transactional artifact claim and atomic publication boundaries to prevent duplicate generation or incomplete downloads. Deleting task results clears their durable generation marker in the same transaction, including when the deleted artifact was failed or pending. Automatic task generation rechecks that marker behind the shared deletion barrier, so an earlier discovery cannot recreate an intentionally removed file. A normal generation failure, including a failure during automatic recovery, remains failed with a visible explicit Retry action; startup must not repeatedly requeue it. Error-triggered discovery retries are distinct from retrying a failed file generation. Export activity never creates attempts, changes sending eligibility, or resumes sending.

During shutdown, leave durable unfinished work recoverable within the existing bounded lifecycle. Markers committed by the dispatcher while draining remain discoverable on the next startup even if the export worker has already stopped. Keep notifications nonblocking during termination and never close a channel while a registered hook can still signal it. Existing manual task regeneration and new campaign-export requests remain explicit actions; this revision does not turn every export endpoint into a new background-job API.

### Retention and scoped deletion

Keep original sources, revisions, admitted candidate snapshots, attempts and artifacts without a TTL. Allow removal of generated files without clearing history; regeneration remains possible. Templates/source files referenced by retained tasks are not independently removable in a way that breaks audit/export.

Campaign deletion explicitly includes its associated records/files and its duplicate-prevention history. Require confirmation and reject deletion while claims or in-flight work still exist. Implement guarded record deletion plus retryable file cleanup, not implicit database cascade without file handling. Explain that independent backups can retain older data; no external backup is rewritten by live deletion.

### Single Coolify deployment using current container foundation

Keep one Go application instance and one persistent data volume; Pandoc was added to the runtime image by change 2. Store database, private uploads, artifacts, ownership lock and dispatcher metadata under /app/pb_data. Keep tokens encrypted with a separately supplied environment secret. Configure Coolify routing/domain and HTTPS to the internal application port rather than assuming the starter's loopback host port is externally routable.

Maintain the current non-root UID/GID and verify volume ownership on first deployment. Build frontend assets at image build time and serve locally. No runtime Node, external database or Redis is required. Pin converter/Go/library dependencies using the project's existing versioned dependency/build mechanisms.

Update the container stop allowance to comfortably exceed bounded Graph request deadlines plus final persistence; design default request deadline is 20 seconds and shutdown grace 45 seconds. Keep the ownership lock until the final in-flight persistence finishes. A forced kill still triggers REVIEW_REQUIRED recovery.

### Backup/restore and release verification

Use PocketBase's supported consistent backup/restore facility, verifying that the chosen backup includes the private source/export files; if any application-owned files fall outside that facility, include them in a stopped-app backup of the complete data volume. Do not copy a live SQLite database file alone.

Document matching encryption-key recovery separately. Restore to an isolated instance with dispatch disabled and a restore-review flag. An old backup may omit emails submitted after the backup: require reconciliation of that time gap before candidate dispatch is enabled. File deletion and backup cleanup are separate operator actions.

Production acceptance combines existing make test, focused concurrency/race checks, image startup health, private-file access checks, a forced restart with simulated pending/in-flight attempts, artifact regeneration and volume-preserving redeploy. A real test email goes only to the operator-designated address and requires explicit received-format approval; never send a candidate batch as a deployment smoke test.

## Risks / Trade-offs

- Volume persistence alone does not recover a lost server -> verify an independent backup restore and document its limits.
- Complete task sources can contain formulas or unusual workbook features -> preserve existing data, do not evaluate formulas, and test a fixture with original cell types and multiple sheets.
- Database and files have different publication/deletion boundaries -> staged artifacts, digests and idempotent generation/cleanup.
- Manual retention grows storage -> expose artifact sizes and manual cleanup; do not add an unrequested retention scheduler.

## Migration Plan

Require changes 1-4. Add artifact/cleanup metadata, protected download/history/deletion views and generators, update Docker/Compose/configuration documentation, then run the release checks against temporary data. Verify a clean deployment and a volume-preserving replacement. Back up before schema upgrades. Rollback only after stopping/draining and preserving the sending ledger; a restore of old data must enter restore-review mode.

## References

- [Coolify persistent storage](https://coolify.io/docs/core/persistent-storage/storage-mounts/overview)
- [Excelize cell APIs](https://xuri.me/excelize/en/cell.html)
