# 0014 - Performance and Scaling

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define performance targets, index sizing strategies, caching, sync latency expectations, and daemon resource limits.

Decision
--------
Status: Decided

Decision:
- Target scale for v1: Small (<=100k entities). Optimize for a single-file BoltDB index with modest in-memory caching in the daemon.
- Memory vs disk: prefer on-disk BoltDB with memory caching of hot nodes; avoid sharding in v1.
- Resource defaults: set conservative daemon memory and file-handle limits; document tuning guidance for larger graphs.

Consequences:
- Simplifies implementation and testing; may require rework for very large graphs.

Alternatives Considered
-----------------------
- Single-file BoltDB index vs sharded indexes

Consequences
------------
- Affects architecture of index and daemon resource planning.

Related Decisions
-----------------
- 0001-storage-and-index.md
- 0011-testing-and-observability.md
