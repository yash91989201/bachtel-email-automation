# Release checklist

This is the fifth and final change in the ordered release: no candidate batch
runs until every applicable item below is verified. Automated checks use
simulated providers and temporary data; they never load `.env`, real mailbox
credentials or candidate traffic.

## 1. Repository checks

| Check | Command | Observed |
| --- | --- | --- |
| Format, generation, lint, race tests, production build | `make check` | 2026-10-19: passed (format, generated templates, lint, `go test -race ./...`, production build) |
| Dependency vulnerabilities | `make vulncheck` | 2026-10-19: passed, no reachable vulnerabilities |
| Packaged Pandoc and converter coverage | `make verify-converter` | 2026-10-19: passed; full test suite ran in the image with pinned pandoc 3.10 |
| OpenSpec artifacts | `openspec validate --all --strict --json --no-interactive` | 2026-10-19: 8 specs and 1 change passed |
| 1,000-row simulated workflow and every download | `go test ./internal/exports -run TestExportThousandRowWorkflow` | 2026-10-19: passed, 1,000 rows and every generated download verified |
| Export failure, interrupted generation, deletion and regeneration | `go test ./internal/exports` | 2026-10-19: passed (preservation, failure, interruption, deletion, stopped-volume restore) |
| Restore review and sender history | `go test ./internal/sending -run 'TestRestoreReview|TestAttemptStoresSender'` | 2026-10-19: passed |
| Campaign deletion scope and active-work refusal | `go test ./internal/retention` | 2026-10-19: passed (scoped records and files, active-work refusal, idempotent retry) |
| Export and deletion web routes | `go test ./internal/web -run 'TestExport|TestAttemptHTML|TestCampaignDelete'` | _record date/result_ |

The 1,000-row check must show original rows and values preserved, duplicate
rows excluded without candidate records, distinct retry and manual-resolution
attempts, test attempts outside candidate totals, a report/package that map to
attempts, and every route behind operator authorization. A failing export must
leave attempt history byte-identical.

## 2. Fresh image startup

1. Build the image and start it against an empty persistent volume.
2. Confirm the container starts as UID/GID 10001, `/health` answers, and the
   health check is green.
3. Provision the operator, sign in, and confirm the mailbox connection state.
4. Create a campaign revision from a DOCX and confirm conversion succeeds
   without runtime Node, Word or LibreOffice on the server.
5. Upload and validate a task; uploads, manifests, exclusion reports and
   generated downloads must be written under `/app/pb_data` and stay private.

Observed 2026-10-19: runtime image `bachtel-runtime-test` started healthy on
an empty named volume as `app:app`; `/health` answered; operator provisioning
succeeded; `pandoc 3.10` was present; a campaign revision converted through the
packaged converter; the volume held the database, storage and ownership lock.
No runtime Node, Word or LibreOffice was used.

## 3. Protected browser walkthrough

With a valid operator session:

1. Open the campaign page and generate the report and package; download both
   and open `report.xlsx`, one attempt HTML file and `index.csv`.
2. Open a task page and confirm the results workbook is offered only when
   ready, that Regenerate succeeds, and that removing the file leaves counts,
   rows and exclusion report unchanged.
3. Confirm the sending page reports the restore-review alert only when the flag
   is set, and that downloads are not reachable without a session.
4. Delete a generated file only, confirm history remains, then regenerate.
5. Attempt campaign deletion without confirmation (refused), with confirmation
   on a disposable campaign (removed), and while work is active (refused with
   the settle message).

Observed 2026-10-19 on a disposable local instance: created a campaign through
the session-gated UI, converted a DOCX revision (pandoc 3.10), generated the
report and package, fetched both downloads through the session (XLSX and ZIP
responses), removed and regenerated the generated files while revision history
remained, uploaded and validated a task workbook, generated its results
workbook after a terminal state and downloaded it. No candidate email was sent.

## 4. Volume-preserving redeploy

1. Start a disposable instance with simulated pending and in-flight attempts.
2. Replace the container while keeping the `pb_data` volume.
3. Confirm operator access, connection ciphertext, campaigns, uploads and
   attempt history survive; interrupted work appears as interrupted/uncertain
   and is not resent automatically.
4. Regenerate a task workbook and campaign downloads after the replacement.
5. Delete the container only (not the volume) and confirm the data returns.

Observed 2026-10-19 with Docker: one named volume, four simulated-start
attempts in the automated suite, container replaced (`docker rm` + run with the
same volume), health green again, the authored campaign and its converted
revision still present, and a second operator provisioning correctly refused.
Forced-kill recovery is covered by the sending acceptance suite.

## 5. Operator test email

Candidate sending remains disabled until this section is approved.

1. Designate one test recipient address explicitly (not a candidate list).
2. Send one diagnostic test from a validated task and inspect the received
   subject, formatting, links and personalization.
3. Record the approval of that exact content hash in the application.
4. Only after manual approval may an operator start a candidate batch, and only
   with a separately confirmed fresh count.

Observed: not performed by automated checks. Candidate sending remains gated;
this step requires the operator to designate the address, receive the test, and
record the formatting approval before any batch is enabled.

## 6. Event-driven export follow-up

Observed 2026-10-19 with temporary databases/files and simulated sending:

- Committed terminal-task updates activate the shared export worker. A stopping
  task waits for final settlement; rolled-back updates do not generate results.
  Notifications committed during generation are consumed afterward.
- Sending recovery finishes before export snapshots begin. Startup rebuilds
  interrupted task results and previously requested campaign outputs, including
  cancelled generation. Recovered report/package pairs share one snapshot;
  ready siblings, intentionally deleted files and unrequested exports stay put.
- Ordinary recovered generation failures remain failed across restarts until
  explicit retry. Temporary recovery/discovery failures back off and shutdown
  cancels that wait. A late dispatcher settlement marker survives for restart.
- Real SQL instrumentation observed no additional export-worker queries during
  a 5.2-second idle interval. Recovery preserved attempts, candidates and claims.
- Removing a failed task export clears its automatic-generation marker, and
  startup does not recreate the intentionally removed file.

Final verification passed:

- Focused notification/recovery/cancellation/deletion tests under the race
  detector; the campaign snapshot test also passed five consecutive race runs.
- `make check`: formatting, generated-output consistency, lint (0 issues), the
  full race suite and the production binary build. The final export race suite
  passed in 182.997 seconds; sending in 283.396 seconds and web in 213.837 seconds.
- `make vulncheck`: no reachable vulnerabilities; six module-level advisories
  were outside the application's reported call paths.
- `make verify-converter`: packaged-Pandoc build, vet and full test suite.
- `openspec validate --all --strict --json --no-interactive`: all nine items valid.

These are local verification results. No hosted deployment or real email was
performed for this follow-up. The operator test and candidate-sending gate in
section 5 remain in effect.
