# 0005 - Morphisms DSL

**RETIRED 2026-09-01 by [0031](0031-cli-surface-reduction.md).** `kg morph` and
`pkg/query/morphism.go` are deleted. The DSL was unusable by construction: `kg path`
accepted only `->` and `->*`, so `query.ExpandMorphism` had no caller outside its own
test and a morphism, once defined, could never be applied. The documented syntax did not
parse either — cobra ate the arrow as a shorthand flag, so defining one needed an
undocumented `--`. `docs/PRINCIPLES.md` recorded it as serving no principle. Everything
below describes code that no longer exists; kept because P9 says a decision that was
wrong is still a record.

Status: Retired

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
