# 0005 - Morphisms DSL

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Morphism patterns are reusable path expressions (e.g., `-> worksOn <- worksOn`). Decide on DSL syntax, storage, expansion rules, and versioning.

Decision
--------
Status: Decided

Decision:
- Morphisms are stored as simple named patterns in `.kg/morphisms.yaml` and support the operators `->`, `<-`, `->*`, `<-*`.
- v1 supports only pattern expansion (no parameters, no embedded filters). Morphism entries have `pattern` and optional `description` fields.
- Versioning: morphisms are treated as content; breaking changes should be recorded in a new decision entry.

Example:
```yaml
teammates:
  pattern: "-> worksOn <- worksOn"
  description: "People who work on the same projects"
```


Alternatives Considered
-----------------------
- Simple named patterns vs parameterized templates
- Allowing arbitrary composition vs limited safe subset

Consequences
------------
- Impacts expressiveness for agents and potential for ambiguous/unsafe expansions.

Related Decisions
-----------------
- 0004-cli-grammar-and-ux.md
- 0003-quads-and-labels.md
