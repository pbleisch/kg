# 0021 — Agent Integration CLI (`kg agent`)

**AMENDED 2026-08-30 by [0027](0027-delete-index-and-daemon.md).** `kg agent status`
and `kg agent check` no longer report daemon status, index staleness, or a backend: there
is none of the three. They report the vector sidecar (`absent` / `stale` / `present`) and
where the embedding command was resolved from. That is a contract change on a surface
whose audience is agents.


Status: Decided

Date: 2026-02-08

Authors: Paul

Context
-------
AI coding agents (GitHub Copilot CLI, Claude Code, Cursor, etc.) need a
fast way to discover and use kg when they land in a workspace. Today an
agent must read AGENTS.md, skill files, schema YAML, and CLI help to
piece together how to operate the graph. There is no single command that
gives an agent everything it needs to get started.

Decision 0007 (MCP) solves the runtime protocol, but agents still lack
onboarding, discovery, and platform-specific configuration tooling.

Decision
--------
Add a `kg agent` command group with the following subcommands:

### `kg agent status`
Machine-readable snapshot of the current kg environment:
- Graph directory path
- Entity count (by type)
- Schema types available
- Index freshness (stale / current / missing)
- Daemon status (running / stopped)

Default output in TOON; supports `--json` flag.

### `kg agent skill`
Generate a SKILL.md file that teaches an agent how to use kg via CLI
commands. Platform-aware flags control output format and destination:
- `--github` — writes to `.github/skills/kg/SKILL.md`
- `--claude` — writes to `.claude/skills/kg.md` (or equivalent)
- No flag — prints to stdout

The generated skill includes: available commands, schema summary,
example workflows, and output format notes.

### `kg agent schema`
Dump all schemas with field types, required attributes, and valid
relations in a compact agent-readable format. Richer than `kg types`
and `kg describe` combined — designed for an agent to ingest in one
call.

### `kg agent examples`
Emit example command sequences for common workflows (create → link →
find → path) using the actual schema types present in the current
graph. Gives agents concrete patterns to follow rather than abstract
docs.

### `kg agent mcp-config`
Print a ready-to-paste MCP server configuration block. Platform-aware:
- `--github` — emits JSON for `.github/copilot/mcp.json`
- `--claude` — emits JSON for `claude_desktop_config.json`
- No flag — prints generic MCP stdio config

### `kg agent check`
Quick health check: index stale? daemon running? validation errors?
Returns exit code 0 (healthy) or 1 (problems found) so agents can
gate operations on graph health.

### Design principles
- All output defaults to TOON (token-efficient); `--json` available.
- Subcommands are read-only except `skill` and `mcp-config` (which
  write files only when a platform flag is given).
- Each subcommand should be useful in isolation — no required ordering.
- Keep it thin: delegate to existing internal packages rather than
  reimplementing schema parsing or entity counting.

Consequences
------------
- Agents get a single entry point (`kg agent`) for onboarding and
  discovery, replacing ad-hoc file reading.
- Platform-specific flags (`--github`, `--claude`) reduce friction for
  the most common agent environments.
- The `skill` subcommand generates content from live schema, so skills
  stay in sync with the graph without manual maintenance.
- Adds a new command group to maintain, but subcommands are thin
  wrappers over existing functionality.

Alternatives Considered
-----------------------
- Embedding all agent docs in AGENTS.md: doesn't adapt to the current
  graph state; requires manual updates when schemas change.
- Adding agent info to `kg mcp`: conflates runtime protocol with
  onboarding; agents that use CLI (not MCP) would miss it.
- A standalone `kg onboard` command: less composable than a subcommand
  group; harder to extend.

Related Decisions
-----------------
- 0007-mcp.md — MCP runtime protocol (complements agent onboarding)
- 0008-daemon.md — daemon status surfaced by `kg agent status/check`
- 0004-cli-grammar-and-ux.md — CLI structure and output conventions
