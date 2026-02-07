# 0006 - Output Formats

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define TOON format details, JSON and human output shapes, error formats, paging/sorting, and flags.

Decision
--------
Status: Decided

Decision:
- TOON format: Strict, lossless TOON with explicit array lengths and well-defined field ordering to ensure deterministic output for agents.
- Errors: TOON error objects include `error`, `message`, and optional `suggestions[]` fields.
- Pagination/sorting: `kg find` returns all results by default in v1; later add `--page`/`--per-page` if needed.
- Flags: `--json` and `--human` override TOON. When piping, output format remains what the user requested; no automatic switching for pipes in v1.


Alternatives Considered
-----------------------
- Make TOON strictly lossless vs allow compact optimizations
- Align error format with existing LLM-friendly patterns

Consequences
------------
- Output format impacts downstream consumers, agents, and tooling compatibility.

Related Decisions
-----------------
- 0004-cli-grammar-and-ux.md
- 0010-implementation-and-dependencies.md
