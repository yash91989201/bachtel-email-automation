# Tasks

Prerequisites: changes 1-3. Implement fourth as one safety boundary: no real candidate dispatch until durable recording/recovery and change 5's production release gate pass.

## 1. Durable submission model

- [x] 1.1 Add attempt, approval, manual-resolution, action-id and dispatcher-state migrations; verify fresh setup, relationships, immutable stored content and distinct test/candidate attempt kinds.
- [x] 1.2 Bind explicit action IDs and atomic candidate claims to start/resume/retry; verify repeated browser actions and simultaneous overlapping tasks cannot schedule a recipient twice.
- [x] 1.3 Commit exact content and in-flight intent before transmission, then persist each response immediately; verify injected pre-intent failure sends nothing and post-acceptance save failure halts launches.
- [x] 1.4 Document task/member/attempt states and +05:30 display semantics; verify scenario fixtures distinguish provider acceptance, manual resolution and delivery.

## 2. Shared dispatch controls

- [x] 2.1 Reuse golang.org/x/time/rate with burst 1, a four-slot semaphore and persisted actual-start/cooldown metadata; use a controlled clock to verify default 25, hard maximum 30, rolling limits, rate changes and restart without bursts.
- [x] 2.2 Add the small Graph HTML send adapter with bounded request deadlines and no hidden POST/redirect retry; verify 202, definite rejection, 401/403, 429, 5xx, timeout and disconnect classification through httptest.
- [x] 2.3 Share pacing/capacity across tasks, tests and retries with bounded fair task selection; verify four in-flight maximum and a test receives a permitted slot without bypassing rate limits.
- [x] 2.4 Apply shared Retry-After handling and bounded safe backoff; verify seconds/date headers, missing-header backoff and no automatic resend after ambiguous transmission.
- [x] 2.5 Document pacing settings and observable throttling behavior; verify both browser and server reject out-of-range/non-integer rates.

## 3. Test approval and start UX

- [x] 3.1 Add explicit test sending with a chosen sample/test address and separate test attempt; verify candidate eligibility remains unchanged and tests work for a paused task after authorization is usable.
- [x] 3.2 Add received-format approval bound to the exact configuration hash; verify a successful submission alone does not approve and content changes require a new approval.
- [x] 3.3 Add fresh-count explicit start confirmation using intake's current-configuration preflight; verify no approval/no eligible rows/stale config prevent starting and same-config later uploads can reuse approval.
- [x] 3.4 Document the test/approve/start workflow; verify its UI shows fixed sender, candidate exclusions and the difference between accepted and delivered.

## 4. Stop and restart recovery

- [x] 4.1 Bind a process-lifetime advisory ownership lock and worker lifecycle to app startup/shutdown; verify a second process sharing the data directory cannot dispatch.
- [x] 4.2 Implement manual stop and bounded shutdown drain independent of browser/request contexts; verify four active requests settle and no further candidate launches after Stop.
- [x] 4.3 Recover unfinished intents as REVIEW_REQUIRED and safe work as manually resumable; verify crashes before transmission, after transmission and after acceptance preserve the conservative state and cooldown.
- [x] 4.4 Pause for authorization/storage/persistent-service failures and release only safe unstarted claims; verify reconnection or a diagnostic test never resumes candidate work automatically.
- [x] 4.5 Document interruption and recovery controls; verify the task page distinguishes held candidates from the safe remaining count.

## 5. Retry, review and live progress

- [x] 5.1 Add individual retry and Retry all failed with refreshed counts and confirmation; verify submitted, invalid, duplicate, active and uncertain rows are excluded.
- [x] 5.2 Add uncertain Send again confirmation and audited Mark as already submitted; verify new attempts retain old records and manual resolution does not fabricate a provider acceptance timestamp.
- [x] 5.3 Add two-second grouped htmx progress updates and reopen/reconnect behavior; verify closing the browser does not stop workers and out-of-order responses update the right rows.
- [x] 5.4 Emit durable result-generation-needed markers on completion/stop/recovery; verify marker retry never creates an email submission.
- [x] 5.5 Document retry/review semantics alongside their UI, regenerate templ sources and verify all mutation/progress endpoints enforce operator access.

## 6. Integration acceptance

- [x] 6.1 Run make test and go test -race ./... with simulated Microsoft services and controlled time; verify the full 1,000-candidate pipeline, shared limits, cross-task claims, interruption and replay protection without real candidate emails.
- [x] 6.2 Exercise browser start/test/stop/resume/review flows with a simulated provider and restartable temporary data; verify counters/actions remain consistent and production candidate sending stays gated pending change 5.
