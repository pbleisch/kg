# 0022 — LadybugDB Index Backend

Status: Decided

Date: 2025-07-14

Authors: Paul

Context
-------
kg uses YAML files as the source of truth for all graph data. BoltDB
serves as a disposable quad-style index (SPO/OPS buckets) that is
rebuilt from YAML on demand. This architecture keeps the storage format
human-readable and diff-friendly while still enabling fast lookups.

BoltDB is pure Go and requires no CGO, which simplifies builds and
cross-compilation. However, its key-value model limits query
expressiveness — traversals, pattern matching, and multi-hop path
queries must be implemented in application code.

LadybugDB (formerly Kuzu) is an embedded property graph database with
a Cypher query language. Go bindings are available at
github.com/LadybugDB/go-ladybug but require CGO. LadybugDB offers a
native graph data model with built-in support for pattern matching,
variable-length path queries, and property filters — capabilities that
would be expensive to reimplement on top of BoltDB.

We want richer query capabilities without losing the simple, portable,
pure-Go default.

Decision
--------
Introduce a `store.Indexer` interface (already done) with two
implementations:

- **boltIndex** — pure Go, default backend. Uses the existing SPO/OPS
  bucket layout. No CGO required.
- **ladybugIndex** — CGO-required, opt-in backend. Maps kg entities and
  relations to LadybugDB's native property graph model and exposes
  Cypher query support.

### Build tags

Use Go build tags to gate the CGO backend:

    //go:build ladybug

The default build includes BoltDB only. Building with
`-tags ladybug` compiles in the LadybugDB backend.

### Runtime backend selection

Backend selection at runtime follows this order:

1. Explicit override in `.kg/config.yaml` (`index.backend: ladybug` or
   `index.backend: bolt`).
2. Auto-detect: prefer LadybugDB if compiled in, otherwise fall back to
   BoltDB.

### Binary variants

Ship two binary variants via goreleaser:

- `kg` — pure Go, BoltDB only. Portable, no CGO.
- `kg-ladybug` — compiled with `-tags ladybug`. Requires CGO and
  LadybugDB shared libraries.

### Source of truth unchanged

YAML remains the sole source of truth. Both backends are disposable
caches rebuilt from YAML via `kg reindex`. Switching backends requires
only a reindex, not a data migration.

Consequences
------------
- **Positive:** Richer graph queries via Cypher — pattern matching,
  variable-length paths, and property filters become first-class.
- **Positive:** Native property graph model aligns naturally with kg's
  entity/relation semantics.
- **Positive:** No breaking changes — the default build is unchanged
  and remains pure Go.
- **Positive:** Future extensibility — additional index backends can
  implement the same `store.Indexer` interface.
- **Negative:** CGO complicates cross-compilation and CI for the
  ladybug variant.
- **Negative:** LadybugDB Go bindings are young (7 GitHub stars),
  carrying ecosystem risk.
- **Neutral:** Requires goreleaser dual-build configuration to produce
  both binary variants.

Alternatives Considered
-----------------------
- **Full replacement of BoltDB with LadybugDB:** Rejected because
  requiring CGO for all builds would break the portable pure-Go
  default and complicate CI.
- **Keep BoltDB only:** Rejected because it limits query
  expressiveness, forcing complex graph traversals into application
  code.
- **Use SQLite FTS instead:** Rejected because SQLite is a relational
  database with full-text search, not a graph database. It would not
  provide native graph traversal or Cypher-style pattern matching.

Related Decisions
-----------------
- 0001-storage-and-index.md — original storage and index architecture
