# Proposal

## Why

Authoring a campaign already records the expected recipient header, but the operator must retype every mapped variable by hand and invent variable names. On a brand-new campaign that is pure transcription: the header list defines the columns, and the variable name is only a spelling of that column.

## What Changes

- Pre-fill the mapping table on a campaign that has no content revision yet, deriving one required `text` mapping per comma-separated expected recipient header.
- Derive each variable name from its header: lowercase, every run of non-alphanumeric characters becomes one underscore, trailing underscores trimmed. Draft rows are written in the existing textarea syntax (`variable | source header | required | text`).
- Keep the textarea empty once a revision exists, and never replace mappings the operator typed into a failed submission.
- Leave every existing behavior alone: the draft is only text in the form, so validation, revision immutability, approval hashes and header matching are unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `campaign-templates`: adds the default mapping draft for a campaign without a revision.

## Impact

Implementation order: follow-up to `add-campaign-template-authoring` (change 2 of 5); independent of the unstarted changes 4 and 5.

Touches the campaign view model, the campaign page template and one derivation helper next to the existing mapping validation. No schema change, no new dependency, no new route.

Assumption recorded: the draft uses the `text` format for every row and is offered only while the campaign has no revision, because a revision is what stores operator mappings.
