# Tasks

Prerequisites: changes 1-4. Implement fifth. This is the production release gate; use simulated services until the explicitly requested real test email.

## 1. Complete task results

- [x] 1.1 Add durable export state and a generator consuming completion/stop/recovery markers; verify repeated generation and injected write failure never create or modify email attempts.
- [x] 1.2 Build task XLSX from the retained original workbook and row snapshots/manifests; verify all original rows/columns/values, multiple worksheets, excluded duplicates, India-offset timestamps and collision-free result labels.
- [x] 1.3 Publish generated files atomically and expose protected retry/download actions; verify unfinished files are hidden and an interrupted generation can be retried.
- [x] 1.4 Document workbook statuses and source preservation; verify a stopped and a recovered-interrupted task both produce accurate downloadable results.

## 2. Campaign and historical email exports

- [x] 2.1 Add campaign XLSX candidate summary and attempt-history sheets with as-of metadata; verify retries/manual resolutions remain distinct and tests do not inflate candidate totals.
- [x] 2.2 Add individual attempt HTML using stored subject/body and escaped metadata; verify later template/data changes cannot alter historical content and hostile values remain inert.
- [x] 2.3 Add campaign ZIP using archive/zip with stable IDs and report index; verify entries map to attempts, cannot traverse paths and distinguish submitted/failed/uncertain/test outcomes.
- [x] 2.4 Add history/export UI and document Excel/HTML/ZIP contents; verify consistent snapshot exports during simulated concurrent progress and all direct file URLs require operator authorization.

## 3. Manual retention and deletion

- [x] 3.1 Add scoped generated-file deletion without clearing history and reject deletion of referenced sources; verify eligibility is unchanged and a removed export can be regenerated.
- [x] 3.2 Add confirmed inactive-campaign deletion and idempotent file cleanup; verify active claims/in-flight requests block deletion and a cleanup retry cannot affect another campaign.
- [x] 3.3 Document no automatic expiry, removal of deduplication history and backup boundaries in deletion UI/help; verify these consequences appear before confirmation.

## 4. Coolify runtime configuration

- [x] 4.1 Finalize the existing Docker/Compose image, pinned converter, non-root storage permissions and single-instance persistent volume; verify fresh image startup, DOCX conversion and private file writes without runtime Node.
- [x] 4.2 Configure bounded request/shutdown timing and ownership-lock lifecycle; verify graceful termination drains requests and a forced kill recovers conservatively after container replacement.
- [x] 4.3 Document Coolify HTTPS/domain/internal-port routing, proxy trust, Microsoft callback and secret injection; verify the documented configuration starts with a minimal health check and does not expose source-controlled/image-layer secrets.

## 5. Backup and restore

- [x] 5.1 Verify a supported consistent backup includes database and private source/export files, adding a stopped-volume backup procedure if needed; restore into temporary isolated data and verify historical files and attempt references.
- [x] 5.2 Add/document restore-review mode that disables sending until post-backup submission gaps are reconciled; verify an old backup cannot silently make previously submitted recipients eligible.
- [x] 5.3 Document separate encryption-key recovery, manual backup cleanup and rollback procedure; verify a restored matching key recovers connection ciphertext while dispatch stays paused.

## 6. Release acceptance

- [x] 6.1 Run make test, go test -race ./... and the 1,000-row simulated end-to-end workflow; verify duplicate non-persistence, cross-task claims, pacing/concurrency, failure recovery, approvals and every download without real candidate traffic.
- [x] 6.2 Verify a volume-preserving redeploy with simulated pending/in-flight work, health/access checks and artifact regeneration; record the observed outcomes in the release checklist.
- [x] 6.3 Complete the protected UI/browser walkthrough and document an explicit test-only production check to the operator-designated address; verify received formatting is manually approved before enabling any candidate batch.

## 7. Event-driven export follow-up

The original tasks above retain their recorded completion status. The following tasks capture the subsequent export activation and recovery decisions. Verification outcomes are recorded in docs/release-checklist.md, section 6.

- [x] 7.1 Share one process-owned export service with the worker and notification callers; replace the five-second discovery ticker with after-commit task notifications and a startup scan ordered after sending recovery. Verify completion and final stop settlement activate generation, rolled-back state does not, notifications during startup or ongoing generation are not lost, and recovery notifications cannot start export snapshots before sending recovery completes.
- [x] 7.2 Automatically requeue and rebuild interrupted task results and previously requested campaign report/ZIP generation at startup, including shutdown cancellation. Verify a crash between saving work and notification is recovered, snapshots remain consistent, already ready outputs and intentionally deleted files are preserved, and unrequested campaign exports are not created.
- [x] 7.3 Preserve explicit Retry for ordinary generation failures, including failures during automatic recovery; add error-triggered backoff only for temporary discovery/recovery failures. Verify repeated restarts do not retry ordinary failed generation, rollback/duplicate notifications do not duplicate publication, and no export path creates attempts or changes eligibility.
- [x] 7.4 Verify zero recurring export discovery SQL after startup and pending work drain, plus bounded shutdown with late dispatcher markers recovered on restart. Update export/runtime help to describe automatic interrupted-export rebuilding and explicit retry for ordinary failures.
- [x] 7.5 Run focused recovery/notification tests, make check, make vulncheck, and strict OpenSpec validation for the follow-up; record actual results without treating planning validation as application verification.
