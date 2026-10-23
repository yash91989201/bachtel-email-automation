# Design

## Context

See `proposal.md` for motivation. Current routes register GET `/account` for Mailbox, GET `/account/ai` for AI and GET `/connection` as a second Mailbox entry rendering the same page. `account.templ` already uses real links for the two sections, not the workspace hash adapter. No `/account/mailbox` route exists. The earlier requested mailbox URL is also absent from the current main specs; this change captures it explicitly.

Mailbox forms and OAuth use `/connection/...`; `internal/config/config.go` fixes the registered Microsoft callback at `/connection/callback`. Provider setup uses the view-only `change_provider=1` query and `#provider-setup` anchor. AI POST endpoints already live under `/account/ai`, and current main specs already require editable preset endpoints, model discovery and exact-model validation; routing work does not imply those features are implemented or verified.

## Goals / Non-Goals

**Goals:** One canonical URL per Account section; explicit 404 behavior for unused landing URLs; correct section-specific links and feedback; unchanged connection ownership and pending-task protection.

**Non-Goals:** Moving OAuth callbacks, renaming all mutation URLs, changing provider/model support, implementing discovery gaps, altering credential validation, or sharing the campaign/task tab adapter with Account.

## Decisions

### 1. Canonical GET destinations without landing aliases

| Surface | Observed | Planned |
| --- | --- | --- |
| General Account entry | `/account` renders Mailbox | Unserved, 404 |
| Mailbox | No dedicated account path | Protected `/account/mailbox` renders Mailbox |
| Legacy Mailbox landing | `/connection` renders Mailbox | Unserved, 404 |
| Unused connections landing | `/account/connections` | Unserved, 404 |
| AI connection | `/account/ai` | Retain |
| Provider chooser | `/account?change_provider=1#provider-setup` | `/account/mailbox?change_provider=1#provider-setup` |
| Mailbox callback | `/connection/callback` | Retain |
| Replacement review | `/connection/replace` | Retain |

Reuse the existing Mailbox view-model builder and page behind authentication. Remove the old `/account` and `/connection` landing registrations; do not add `/account/connections`. Parent paths do not need pages for their child routes to work. These unused URLs return 404, including when given old provider/notice queries. Do not add redirects or a query-forwarding compatibility helper.

Generated links use `/account/mailbox` or `/account/ai` directly. Mailbox still accepts its existing supported non-secret setup query state and `#provider-setup` anchor on its canonical path.

Alternative rejected: keeping or redirecting old landing pages solely for bookmarks. The operator chose the simpler route set and accepts those old landing bookmarks returning 404. Keep the existing singular `/account` convention for both sections.

### 2. Keep operational URLs; canonicalize return destinations

Do not redirect or rename POST `/connection/...` actions, GET `/connection/callback`, or replacement review/confirmation routes when removing the old landing pages. These endpoints execute existing ownership, CSRF, OAuth state, lock and explicit replacement checks. After they finish, ordinary Mailbox returns use `/account/mailbox`; replacement-required returns still use the existing explicit replacement flow. Callback landing URLs contain only safe provider/notice values, never OAuth code/state. Keep callback configuration and provider registration unchanged.

On refused forms, preserve the existing Mailbox error rendering and non-secret drafts through `AccountPage`/`ConnectionPage` as appropriate. Full-page feedback remains visibly in Mailbox; no secret is echoed. Successful AI validation/confirmation keeps its current `/account/ai?notice=...` return; discovery stays a POST response and is not triggered by navigation.

Alternative: moving every action under `/account/mailbox` expands callback registration, old-form and replacement migration work. The user-facing section URL can be fixed without doing that.

### 3. Audit every caller, not just the section tabs

Update the Mailbox sidebar link in `account.templ`, Account entry links in `layout.templ`/`home.templ`, setup/repair/provider-choice links in `connection.templ`, and task mailbox remedies in `task.go`. Search for remaining generated `/account` or `/connection` landing links across views and handlers; no compatibility landing aliases remain; old landing references belong only in 404 regression tests or documentation of current behavior. Preserve the Account active state for both canonical paths and ordinary accessibility IDs.

AI setup links in task preparation already use `/account/ai`; keep them and assert that they do not regress to Mailbox. Keep both sections' named blocking-task warnings, same-configuration key repair and same-mailbox reconnect behavior intact.

### 4. Scope and integration boundary

Only `account-settings` needs a delta: its routing contract owns both sections. `ai-connections` remains the source of provider/discovery/validation behavior, and mailbox specs retain their operational contracts. This change does not mark existing provider behavior implemented merely because its specs exist.

The audit's only top-level fragment-selected workspaces, campaign and task, are handled by `use-paths-for-workspace-sections`; its design contains the complete page inventory. This account change can be applied independently. When both are implemented, merge task mailbox remedy changes against the new workspace URL helpers and run final checks on the integrated tree.

## Risks / Trade-offs

- Removing `/connection` could accidentally remove its child operations -> remove only the exact landing registration, retaining explicit callback, replacement and POST routes; pin with HTTP tests.
- Old landing bookmarks stop working -> this is intentional; update every generated internal link and verify unused landing URLs return 404 without redirects.
- A mailbox action may still return through the old URL -> inspect every redirect and test settings, callback success/failure, disconnect and replacement return paths.
- Routing may accidentally trigger provider calls -> use simulated services and verify landing navigation causes no discovery, validation, reconnect or dispatch.

## Migration Plan

Add the canonical Mailbox GET, remove the two old landing registrations, leave `/account/connections` unserved, then update generated links and operation return destinations. Retain the existing callback URL and actions so deployed provider registrations and old forms continue working. Regenerate only changed templates and run focused account/mailbox HTTP and view tests before the final checks. No data migration is needed. A code rollback restores the old mailbox entry; bookmarks newly created at `/account/mailbox` would then require the older `/account` entry.
