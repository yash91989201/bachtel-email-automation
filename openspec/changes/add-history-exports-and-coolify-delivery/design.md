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
