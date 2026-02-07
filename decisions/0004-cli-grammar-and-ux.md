# 0004 - CLI Grammar and UX

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define exact CLI grammar for commands like `kg find`, `kg path`, `kg get`, morphism syntax, where-clause operators, precedence rules, flags semantics, and output behavior.

Decision
--------
Status: Decided

Decision:
- Where-clause: Comma-separated filters with operators `=`, `!=`, `~`, `>`, `<`, `>=`, `<=`. Comma implies AND. No OR or parentheses in v1.
- Path traversal: Keep operators `->`, `<-`, `->*`, `<-*` and morphism usage `@name` as described in the overview.
- Mutations: Write-to-YAML-first semantics. `--dry-run` previews changes without writing. Manifest is updated after successful write. Transactional rollbacks not implemented in v1.
- Output: Default TOON, with `--json` and `--human` flags to override.


Alternatives Considered
-----------------------
- Use SQL-like WHERE vs custom mini-language
- Strict vs permissive parsing for user-friendly errors

Consequences
------------
- Affects learning curve for users and agent prompts; grammar choices may be hard to change later.

Related Decisions
-----------------
- 0006-output-formats.md
- 0005-morphisms-dsl.md
