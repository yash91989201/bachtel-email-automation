# Tasks

Prerequisite: add-operator-and-microsoft-auth. Implement second; no actual test/candidate email submission is enabled until change 4.

## 1. Campaign revisions and configuration

- [ ] 1.1 Add campaign and immutable configuration migrations with subject, private DOCX reference, mappings, output labels and content hash; verify fresh migration and preservation of older revisions.
- [ ] 1.2 Add protected campaign create/edit operations and validate unique variable names/result labels; verify separate subject behavior and no subject extraction from DOCX.
- [ ] 1.3 Document revision and configurable-header behavior; verify an output-label-only change does not change the personalized-content approval hash.

## 2. Established DOCX conversion

- [ ] 2.1 Package a pinned Pandoc executable compatible with the runtime image and local setup; verify DOCX-to-HTML conversion in the built image without runtime Node or Word.
- [ ] 2.2 Invoke conversion without a shell, with fixed arguments, bounded resources/deadline and private temporary files; verify corrupt/oversized inputs and unsafe resource/filter requests fail safely.
- [ ] 2.3 Add a DOCX fixture with paragraphs, headings, numbered/bulleted lists, bold, italics, links and split-run placeholders; verify semantic output and document unsupported layout/images plus the converter setup command.

## 3. Mapping and personalization

- [ ] 3.1 Compile literal user placeholders to standard Go text/html templates using existing HTML traversal; verify subject/body variables, subject-only variables, missing/unknown tokens and split-run replacement without a custom DOCX parser.
- [ ] 3.2 Add field-specific normalization with existing strings/x/text facilities and a documented acronym set; verify names, designations, preserve-text, email lowercasing and raw-value preservation.
- [ ] 3.3 Add required-by-default mappings, optional fallbacks and single-address validation; verify whitespace-only required values, absent optional columns, invalid recipients and subject controls.
- [ ] 3.4 Apply bluemonday allowlisting and context-safe replacement; verify hostile candidate text, unsafe URLs and literal template expressions stay inert while supported formatting survives.
- [ ] 3.5 Document variable syntax, mapping choices, acronym behavior and required/optional semantics with one runnable sample configuration; verify it renders the documented subject/body.

## 4. Campaign UI and preview

- [ ] 4.1 Build campaign/subject/DOCX/mapping forms with existing templ/htmx; verify inline variable errors prevent a configuration becoming ready and no form sends email.
- [ ] 4.2 Add sample preview with raw-versus-normalized values and revision metadata; verify preview output uses the same rendering entry point that task/test sending will consume.
- [ ] 4.3 Preserve pinned revisions and expose approval-invalidated state for content edits; verify old previews/history remain unchanged and regenerated templ sources are committed.

## 5. Integration acceptance

- [ ] 5.1 Run make test plus converter/rendering integration checks; inspect the campaign form and representative preview in a real browser for supported formatting and error clarity.
