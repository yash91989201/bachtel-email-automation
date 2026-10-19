---
name: smart-commit
description: Group all uncommitted changes in a repository into separate git commits by intent. Use this whenever the user asks to "smart commit", "go through the changes and commit", "split my changes into commits", "make meaningful commits", "group the changes", or "commit everything" — and whenever the working tree holds mixed changes (features, docs, build tooling) that should not land in a single commit. Also use it when the user describes work they did in several areas and wants it committed properly, even if they never say the word "commit".
---

# Smart commit

## Why

One purpose per commit. A reviewer should understand a commit without unrelated noise, and `git log` and bisect should map cleanly to features. Your job: read every change, infer what each was for, group by intent, and commit — losing nothing, misattributing nothing.

## 1. Survey the working tree

Run in parallel:

- `git status --short` — modified, staged, and untracked files
- `git diff --stat` and `git diff --cached --stat` — scale of each
- `git log -8 --format='%h %s'` — subject convention (conventional commits? scopes?)

Then read 2–3 full messages with `git log -3 --format='--- %h ---%n%B'`. Match the repo's subject pattern, body style (prose paragraph vs bullets), and tone. A commit that matches the repo reads as if the maintainer wrote it.

## 2. Read every change

- Diff every modified file and open every new file. File names mislead; the intent lives in the content.
- Check docs, specs, and changelog directories — they often state the intent in prose.
- For huge change sets, delegate the reading to a subagent and ask for an intent list, not raw diffs.

Build a list of `intent -> files`. Expect 2–5 intents (for example: a feature, its docs, a dev-tooling change).

## 3. Watch for cross-cutting files

Two traps:

- **Generated files** (`*_templ.go`, `*.pb.go`, lockfiles, snapshots) belong with the source that generates them. Never commit generated output without its source, and do not split them across commits unless you can regenerate the intermediate state — without that tooling they must move together.
- **One file, two intents** (a view holding two features, a README describing several changes). Stage hunks instead of the whole file.

## 4. Choose the grouping and order

- Don't over-split. Sequential follow-ups that touch the same files and share a theme can be one commit; name the parts in the body.
- Each commit should read as coherent on its own.
- Order by dependency: foundations first. Avoid, when cheap, a commit that references something only a later commit adds.
- If a change is genuinely unrelated junk (scratch files, personal config), leave it out and tell the user.

## 5. Stage precisely

Whole files: `git add -- <paths>`.

A file that spans intents: build a partial patch and apply it to the index:

```bash
git diff -- path/to/file > /tmp/split.patch
# keep only the hunks belonging to this commit — each begins with @@
git apply --cached /tmp/split.patch
git diff --cached --stat        # confirm only the intended hunks are staged
```

The unstaged remainder stays in the working tree for the next commit. `git add -p` is interactive — use the patch route when you cannot click.

## 6. Write the message

- Imperative subject matching repo convention, e.g. `feat(scope): ...`.
- The body explains **why** the change exists and what it enables, not a file list — the diff already shows what changed.
- Multi-part commits: name each part briefly.
- Commit with `git commit -F - <<'EOF' ... EOF` to avoid quoting problems.

## 7. Verify and report

- `git status --short` must come back empty — everything committed or explicitly left out.
- Scan new files for secrets before staging (`.env`, keys, tokens).
- Run the repo's test/lint target if its toolchain is available. If it isn't, say so plainly; never imply tests passed.
- Finish with a short table: commit, intent, key files, and any grouping decision the user might question (for example: "the view file also carried a link to the second feature, so it moved with the view").

## Pitfalls

- Never leave generated output disagreeing with its source in a commit.
- Don't push unless asked.
- Don't discard or "tidy" uncommitted work to get a clean tree.
- Don't invent an intent to justify a grouping; if the purpose of a change is unclear, ask.
