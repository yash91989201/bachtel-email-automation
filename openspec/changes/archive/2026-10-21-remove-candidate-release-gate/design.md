# Design

## Context

Production currently omits AllowCandidateSending, leaving the store disabled. Independent approval checks already protect candidate actions, schedule creation and scheduled firing. See proposal.md for the requested policy change.

## Goals / Non-Goals

Remove the obsolete global lock throughout store and UI. Preserve configuration-specific approval and operator actions. Do not send emails, start live tasks, restart the live server or deploy during this work.

## Decisions

Delete AllowCandidateSending, ErrReleaseGate and their UI state instead of keeping a permanently enabled switch. Default stores will support candidate dispatch after ordinary authorization checks. Preserve restore-review, eligibility, pacing, durable intent and recovery protections. Add a direct restore-review refusal at scheduled fire: the old global gate had masked the absence of that check, allowing a due restored schedule to claim recipients even though dispatch itself remained blocked. Retain approval checks and cover all candidate action kinds plus scheduled approval refusals with simulated tests. Simplify fixture setup and remove obsolete gated-browser URLs.

Update active docs and project guidance without rewriting archived change records or historical verification observations. Existing running tasks and schedules must be reviewed by the operator before deploying this revision; no live process is restarted here. Pause the existing Air watcher during source edits to prevent automatic activation; do not resume it as part of verification.

## Risks / Trade-offs

- Candidate dispatch becomes available on deployment: the existing test approval and explicit start/schedule confirmations remain mandatory.
- Local checks do not establish delivery or production acceptance: report results and existing failures accurately.

## Migration Plan

No database migration. Rebuild and deploy only through a separately authorized operation. Rollback uses the previous binary. No deployment is part of this change.
