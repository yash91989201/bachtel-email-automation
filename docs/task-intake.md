# Task intake

How an Excel workbook becomes a validated task: workbook support and resource
limits, worksheet and header selection, recipient-key matching, duplicate
handling, the immutable row snapshot, and the downloads an operator can take
before anything is sent.

Intake never sends email. A validated task is **ready**, not running, and no
sending attempt exists until the dispatch work starts it.

## Supported workbooks

| Input | Result |
| --- | --- |
| `.xlsx` workbook | Read with the pinned Excelize reader |
| Password-protected workbook | Rejected as an unsupported workbook |
| Legacy `.xls`, macro-only `.xlsm`-style container | Rejected as an unsupported workbook |
| Truncated or corrupt file | Rejected as unreadable |
| File above the upload limit | Rejected before the workbook is opened |
| Archive expanding beyond the resource limits | Rejected before the workbook is opened |

Cell values are read as Excel displays them: a formula cell yields its cached
value, and the workbook's formulas are never evaluated. A formula whose cached
value is missing reads as an empty cell, so a required value it feeds is
reported as missing — export the workbook with values, or correct the row, and
upload it again.

### Limits

| Limit | Default | Setting |
| --- | --- | --- |
| Upload size | 10 MiB | `INTAKE_MAX_UPLOAD_BYTES` |
| Expanded workbook archive | 64 MiB | `INTAKE_UNZIP_SIZE_LIMIT` |
| One expanded XML part | 16 MiB | `INTAKE_UNZIP_XML_SIZE_LIMIT` |
| Expanded recipient entries per task | 1,000 (fixed support bound) | — |
| Rows per selected worksheet | 10,000 | — |
| Columns per selected worksheet | 256 | — |

Values are byte counts. The upload limit can be lowered but never raised above
10 MiB; a malformed or out-of-range value fails startup. The 1,000-entry,
10,000-row and 256-column bounds are support bounds, not settings: a larger
input is rejected with an actionable message, and the task stays unvalidated.
The entry bound counts every expanded entry before duplicate or invalid-address
exclusions, so a workbook of 1,001 entries fails even when most would be held.

## Worksheet and header selection

Uploading stores the original workbook as a **staging** task and reads only its
worksheet inventory. A staging task is never sendable.

- The default selection is the **first visible** worksheet with header row `1`.
  A hidden worksheet is only used when no worksheet is visible.
- The operator confirms the worksheet and the header row before validation.
  Only the selected worksheet is considered; other worksheets are ignored.
- Header labels are matched after trimming surrounding whitespace and are
  **case sensitive**: `Email` and `email` are different columns.
- Every source header mapped by the selected revision, including the recipient column, must exist
  **exactly once** among the header columns of the chosen header row. A missing
  header blocks the whole task, because no row could be mapped; a header label
  that appears on two columns is ambiguous and blocks the task as well. Add or
  rename the column, or choose another header row, and validate again.
- A data row is a **candidate row** when at least one mapped column — the
  recipient column or a mapped source column — holds a value. A row whose mapped
  columns are all empty is not a row of the table: it produces no counts and no
  feedback. Values in columns that no mapping uses are source data only.

A failed validation leaves the task in staging with the reason recorded on the
task: nothing is published, no candidate exists and no manifest or report is
written. Correct the workbook or the selection and validate again.

## Recipient expansion

The selected `recipient` mapping's cell is split on **commas only**, and each
token becomes its own recipient entry:

- Surrounding whitespace is trimmed per entry. `a@example.com,b@example.com`,
  `a@example.com, b@example.com` and ` a@example.com ,  b@example.com ` produce
  the same two recipients.
- The other mapped values are copied to every sibling, so one row
  `Person 1 | a@example.com, b@example.com | Designation 1` saves two
  recipients with the same name and designation.
- Each entry keeps its source worksheet row and its 1-based position within the
  recipient cell, and the full original cell is stored for comparison previews.
  A preview shows only that entry's own address in the To field and in a
  recipient placeholder.
- A wholly empty recipient cell on a row that carries other mapped data produces
  one invalid entry. A row whose mapped values are all empty is still ignored.
- Empty tokens, spaces without commas, and semicolon-separated lists are invalid
  entries, never silently expanded address lists. Only one address is ever used
  per email.
- An invalid or duplicate entry affects only itself: valid unique siblings from
  the same cell are saved under the usual required-value and campaign-history
  checks.
- Every expanded entry — valid, invalid, duplicate or empty — counts toward the
  1,000-entry bound. Exceeding it refuses the whole validation and publishes no
  candidate memberships or snapshot.

