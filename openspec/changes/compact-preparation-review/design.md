# Design

## Context

See `proposal.md` for the reported problem and scope. Observed in the current checkout:

- `taskPreparation` in `internal/web/views/task.templ` renders the instruction editor, run table, findings, corrections and provisional held entries directly in page flow. `internal/web/preparation_data.go` caps each evidence list at 100 and separately counts omitted entries; this is already a truncated preview, not the complete audit.
- `taskFinalization` renders all backend-accepted entries before held exclusions. Its accepted table omits the shared Table primitive, producing inconsistent spacing. `taskFinalExclusions` is shared by prepared and selected-final reviews and has no vertical height bound.
- Backend outcome totals are repeated in preparation and finalization; sanitization, exclusion and sending-sheet downloads are spread around approval and upload forms.
- The checked-in `scrollarea.ScrollArea` component is already used for task history with a content-sized viewport and a maximum height. It has an overflow-measuring script and supports horizontal scrollbars via `scrollarea.ScrollBar`. Accordion, Table and button primitives also exist. No component installation is needed.
- Existing view regressions are in `internal/web/views/finalization_test.go` and `workspace_test.go`; preparation workflow, finding evidence and report gating are covered in `internal/web/preparation_test.go`.

## Goals / Non-Goals

**Goals:** A compact default view, one actionable backend-held list, consistent bounded evidence tables, and a visually clear choice between approving the reviewed data and editing a sheet. Reuse existing view data and components.

**Non-Goals:** See proposal exclusions. In particular, a scroll container does not change the existing 100-item evidence preview cap or fetch additional records. Complete audit remains the existing workbook download.

## Decisions

### 1. Separate summaries, evidence and operations

Use the existing cards and shadcn-templ Accordion for full-width, keyboard-operable evidence headers. Each header names the evidence and total count. Keep totals and important guidance in ordinary card content rather than inside a list viewport.

Proposed normal completed-run layout:

```text
AI data preparation
  Selected run · N corrections · N unresolved AI findings
  [Unresolved AI findings · N]                 collapsed audit detail
  [Resolved corrections · N]                  collapsed routine detail
  [Preparation settings and run history]      secondary disclosure

Final sending data
  Source rows · Recipient entries · Accepted · Held
  Held entries · N                           open when nonzero
    bounded ScrollArea with reasons
  [Accepted recipients · N]                   collapsed
  [Download review reports]                   grouped secondary links

  Use prepared data
    required review confirmation
    [Approve prepared data as-is]
    visible dataset/formatting/sending distinction

  [Edit data before approval]                 collapsed alternative
    1. Download prepared sending sheet
    2. Edit in Excel
    3. Upload and validate final sheet
```

Run history can share the settings disclosure, but latest queued/running/failed state and retry actions must remain visible outside it. Keep the current draft/instruction semantics and explicit older-run selection. Avoid hiding the configuration remedy on an unconfigured account or instruction editor before the first preparation; the secondary disclosure is the normal completed-run presentation.

For current role-mapping tasks, show backend recipient totals in the final review once, and AI corrections/findings in the preparation summary. Remove the redundant provisional held-recipient table only when the backend review supplies the canonical reasons. Keep the distinct field-level AI-finding audit collapsed. For legacy tasks or missing backend review, retain accessible provisional held evidence and pending labels. For finalized tasks, show the selected final outcome and historical preparation labels.

Alternative considered: merely cap the old expanded tables. This bounds scrolling but leaves repeated summaries, duplicate held evidence and competing action groups, so disclosures and hierarchy are also needed. Do not introduce filtering, pagination or client-side data stores.

### 2. Use shadcn-templ ScrollArea for every long review list

This is an explicit user requirement. Wrap accepted recipients, backend/provisional held entries, AI findings, corrections, preparation runs and final exclusions with `scrollarea.ScrollArea`; do not substitute a plain `overflow-auto` div. Bound the actual viewport, reusing the history pattern with content height and `max-height: min(20rem, 60vh)` for review lists. A small wrapper template is reasonable if it centralizes this repeated viewport contract without changing the shared UI primitive.

