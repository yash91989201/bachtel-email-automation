# Design

## Context

See proposal.md. Changes 1 and 2 provide private access, configuration revisions and normalization/rendering. No candidate/task collections exist in the starter. This is change 3; its atomic eligibility service is reused by dispatch rather than independently reimplemented there.

## Goals / Non-Goals

**Goals:** Validate the complete upload before admitting rows, preserve immutable task data and report duplicate groups without persisting them as candidates.

**Non-Goals:** Google APIs, source-sheet ID management, an inline spreadsheet editor, a custom XLSX reader/writer or candidate submissions before change 4.

## Decisions

### Excelize and file artifacts

Add a pinned compatible github.com/xuri/excelize/v2 dependency for XLSX parsing, duplicate reports and later result workbooks. One task selects one worksheet and header row; default to the first visible worksheet and row 1, with a preview and explicit selection before validation. Map headers after trimming surrounding whitespace, with case-sensitive labels and ambiguous duplicate labels rejected. The immutable campaign revision selects the recipient source column through exactly one explicit recipient mapping; campaign source-header suggestions only seed the first mapping draft. Intake validates all mapped columns, reads the selected mapping's raw source value, and uses its trimmed lowercase normalized address for routing, duplicate grouping and campaign candidate identity.

Store campaign source-header suggestions in the campaign record as `source_headers`; remove the former `recipient_header` field through a forward schema migration. Do not convert its old value or infer a recipient mapping: existing drafts begin with required text mappings and the operator explicitly selects one recipient mapping. These storage and migration details do not change the visible mapping behavior.

Enforce the 1,000 non-empty candidate-row support bound plus documented configurable upload/expanded-archive resource limits. Preserve workbook bytes and a digest in private persistent storage. Read displayed/cached cell values without evaluating uploaded formulas or following external links; missing formula cached values needed for replacement require corrected input.

Keep a private file-based validation manifest keyed by original sheet/row. It contains excluded-row reasons, duplicate groups and original-row references; the database stores task-level counts/artifact references only for duplicate data. Use Excelize typed string writes for added report text. If configured output labels collide, choose a deterministic unoccupied suffix and show resolved labels before start.

### Validate in stages before candidate persistence

1. Store the original upload privately and validate file, worksheet and header structure against the current campaign configuration.
2. Normalize recipient keys across the full selected input and detect duplicate groups before filtering missing fields.
3. Write duplicate/invalid-address feedback to the file manifest; create the immediate duplicates XLSX.
4. Admit only unique valid-address candidates into campaign/task records, applying required-value checks and campaign-history eligibility.
5. Show counts, reasons and personalized previews; change 4 performs final approval/start checks.

A missing mapped header blocks the whole task. An empty value only invalidates that row. All duplicate rows are excluded even if identical or also invalid. No duplicate row causes a candidate upsert, task-candidate insert or attempt. A task with zero eligible rows still offers useful validation downloads.

### Data model and unique identity

Use campaigns/configurations from change 2; add send_tasks, campaign_candidates and task_candidates. Enforce a unique database index on campaign_candidates(campaign_id, normalized_email). IDs are generated internally. No global person-matching table is needed.

send_tasks references the original workbook, selected sheet/header, configuration revision, validation manifest, counts and result-column label snapshot. task_candidates references candidate/task/source row and stores raw mapped values, normalized values, row validation state and rendered preview/content snapshot. Unique valid-address rows missing other required values can have an ineligible membership. Invalid addresses remain file-only.

Historical membership/attempt data is immutable. New unique uploads can point to an existing candidate but do not rewrite earlier snapshots. Exported status columns are just source data; only app campaign history decides eligibility.

### Eligibility and cross-task reservations

Implement a single transactional eligibility/reservation operation consumed by start/resume/retry. Use candidate identity uniqueness plus an active-task claim with an owner reference, not a check-then-insert sequence outside a transaction. Unstarted previews do not reserve candidates.

Submission history, unresolved uncertainty and active claims take precedence over a new upload. A second simultaneous start gets refreshed Already scheduled exclusions. Submitted candidates stay excluded in the same campaign; new campaigns have independent identities. Once a task is stopped or recovered, safe unstarted reservations can be released transactionally; submitted and uncertain history is never cleared. A later resume must recheck, not reclaim candidates blindly.

No database transaction spans a network request. Change 4 adds the dispatch claim lifecycle and attempts in the same database.

## Risks / Trade-offs

- Mixed-case/space-padded duplicate addresses can escape superficial checks -> normalize the complete input before any candidate persistence.
- File and database commits are not one transaction -> stage/atomically publish manifests, store digests and mark validation ready only after referenced artifacts exist; clean abandoned temporary files.
- Corrected email addresses are new identities -> display this matching rule; do not guess identity from names.
- Deferred sending means eligibility can change after preview -> recheck atomically at every explicit submission action.

## Migration Plan

Implement after changes 1 and 2. Add indexed collections and file manifests, then protected upload/review/download routes. Exercise migrations with temporary SQLite data and real XLSX fixtures, including 1,000 rows and zero DB writes for duplicate groups. The intake UI remains non-sending until change 4.

## References

- [Excelize cell and row APIs](https://xuri.me/excelize/en/cell.html)
