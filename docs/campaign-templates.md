# Campaign templates

How personalized campaign email content works: placeholder syntax, the mapping
table that binds placeholders to worksheet columns, value formatting, output
settings, and the DOCX-to-HTML converter.

## Placeholder syntax

Write `{{variable_name}}` literally, in the subject or in the body document's
text. Names start with a letter and contain only letters, digits and
underscores. Whitespace inside the braces is trimmed, so `{{ first_name }}`
and `{{first_name}}` are the same variable.

- A placeholder split across adjacent Word formatting runs inside one
  paragraph is joined and replaced normally. A placeholder spanning two
  paragraphs is not joined: the braces render as literal text, and the
  configuration then fails validation because the variable is reported as
  missing from the subject and body.
- Placeholders inside link URLs or other HTML attributes are **not**
  substituted; they render as literal text.
- A literal `{{` that is not a complete placeholder renders verbatim.
- Values are inserted as inert data: markup or template expressions inside a
  candidate value stay literal text in the email and can never execute.

## Valid variables

A configuration is ready only when both directions agree:

- Every ordinary configured variable must appear at least once in the subject
  or body. A subject-only variable is fine. The recipient mapping is exempt: it
  supplies the sending address without adding it to the email content.
- Every placeholder must have a mapping.

Otherwise the configuration stays unready, and the error names the offending
variables and the remedy:

- `no mapping for variable(s): ...` — add a mapping row for each name, or
  rename the placeholder in the subject or body.
- `configured variable(s) missing from the subject and body: ...` — remove each
  mapping row, or add its placeholder to the subject or body.

Both sides must use the same name. The mapping form's worked example is drawn
from the campaign's source headers for the mapping draft, so the example's variable
name (for example `candidate_name` for the header `Candidate Name`) is the name
its draft rows use. A document that spells the placeholder differently — for
example `{{full_name}}` — needs a row whose Variable is `full_name`, or an edit
to the placeholder.

## Mapping fields

Each mapping row has:

| Field | Meaning |
|---|---|
| Variable | The placeholder name, or the name of the recipient value. |
| Source header | The exact worksheet header to read. It need not equal the variable name. |
| Required / optional / recipient | Defaults to **required**. Exactly one row must be **recipient**, which implies a required email value with no fallback. An optional variable may declare a fallback. |
| Fallback | Optional variables only. Used when the cell is empty after normalization; then an empty string. |
| Formatting | One of `text`, `name`, `designation`, `email` (below). |

## Formatting

Whitespace is trimmed and internal runs are collapsed to single spaces
**before** any format transform. The raw uploaded value is always preserved
next to the normalized value in the preview.

| Format | Behavior |
|---|---|
| `text` | Whitespace normalized, capitalization preserved. |
| `name` | Word capitalization; capitalization already present is preserved. |
| `designation` | Word capitalization plus the acronym set `HR`, `IT`, `CEO`, `CTO`, `CFO`, `COO`, `QA`. No other word is auto-uppercased. |
| `email` | Trimmed and lowercased. |

## Required vs optional

- A required value that is empty after normalization holds only that row and
  reports `missing required value(s): <variable>`; other rows are unaffected.
- An optional value uses its fallback when empty, then an empty string.
- Every mapped column must still be present in the worksheet header row, even
  when the variable is optional.

## Recipients

Select the sending address explicitly in the mapping textarea:

```text
candidate_email | Candidate Email | recipient | email
```

Exactly one row must be marked `recipient`. Its format defaults to `email` when
omitted; another format or a fallback is rejected. The recipient must be present
in the worksheet but needs no placeholder in the DOCX or subject. Add
`{{candidate_email}}` only if the address should also appear in that content.
The preview takes a sample value for every mapping and shows **To**, subject and
body separately.

The campaign's comma-separated source headers only generate draft mapping rows.
The draft starts with required text rows; explicitly change the address row to
`recipient` before saving. Intake uses the saved revision's mappings, independently
of the draft headers.

A worksheet cell may carry several email addresses separated by commas; intake
expands them into one recipient entry per address with the row's other mapped
values. Semicolon or space-separated lists, `Name <addr>` wrappers and control
characters are rejected, and each entry is validated on its own. The trimmed
lowercase address is the campaign identity. The application validates the
address syntax; it does not check that the mailbox exists.

## Output labels

