---
name: git-commit
description: >
  Commit staged or unstaged changes with a good, concise commit message.
  Use this skill when committing work, landing changes, or ending a session.
  Produces conventional-style commits with imperative subject lines.
---

# Git Commit Skill

Write clear, concise commit messages that describe *what changed and why*
in the fewest words possible.

## Workflow

1. **Review what changed:**
   ```bash
   git --no-pager diff --stat          # unstaged
   git --no-pager diff --cached --stat # staged
   ```
2. **Understand the change** — read the diffs if the stat isn't enough:
   ```bash
   git --no-pager diff          # unstaged details
   git --no-pager diff --cached # staged details
   ```
3. **Stage if needed:**
   ```bash
   git add -A            # everything
   git add <paths>       # selective
   ```
4. **Commit with a good message:**
   ```bash
   git commit -m "Subject line"
   # or with body:
   git commit -m "Subject line" -m "Optional body with context."
   ```

## Message Format

### Subject line (required)

- **Imperative mood** — "Add parser" not "Added parser" or "Adds parser"
- **≤ 50 characters** — hard limit; be ruthless
- **No trailing period**
- **Lowercase after prefix** — `feat: add parser` not `feat: Add Parser`
- **Start with a type prefix** when the project uses them:

| Prefix | Use for |
|--------|---------|
| `feat:` | New user-visible capability |
| `fix:` | Bug fix |
| `refactor:` | Code change that doesn't add/fix |
| `docs:` | Documentation only |
| `test:` | Adding or fixing tests |
| `chore:` | Build, deps, config, tooling |

### Body (optional, use when helpful)

- Separated from subject by a blank line (use a second `-m`)
- Explain **why**, not what (the diff shows what)
- Wrap at 72 characters
- Reference issue numbers if applicable: `Closes #123`

### Examples

```
feat: add YAML entity storage
```

```
fix: handle missing schema dir on init
```

```
refactor: extract sync logic from store package
```

```
chore: add go.mod and initial dependencies
```

```
docs: record decision on ID encoding
```

With body:
```bash
git commit -m "feat: add kg find with attribute filters" \
  -m "Supports =, !=, ~, >, <, >=, <= operators.
Reads from LadybugDB index. Falls back to scan if index stale.

Closes kg-x7f"
```

## Anti-patterns

- ❌ `"update files"` — says nothing
- ❌ `"WIP"` — commit when the work is coherent, or use `--fixup`
- ❌ `"fix bug"` — which bug?
- ❌ `"Add new feature to handle the creation of YAML entity files for the knowledge graph system"` — way too long
- ✅ `"feat: add YAML entity storage"` — short, clear, imperative

## Multiple logical changes

If the diff contains unrelated changes, split them:

```bash
git add src/store/         && git commit -m "feat: add YAML entity storage"
git add src/output/toon.go && git commit -m "feat: add TOON encoder"
```

One commit = one logical change.

## Deciding the message

Read the diff and ask: *"If someone sees only this subject line in `git log --oneline`, will they understand what this commit does?"*

If yes, you're done. If not, make it more specific.
