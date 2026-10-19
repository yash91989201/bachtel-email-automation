# Tasks

Prerequisite: align-mapping-example. One error function, its tests and one documentation section.

## 1. Remedy for the unused-mapping error

- [x] 1.1 Extend the unused-mapping error to state the remedy; verify a unit test asserts the named variable and both remedies, and the revision form shows them inline.
- [x] 1.2 Document both remedies for the variable-consistency rule in `docs/campaign-templates.md`.

## 2. Verification

- [x] 2.1 Run the campaign and web package tests plus `go test ./...` and `go vet ./...`.
- [x] 2.2 Reproduce against a running server with a campaign whose headers derive a `candidate_email` row and a DOCX that never uses `{{candidate_email}}`: verify the inline error states the remedy and removing the row creates the revision.
