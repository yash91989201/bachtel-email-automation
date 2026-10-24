# Proposal

## Why

Campaign and task sections currently use URL fragments, so the server cannot select a section from its URL. Give each section a directly loadable path using ordinary server-rendered pages and native browser history, while retaining the visual workspace layout and section-local live updates.

## What Changes

- Give campaign Tasks, Template & preview, Activity & downloads, and Settings dedicated section paths.
- Give task Overview, Recipients, Test & approve, Sending, and Results dedicated section paths.
- Update section navigation, breadcrumbs, next-action/remedy links and successful mutation return destinations to use those paths.
- Replace tab buttons and hidden panels with normal section links; each GET renders only the selected section, and browser navigation handles Back/Forward.
- Remove custom section selection/history JavaScript. Keep rejected actions on their relevant section, and preserve drafts/focus during section-local polling. Full-page section navigation does not guarantee unsaved draft retention.
- Stop using old fragments and aliases for section selection; no JavaScript bookmark translator is retained. Keep legitimate within-section anchors and disclosure behavior.
- Keep the current default entries, protected ownership checks and explicit action boundaries.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-task-workspaces`: Native link navigation, server-selected sections, removal of fragment selection and section-aware action feedback.

## Impact

Expected implementation touches `internal/web/{campaign,task,sending,preparation,finalization}.go`, `internal/web/views/{campaign,task}.templ`, generated template output and `web/js/workspace.js`, with related HTTP/view tests. No new dependency, database migration or domain-service redesign is needed. This supersedes the fragment-format decision in the archived workspace design; the existing action, status and authorization requirements remain binding, while navigation now uses full-page requests. The delta explicitly revises the task workspace and legacy history behavior to match.

Account/Mailbox/AI URL consistency is owned by the independent `canonicalize-account-section-paths` change. No account route needs to change for this change to work. Existing action and download endpoints remain supported; this does not authorize real mail, external AI calls or deployment.
