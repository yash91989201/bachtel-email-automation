# Proposal

## Why

An operator authored the campaign DOCX from the example line shipped in the mapping help (`full_name | Full Name | required | name`). The mapping draft in the same panel derives variable names from the campaign's own expected headers, so its first row said `candidate_name | Candidate Name | required | text`. Submitting the prefilled rows with that document was rejected: `no mapping for variable(s): full_name`.

The rejection is correct — the body referenced a placeholder no row declares — but the panel taught the mismatched name and the message named no remedy, so the operator could not tell which of the two sides to change.

## What Changes

- Derive the help's example row and example line from the campaign's own expected recipient header, so every variable name the help shows equals the name the draft uses for that header. A generic `variable_name` example appears only when the campaign has no usable header.
- State the rule in the form hint: the names in the mapping table must match the `{{...}}` placeholders in the subject and body.
- Extend the unmapped-placeholder error with the remedy: add a mapping row for the named placeholder, or rename the placeholder in the subject or body.
- Document both.

No change to the placeholder syntax, the mapping fields, the conversion, revision immutability or the validation rules themselves: only the help text and the error wording change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-templates`: the mapping help example follows the campaign's expected headers, and the unmapped-placeholder error names the remedy.

## Impact

Follow-up to `prefill-default-mappings`; the campaign page, its view model and the campaign package's error text are the only touch points. No schema change, no new dependency, no new route.
