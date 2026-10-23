# Tasks

## 1. Provider configuration and Sent-copy contract

- [x] 1.1 Extend the existing Titan config/options and composition wiring with the fixed encrypted IMAP endpoint and simulated-test override; pin `github.com/emersion/go-imap v1.2.1`. Verify config tests cover the default and override, the operator form gains no new fields, and dependency resolution succeeds without unrelated upgrades.
- [x] 1.2 Add and document the small optional post-submission hook on the existing `SendResult`, keeping `Sender`, `PreparingSender`, provider selection and authentication structure intact. Document that native providers retain Sent saving and separate-save providers return the hook; verify compilation and focused sender tests cover both conventions.
- [x] 1.3 Retain Graph's native Sent Items configuration and strengthen direct/prepared Graph candidate/test coverage: prove saving is not disabled, one send POST occurs, no manual copy/polling operation occurs, and 202/cleanup failure preserves submitted semantics. Verify with a simulated Graph server and document the asynchronous saving distinction alongside the provider code.

## 2. Titan's separate Sent-copy operation

- [x] 2.1 Implement the bounded IMAP save helper and return its hook only after definite SMTP acceptance, capturing the exact original MIME and bound mailbox credentials. Verify simulated SMTP/IMAP tests prove accepted candidate/test messages save the actual recipient, subject, HTML, headers, read flag and acceptance timestamp without an additional SMTP transaction; rejected/uncertain sends and connection validation create no copy.
- [x] 2.2 Discover the existing Sent folder by advertised special-use attribute with the existing exact `Sent` fallback. Verify simulated server cases for advertised, fallback, missing and ambiguous folders; assert no folder creation, Inbox append or unsafe mailbox operation occurs.
- [x] 2.3 Enforce context-aware TLS dialing, absolute operation deadlines, cancellation cleanup and owned helper goroutines without protocol debug output. Verify certificate/authentication/append refusal, quota, stalled greeting, timeout, cancellation, lost APPEND response and post-confirmation logout failure; assert one append attempt at most and sanitized diagnostics.

## 3. Durable dispatch integration and warnings

- [x] 3.1 Persist confirmed acceptance and its timestamp before invoking a copy hook, keeping the copy outside dispatcher locks and database transactions while retaining bounded worker/drain ownership. Verify worker/store tests hold IMAP completion open and observe SUBMITTED first, and ensure an outcome-storage failure skips the hook and retains existing storage safeguards.
- [x] 3.2 Record a safe copy warning through a narrow owner/connection-scoped update to existing attempt details without changing state, acceptance time, approval or eligibility. Verify candidate/test details distinguish the warning from send failure, copy failure alone does not pause sending, and warning-storage failure preserves SUBMITTED while reporting the storage error safely.
- [x] 3.3 Verify integrated simulated-provider regressions for stop/shutdown/disconnect/replacement during copying, two-operator credential isolation, continued progress of another mailbox and existing pacing/concurrency bounds. Assert the copy stays bound to its original sender and completes or cancels within the worker deadline.
- [x] 3.4 Verify restart after durable acceptance before/during copy completion retains SUBMITTED with no resend or copy replay. Seed older attempts representing the first run and confirm startup leaves them unchanged. Document future-only behavior and the missing-copy interruption limit in existing operator documentation, with no new button, route, toggle, permissions or historical repair workflow.

## 4. Integration verification

- [x] 4.1 Run focused sending/configuration tests and `make lint` during implementation using temporary databases/files and simulated providers only; verify no local `.env`, live mailbox credentials or production data is used.
- [x] 4.2 Run `npm ci` if implementing in a fresh checkout, stage only intentional regenerated output if any, then run `make check`, `make vulncheck`, and `make verify-converter` (dependency change). Record actual results and any unavailable coverage without weakening checks.
- [x] 4.3 Run `openspec validate --all --strict --json --no-interactive` and check all artifacts against the implemented behavior. Mark tasks complete only after their implementation and verification pass; distinguish local validation from hosted CI and live mailbox acceptance.
