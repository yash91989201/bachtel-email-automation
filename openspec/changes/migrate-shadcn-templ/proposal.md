# Proposal

## Why

Replace daisyUI with shadcn-templ while preserving the existing Bachtel interface. The operator should see the same layout, colors, spacing, typography, content and workflows after the migration.

## What Changes

- Adopt pinned, locally owned shadcn-templ components in the existing Go/templ application.
- Replace daisyUI components and theme utilities across every page without redesigning the UI.
- Update asset generation, development, production builds, dependencies and contributor guidance.
- Verify appearance, responsive behavior, native form contracts, accessible interactions and dynamic updates with synthetic data.
- After the migration baseline is verified, apply the subsequently requested preset `b870VEw0in`; this intentionally changes palette, typography and component styling while preserving the page structure and workflows.

## Capabilities

### New Capabilities

None. This is an implementation refactor; `skip_specs: true` applies.

### Modified Capabilities

None. Existing operator-access, campaign-templates, excel-intake, email-dispatch, sending-recovery and history-exports requirements remain unchanged.

## Impact

Affects `internal/web/views`, a dedicated `internal/web/ui` component package, `web/css`, asset scripts, dependency locks, build/development/deployment configuration, and UI regression coverage. No database or endpoint changes, no live email, no deployment, and no release-gate changes. Existing production candidate-sending authorization and release verification remain prerequisites for real sending.
