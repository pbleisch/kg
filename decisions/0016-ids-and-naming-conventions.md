# 0016 - IDs and Naming Conventions

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define canonical ID rules, reserved prefixes, case rules, and mapping between filenames and IDs.

Decision
--------
Status: Decided

Decision:
- Enforce lowercase kebab-case types in IDs and filesystem-safe local-ids. Local-ids containing special characters are percent-encoded when used as filenames.
- Reserved prefixes: `kg:` and `.kg/` are reserved for internal use and should not be used as entity types or local ids.
- Mapping: `.kg/entities/<type>/<percent-encoded-local-id>.yaml` maps to `type:<local-id>` in canonical ID form.

Consequences:
- Ensures cross-platform filename safety and consistent ID parsing.

Alternatives Considered
-----------------------
- URL-safe IDs vs free-form local IDs

Consequences
------------
- Affects portability, cross-platform filename safety, and agent serialization.

Related Decisions
-----------------
- 0002-data-model-and-schema.md
