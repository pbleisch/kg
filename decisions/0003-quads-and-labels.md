# 0003 - Quads and Labels

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Consider whether to support quads (labels) in the index and YAML, how to represent labels in files, and query semantics for labels.

Decision
--------
Status: Decided

Decision:
- Defer quad/label support for the initial implementation. Use simple relation lists and optional enriched relation objects may be introduced in a later decision when use cases justify added complexity.
- No `--label`/`--exclude-label` CLI flags are implemented initially.

Consequences:
- Keeps YAML and query semantics simpler for v1; provenance and confidence can be handled via separate fields or future extension.


Alternatives Considered
-----------------------
- Defer labels until a clear use case emerges (low immediate complexity)
- Support labels as relation objects with `target` and `label` fields

Consequences
------------
- Labels add expressiveness (provenance, confidence, temporal) but increase YAML complexity and query semantics.

Related Decisions
-----------------
- 0002-data-model-and-schema.md
- 0016-ids-and-naming-conventions.md
- 0017-provenance-and-audit.md
