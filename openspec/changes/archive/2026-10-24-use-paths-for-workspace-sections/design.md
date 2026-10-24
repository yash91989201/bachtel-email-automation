# Design

## Context

See `proposal.md` for motivation. Campaign and task currently use the owned tabs components, render every section as a panel, and rely on `web/js/workspace.js` for fragment selection, browser history and swap synchronization. Their existing view models accept a server-selected section. Campaign `/tasks` and task `/sending` already render the shared page with those sections selected.

The operator explicitly chose ordinary server-rendered navigation: normal links, a fresh page request per section, and native browser history. This supersedes the archived workspace design's fragment URL choice and this change's earlier proposal to preserve client-side panel switching. Main specs remain unchanged during planning; the delta records the revised behavior.

### Final audit list

| Surface | Observed selection | Planned destination | Owner |
| --- | --- | --- | --- |
| Campaign Tasks | campaign root `#tasks`; existing `/tasks` GET | `/campaigns/{id}/tasks` | This change |
| Campaign Template & preview | `#template` | `/campaigns/{id}/template` | This change |
| Campaign Activity & downloads | `#activity` | `/campaigns/{id}/activity` | This change |
| Campaign Settings | `#settings` | `/campaigns/{id}/settings` | This change |
| Task Overview | task root or `#overview` | `/campaigns/{id}/tasks/{task}/overview` | This change |
| Task Recipients | `#recipients` | `/campaigns/{id}/tasks/{task}/recipients` | This change |
| Task Test & approve | `#test` | `/campaigns/{id}/tasks/{task}/test` | This change |
| Task Sending | `#sending`; existing `/sending` GET | `/campaigns/{id}/tasks/{task}/sending` | This change |
| Task Results | `#results` | `/campaigns/{id}/tasks/{task}/results` | This change |
| Account Mailbox | `/account`, legacy `/connection` | `/account/mailbox` | `canonicalize-account-section-paths` |
| Account AI connection | `/account/ai` | Retain `/account/ai` | `canonicalize-account-section-paths` |
| Mailbox provider chooser | `?change_provider=1#provider-setup` | Same view state/anchor under `/account/mailbox` | Account change |
| Accessibility skip link | `#main-content` | Retain | No section migration |
| Accordions, labels and htmx targets | DOM IDs and selectors | Retain | No section migration |

The repository-wide view/JS audit found no other application page using fragments to select top-level sections. Upload, status, login, mailbox replacement, downloads and stored-email views are existing distinct routes. This is an audit of application pages, not PocketBase's separate admin dashboard.

## Goals / Non-Goals

**Goals:** Keep the visual sidebar, identity and section content; make each section a standalone server-rendered destination; delete section-navigation JavaScript and use native browser behavior.

**Non-Goals:** Client-side routing, hidden inactive panels, intercepted section clicks, custom history restoration, cross-page draft storage, legacy fragment translation, dispatcher changes or unrelated UI primitive changes.

## Decisions

### 1. Explicit section GETs reuse existing server boundaries

Register finite allowed GET section routes on `http.ServeMux`, reusing campaign `/tasks` and task `/sending`. Pass the corresponding section into the existing view-model builders; keep `requireOperator`, campaign ownership and campaign-task membership checks on every entry. Unknown section paths return 404. Keep existing root GETs as default Tasks/Overview entries, but generate explicit section links everywhere.

Small campaign/task path helpers keep links and redirects consistent. Do not introduce a generic router registry or alter action/download method contracts. Results GET can coexist with Results POST; Sending GET must not swallow progress or action routes.

### 2. Native navigation and selected-section rendering

Replace the two workspace tab wrappers/triggers with ordinary sidebar navigation anchors, preserving their visual styling. Mark the selected destination with `aria-current="page"` and visible focus. Use standard link keyboard behavior; remove tab/tablist/tabpanel semantics from these page-level sections. Keep reusable tabs primitives untouched for unrelated consumers.

