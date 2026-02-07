# 0002 - Data Model and Schema

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define canonical ID format, filename ↔ type/id mapping, attribute types and coercion rules, relation YAML shape, cardinality and schema strictness.

Decision
--------
Status: Decided

Decision:
- ID format: `type:local-id` with lowercase, kebab-case type and filesystem-safe local-id. Types will be lowercase and filenames will map directly to local-ids.
- File layout: `.kg/entities/<type>/<local-id>.yaml` and `.kg/schema/<type>.yaml` where `<type>` is kebab-case lowercase and matches the ID type prefix.
- Attribute types: support string, number, boolean, date, datetime. Coercion rules: YAML scalars will be coerced by schema types where provided; unspecified attributes remain strings.
- Relation representation: Use simple lists of target IDs for common cases, and allow enriched relation objects when metadata (label, confidence, asof) is required. Example:
  relations:
    worksOn:
      - project:atlas
      - target: project:beacon
        label: source:inferred
        confidence: low
- Cardinality enforcement: Advisory by default (validation issues reported by `kg validate`) with optional strict mode `kg validate --strict` or repository-level config to enforce blocking validation.


Alternatives Considered
-----------------------
- Lowercase type in IDs (person:alice) vs namespaced or kebab-case IDs
- Flat relation lists vs enriched relation objects

Consequences
------------
- Affects agent ergonomics, on-disk readability, and validation complexity.
- Schema strictness impacts backward compatibility and agent workflows.

Related Decisions
-----------------
- 0003-quads-and-labels.md
- 0016-ids-and-naming-conventions.md
