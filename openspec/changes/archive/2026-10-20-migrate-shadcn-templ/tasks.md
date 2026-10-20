# Tasks

## 1. Baseline and component setup

- [x] 1.1 Capture synthetic representative desktop/mobile baseline screenshots and key computed styles before editing the UI; retain the reusable fixture server and recorded artifacts.
- [x] 1.2 Install pinned, locally owned shadcn-templ components under internal/web/ui; configure theme/source paths and record provenance/maintenance instructions; verify generation and component rendering.

## 2. Page migration

- [x] 2.1 Migrate campaign, task and sending pages to the owned primitives without changing copy, control attributes, routing or persistent workspace state; verify rendering and relevant behavioral tests.
- [x] 2.2 Migrate layout, overview, login and mailbox pages, preserve visual theme and add regression coverage for native forms, disabled actions and accessible labels; verify rendering tests.

## 3. Build integration and cleanup

- [x] 3.1 Remove daisyUI dependency/plugin/theme/classes and obsolete styles, update npm lock, Make/Docker/watch/check configuration and contributor documentation; verify fresh npm ci, deterministic assets and template generation.

## 4. Integration verification

- [x] 4.1 Compare representative desktop/mobile pages with the baseline and verify tab keyboard navigation, form-state preservation, htmx progress updates and native controls using isolated synthetic data; record actual outcomes and fix regressions.
- [x] 4.2 Run make lint, make check, make vulncheck, make verify-converter and openspec validate --all --strict --json --no-interactive; report actual results and remaining limitations without treating local checks as deployment or production release acceptance.

## 5. Requested preset after migration

- [x] 5.1 Resolve and apply preset b870VEw0in with the pinned shadcn-templ release, update theme/configuration and document the intentional theme change while preserving application layout and workflows; verify exact preset settings and local assets.
- [x] 5.2 Verify the preset on desktop/mobile and run affected formatting, build, lint, template, application and OpenSpec checks; record any pre-existing failures separately from migration regressions.

## 6. Documentation and configuration follow-up

- [x] 6.1 Audit README, deployment, development, UI maintenance and release guidance against the migrated build; update active documentation without rewriting historical verification records.
- [x] 6.2 Check asset embedding, container context and development watching for local fonts and component sources; correct gaps and verify the final configuration and documentation together.
