# 0008 - Daemon (background index watcher)

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Daemon mode runs a background process per knowledge graph to keep the index hot and watch YAML files for changes. Decisions to record: IPC mechanism for client communication, file watch backend (fsnotify/inotify/fsevents), autostart behavior (system/user service vs none), resource limits, security & access control, and sync triggers.

Decision
--------
Status: Decided

Decision:
- IPC: gRPC over a Unix domain socket is the primary transport. gRPC provides a typed protobuf API, streaming, deadlines, and metadata that suits daemon<->CLI communication while running over UDS preserves OS-level socket permissions.
- Watch strategy: Use Go's `fsnotify` to subscribe to native filesystem events (inotify, fsevents, etc.). Debounce rapid event bursts and apply exponential backoff on repeated failures.
- Lifecycle: Default to one daemon per knowledge graph. Provide `kg daemon start|stop|status|logs` commands and document optional OS service integration (systemd/launchd) for autostart; CLI falls back to lazy-on-read sync if daemon is not running.
- Activation & Discovery (detailed):
  - Socket path: The daemon exposes a per-graph Unix domain socket at `.kg/.cache/daemon-<workspace-hash>.sock`. The `<workspace-hash>` is derived deterministically (e.g., SHA256) from the absolute path of the graph root to avoid collisions when the same repo is mounted at different locations.
  - Token and metadata files: On startup the daemon writes `.kg/.cache/daemon.token` (random secret, file-perms 0600) and `.kg/.cache/daemon.meta.json` containing `workspace_id`, `pid`, `version`, and `manifest_checksum`.
  - Client discovery flow (kg CLI or MCP adapter):
    1. Check for socket presence at expected path. If missing, attempt to atomically create a lockfile `.kg/.cache/daemon.lock` using O_EXCL; if lock acquisition succeeds, spawn a detached daemon process bound to the graph and wait up to configurable timeout (e.g., 3s) for the socket to become ready.
    2. If socket exists, attempt a gRPC handshake that includes the client's expected `workspace_id` and `token` (if present). The daemon responds with its metadata. If the workspace_id and token match, proceed to use the daemon.
    3. If the handshake reveals a different `workspace_id` or missing/invalid token, treat the socket as stale/untrusted: attempt to check the PID in `daemon.meta.json` for liveness; if dead or unresponsive, attempt atomic takeover by removing socket and lockfile and spawning a new daemon as in (1). If takeover fails, fallback to lazy-on-read sync.
    4. If handshake completes but manifest checksums differ (daemon index stale), the client may request a quick sync or trigger a `kg sync` operation; the daemon may be asked to perform an immediate sync or schedule one.
  - Race/edge-case handling: Clients must re-validate the socket metadata after connect (race between takeover and a true daemon start). All operations that mutate the state should verify the daemon's `manifest_checksum` before proceeding and abort with actionable errors if a mismatch is observed.
- Debugging / CLI shim: `kg` is the primary debugging shim. Expose `kg daemon status`, `kg daemon logs [--follow]`, `kg debug call <method> [--json|--human]`, and `kg debug socket` to inspect or invoke daemon RPCs interactively; protobuf↔JSON conversion is used for human-friendly views.
- Security & auth: Use UDS with OS-level file permissions by default; document token-based auth and TLS for remote/advanced deployments. Aligns with decision 0013 for more details. The `.kg/.cache/daemon.token` is used as a shared secret for handshake to prevent accidental cross-workspace connections.
- Sync interaction: Daemon auto-syncs the index on file events and updates `.kg/.cache/manifest.json` and the audit log; `kg sync` and `kg sync --rebuild` remain available for explicit control. Git operations remain user-managed.


Alternatives Considered
-----------------------
- Per-graph daemon vs single shared daemon
- Unix socket vs HTTP/TCP
- Filesystem events vs polling

Consequences
------------
- A daemon improves CLI latency and enables auto-sync but increases operational complexity and security surface.
- IPC choice affects portability, discovery, and ease of client implementation.

Related Decisions
-----------------
- 0007-mcp.md