To correct an entry, fix the mapped recipient cell in the workbook and upload it
as a new task. The saved task snapshot is never edited or re-split.

## Recipient key matching

A candidate is identified by **campaign plus trimmed lowercase email address**.
The address comes from the revision's one `recipient` mapping. Campaign source
headers are draft hints and do not select or validate the recipient.

- Surrounding whitespace is trimmed and the address is lowercased, so
  `  Ana@Example.com ` and `ana@example.com` are the same recipient.
- No input ID column is needed or read: identities are generated by the
  application.
- The same address in a different campaign is a different candidate, so a
  submission in one campaign never makes a recipient ineligible in another.
- A **changed address is a new candidate key**. A phone number, name or ID that
  matches an earlier upload never links two identities: if a row's address
  changes, correct it in a new upload and that row starts a fresh identity with
  its own history.
- A status column carried over from another campaign or another system
  (`SUBMITTED`, `SENT`, …) is ordinary source data. Only this application's
  campaign history decides eligibility.

## Duplicate handling

Duplicate detection runs over **every expanded entry of the complete selected
input before any candidate is created**, using each entry's raw recipient token
trimmed of surrounding whitespace and lowercased. It is deliberately not a
validity check first, so an entry that is both duplicated and otherwise wrong is
still reported as a duplicate.

- Every entry of a duplicate group is excluded, including identical copies,
  repetitions inside one cell, and an entry that is also missing a required
  value. No entry is chosen as the preferred copy.
- Duplicate entries never create or update a candidate, never create a task
  membership and never create a sending attempt. Their details live only in the
  stored workbook, the validation manifest and the exclusion report; the task
  keeps a duplicate **count**.
- That holds when the duplicated address already exists in the campaign: the
  existing candidate is not modified.

**Correct it by uploading a new workbook.** Fix the duplicate entries — or
remove the addresses you do not want to send — and upload the corrected file as
a new task. The earlier task keeps its snapshot and its counts; the unique
entries of the corrected upload are validated independently, so a duplicate
report can be downloaded while the eligible entries of the same task still
proceed.

## Row outcomes

| Outcome | Meaning | Where it is recorded |
| --- | --- | --- |
| Eligible | Unique, valid address with every required value | Membership snapshot; claimable |
| Ineligible | Unique, valid address missing a required value | Membership snapshot; never claimable |
| Duplicate | Same recipient key as another row of this upload | Report and manifest only |
| Invalid address | One entry's token is empty or not one usable address | Report and manifest only |
| Already submitted | The address was already submitted in this campaign | Membership snapshot |
| Already scheduled | The address is reserved by another task of this campaign | Membership snapshot |
| Review required | An earlier attempt has an uncertain outcome and needs review | Membership snapshot |

The operator-facing wording is:

- duplicate — "duplicate recipient address in this upload; every row of the group is held" (every entry of the group)
- invalid address — "the recipient cell is empty or is not a single usable email address"
- missing value — "a required mapped value is empty", and the task row names the
  variables (`missing required value(s): candidate_name`)
- already submitted — "already submitted in this campaign"
- already scheduled — "already scheduled in another task of this campaign"
- review required — "an earlier attempt has an uncertain outcome and needs review"

A valid address that is missing a required value keeps its campaign-scoped
identity and an ineligible membership, so a corrected upload reuses that
identity instead of creating a second one. A row with an unusable address never
gets an identity at all.

## Immutable snapshots

Validation publishes one task snapshot:

- The original workbook bytes, unmodified, with their SHA-256 digest. Results can
  reproduce source cells and original worksheet row numbers exactly.
- The selected worksheet, header row and source header labels.
- The renamed result column labels (see below).
- One membership row per unique valid expanded entry holding the **raw** value
  and the **normalized** replacement value per mapped variable, its original
  worksheet row, its position inside the recipient cell, the full original
  recipient cell, its outcome and its reason.
- The content revision the task was validated against, pinned by its content
  hash.

Rendering a row is a pure function of that pinned revision and the stored raw
values, so previews, and later the emails themselves, reproduce exactly what was
reviewed. Once a task has started, a later upload or a campaign edit cannot
change its content or its earlier history.

### Revalidation

Changing campaign content creates a new revision, which makes a task validated
against the older revision **stale**: it can be reviewed, but it cannot start.
Revalidate it to rebuild the snapshot — counts, labels and previews — against the
current revision. Revalidation is allowed only while a task has not started;
a running or finished task keeps its snapshot, and the fresh counts that are
shown when starting a task come from the same preflight check.

