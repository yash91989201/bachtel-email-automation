# Design

## Context

See `proposal.md` for motivation and the delta specs for behavior. This is a plan, not evidence that Sent-copy saving has been implemented or tested against either live mailbox.

Observed current behavior:

- `internal/sending/sender.go` defines the existing `Sender.Send(context.Context, Message) SendResult` boundary. Tests and candidate work share `Dispatcher.send` in `internal/sending/worker.go`.
- `Titan.Send` builds MIME once using pinned mailyak, submits through SMTP and returns `OutcomeSubmitted` after the final DATA acceptance. It performs no IMAP operation. `config.Titan` currently contains only the fixed SMTP address.
- `Graph.send` posts to `/me/sendMail` and deliberately omits `saveToSentItems`, retaining Microsoft's default of true. `preparedGraph` delegates to this same request. The existing Graph test asserts the message-only envelope, so native saving is already part of its request contract.
- `Dispatcher.send` records the outcome through `Store.CompleteAttempt` after the adapter returns. It currently holds the dispatcher mutex during completion. Attempts already have a `reason` field exposed through existing attempt/progress details. `CompleteAttempt` only accepts in-flight attempts, so it cannot be called a second time to record a copy warning.
- No IMAP client is present in go.mod/go.sum. Existing owner-scoped Titan credentials can authenticate both SMTP and IMAP.

## Goals / Non-Goals

**Goals:** Add the necessary Titan copy operation and document/test both providers' Sent-copy configuration while preserving the current package boundaries, provider switch, sender interface, approval flow and durable submission state machine.

**Non-Goals:** A provider-framework redesign, mandatory new adapter methods, capability registry, copy-job queue, new copy-status schema, mailbox polling, historical repair or new operator controls. No additional Microsoft permissions.

## Decisions

### 1. Keep native Microsoft saving and extend Titan's existing configuration

Keep the Microsoft request's existing omission of `saveToSentItems`: the official API specifies true by default and recommends specifying the parameter only to disable saving. Test and document that neither direct nor prepared Graph sending disables this behavior. Do not add IMAP, a second Graph copy request or Mail.Read/Mail.ReadWrite permissions. Graph 202 confirms acceptance only; native Sent Items saving and delivery can complete asynchronously and are not separately verified by the app.

Extend the existing Titan config/options with the GoDaddy IMAP endpoint `imap.secureserver.net:993`, using certificate/hostname-validated implicit TLS. Reuse the same bound mailbox username and encrypted password. Like the SMTP override, an IMAP endpoint override exists only for simulated tests; add no host, folder, credential or enable/disable field to the operator UI. Connection validation remains the existing SMTP authentication-only check, without sending or appending mail.

### 2. Add a small post-submission result hook, preserving Sender

Keep `Sender` and `PreparingSender` signatures unchanged. Add a narrow optional Sent-copy completion function to `SendResult`, taking a bounded context and the recorded acceptance time. On confirmed SMTP acceptance, Titan returns a hook capturing the exact MIME bytes already transmitted and its immutable bound connection. It does not append within `Send`.

Microsoft returns no hook because Graph handles saving natively. Document this convention alongside the existing sender/result types: no hook is correct only for a provider whose native request retains Sent saving. Future provider configuration and tests must establish native saving or supply this post-submission step. There is no user-selectable third mode that silently skips copies.

This avoids changing all senders and test doubles to implement an irrelevant manual-copy method, while keeping provider-specific transport work out of shared dispatch. A callback is process-local and never persisted or replayed. Building a provider strategy framework or performing IMAP inside `Titan.Send` was rejected: the former exceeds the request, and the latter delays durable recording of definite acceptance.

### 3. Persist acceptance before invoking the hook

In `Dispatcher.send`, capture the acceptance time when the sender reports definite acceptance and persist it as the submission completion timestamp before any copy work. Make the smallest change to the existing outcome/store contract needed to carry that timestamp; do not reuse a later IMAP completion time as the send time.

After successful `CompleteAttempt`, invoke a non-nil hook once outside the dispatcher mutex and all database transactions. Retain the worker's in-flight accounting until copy handling completes so stop, disconnect, replacement and shutdown still drain the old connection before releasing credentials. Use the existing server-owned request deadline for SMTP plus copy work; do not create untracked goroutines, hold global coordination while waiting on IMAP or reset an unlimited timeout. An exhausted deadline becomes a copy warning while the saved submission stays intact.

