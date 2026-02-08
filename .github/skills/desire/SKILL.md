---
name: desire
description: >
  Assist users in creating well-formed definitions of desired future needs
  (pains, problems, wishes, gaps) and recording them as markdown files in
  desires/. Use this skill when the user wants to capture something they
  wish were different, a pain point, a problem to solve, or a future need.
---

# Desire Skill

Help the user articulate and record a **desire** — something they want to
be different in the future. Desires are raw needs, not solutions. They
capture pains, problems, wishes, and gaps before any decision about *how*
to address them.

## When to Use

- User says they wish something worked differently
- User describes a pain, frustration, or gap
- User wants to record a problem for future work
- User explicitly asks to create a desire

## Workflow

1. **Listen and clarify** — understand what the user wants to be different.
   Ask one or two short clarifying questions if the need is vague, but
   don't interrogate. The goal is a clear one-paragraph statement, not a
   requirements document.

2. **Draft the desire** — write a markdown file using the template below.
   Show it to the user for confirmation before saving.

3. **Assign a sequence number** — find the highest `NNNN` prefix in
   `desires/` and increment by one. Pad to four digits.

4. **Choose a slug** — short kebab-case label derived from the desire
   title (2–4 words).

5. **Save** — write the file to `desires/NNNN-desire-slug.md`.

6. **Commit** — stage and commit with message format:
   ```
   docs: record desire NNNN — short title
   ```

## File Template

```markdown
# NNNN — Short Desire Title

Status: Open

Date: YYYY-MM-DD

## What hurts

One or two paragraphs describing the current pain, problem, or gap.
Focus on *what is wrong or missing*, not on solutions.

## Why it matters

A sentence or two on impact — who is affected, what happens if this
stays unresolved, or what opportunity is being missed.

## Ideal outcome

A brief description of what "better" looks like from the user's
perspective. Stay in problem-space language; avoid prescribing an
implementation.

## Notes

Optional. Any extra context, links, or related desires.
```

## Guidelines

- **Stay in problem space.** Desires describe *what* and *why*, never
  *how*. If the user jumps to a solution, gently steer back: "Let's
  capture what you need first — we can figure out how later."
- **Keep it short.** Each section should be a few sentences, not pages.
  If it grows beyond a screenful, it's probably multiple desires.
- **One desire per file.** If the user describes several needs, create
  separate files for each.
- **Don't gate-keep.** Any need is valid. Don't require justification
  or scoring. The point is to get it written down.
- **Status values:** `Open` (new), `Addressed` (solution shipped),
  `Withdrawn` (no longer relevant). Most desires stay `Open` until
  something is built.

## Numbering

```bash
# Find next number
ls desires/*.md 2>/dev/null | sed 's|desires/||;s|-.*||' | sort -n | tail -1
# If empty, start at 0001
```

## Examples

Filename: `desires/0001-faster-entity-lookup.md`

```markdown
# 0001 — Faster Entity Lookup

Status: Open

Date: 2026-02-08

## What hurts

Looking up an entity by name requires scanning every YAML file in the
store. On a graph with hundreds of entities this takes noticeable time
and makes interactive use frustrating.

## Why it matters

Slow lookups break flow during exploratory queries and make the CLI
feel sluggish compared to alternatives.

## Ideal outcome

Entity lookups by name or ID return in under 100ms regardless of
graph size.

## Notes

Related to the indexing work in decisions/0001-storage-and-index.md.
```
