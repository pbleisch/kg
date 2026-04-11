# 0014 - Performance and Scaling

Status: Accepted

Date: 2026-02-01
Updated: 2026-04

Authors: Paul

Context
-------
As the graph grew, several O(N²) and O(N×E) hot paths became unacceptable. This decision records the concrete optimizations implemented to keep kg responsive at scale. Target: ≤100k entities on single-file BoltDB or LadybugDB.

Decision
--------

### Index Operations

- **Index-based merge** — `merge.go` replaced `ListEntities()` (full YAML walk) with `FindReferrers()`, which does a BoltDB OPS prefix seek or Cypher query. Merge went from O(N×E) to O(1) index lookup per entity.
- **Batch BoltDB writes during sync** — `SyncWithResult` collects all changes during walk, then batch-writes via `UpsertEntities` / `RemoveEntities` in single transactions. `UpsertEntity` combines remove+add in one `db.Update()` (was two separate transactions with two fsyncs each).
- **Batch dedup embeddings** — Dedup loop replaced per-entity `RunEmbedSingle()` (spawns python per entity, ~1-2s each) with a pre-scan of the manifest and a single `RunEmbedBatch()` call. 100 entities: ~200s → ~3s.
- **Stats from index** — `kg stats` replaced full YAML scan with `StatsProvider` interface: bolt scans SPO bucket once, ladybug uses Cypher aggregation. Sub-millisecond via daemon RPC.

### Similarity Search

- **BoltDB VectorSearcher** — Implemented brute-force cosine similarity over bolt index (FindByType + GetEntity + CosineSimilarity). Replaces O(N²) YAML scan fallback. Adequate for current scale; proper ANN would need an HNSW library.
- **HNSW vector index (LadybugDB)** — `SimilarEntities` rewritten with HNSW vector extension. O(log N) approximate nearest neighbor instead of O(N) brute scan. Top-5 similar: ~1.2s → ~12ms (100× faster).

### Daemon

- **Daemon as index server** — Eliminates repeated index open/close/sync per CLI command. Index stays hot in daemon memory; CLI queries via net/rpc. Sync() skips when daemon running. Auto-start on first command, auto-stop after 30 min idle.

### Query Optimization

- **Server-side filtering (FindEntities, QueryOptions)** — Filters pushed down to backend (Cypher WHERE clause for ladybug). Supports pagination (limit/offset), embedding stripping, relation skipping. 12,657 entities: filtered query 3.5s → 1.1s.
- **Embedding stripping** — `get`, `back`, `similar`, `find` strip embedding vectors from output by default (`--include-embeddings` to restore). Reduces output size significantly (embeddings are ~2-3KB each).

Consequences
------------
- All common operations are sub-second for graphs up to ~13k entities.
- Batch writes and daemon caching reduce fsync pressure on BoltDB.
- HNSW in LadybugDB makes similarity search viable at scale; BoltDB brute-force remains a simpler fallback.
- May require rework (sharding, streaming) for very large graphs (>100k entities).

Related Decisions
-----------------
- 0001-storage-and-index.md
- 0011-testing-and-observability.md