Render shared identity/navigation plus only the selected section using the view model's validated section. Inactive forms, panels and pollers are absent from the response. Keep existing view-model loading initially unless a small required adjustment is needed; a broader query optimization is out of scope. Every section link performs native page navigation. Do not intercept clicks, use `hx-boost` for section links, call `pushState`/`replaceState`, or install `popstate`/`hashchange` section handlers. Copy link, modified click, reload and Back/Forward work through the browser.

Unsaved inputs and disclosure choices are not guaranteed to survive section changes. Do not add local/session storage, persisted UI state or a draft framework to emulate the old panel behavior. Existing saved `sending_type` remains authoritative; `sending_type=choose` stays a view-only query on Sending and cannot change saved preferences.

Alternative rejected: enhancing path links to keep all panels loaded. It retains the custom routing/tab machinery the operator asked to remove.

### 3. Remove fragment section selection without a compatibility script

Remove the workspace section-hash parser, legacy section alias map, history writes, hash-link rewriting and active-tab restoration after swaps. Old root `#template`, `#recipients`, `#recent` and similar fragments no longer select sections; the root renders its default. An existing explicit `/sending` URL continues to select Sending regardless of an obsolete section fragment. Browsers do not send fragments to the server, so supporting those old selections would require the client-side translation intentionally excluded here.

Retain legitimate within-section anchors, `#main-content`, ARIA IDs, htmx selectors and disclosure state logic. Old fragment bookmarks are not guaranteed to reveal their former section; update generated links instead. Results history is available through its ordinary keyboard-operable disclosure on the Results page, without a legacy `#recent` opener.

### 4. Retain section-local interactivity and correct error feedback

`workspace.js` also preserves collapsible/accordion choices during swaps and resets restored sending confirmations. Keep those still-required functions, reducing or renaming the adapter only if useful. Do not delete the entire file indiscriminately. htmx progress/preview updates, approval confirmations, forms and accordions keep their existing behavior inside the selected section. Polling must preserve valid drafts, disclosure state and focus and invalidate stale confirmations; it cannot switch sections. No section-navigation JavaScript is required for any of this.

Successful POSTs redirect to the canonical section GET after existing operation checks. Refused POSTs render the full page with the operation's section selected and safe valid input retained. Do not add a client-side URL rewrite or replay POSTs to make an error page's address look like a GET section. The error page may retain its action URL until normal navigation; native reload can have standard form-resubmission semantics. Normal section links remain usable from that page.

### 5. Complete link migration and Account ownership

Update breadcrumbs, task rows, next-action/blocker helpers, preparation/finalization returns, exports and forms end to end. Remove section-fragment form hints; preserve actual within-section anchors and htmx targets. Account routing stays in `canonicalize-account-section-paths`, including task mailbox remedies. The changes are independently applicable; settle shared helper edits before an integrated final validation batch.

## Risks / Trade-offs

- Full-page navigation may lose unsaved form values -> accepted native behavior; do not promise cross-section persistence or add custom draft storage.
- Old fragment bookmarks no longer select their old section -> accepted removal; generate path links everywhere and test that obsolete fragments cannot select hidden sections or trigger operations.
- Removing the tab adapter too broadly could break polling/disclosures or confirmation reset -> retain and verify those section-local responsibilities.
- Selected-section rendering may leave a handler referring to absent markup -> check preview/progress targets and error pages for the selected section.
- Rejected POST reload can ask to resubmit -> standard browser behavior; preserve existing server-side repeat-request protections and do not introduce a draft/session subsystem merely to hide it.

## Migration Plan

Add protected GET section routes and helpers, replace workspace tab markup with native links and selected-section rendering, migrate all generated links/returns, then remove only section-navigation JavaScript. Regenerate only changed templates. Verify focused route/view behavior and section-local interactions before final gates. No data migration, provider registration change or new dependency is needed. A code rollback restores prior navigation without modifying task data; newly bookmarked section paths would no longer resolve on the old version.