Downloadable result columns default to `Email Status`, `Submitted At`,
`Email Error`. They are written to the added `Recipient results` worksheet;
a label that collides with a source column or with that sheet's metadata
columns is renamed with a numbered suffix. Labels are output settings: changing
them does not change the personalized-content hash, so a pending content
approval is unaffected.

## Revisions

Campaign content is immutable. Changing the subject, the document, the
mappings, recipient selection, required flags, fallbacks or formatting creates a new revision that
starts unapproved. Approval covers one exact content hash; stored
personalized content and running task snapshots never change under you.

### The revision form

The campaign template tab shows three cards in order:

1. **Current revision** — a collapsed accordion with the current revision,
   its approval and the conversion details.
2. **Mapping reference** — a collapsed accordion with the campaign's source
   headers, the column-order table, the format list, the example line and the
   note that revisions are immutable.
3. **Create a new content revision** — always open, and it leads with what you
   have to fill in:
   - **Email content** — the subject, then the body document.
   - **Recipient mapping** — the mapping table.

Each field has a `?` control beside its label. Hover it, or move keyboard
focus to it, to see the help for that field: the mapping row syntax, the rule
that exactly one row is marked `recipient`, the example sending-address row,
and a placeholder example such as `{{candidate_name}}`. The help text is also
announced by screen readers, because each control names its help with
`aria-describedby`.

Nothing in the two accordions affects what is submitted.

## DOCX support

The body document is a `.docx` file converted to HTML. Surviving content:
paragraphs, headings, ordered/unordered lists, bold, italics, and safe links
(`http`, `https`, `mailto`). Known limits:

- Embedded images and page headers/footers are dropped, with a visible
  warning before approval.
- Tables are converted as plain text.
- Macro-enabled documents are refused.
- No attachment is ever added to the email.
- Word page layout (spacing, columns, colors as layout) is not reproduced.

## Converter setup

The application image packages a pinned Pandoc 3.x. For local development
either install the distribution package:

```sh
sudo pacman -S pandoc      # Arch
sudo apt install pandoc    # Debian/Ubuntu
```

or point `PANDOC_PATH` at a Pandoc 3.x executable. Authoring reports an
actionable error when no converter is available — a missing converter never
crashes the server.

Conversion limits: a 30 second deadline, at most 10 MiB of document input and
10 MiB of HTML output, a private temporary directory, no shell, Pandoc's own
sandbox restricting file access to the given document, and no
operator-supplied filters or templates reaching the process.

## Complete example

Subject:

```text
Welcome aboard, {{first_name}}
```

Body document text (as converted from the DOCX):

```html
<h1>Welcome {{first_name}}</h1>
<p>Dear {{first_name}}, your title is {{job_title}}.</p>
<ul>
<li>Team: {{team}}</li>
<li>Reply to this email with questions</li>
</ul>
<p><strong>{{first_name}}</strong>, contact your manager for access.</p>
```

Mapping table:

| Variable | Source header | Mode | Fallback | Formatting |
|---|---|---|---|---|
| `candidate_email` | `Email` | recipient | — | `email` |
| `first_name` | `First Name` | required | — | `name` |
| `job_title` | `Title` | required | — | `designation` |
| `team` | `Team` | optional | `our team` | `text` |

Sample worksheet values for one row:

| Email | First Name | Title | Team |
|---|---|---|---|
| `ada@example.com` | `  ada   lovelace ` | `head of qa` | *(empty)* |

The resulting email for that row:

To: `ada@example.com`

Subject:

```text
Welcome aboard, Ada Lovelace
```

Body:

```html
<h1>Welcome Ada Lovelace</h1>
<p>Dear Ada Lovelace, your title is Head Of QA.</p>
<ul>
<li>Team: our team</li>
<li>Reply to this email with questions</li>
</ul>
<p><strong>Ada Lovelace</strong>, contact your manager for access.</p>
```

Notes on this example: `  ada   lovelace ` is trimmed and collapsed to
`ada lovelace`, then word-capitalized by `name`; `head of qa` becomes `Head
Of QA` because `qa` is in the designation acronym set; the empty `team` cell
uses the fallback `our team`. The preview shows each raw value next to its
normalized replacement. A row with an empty `First Name` holds only that row
with `missing required value(s): first_name`.

This example is checked by `TestDocumentedSampleRenders` in
`internal/campaign`: if the sample or the documented normalization changes,
the test fails.
