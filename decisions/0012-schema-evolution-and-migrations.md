# 0012 - Schema Evolution and Migrations

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
How schema changes are managed, versioning, backward/forward compatibility, and migration tooling.

Decision
--------
Status: Decided

Decision:
- Schema versioning: Per-file `version` field in each schema YAML to allow independent evolution.
- Migration tooling: Provide migration helpers and a `kg migrate` helper for common transformations; automated migrations will be opt-in and reviewed.
- Compatibility: Aim for backward-compatible additions; breaking changes must increment the schema version and include migration guidance.

Consequences:
- Flexibility for independent type evolution; requires migration tooling and documentation to be maintained.

Alternatives Considered
-----------------------
- No automated migrations vs providing migration helpers

Consequences
------------
- Affects long-term maintenance and upgrades across repositories.

Related Decisions
-----------------
- 0002-data-model-and-schema.md
- 0010-implementation-and-dependencies.md
