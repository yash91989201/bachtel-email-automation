# Design

## Context

The existing application uses Go, templ, htmx, Alpine.js, Tailwind CSS 4.3.3 and daisyUI 5.7.50. Eight view templates depend on daisyUI classes and a custom 1,152-line stylesheet already defines the Bachtel appearance. The working tree was clean at discovery. All earlier changes are archived; this change does not complete or bypass any production sending release gate. See proposal.md for motivation.

## Goals / Non-Goals

Preserve the current visual interface and all native form, authentication, operator authorization, preview, export, scheduling and recovery behavior. Use real shadcn-templ components with a coherent internal Go package structure. Do not redesign the app, alter server routes or data, add React, or replace established Alpine workspace state and htmx polling.

## Decisions

1. Pin shadcn-templ v2.0.0-beta.15 and use its supported copied-source workflow. The v2 package-import workflow is explicitly experimental. Pin maintenance commands and record provenance; avoid blindly applying future upstream updates. Keep locally owned components under `internal/web/ui`, independent of page-specific view models in `internal/web/views`.
2. Install only components needed by this app: static rendering primitives for buttons/links, cards, badges, alerts, inputs, textarea, labels, native selects and tables where their semantics match existing markup. Keep native controls and the existing Alpine workspace tab implementation so form state and keyboard behavior survive polling. Add no browser runtime when static components suffice.
3. Preserve the current Bachtel CSS values and map theme utilities to Tailwind theme tokens. Adopt the components' source API while adapting presentation classes to Bachtel. Remove daisyUI tokens, plugin, dependency and implicit CSS behavior. Do not merely rename daisyUI classes or retain an undeclared compatibility layer: each primitive must be owned by an actual component or explicit application styling. Keep page layout styles separate from component styles and theme tokens.
4. Extend the current Make/npm pipeline instead of introducing Task alongside Make. Generation and format checks cover both views and owned UI templates. Production assets remain embedded in the single Go binary with no CDN. Update Docker source copying and development watching for the new component location.
5. Preserve native submission attributes and defaults explicitly, especially button submit types, multipart/file controls, input values and constraints, labels, booleans, htmx attributes and Alpine bindings. No on-load component action can send email. Native disclosure, checkbox and select controls remain native where no richer interaction is needed.

## Risks / Trade-offs

- Beta APIs and upstream style changes: pin exact provenance and retain source ownership; validate generated Go against the repository's templ version.
- Removing daisyUI loses implicit layout, form and status styling: capture baseline screenshots/computed styles before edits and compare synthetic representative pages at desktop and mobile sizes.
- New wrapper markup can affect selectors, labels or native form behavior: prefer same-tag rendering, inspect actual HTML and test critical form/disabled/selected/multipart contracts.
- Shared account usage is variable: use at most two focused Luna children, bounded context, phase-boundary usage checks and a reserve for final validation. Do not redeem resets or buy credits.

## Migration Plan

Capture synthetic baseline pages; set up pinned component sources and tokens; migrate independent page groups; integrate and delete obsolete daisyUI configuration and CSS; regenerate; verify browser appearance and dynamic state; run full repository checks. Rollback is reverting this cohesive source/configuration change and rebuilding assets; no database migration is involved.

## Requested preset after the migration

After migration and original-appearance verification, the operator requested
`--preset b870VEw0in`. This intentionally supersedes preservation of the old
palette, typography and corner styling. The exact beta.15 CLI resolved the
preset in an isolated module: base-luma, mist base, cyan theme, lime charts,
Noto Sans body, EB Garamond headings, Remix Icon preference, small radius
(0.45rem), inverted menu color and bold menu accent. Preserve application
structure, content and workflows while activating the upstream Luma style and
resolved theme variables. Fonts are self-hosted; no runtime CDN is introduced.
Unused chart/menu/icon settings are retained for future components without
adding unused runtime libraries.
