# Proposal

## Why

Campaigns need reusable, reviewable email configuration rather than a hard-coded spreadsheet/template contract. A separate subject and uploaded DOCX body must support configurable variables, explicit missing-value behavior and predictable normalization without losing the agreed formatting.

## What Changes

- Add campaign creation and immutable configuration revisions with a separate subject field and DOCX body upload.
- Configure expected Excel headers, recipient column, variable-to-header mappings, required/optional flags, fallback values, field formatting and result-column names.
- Validate configured variables against subject/body usage and reject unknown or missing variables before configuration can be used.
- Convert DOCX using an existing converter; preserve paragraphs, headings, lists, bold, italics and links.
- Render safe personalized previews and preserve raw inputs separately from normalized replacement values.
- Identify every configuration revision so change 4 can bind test-email approval to its exact content.

## Capabilities

### New Capabilities

- `campaign-templates`: Campaign configuration, revisioning, DOCX conversion and variable consistency.
- `email-personalization`: Required/optional replacement values, field-specific normalization and safe subject/body rendering.

### Modified Capabilities

None.

## Impact

Implementation order: **2 of 5**; depends on `add-operator-and-microsoft-auth` for protected operator access. Change 3 supplies real uploaded candidate previews; change 4 implements test sending and approval.

Add campaign/configuration migrations and feature views under the existing Go application. Reuse Pandoc for DOCX conversion, Go template libraries for rendering, an established HTML sanitizer and existing Unicode/text packages; do not build a Word parser, rich text editor or template programming language.

No extraction of a subject from DOCX, Word page-layout reproduction, attachments or automatic sends. Configuring header names does not require a Google account or a live spreadsheet.