If outcome persistence fails, use the existing storage-failure barrier, skip the hook and retain conservative recovery. If the process stops after recording acceptance, startup keeps SUBMITTED and never reconstructs a missing hook or copy job. The original unavoidable gap between provider acceptance and database commit still uses existing review-required recovery.

### 4. Save Titan's exact MIME in its existing Sent folder

Use `github.com/emersion/go-imap v1.2.1`, pinned when implementing. The standard library has no IMAP client; an existing protocol library provides parsing, mailbox listing and APPEND without a handwritten IMAP stack. Its documented v1 API is sufficient for this bounded operation; no extension framework is needed.

Create a separate client per copy operation. Establish TLS with context-aware dialing, set the connection's absolute deadline before reading the greeting, and close the transport on cancellation. The library's default command timeout is unlimited, so enforce the outer deadline through login, discovery, APPEND and logout; configure its command timeout within that remaining budget as well. Do not share a client concurrently or enable protocol debug output.

Discover the existing folder carrying the `\Sent` special-use attribute when returned by LIST. If none is identified, accept the provider's existing exact `Sent` mailbox as the fallback. Reject ambiguous or missing destinations with a warning; do not create folders or append to Inbox. Append the original MIME with `\Seen` and the accepted timestamp as INTERNALDATE. Do not rebuild MIME or manufacture a new Message-ID. Drain listing channels and terminate any helper goroutine with the owning operation. End the session with logout/transport cleanup; IMAP CLOSE is not transport cleanup and must not be used to expunge mail.

Make one append attempt. A lost APPEND response is an unconfirmed copy, with no automatic retry; a confirmed append followed by logout failure stays successful. This is a best-effort Sent copy, not a second email transmission or a delivery receipt.

### 5. Reuse attempt details for warnings

On hook failure, use a narrow owner/connection-scoped store update to add a fixed safe warning to the already submitted attempt's existing `reason`, preserving its state, acceptance timestamp and candidate eligibility. Do not call `CompleteAttempt` again, introduce a failure sending state, pause solely for IMAP failure or grant test formatting approval. Use wording such as: "Titan accepted this email, but saving its Sent copy could not be confirmed."

Use existing attempt/progress details for display and notify those readers after the warning update. Verify candidate and test details present it as a copy warning rather than a failed send; no new button or page is required. If warning persistence fails, retain the previously committed result, report sanitized server diagnostics, and apply the existing storage-error protections. Never log provider responses, credentials or MIME content.

## Risks / Trade-offs

- Process failure after submission recording can leave no Sent copy → preserve SUBMITTED and accept missing copies without replay or backfill, as specified.
- IMAP latency can occupy Titan's existing bounded worker slots longer → share the request deadline, avoid holding global locks and test independent mailbox progress.
- Folder names/special-use support vary → prefer the advertised Sent folder, use only the existing exact fallback, and warn safely if neither is usable.
- SMTP can work while IMAP is unavailable or full → retain accepted sends and warn; no new setup gate or automatic resend.
- Native Microsoft saving is asynchronous → verify request configuration with simulated Graph and document that acceptance cannot prove copy completion or delivery.

## Migration Plan

1. Implement the focused config/result/dispatcher changes and simulated SMTP/IMAP/Graph tests. No historical data scan, mailbox mutation or schema reset is needed.
2. Run repository-required checks, including vulnerability analysis for the pinned new dependency, before deployment.
3. Deploy normally. Existing attempts are untouched; new sends use the provider's configured saving path. No reconnect or new Microsoft consent is required solely for this change.
4. Rollback restores the previous sending path; it must not remove Sent copies already created or replay accepted submissions. Any real mailbox acceptance check needs a separately designated test recipient and operator authorization; it is not part of automated verification.

## References

- [Microsoft Graph sendMail](https://learn.microsoft.com/en-us/graph/api/user-sendmail?view=graph-rest-1.0): native Sent Items default, Mail.Send permission and asynchronous 202 semantics.
- [GoDaddy Titan IMAP settings](https://www.godaddy.com/help/use-imap-settings-to-add-my-professional-email-powered-by-titan-to-a-client-32204): fixed server, TLS port and mailbox credentials.
- [go-imap v1.2.1 client API](https://pkg.go.dev/github.com/emersion/go-imap@v1.2.1/client): Append, List, New, command timeout and session termination behavior.
