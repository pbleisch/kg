# 0017 - Provenance and Audit

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define how provenance, timestamps, and audit trails are recorded for entities and relations.

Decision
--------
Status: Decided

Decision:
- Record `created_at` and `updated_at` timestamps in each entity YAML file where practical.
- Maintain a lightweight append-only audit log at `.kg/.cache/audit.log` that records mutation events (`create`, `update`, `delete`, `link`, `unlink`) with timestamps, actor, and a short diff/summary.
- Source/provenance metadata: since quads are deferred, provenance is limited to audit log entries and optional `source` attribute fields on entities; labels/quads may be introduced later.

Consequences:
- Provides reasonable auditability for local workflows without full quad support; audit log is gitignored by default.

Alternatives Considered
-----------------------
- Minimal provenance in YAML vs full audit trail in separate logs

Consequences
------------
- Affects compliance, debugging, and data lineage capabilities.

Related Decisions
-----------------
- 0003-quads-and-labels.md
- 0015-backup-and-recovery.md
