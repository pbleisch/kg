# 0018 - Error Handling and Retries

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define CLI and MCP error models, retry semantics, and user-visible error messages.

Decision
--------
Status: Decided

Decision:
- Use structured TOON errors containing `error`, `message`, and optional `suggestions[]` fields for CLI and MCP outputs.
- Retry/backoff: implement simple exponential backoff for transient IO/index errors in daemon and CLI retry helpers; report final failure to the user with actionable suggestions.
- Validation errors: reported by `kg validate`; `--strict` mode will block mutations. Validation warnings do not block by default.

Consequences:
- Improves agent and scriptability with structured error payloads; retry behavior handles transient IO issues gracefully.

Alternatives Considered
-----------------------
- Minimal error model vs richer structured errors with suggestions

Consequences
------------
- Affects UX for both humans and agents, and robustness against transient faults.

Related Decisions
-----------------
- 0006-output-formats.md
- 0011-testing-and-observability.md
