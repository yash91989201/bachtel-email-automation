# Tasks

Planning only; all tasks remain incomplete. Use temporary data and simulated providers.

## 1. Implementation and focused verification

- [x] 1.1 Add one owner-scoped confirmed AI configuration with staged proposals, configuration generations and separately encrypted/versioned credentials using existing secret storage; verify temporary-database ownership, restart, missing-key, no-prefill and redaction tests and document Account setup without deployment-wide provider keys.
- [x] 1.2 Verify current OpenCode Go deployment access, permitted usage, data handling and exact Muse/DeepSeek HTTP contracts independently of Pi; document readiness limitations and implement small Responses and Chat Completions adapters with OpenCode Go and Custom compatible setup, bounded requests, explicit model selection and no fallback. Verify simulated-server tests for both protocols, malformed output, timeout, throttling and size limits; surface any dependency or access-model change before adding it.
- [x] 1.3 Implement synthetic exact-model validation and explicit configuration confirmation, staging outside activation transactions, same-configuration key repair and safe endpoint policy; verify failures/cancellation/stale callbacks retain active settings, private-address and cross-origin redirect requests cannot receive secrets, in-flight repair settles, validation does not launch work and AI switching never resets task/history data.
- [x] 1.4 Implement the account-wide pending-task predicate and owner-scoped serialization across task staging/reactivation, AI settings activation and every mailbox change/reset/disconnect path; verify race tests, cross-campaign blockers, staged/failed/paused/stopped/scheduled states, completed-task recovery reacquisition, settled-operation checks, initial setup and same-identity credential repair exceptions.
- [x] 1.5 Implement idempotent terminal Close task with durable actor/time, queue/schedule cancellation, bounded settlement and worker/dispatch publication barriers; verify active/staged/stopped tasks, late provider responses (preparation publication is integrated by the next change), unknown submission outcomes, retained history/reservations, manual resolution, foreign requests and refusal of every subsequent processing/test/sending action. Document completion versus stop versus close.
- [x] 1.6 Preserve existing mailbox settings/credentials/generations when relocating setup and applying locks to historical tasks; verify Microsoft callback, Titan reconnect, same-mailbox rotation and confirmed replacement/reset tests, including that pending tasks cannot be erased to bypass the lock and mailbox replacement retains AI settings.
- [x] 1.7 Add Account settings navigation and Mailbox/AI sections with provider-first setup, exact model/API-format guidance, confirmation, status, non-secret saved values and repair controls; verify owner-only routes, legacy mailbox navigation, validation feedback and named blocking-task links across campaigns. Document future preset/adapter additions and provider data handling; native MiMo/Fireworks presets remain future work.
- [x] 1.8 Add explicit Close task confirmation, closing/closed states and retained history/download access; verify UI/handler tests for terminal action refusal and pending-task lock release only after settlement, plus responsive/keyboard checks for all affected Account and task pages.

## 2. Integration verification

- [x] 2.1 Verify this change independently with temporary databases and simulated providers, including ownership, failures, races and the proposal dependency/activation boundary; record results and do not enable downstream unfinished workflows.
- [x] 2.2 Run npm ci on a fresh checkout, format/regenerate only changed templ files, stage intentional generated output and run make check, make vulncheck, make verify-converter and openspec validate --all --strict --json --no-interactive; report actual local results and skipped coverage without claiming deployment.

## Workflow follow-up

- Begin only on an explicit apply request and after verified prerequisites.
- Archive only after implementation and required verification.
