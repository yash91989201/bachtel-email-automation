---
name: go-review
description: Review this project's Go changes for correctness, resource ownership, concurrency, security, and meaningful test coverage. Use for requested Go reviews, not routine implementation or style-only simplification.
---

# Go review

Review the requested diff or area without modifying it. Read `AGENTS.md` and
identify the applicable OpenSpec requirements and current implementation stage.
Resolve the comparison base explicitly; preserve uncommitted work. Use CodeGraph
when this checkout is indexed, otherwise inspect source directly.

Trace changed behavior through callers and external boundaries. Focus on:

- Lost or misclassified errors, partial writes, and misleading success responses.
- Request context propagation, bounded HTTP/subprocess calls, closed resources,
  goroutine termination, and synchronization of shared state. Background jobs
  need a server-owned lifetime with explicit shutdown.
- SQLite/PocketBase transaction boundaries and uniqueness enforcement under
  concurrent actions. Check migrations against existing data and private
  collection access rules, rather than testing only a fresh happy path.
- Operator authorization, cross-origin mutation protection, safe rendering,
  resource-bounded uploads, protected file access, and credential leakage.
- Tests that exercise the changed outcome, realistic failures, and relevant
  concurrency; mocks alone do not establish database atomicity.

Use existing dependencies and package conventions. Suggest an abstraction only
when it solves a demonstrated problem. Let automated formatting checks handle
style. Consult version-matched official documentation when API behavior is
uncertain. Use `make check` for validation when appropriate; disclose anything
not run. Do not send real mail or read production data to verify a finding.

Report actionable findings ordered by severity, each with file/line, triggering
scenario, concrete consequence, applicable requirement, and a focused regression
check. Separate confirmed defects from unresolved questions. If none are found,
say so and state coverage limits. Planned features outside the reviewed change
are not regressions; a green suite does not prove their implementation.
