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
- Enforce snake_case for all identifiers: entity types, local IDs, relation names, and schema filenames. Input in any format (PascalCase, camelCase, SCREAMING_SNAKE, spaces, hyphens) is normalized to snake_case at the CLI edge and in manifest/import parsing.
- Reserved prefixes: `kg:` and `.kg/` are reserved for internal use and should not be used as entity types or local ids.
- Mapping: `.kg/entities/<type>/<percent-encoded-local-id>.yaml` maps to `type:<local-id>` in canonical ID form, where both `<type>` and `<local-id>` are snake_case.
- `validate --fix` migrates existing entities to snake_case (renames directories, files, updates YAML).

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