### Result column labels

The campaign's result column labels are snapshotted per task. A label that
already exists among the source columns **or** the recipient-results metadata
columns is renamed with a numbered suffix (`Email Status` → `Email Status (2)`),
deterministically and in listed order, so a later result export can never
duplicate or overwrite a label. The resolved labels are shown on the task page
and are re-resolved from the current campaign labels when a start is prepared.

## Result columns and result workbook

After a task completes, stops or is recovered, the retained original workbook
keeps every original worksheet, row, cell and column order, and gains a separate
`Recipient results` worksheet (or the first free `Recipient results (N)` when an
input worksheet already uses that name). That sheet copies the selected source
columns and adds the source worksheet, original row, position in the recipient
cell, individual recipient and the snapshotted result labels — one row per
expanded entry, so two siblings from one source row get their own outcomes.
New values are written as strings, so formula-like source text is never
evaluated. See [history-exports.md](history-exports.md) for the status
vocabulary, timestamps and download behavior.

## Private artifacts

Every artifact below is stored as a private file on the task record. Collection
rules are empty, so the files are unreachable through the public API and only
the session-gated task pages can serve them.

| Artifact | Content |
| --- | --- |
| Original workbook | The uploaded bytes, unchanged |
| Validation manifest (JSON) | Layout below |
| Exclusion report (XLSX) | Every held entry: the source columns in their original order, then `Source worksheet row`, `Recipient position`, `Recipient` and `Reason` (metadata labels are collision-safe; an unnamed source column is labelled `Column N`) |

The exclusion report includes every entry that is not eligible — duplicates,
unusable addresses, missing values, already submitted, already scheduled and
review-required entries — in source-row and position order, with the original
columns so the row can be corrected in place. Multiple held entries from one
source row produce multiple report rows. It is written as soon as validation
finishes, even when no entry is eligible: a task with zero eligible entries
still offers its report and no send action. Re-uploading a corrected report never
overrides the original recipient column: correct the mapped recipient cell
itself. The appended columns are informational.

### Manifest layout

```json
{
  "version": 2,
  "sheet": "Candidates",
  "header_row": 1,
  "headers": ["Email", "Applicant Full Name", "Location"],
  "source_rows": 2,
  "rows": [
    {"row": 2, "position": 1, "recipient": "alice@example.com", "email_key": "alice@example.com", "state": "eligible"},
    {"row": 2, "position": 2, "recipient": " bob@example.com ", "email_key": "bob@example.com", "reasons": ["duplicate"], "group": 1},
    {"row": 3, "position": 1, "recipient": "alice@example.com", "email_key": "alice@example.com", "reasons": ["duplicate"], "group": 1}
  ],
  "duplicate_groups": [
    {"group": 1, "email_key": "alice@example.com", "occurrences": [{"row": 2, "position": 2}, {"row": 3, "position": 1}]}
  ]
}
```

`rows` covers every expanded entry with its source coordinates and raw
recipient token. An entry carries `reasons` exactly when it was held back from
candidate storage; an admitted entry carries the membership `state` instead.
`source_rows` counts the distinct worksheet rows that contributed entries, and
`duplicate_groups` lists every group with all of its occurrences. A saved
version 1 manifest is read as singleton occurrences at position 1 without
re-splitting its stored cells.

## Reservation and history

Candidate eligibility is decided by one transactional operation that the
starting, resuming and retry actions share:

- Reserving a candidate is a single compare-and-swap: a candidate can be
  reserved only while it is free and has no submission history, so two tasks
  starting at the same moment can never schedule one recipient twice. The loser
  reports the candidate as already scheduled with fresh counts.
- Only eligible rows of a task can be reserved. A row that is ineligible, already
  submitted, already scheduled or review-required is never reserved.
- Releasing a task's reservations clears only the claims that task owns.
  Submission history and unresolved uncertainty are never cleared, so a released
  or re-uploaded candidate stays excluded until an explicit review resolves it.
- A resume or retry rechecks eligibility instead of reclaiming candidates, so a
  recipient who was submitted in the meantime is not sent to again.

Source documentation: [Excelize cell and row APIs](https://xuri.me/excelize/en/cell.html).

## Final sending data

A validated task is not yet a final sending dataset. Prepared values or an
operator-edited final sheet become one immutable snapshot; preview, test and
candidate work read that snapshot directly. See
[final-sending-snapshots.md](final-sending-snapshots.md).
