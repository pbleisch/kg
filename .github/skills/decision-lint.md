---
name: decision-lint
description: A skill that checks for conformance to recorded decisions in /docs/decisions and emits remediation recommendations.
---
Purpose
-------
A lightweight "decision-lint" skill that scans the workspace for places where the repository does not honor a recorded decision in /docs/decisions and emits short (1–3 sentence) remediation recommendations.

Scope
-----
- Input: all markdown files in /docs/decisions and the repository workspace.
- Output: a list of short recommendations (1–3 sentences each) for each detected nonconformance.
- This is a diagnostics skill (documentation + small script) — it does not apply fixes automatically.

Behavior (high level)
---------------------
1. Discover all decision files: `docs/decisions/**.md` and treat the filename (kebab slug) as the decision id.
2. For each known decision determine if the workspace implements the decision correctly / completely / appropriately.
3. For each failing check, emit a single short recommendation (1–3 sentences) that describes the problem and how to resolve it.

Example output
--------------
toon-spec: TOON serialization spec or encoder missing. Add docs/toon-spec.md and implement an encoder at pkg/output/toon.go or add the github.com/toon-format/toon-go dependency and document field ordering, escaping, and array-length semantics.

go-module-and-deps: No go.mod found. Run `go mod init <module-path>` (for example 'github.com/<org>/kg') and add pinned dependency versions as recorded in docs/decisions/go-module-and-deps.

