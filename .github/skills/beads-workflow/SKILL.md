---
name: beads-workflow
description: >
  Workflow for planning, tracking, and implementing work using beads (bd).
  Use this skill when starting a session, planning work, picking up tasks,
  completing tasks, or ending a session. Also use when asked about project
  status, what to work on next, or how to organize implementation work.
---

# Beads Workflow for kg

This skill defines the standard workflow for planning, tracking, and
implementing work in this repository using `bd` (beads).

## Session Start

Every session begins with orientation:

```bash
bd prime            # Load workflow context (run after /compact or /clear too)
bd ready            # See what's unblocked and available
bd stats            # Quick health check: open/closed/blocked counts
```

Review the output of `bd ready` to decide what to work on. Prefer tasks
that are already `in_progress` (picked up in a prior session) before
starting new ones.

## Planning New Work

When decomposing a feature or milestone into tasks:

1. **Check existing decisions** — read relevant files in `decisions/` and
   `docs/` to understand design intent before creating implementation tasks.
2. **Create beads** for each concrete unit of work:
   ```bash
   bd create --title="Short imperative description" --type=task --priority=2
   ```
   - Use `--type=feature` for user-visible capabilities.
   - Use `--type=task` for internal/engineering work.
   - Use `--type=bug` for defects.
   - Priority: 0=critical, 1=high, 2=medium (default), 3=low, 4=backlog.
3. **Add dependencies** so `bd ready` only surfaces unblocked work:
   ```bash
   bd dep add <downstream-id> <upstream-id>  # downstream depends on upstream
   ```
4. **Keep beads small** — each should be completable in a single session
   (roughly 1–3 files changed). If a task feels large, split it.

### Milestone Pattern

Group related beads under a naming convention:

```
M1: kg init — scaffold .kg/ directory
M1: YAML entity storage — create/read/update/delete
M1: Schema files — load and validate
M2: BoltDB index — sync from YAML
M2: kg find — query the index
...
```

Use `bd dep add` to enforce ordering between milestones.

## Working a Task

```bash
bd ready                              # Pick a task
bd show <id>                          # Read details, check context
bd update <id> --status in_progress   # Claim it
```

Then implement. While working:

- Keep commits focused on the claimed bead.
- If you discover additional work, create new beads for it rather than
  scope-creeping the current task:
  ```bash
  bd create --title="Discovered: handle edge case X" --type=task --priority=2
  ```
- If the task is blocked by something unexpected:
  ```bash
  bd update <id> --status blocked
  bd create --title="Resolve blocker Y" --type=task --priority=1
  bd dep add <blocked-id> <blocker-id>
  ```

## Completing a Task

```bash
bd close <id>                         # Mark done
```

Close multiple at once if you finished several:
```bash
bd close <id1> <id2> <id3>
```

## Checking Progress

```bash
bd stats                              # Summary counts
bd ready                              # What's next?
bd blocked                            # What's stuck?
bd list --status=open                 # Full backlog
bd list --status=in_progress          # Active work
```

## Session End (Landing the Plane)

**Every session MUST complete all of these steps. Work is not done until
`git push` succeeds.**

1. **File issues for remaining work** — create beads for anything discovered
   but not finished.
2. **Run quality gates** (if code changed) — tests, linters, build.
3. **Update issue status** — close finished beads, mark others appropriately.
4. **Push to remote**:
   ```bash
   git add -A && git commit -m "descriptive message"
   git pull --rebase
   bd sync
   git push
   git status  # Must show "up to date with origin"
   ```
5. **Verify** — all changes committed AND pushed.

## Relationship to Copilot CLI Features

| Tool | Purpose | Scope |
|------|---------|-------|
| `bd` (beads) | Cross-session task tracking, dependencies, progress | Persists in `.beads/`, survives across sessions |
| `/plan` mode | Within-session implementation planning | Ephemeral, lives in session only |
| SQL todos | Within-session step tracking | Ephemeral, per-session only |

**Beads is the source of truth for what work exists and what's done.**
Use `/plan` and SQL todos for intra-session decomposition of a single bead
into implementation steps, but always reflect completion back to beads.
