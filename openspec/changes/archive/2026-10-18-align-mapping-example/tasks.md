# Tasks

Prerequisite: prefill-default-mappings. Documentation, view-model, template and one error-text change; no schema or route change.

## 1. Align the help example with the campaign

- [x] 1.1 Add the example variable and source header to the campaign page view model from the campaign's first draftable expected header, with a generic fallback; verify a campaign whose headers start with `Candidate Name` renders `candidate_name | Candidate Name | required | text` in both the column-order table and the example line.
- [x] 1.2 State in the mapping hint that the names entered must match the `{{...}}` placeholders in the subject and body.

## 2. Actionable unmapped-placeholder error

- [x] 2.1 Extend the unmapped-placeholder error so it names the remedy as well as the placeholders; verify the campaign revision form shows the remedy inline and the existing name list is unchanged.
- [x] 2.2 Document the help-example rule and the extended error in `docs/campaign-templates.md`.

## 3. Verification

- [x] 3.1 Run the campaign and web package tests plus `go test ./...` and `go vet ./...`.
- [x] 3.2 Reproduce the reported failure against a running server with a DOCX that uses `full_name` and a campaign whose headers derive `candidate_name`: verify the page shows the campaign-derived example and the rejection states the remedy.
