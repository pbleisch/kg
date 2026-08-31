# 0027 — Delete LadybugDB, the Index and the Daemon

Status: Decided

Date: 2026-08-30

Authors: Paul

Supersedes: 0008 (daemon), 0022 (LadybugDB index backend). Amends 0001, 0010,
0014, 0024.

Context
-------
kg kept a LadybugDB index of the graph, and a daemon to keep it open. Both were
choices in service of one property — "reads are fast" — and `docs/PRINCIPLES.md`
says so explicitly: neither is a principle, and the daemon "exists *only*
because opening the index costs 22 s".

What made the index expensive was not the graph. 97.55% of the entity store was
embedding floats — 643.5 MB against 16.2 MB of content — written into the source
of truth by `pkg/store/embed.go`. The index was 1.3 GB over 15 MB of knowledge.
The daemon existed to amortize building it.

ADR-0026 records what happens when the floats leave: the corpus walks cold in
0.81 s, a single-entity read costs 3 ms, and the first read after a clone drops
from **783.8 s to 3 ms**. At that point there is nothing left for a resident
process to amortize, and the daemon is dead weight carrying live bugs.

The order matters and is why this was one change and not two. Deleting the
daemon while the floats remained would have been an outage — a thirteen-minute
`kg get`. Evicting the floats while keeping the daemon would have left a
resident process guarding a cache nobody needs.

### What is lost

Cypher, and variable-length path queries. **Confirmed to have no caller**, three
ways:

1. `grep -rn 'Cypher\|MATCH \|conn.Query'` over the tree: every Cypher string
   is inside `pkg/store/ladybug.go`, none is reachable from outside `pkg/store`,
   and no function accepts a query from a caller.
2. `grep -rn 'HasRel\*\|\*1\.\.\|shortestPath\|\[\*'`: no variable-length
   pattern is written anywhere. Recursion — `kg path ->*` and
   `query.BackRecursive` — is a Go BFS issuing one-hop lookups, and always was.
3. The CLI and MCP surfaces expose no query passthrough. The ~30 `kg`
   subcommands and the 13 MCP tools are enumerated in `cmd/kg/` and
   `internal/mcp/server.go`; none takes a query language. Every graph read
   reduces to the six calls the old `store.Indexer` interface named:
   `GetEntity`, `FindByType`, `FindEntities`, `FindByRelationTarget`,
   `FindReferrers`, and vector search.

Decision
--------
Delete `pkg/store/ladybug.go`, `pkg/store/remote.go`, `pkg/store/autostart.go`,
`pkg/store/indexer.go`, `internal/daemon/`, `cmd/kg/daemon.go`, and the tracked
11.7 MB `kg-ladybug` binary at the repository root.

**A read opens files.** Three shapes, chosen per query rather than one index
serving all:

- **A point read is one file.** `kg get` is `ReadEntity` — 3 ms, independent of
  corpus size.
- **A typed find is one directory.** `store.FindByType` reads
  `entities/<type>/`, and does not pay for the rest of the corpus.
- **A whole-graph read is one walk.** `store.LoadGraph` builds ids, types and
  the reverse-edge map in memory, for the queries that genuinely need every
  entity: `back`, `stats`, `validate`, and the referrer scan a merge does.
  Traversal uses `store.Loader`, which reads entities as it reaches them and
  caches what it read, so a path costs the reachable set rather than the corpus.

**Vectors move to a gitignored sidecar** — `.kg/.cache/vectors.bin` plus
`vectors.idx`, `pkg/vector` — and `kg similar` scans it by brute force. Exact,
not approximate: the HNSW index it replaces gave an answer that depended on how
it was built, and returned entities with no attributes on its fast path. The
sidecar returns the same top-10 in the same order with the same scores
(ADR-0026), in 9 ms of scanning.

**No CGO.** kg is pure Go. With that go the 35 MB native library, the download
step in the build, the pin to LadybugDB v0.13.1 in the release workflow, the
three native-runner release jobs, and the `kg-ladybug` binary variant. One
GoReleaser job now cross-compiles every platform. `CGO_ENABLED=0` in CI is the
check that it stays that way.

**`store.Sync` stops meaning "reconcile the index".** Nothing is kept in sync
with the graph any more, so no read calls it and no write calls it. `kg sync`
now means "rebuild the derived data", which is one artifact: the sidecar.

### The bugs that go with it

Named in `docs/PRINCIPLES.md` and now unreachable rather than fixed: the
daemon-status bug, the stale-PID bug, the second-daemon-clobbers-the-first bug,
and the 100-character Unix socket path limit that forced a hashed fallback into
`os.TempDir()`.

Consequences
------------
- **Positive:** The first read on a fresh clone goes from 783.8 s to 3 ms
  (ADR-0026). The reason every consumer script bypassed kg and parsed the YAML
  directly is gone.
- **Positive:** `installs in one command` becomes true. No CGO, no bundled
  native library, no platform-specific archive — which is what increment 8 was
  blocked on.
- **Positive:** ~3,900 lines deleted, including an entire RPC layer, a process
  supervisor, a pid file, a log file and a socket path scheme.
- **Positive:** The remaining derived artifact is one file pair that one command
  rebuilds, which is P2's check verbatim.
- **Negative:** Cypher is gone. If a query arrives that a cold walk cannot
  answer, the answer is a Go function over `store.Graph`, not a database.
- **Negative:** `kg back`, `kg stats` and `kg validate` cost a whole-graph walk
  every time — 0.81 s at 27k, projected ~3 s at the ceiling. The index made
  these cheap *after* it had been built. They are now cheap immediately and
  never cheaper.
- **Negative:** `kg agent status` no longer reports `backend` or
  `daemon_status`; it reports `vectors` and `embedder` instead. That is a
  contract change for a surface whose audience is agents.
- **Negative:** `kg watch --daemon` is gone. Watching is direct only.
- **Negative:** The scale ceiling is now load-bearing. Past it this architecture
  does not hold, which is why ADR-0026 states it.

Related
-------
- ADR-0026 — the measurements this rests on, and the budget that keeps a service
  from coming back. **Unratified**: the deletion is done, the budget is proposed.
- ADR-0025 — the embedder configuration fix, which had to land first or the
  corpus split would have stopped embedding silently.
- P4, P2, P1 — the principles this change is trying to hold.
