# 0015 - Backup and Recovery

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define backup procedures for YAML and index, manifest backups, and recovery steps (rebuild index from YAML).

Decision
--------
Status: Decided

Decision:
- Backup strategy: Rely on git for YAML backups. Provide `kg sync --rebuild` to rebuild the index from YAML sources and a verification step to ensure entities count matches expected.
- Index/manifest backups: Not stored in git by default. Document optional external backup procedures for the index and manifest for users who need them.

Consequences:
- Simpler operational model leveraging git; index corruption can be recovered from YAML source via `kg sync --rebuild`.

Alternatives Considered
-----------------------
- Rely on git for YAML backup vs dedicated backup for index and manifest

Consequences
------------
- Affects recoverability after corruption and operations complexity.

Related Decisions
-----------------
- 0001-storage-and-index.md
