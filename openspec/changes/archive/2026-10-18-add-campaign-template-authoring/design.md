# Design

## Context

See proposal.md. Change 1 supplies operator protection. The starter currently has no campaign model or document-processing dependency; Node is frontend build tooling only. This is change 2, and supplies the rendering/configuration contract consumed by Excel intake and sending.

## Goals / Non-Goals

**Goals:** Convert once per immutable configuration, share one renderer across previews/tests/submissions and retain the original document.

**Non-Goals:** Hand-written OOXML conversion, a Word editor, arbitrary template expressions, exact page layout, attachments or a runtime Node service.

## Decisions

### Campaign and revision records

Use PocketBase collections/migrations for campaigns and immutable campaign_configurations. A campaign points to its current configuration. Revisions store subject, original private DOCX reference, converted/sanitized HTML, compiled-variable metadata, recipient/header mappings, required flags, fallbacks, normalization choices and renderer/converter identity. Content hashes identify exactly what a later formatting approval covers.

Result-column labels default to Email Status, Submitted At and Email Error. They are distinct output settings snapshotted per task; changing only these labels does not invalidate content approval. Configure expected source headers explicitly at campaign setup; change 3 displays uploaded headers and reports mismatches.

A new content revision starts unapproved. A running task retains the approval/content revision it started with. A not-yet-started task must revalidate against the current revision before its first submission. Do not mutate historical configuration or stored personalized emails.

### Reuse Pandoc rather than writing a Word parser

Package a pinned, platform-compatible Pandoc executable in the existing application image and invoke it through bounded Go exec.CommandContext calls without a shell. Convert DOCX to semantic HTML once per revision, using a private temporary directory, fixed application-owned arguments, input/output limits and no operator-supplied filters, commands or resource URLs. Restrict process/network/filesystem access using the supported converter sandbox/container facilities; do not assume a flag alone protects every format.

Use supported paragraphs/headings/lists/emphasis/links as the acceptance fixture. Surface unsupported embedded images/layout features before approval; do not silently promise Word fidelity. The binary now depends on the packaged converter, which must be documented for local development and deployment. Do not add Python/LibreOffice or a second service.

Alternative: a custom ZIP/XML DOCX parser would duplicate format handling and lose list/style semantics. A library-only converter is acceptable only as an implementation substitution if it passes the same fixtures and preserves the public contract.

### Existing template and HTML libraries

Use Go text/template for the subject, html/template for the body, golang.org/x/net/html for structural traversal and bluemonday for an explicit allowlist of supported HTML and safe links. Add only the small glue that discovers user tokens, assigns canonical internal keys and joins placeholders split across adjacent inline text runs. Do not implement a general-purpose template engine.

User-facing variables can differ from column labels and contain supported literal names; compile them into generated internal template keys rather than evaluating user expressions. Validate the set used in both subject and logical body text against the mapping set. Reject malformed or cross-paragraph placeholders. Preserve formatting outside a placeholder; values are inert strings and are not rendered a second time. Allowlist link schemes and disallow executable HTML.

### Explicit normalization policies

Reuse strings and existing golang.org/x/text/cases/language facilities. Trim values and collapse internal whitespace for single-line fields. Name formatting capitalizes words; designation/department formatting adds a documented acronym set initially including HR, IT, CEO, CTO, CFO, COO and QA. Preserve-text leaves capitalization intact. Normalization cannot infer every preferred spelling; raw-versus-normalized previews let the operator choose preserve-text when needed.

Use net/mail parsing plus explicit single-address checks for recipients, reject display wrappers/list syntax and control characters, then apply trimmed lowercase identity. No DNS/mailbox existence checks. Required validation occurs after normalization; optional fallback is normalized using its mapping policy. Reject subject header controls rather than permitting injection.

## Risks / Trade-offs

- DOCX converter output differs from email-client presentation -> semantic fixtures, safe consistent email styling and mandatory real test approval in change 4.
- Word splits placeholders across differently formatted runs -> test split-run cases explicitly; transform token boundaries, not the entire paragraph.
- Case normalization can change meaningful capitalization -> field choices, original-value retention and side-by-side preview.
- Converter/library updates can alter output -> retain rendered revisions and converter identity; changed output creates a new unapproved revision.

## Migration Plan

Implement after change 1. Add campaign/revision schema, converter packaging, form/view integration and focused fixtures; commit generated templ Go sources using the existing generation process. No sending is enabled here. Rollback restores a backup taken before schema changes rather than deleting revisions referenced by later tasks.

## References

- [Pandoc DOCX reader and HTML writer](https://pandoc.org/MANUAL.html)
- [bluemonday HTML sanitizer](https://github.com/microcosm-cc/bluemonday)
