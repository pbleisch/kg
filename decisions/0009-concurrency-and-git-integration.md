# 0009 - Concurrency and Git Integration

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define file-locking, concurrent edit policies, how kg interacts with git (auto-commit, hooks, user-managed workflows), and conflict resolution.

Decision
--------
Status: Decided

Decision:
- Use optimistic concurrency with validation: `kg` will validate writes and report conflicts but will not take file locks. If a race is detected (hash/mtime mismatch), the operation fails with instructions to retry after pulling latest changes.
- No automatic git operations in v1. `kg` will not auto-commit; users manage git workflows. Provide guidance in docs for conflict resolution and recommended workflows.

Consequences:
- Easier cross-platform ops and no lock-file churn; requires good user workflows for multi-user edits.


Alternatives Considered
-----------------------
- Use file locks (flock) to prevent concurrent writes vs optimistic merges with validation
- Auto-commit vs no automatic git operations

Consequences
------------
- Affects multi-user workflows, CI integration, and developer expectations.

Related Decisions
-----------------
- 0001-storage-and-index.md
- 0015-backup-and-recovery.md
