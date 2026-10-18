# Tasks

Prerequisite: add-campaign-template-authoring (the campaign page and mapping textarea already exist). No schema, route or dependency change.

## 1. Draft derivation

- [x] 1.1 Add a campaign-package helper that turns a comma-separated expected recipient header into required `text` mappings, deriving each variable name by lowercasing the header and replacing every non-alphanumeric run with one underscore; verify multi-word headers, punctuation (`Department/Division`), duplicate names, empty entries and headers that cannot form a variable name.
- [x] 1.2 Verify a draft built from an expected recipient header passes the existing mapping validation for realistic header lists.

## 2. Campaign page pre-fill

- [x] 2.1 Render the drafted mappings in the mapping textarea when the campaign has no content revision, and leave the textarea empty when it has one; verify both pages in a browser-level test.
- [x] 2.2 Keep the submitted mapping text in the textarea when a revision submission fails validation; verify the re-rendered page keeps the operator's lines instead of the draft.

## 3. Verification

- [x] 3.1 Run the campaign and web package tests plus `make test`; verify the new-campaign page shows the draft, a revision-backed page shows an empty table, and no revision is created by loading a page.
