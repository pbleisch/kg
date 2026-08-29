# Agent Instructions

> **Note for human contributors:** This file provides instructions for AI
> coding agents (GitHub Copilot, Claude, etc.) that work on this repository.
> If you're a human contributor, see [CONTRIBUTING.md](CONTRIBUTING.md) instead.

## Before a structural change

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) records the properties any rewrite of kg must
preserve, what each costs, and which current choices (LadybugDB, the daemon, YAML,
TOON, MCP) are explicitly *not* principles and may be discarded freely. It also lists
where the code currently violates its own principles. The nine principles are grouped
into four areas — the graph on disk, the interface, the runtime, the record — so find
the area your change lands on first: the principles in play are that group's two or
three, not all nine. Read it before changing storage, the index, the write boundary, the
library API, or the CLI surface; individual decisions live in `decisions/` as ADRs.

P7 was revised on 2026-08-29: kg is a library with a **thin, complete** CLI over it. The
CLI composes library calls and adds only presentation — it must not hold validation,
defaults, or normalization the library lacks, and it must expose every operation the
library does. Read P7 before adding behaviour under `cmd/`.

Build and test require CGO and the LadybugDB native library (`mage build`; see
[CONTRIBUTING.md](CONTRIBUTING.md)). **Verify locally — CI is not a usable signal.**
Every run on `main` since v0.9.3 fails at the `Test` step (Linux-only; the suite passes
on macOS), and as of 2026-08 GitHub Actions will not start jobs on this account at all
for billing reasons.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create GitHub issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
4. **Clean up** - Clear stashes, prune remote branches
5. **Verify** - All changes committed AND pushed
6. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
