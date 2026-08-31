# 0001 - Storage and Index

**AMENDED 2026-08-30 by [0027](0027-delete-index-and-daemon.md).** The index is gone
and so is the daemon that watched it. The storage half of this ADR — YAML files as the
sole source of truth — stands and is the whole architecture now; the "hybrid sync model"
below describes deleted code. The one derived artifact left is the vector sidecar, which
no read depends on and one command rebuilds.


Status: Proposed

Date: 2026-02-01

Updated: 2026-04 — BoltDB/Cayley has been superseded by LadybugDB as the
sole index backend. See 0022-ladybug-index-backend.md.

Authors: Paul

Context
-------
The knowledge graph stores YAML source-of-truth on disk and an index for fast queries. This decision records index backend, index location, sync model, manifest format, and rebuild strategy.

Decision
--------
Status: Decided

Decision:
- Index engine: Cayley + BoltDB
- Index file path: .kg/.cache/cayley.bolt (gitignored)
- Manifest path: .kg/.cache/manifest.json (gitignored)
- Sync model: Hybrid — daemon (file-watch) keeps the index up-to-date for normal CLI usage; CLI falls back to lazy-on-read and may trigger `kg sync` when the index is stale.
- Rebuild strategy: Provide `kg sync --rebuild` to rebuild the index from YAML sources; manifest checksums and mtimes are used to detect stale or corrupted indexes and trigger rebuilds.


Alternatives Considered
-----------------------
- Cayley + BoltDB: Pros—graph semantics, existing integrations; Cons—added dependency and complexity
- SQLite-based inverted index: Pros—simplicity and single binary; Cons—limited graph semantics
- In-memory index with on-disk snapshots: Pros—fast, Cons—persistence complexity

Consequences
------------
- Choice impacts performance, portability, and operational complexity.
- Manifest design affects sync correctness and cache invalidation.

Related Decisions
-----------------
- 0009-concurrency-and-git-integration.md
- 0014-performance-and-scaling.md
