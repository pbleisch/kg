# 0008 - Daemon (background index watcher)

Status: Accepted

Date: 2026-02-01

Updated: 2026-07

Authors: Paul

Context
-------
Opening the index on every CLI invocation adds latency. A background daemon can hold the index open, watch for YAML file changes, and serve queries over IPC — eliminating per-command open/close cost and keeping the index continuously fresh.

Decision
--------

### IPC: net/rpc over Unix sockets

Go stdlib `net/rpc` with gob encoding — zero external dependencies. Two sockets per kg directory:

- **Control socket** (`daemon.sock`) — text protocol for `stop`, `status`, and `subscribe` (NDJSON event streaming).
- **RPC socket** (`daemon-rpc.sock`) — `net/rpc` for index operations (`GetEntity`, `FindByType`, `FindByRelationTarget`, `FindReferrers`, `AddEntity`, `RemoveEntity`, `UpsertEntity`, `UpsertEntities`, `RemoveEntities`, `SimilarEntities`, `IndexStats`, `Sync`).

Socket paths: `<kgDir>/.cache/daemon.sock` and `<kgDir>/.cache/daemon-rpc.sock`. When the absolute path is ≥100 bytes (Unix socket limit), a hashed fallback in `$TMPDIR` is used (e.g., `kg-daemon-<sha256[:16]>.sock`).

### Index server model

`OpenIndex(kgDir)` probes the daemon socket (100ms timeout). If a daemon is running, it returns a `remoteIndex` that proxies `Indexer`, `BatchIndexer`, `VectorSearcher`, and `StatsProvider` via RPC. If no daemon is available, it falls back to opening the LadybugDB index directly. `Sync()` is a no-op when the daemon is running since the daemon keeps the index fresh via file watcher.

### RPC service

`IndexService` wraps the real `store.Indexer` behind a `sync.RWMutex`. Read methods (`Get`, `Find*`, `Similar`, `Stats`) take an RLock for concurrent reads; write methods (`Add`, `Remove`, `Upsert*`, `Sync`) take an exclusive Lock.

### Watch strategy

`fsnotify` subscribes to native filesystem events on `.kg/entities/`. File change events are debounced (100ms) before re-indexing. The watch also powers the subscribe stream: events (`created`, `updated`, `deleted` with entity diffs) are pushed as NDJSON to connected control-socket subscribers. `kg watch --daemon` connects to this stream.

### Lifecycle: `kg daemon start|stop|status`

- **start**: runs the daemon in the foreground, writes PID file at `.kg/.cache/daemon.pid`, opens the index, listens on both sockets.
- **stop**: sends `stop` to the control socket; daemon shuts down gracefully.
- **status**: sends `status` to the control socket; prints `running` or `not running`.

### Auto-start

The first `kg` command that finds no daemon auto-forks one (`kg --dir <absPath> daemon start` as a detached process). Guard: only auto-starts from a real `kg` or `kg-ladybug` binary, not test binaries. Opt-out via `KG_NO_DAEMON` environment variable. Race-safe: only one daemon can bind the socket; losers fail silently.

### Auto-stop (idle timeout)

Default: 30 minutes of no activity. Activity = file changes, RPC calls, or control commands (except `status`). Checked every minute; graceful shutdown when idle. Disabled if `idleTimeout ≤ 0`.

### Per-graph isolation

Each `.kg` directory gets its own daemon, sockets, and PID file. Socket paths are derived from the kgDir so there are no collisions between repositories.

### Security

Unix socket file permissions provide OS-level access control. No token auth — this is a single-user local tool. The daemon is only accessible to the file owner.

### Daemon lifecycle redesign

**Socket-bind-as-lock.** `net.Listen("unix", sockPath)` is the atomic mutex. If it fails with EADDRINUSE, another daemon is running. This replaces all check-then-act (TOCTOU) patterns in auto-start.

**PID validation on startup.** `cleanStale()` reads the PID file, checks if the process is alive via `Signal(0)`. Dead process → clean up stale sockets and PID. Live process → let socket-bind detect the conflict.

**Idempotent Stop().** Uses `sync.Once` to prevent double-close panic on the `done` channel. Multiple concurrent callers (idle timeout, signal handler, control socket) all safely converge.

**RPC drain before shutdown.** `IndexService` tracks in-flight RPCs via `sync.WaitGroup`. `Drain()` sets a draining flag (rejects new RPCs) then waits for active ones to complete. `Stop()` calls `Drain()` before closing the index.

**Ping health check.** `DaemonAvailable()` now performs an actual RPC `Ping()` call instead of just probing the socket. Distinguishes "alive and responsive" from "stale socket on disk."

**Start() error cleanup.** Success-flag pattern ensures sockets, PID file, and listeners are cleaned up on any error during startup.

**OpenIndex fallback.** If Ping succeeds but DialRemote fails, falls back to direct index access with a warning log instead of returning an error.

**Post-crash index smoke test.** On startup, daemon runs `FindByType("__smoke_test__")` to verify the index is queryable. If corrupted, blows away `.cache/ladybug/` and rebuilds from YAML.

### Index durability

**Atomic manifest writes.** `SaveManifest()` uses temp file → fsync → rename pattern. Crash during write leaves either the old or new manifest, never a truncated one.

**Transaction result cleanup.** All BEGIN/COMMIT/ROLLBACK `Query()` results properly closed to prevent resource leaks in go-ladybug connection.

**Embedding errors propagated.** `upsertEmbeddings()` returns errors instead of silently swallowing them. Embedding failures prevent manifest update for affected entities, ensuring retry on next sync.

**ensureEmbVec state safety.** `embDim` and `embStmt` only set after all initialization steps succeed, preventing nil-pointer use from partial init.

**Rollback error handling.** Rollback failures logged as warnings instead of suppressed, making connection-state issues visible.

### Daemon logging

**File-based logging with rotation.** Daemon logs to `.cache/daemon.log` with 5 MB size-based rotation (1 backup). Sentinel file `.cache/daemon.logfile` points to the active log path.

**`kg daemon logs` command.** New subcommand to view daemon logs, with `--follow`/`-f` flag for real-time tailing.

Alternatives Considered
-----------------------
- **gRPC / protobuf** — typed API and streaming, but adds a large dependency for a local single-user tool. `net/rpc` with gob is zero-dep and sufficient.
- **Single shared daemon** vs per-graph daemon — per-graph is simpler (no multiplexing, no cross-graph auth) and matches the one-repo-one-index model.
- **HTTP/TCP** — unnecessary for local IPC; Unix sockets give free file-permission security.
- **Polling** vs filesystem events — `fsnotify` gives sub-second latency without CPU cost.

Consequences
------------
- CLI latency drops significantly (no per-command index open).
- Auto-start/stop means users don't manage daemons manually; `KG_NO_DAEMON` is the escape hatch.
- Two sockets add modest complexity but cleanly separate streaming (control) from request/response (RPC).
- Transparent fallback to direct index access means the daemon is never required.
- Socket-bind-as-lock eliminates TOCTOU races in auto-start without advisory lock files.
- RPC drain guarantees in-flight operations complete before shutdown, avoiding index corruption.
- Atomic manifest writes and proper transaction cleanup make the index resilient to crashes.
- File-based logging with rotation provides post-mortem debugging without unbounded disk growth.

Related Decisions
-----------------
- 0007-mcp.md
