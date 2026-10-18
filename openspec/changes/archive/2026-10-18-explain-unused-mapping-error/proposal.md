# Proposal

## Why

Submitting the campaign's prefilled mapping table against a body document that never uses `{{candidate_email}}` was rejected with `configured variable(s) missing from the subject and body: candidate_email`. The rejection is correct — the rule requires both directions to agree — but unlike the unmapped-placeholder error, the message named no remedy, so the operator had to guess that the row, not the document, is what changes.

The cause is a mismatch between the campaign's declared headers and the document's placeholders: the prefilled table proposes one variable per expected recipient header entry, while only the names that appear as `{{...}}` placeholders may be configured.

## What Changes

- Extend the unused-mapping error with the remedy: remove each mapping row, or add its placeholder to the subject or body.
- Document both remedies for the two directions of the variable-consistency rule.

No change to the rule itself, the conversion, the revision content hash or the mapping syntax: only the operator-facing wording.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-templates`: the unused-mapping error now states its remedy, matching the unmapped-placeholder error.

## Impact

Follow-up to `align-mapping-example`. One error function, one documentation section and their tests.