Use the shared Table primitive for all these tables, sticky header cells with an opaque theme background, wrapped reason text, and adequate scrollbar clearance. Provide a horizontal ScrollBar for wide tables. Do not retain an inner `.table-scroll` overflow wrapper that would become a second competing scroll container or defeat sticky headings.

Keep an accessible name associated with the scrollable evidence and verify the actual viewport receives keyboard focus when overflow exists. The component initially renders `tabindex="-1"`, and `scrollarea.js` changes it to `0` when its overflow measurement detects scrollable content; preserve and exercise that mechanism after disclosure opening. Root attributes currently target a presentation wrapper rather than the viewport, so naming the table/section alone must not be mistaken for naming the focusable viewport. If needed, add an optional viewport-attributes field to the existing ScrollArea props and pass a region role and accessible name there, preserving all existing callers and defaults. Scope any such adjustment to the existing component and its focused accessibility regressions. Test initially collapsed accordions opened after page load, short and long contents, and resizing, because scrollbars are measured at runtime.

Leave the 100-item evidence limits unchanged. Put “Showing X of Y” and full-report guidance outside each viewport when items are omitted. Accepted/backend-held entries currently supplied in full remain inspectable in full. Do not promise complete scrolling access to truncated AI evidence.

### 3. Group downloads and split finalization paths without changing guards

Use one secondary report disclosure for the sanitization workbook and the applicable exclusion workbook, retaining existing hrefs and precedence. Give each a short purpose description; avoid duplicate links elsewhere in the card. The prepared sending-sheet link belongs to the edit path because that is the workbook intended for re-upload. The accepted final-sheet download remains directly available with the selected-final summary.

Keep the existing confirmation checkbox and approve POST in a primary area. Put the sending-sheet download, edit guidance, file input and upload POST in a secondary accordion/disclosure. Keep a short no-AI/deterministic-validation explanation visible inside that path; longer supported-edit/fallback detail can be secondary help.

Use existing availability flags and error state rather than adding a second eligibility model. Open the edit path when finalization returns an error, when editing is the available remedy before review, or when accepted count is zero; otherwise it is collapsed by default. A selected final snapshot still supports replacement only under the existing guards, with the current fresh-test/received-format warning visible. Never move the primary operation error into a closed disclosure. Opening a disclosure is UI state only; retain form endpoints, action IDs, file constraints and approval checks.

Alternative considered: putting approve and upload into equally prominent adjacent cards. A primary approve area plus a secondary editing disclosure better reduces the reported clutter and stacks naturally on narrow screens.

### 4. Validate behavior and geometry with isolated data

Extend existing parsed-HTML view contracts for default disclosure states, held-before-accepted ordering, ScrollArea viewport constraints, truncation labels, single canonical download locations, and upload-error expansion. Preserve preflight, legacy, selected-final, zero-accepted and blocked/closed/started-task states.

Run the existing preparation integration tests for findings, automatic selection, older-run selection and protected report downloads. Exercise the rendered page in a local browser using a temporary fixture and simulated provider, with the reported scale (150 source rows, 151 entries, 138 accepted, 13 held, more than 200 corrections), long reason text, and many runs. Check 360/768/1440 widths and keyboard scrolling; record actual observations. No live task data, local `.env` or real mail is needed.

## Risks / Trade-offs

- Hidden issue detail could be missed → show held/finding counts and remedies outside disclosures; open backend-held entries by default and retain all reasons.
- Nested overflow or initially hidden viewport measurement could break scrolling → one ScrollArea viewport per list, opening/resizing browser checks, and no competing overflow wrapper.
- Truncated evidence could look complete → explicit shown/total labels and the existing complete sanitization download when selected and available.
- Disclosures could hide a rejected upload → derive expansion from existing error state and keep the error visible.
- Less page height does not reduce DOM/render payload → retain current data bounds; server-side pagination remains outside this change.

## Migration Plan

No data or schema migration. Implement the view changes, regenerate only changed templates and rebuild assets, verify with isolated fixtures, then run the repository checks. Rollback restores the former view/layout; stored runs, snapshots and workbooks do not change. Deployment and real-email acceptance are outside this change.
