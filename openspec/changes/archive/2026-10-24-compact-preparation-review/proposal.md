# Proposal

## Why

The preparation review expands hundreds of corrections and accepted recipients into the page, repeats held-entry evidence in several tables, and mixes report downloads, dataset approval and edited-sheet upload in one action stack. Operators must scroll through routine results to find issues and decide what to do next.

## What Changes

- Lead with compact, clearly labeled totals; keep backend accepted/held outcomes distinct from AI correction/finding counts.
- Prioritize held entries and their reasons. Collapse routine accepted-recipient and correction lists behind count-bearing disclosures, with every long review list in a height-constrained scroll area.
- Keep detailed AI findings separately inspectable, while using the backend-held table as the canonical actionable list when that review is available. Preserve provisional evidence when backend validation is pending and historical evidence after finalization.
- Group audit/exclusion reports in one secondary download area, give approving the reviewed prepared data a clear primary path, and put the download–edit–upload workflow behind an explicit secondary disclosure.
- Keep blockers and operation errors visible; open the relevant edit path after a rejected upload. Retain all existing confirmation, ownership and sending prerequisites.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-task-workspaces`: compact preparation review, bounded evidence lists, and clearly separated dataset-finalization paths.

## Impact

- Primarily `internal/web/views/task.templ` and its generated output, with existing shadcn-templ Accordion/ScrollArea/Table components and narrowly scoped styling or optional ScrollArea viewport-attribute support if needed for accessible naming. No new dependencies.
- Extend existing view and preparation workflow regressions and update operator documentation for the revised disclosure/download layout.
- Prerequisites are already present in the checkout: automatic completed-run selection, backend prepared review, and the separate sanitization report. The pending `add-ai-sample-preview` change is independent.
- No change to AI requests, normalization, validation/classification, stored evidence, workbook formats, routes, approval rules or sending operations. Pagination, filtering, editing individual rows in the browser and a general workspace redesign are outside scope.
- Planning only: implementation and verification tasks remain pending.
