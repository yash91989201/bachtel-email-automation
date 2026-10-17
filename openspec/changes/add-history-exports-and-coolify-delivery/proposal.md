# Proposal

## Why

The operator needs downloadable original-data results and historical email content, and a Coolify redeployment must preserve both files and sending history. Reporting and deletion must never create a new send opportunity accidentally.

## What Changes

- Generate and retain complete task-results XLSX files after completion, stopping and interruption, including excluded duplicate rows.
- Add campaign XLSX summaries with candidate and attempt history, individual historical HTML emails and campaign ZIP packages.
- Keep original uploads, configuration revisions, snapshots, attempts and generated artifacts until explicit manual deletion; no automatic expiry.
- Add guarded deletion that distinguishes generated-file cleanup from removing campaign history.
- Finalize the single-instance Coolify deployment, persistent storage, secret configuration, graceful shutdown, health checks and backup/restore instructions.
- Gate production use on simulated failure/concurrency tests, restart/redeploy persistence verification and a user-approved real test email to a designated test address.

## Capabilities

### New Capabilities

- `history-exports`: Protected, faithful task/campaign/email downloads and explicit retention/deletion behavior.
- `coolify-deployment`: Persistent, single-instance production operation and verifiable deployment/recovery instructions.

### Modified Capabilities

None.

## Impact

Implementation order: **5 of 5**; depends on changes 1-4. The five changes are an ordered release, not five separately deployable candidate-sending releases.

Reuse Excelize, Go HTML templates and `archive/zip`, PocketBase files/backups, existing Docker/Compose and the converter introduced in change 2. Extend protected views and artifact generation; update Docker, Compose, configuration examples and README only during implementation.

No PDF/EML exports, cloud-storage integration, retention scheduler, extra database service or automatic candidate batch during validation.
