---
name: automation-reliability-review
description: Review this project's intake claims, email dispatch, retries, crash recovery, and exports against approved OpenSpec reliability requirements. Use for reliability reviews or release readiness checks, not ordinary UI edits.
---

# Automation reliability review

Review without editing code or sending email. Read `AGENTS.md`, main specs, and
applicable active change artifacts. Follow a capability into `openspec/specs/`
after sync/archive rather than relying on an obsolete delta path. Derive exact
limits, states, and retry policies from those artifacts; do not invent stricter
product behavior or treat future change tasks as implemented functionality.

Trace the relevant state transition from operator action through transaction,
request, persisted outcome, recovery, and report. Check these approved boundaries
where they apply:

- Intake detects complete normalized duplicate groups before admission; duplicate
  details remain file-only. Campaign/email identity, immutable snapshots, and
  atomic eligibility/claims prevent overlapping task submissions.
- Successful test submission and manual approval are distinct and tied to the
  exact configuration. Tests do not change candidate sending eligibility.
- Intent and exact content are durable before transmission. Persistence failure
  prevents launches; outcome-save failure stops further launches and leaves
  conservative recovery evidence.
- Microsoft acceptance is not delivery. Possible transmission without a definite
  outcome requires review, not automatic retry. Check transport, redirect, and
  middleware replay as well as explicit application retry loops.
- Candidate, test, retry, and explicit uncertain-resend actions share the approved
  pacing, concurrency, cooldown, ownership, and transactional action controls.
  Reconnection or diagnostics must not silently resume tasks.
- Stop/shutdown drain bounded in-flight work; browser closure/logout do not stop
  server work. Restart retains submitted/uncertain history and persisted pacing;
  safe remainder waits for explicit resume.
- Uncertain resend needs duplicate-risk confirmation and a new retained attempt.
  Manual resolution must not invent a provider acceptance timestamp.
- Repeated/failed exports and file cleanup do not change eligibility or create
  attempts. Historical content, protected downloads, original workbook data,
  explicit India-offset timestamps, deletion boundaries, and restore-review mode
  follow the applicable specs. Keep production sending gated by release acceptance.

Use temporary stores, fake providers, controlled clocks where already supported,
and failure injection. Choose scenarios relevant to the diff: simultaneous
claims, crash before/after transmission, outcome-save failure, restart during
cooldown, repeated browser actions, stop during requests, or failed export.
Run existing focused checks and race tests when useful. Do not claim exactly-once
delivery, live provider verification, or completed release acceptance from mocks.

For each actionable finding, cite file/line, the OpenSpec requirement/scenario,
a reproducible failure sequence, its effect on history or sending, and the
smallest meaningful regression test. Rank by severity. Report no findings when
supported, with untested paths and future work clearly separated.
