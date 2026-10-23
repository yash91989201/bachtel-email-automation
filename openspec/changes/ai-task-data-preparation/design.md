# Design

## Context

Depends on account-connection-settings. Existing task intake continues until reviewed-task-sending-snapshots installs the complete operator workflow.

## Decisions

### 3. Prompt and run history

Store one editable task-instruction draft, not a campaign-like revision UI for every keystroke. An explicit initial preparation/rerun captures immutable source digest, campaign revision, complete base prompt/version, task extension, AI connection/configuration generation, exact provider/endpoint/API format/model/options, actual outputs, corrections and findings. Persist completed output, because prompts and model ids alone cannot reproduce inference.

Use the header-first tested prompt in Appendix A as the initial base. Add the explicit output distinction: corrections that were successfully resolved are not review findings; unresolved semantic mismatches or uncertainty are findings. Do not place required/optional enforcement in the model. Task instructions may override cleanup defaults but not fabricate recipient identities or bypass application validation.

Changing the draft alone does not change selected results. A successful new run becomes available for explicit review/selection; selection remains preparation-only until the next change installs finalization and approval invalidation. Show failed and previous runs without mutating historical results. Restrict rerun/selection changes to unstarted tasks; refuse them while a future schedule is enabled until the operator clears it.


### Output and lifecycle boundary

Use the narrow adapter and structured response validation established by account-connection-settings. Validate complete row/field coverage and preserved ordered recipient occurrences, including invalid/empty/duplicate entries; unknown, omitted or duplicated output fails the batch. Persist owner-scoped runs and server-owned bounded jobs. Only mapped data is transmitted. Closure cancels claims and rejects late publication. No raw fallback or automatic rerun. Prepared output is evidence, not candidates or sending authority. Findings classify proposed eligibility; final backend eligibility and snapshot publication belong to the next change. Run selection must not mutate any legacy sending data. The later snapshot integration supplies finalization invalidation hooks atomically.

## Risks / Trade-offs

AI can confidently make incorrect corrections or miss problems. Preserve originals and actual outputs, require later human review, and do not promise repeatable model output or stable vendor weights.

## Appendix A: Initial visible base prompt

The following is the prompt tested during exploration, with header-first guidance, name-spacing repairs and comma-space lists. The final application output contract additionally separates resolved corrections from unresolved findings and enforces recipient occurrence preservation. Prompt updates create a new prompt version and affect only explicit future runs.

```text
Normalize the supplied mapped values for use in personalized email content.

Before normalizing each field, identify its semantic purpose from its
source-column header. Use the mapped variable name and supplied row context
as supporting context. Apply normalization appropriate to that field’s
purpose, rather than applying one generic transformation to every column.
Respect distinctions in the header such as current versus expected values.
If a cell does not fit its header, flag the mismatch rather than redefining
the field or inventing a value that would fit.

For personal names, clear accidental internal-spacing and word-boundary
repairs are allowed when they preserve the same letters and their order.
Preserve initials and do not substitute a more familiar name spelling.

Apply task-specific instructions where provided. For anything they do not
address, use the defaults below.

Normalization defaults

1. Produce clean, readable text. Trim surrounding whitespace, collapse
   unnecessary whitespace, and correct punctuation spacing. Remove stray
   separators only when they clearly represent empty content.

2. Normalize noisy capitalization appropriately for the field. Preserve
   initials, recognizable acronyms, technical terms, and intentional spelling.
   Avoid mechanically title-casing every word.

3. Correct clear spelling and typing errors when the intended wording is
   unambiguous. Be especially careful with personal names, uncommon terms,
   and identifiers. If a correction would require guessing, retain the
   supplied wording and report the uncertainty.

4. Preserve the supplied meaning, specificity, and qualifications. When
   any column contains multiple distinct values, keep every value in its
   original order and separate the values with a comma followed by one
   space. Use the column header and context to distinguish a list from a
   compound expression or an internal part of one value; preserve compound
   technical terms, initials, meaningful parenthetical details, and punctuation
   within individual values. Apply the same list-separator rule to email
   address fields, preserving every supplied address. Do not select,
   summarize, expand abbreviations, or substitute preferred terminology
   unless task instructions request it.

5. Preserve factual values, units, identifiers, and date meanings. Improve
   presentation only where the interpretation is clear. Do not infer a
   missing currency, date interpretation, qualification, or other fact.

6. Leave missing values empty. Recognize missing-value markers only when
   their meaning is clear in context. Preserve meaningful values such as
   zero or “No”. The application handles required fields and fallbacks.

7. When a value appears misplaced, corrupted, ambiguous, or inconsistent
   with its field, retain the supplied value and report a short issue.
   Do not silently move information between fields or invent a repair.

8. Apply the same interpretation and formatting to equivalent inputs
   throughout the task.

Task-specific instructions may extend or override these normalization
defaults for particular fields or for the whole task. Apply those
instructions directly; do not generate a normalization recipe.

Return normalized text and any separate issues using the application’s
required output structure.
```
