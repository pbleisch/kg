# 0022 — LadybugDB Index Backend

Status: Decided

Date: 2025-07-14

Updated: 2026-04

Authors: Paul

Context
-------
kg uses YAML files as the source of truth for all graph data. The index
is a disposable cache rebuilt from YAML on demand. This architecture keeps
the storage format human-readable and diff-friendly while enabling fast
lookups.

LadybugDB (formerly Kuzu) is an embedded property graph database with a
Cypher query language. Go bindings are available at
github.com/LadybugDB/go-ladybug and require CGO. LadybugDB offers a
native graph data model with built-in support for pattern matching,
variable-length path queries, and property filters.

Previously, BoltDB served as a pure-Go fallback backend that required no
CGO. As of 2026-04, LadybugDB is the sole backend — the BoltDB backend
and the dual-binary strategy have been removed.

Decision
--------
Use LadybugDB as the only index backend behind the `store.Indexer`
interface.

### Build requirements

All builds require CGO and the LadybugDB native library. The `kg` binary
ships with the native library bundled in `lib-ladybug/`.

### Runtime backend selection

The `backend:` field in `.kg/config.yaml` is retained for forward
compatibility but only `ladybug` is accepted. Legacy values (`auto`,
`bolt`) are treated as `ladybug`.

### Binary variants (superseded)

The previous dual-binary strategy (`kg` pure-Go + `kg-ladybug` with CGO)
is no longer used. A single `kg` binary is produced, linked against
LadybugDB. The `kg-ladybug` name is retained as a legacy alias.

### Source of truth unchanged

YAML remains the sole source of truth. The LadybugDB index is a disposable
cache rebuilt from YAML via `kg reindex`. No data migration is needed.

Consequences
------------
- **Positive:** Richer graph queries via Cypher — pattern matching,
  variable-length paths, and property filters are first-class.
- **Positive:** Native property graph model aligns naturally with kg's
  entity/relation semantics.
- **Positive:** Simpler build and release pipeline — one binary, one backend.
- **Positive:** Future extensibility — additional index backends can
  implement the same `store.Indexer` interface.
- **Negative:** CGO is now required for all builds, complicating
  cross-compilation.
- **Negative:** LadybugDB Go bindings are young, carrying ecosystem risk.

Alternatives Considered
-----------------------
- **Keep dual BoltDB + LadybugDB backends:** Rejected because maintaining
  two backends added complexity with diminishing benefit — LadybugDB is
  strictly more capable.
- **Keep BoltDB only:** Rejected because it limits query expressiveness,
  forcing complex graph traversals into application code.
- **Use SQLite FTS instead:** Rejected because SQLite is a relational
  database with full-text search, not a graph database. It would not
  provide native graph traversal or Cypher-style pattern matching.

Related Decisions
-----------------
- 0001-storage-and-index.md — original storage and index architecture
