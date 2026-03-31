# 0023 — kg watch Event Stream

Status: Decided

Date: 2026-02-22

Authors: Paul

Context
-------
External tools that need to react to knowledge graph changes have no
clean integration point. They must poll `kg find` and diff, implement
their own filesystem watcher on `.kg/entities/`, or dig into daemon
internals. All three approaches are fragile and duplicate change
detection that kg should own. See desire 0002.

Decision
--------
Add a `kg watch` command that streams entity and relation change events
to stdout as newline-delimited JSON (NDJSON).

### Command interface

```
kg watch [flags]

Flags:
  --json              NDJSON output (default for non-TTY)
  --human             Human-readable lines (default for TTY)
  --snapshot           Emit all current entities as "created" events on startup
  --type <types>      Comma-separated type filter (e.g., --type game,mechanic)
  --debounce <dur>    Coalesce rapid changes (e.g., --debounce 100ms)
  --daemon            Subscribe to running kg daemon instead of watching directly
```

Default behavior: run in foreground, watch `.kg/entities/` via fsnotify,
emit one NDJSON line per event, exit on SIGINT/SIGTERM, auto-detect TTY
for output format.

### Event schema

**Init event** (always emitted first):

```jsonl
{"event":"init","entity_count":147,"types":["game","mechanic",...],"ts":"2026-02-21T21:40:00Z"}
```

**Entity events**:

```jsonl
{"event":"created","ref":"mechanic:wall-jump","type":"mechanic","name":"Wall Jump","ts":"..."}
{"event":"updated","ref":"game:hades","type":"game","name":"Hades","changed":["description","relations"],"ts":"..."}
{"event":"deleted","ref":"mechanic:wall-jump","type":"mechanic","ts":"..."}
```

**Relation events**:

```jsonl
{"event":"linked","from":"game:hades","rel":"uses_mechanic","to":"mechanic:wall-jump","ts":"..."}
{"event":"unlinked","from":"game:hades","rel":"uses_mechanic","to":"mechanic:wall-jump","ts":"..."}
```

**Snapshot events** (with `--snapshot`):

```jsonl
{"event":"snapshot_start","entity_count":147,"ts":"..."}
{"event":"created","ref":"mechanic:permadeath","type":"mechanic","name":"Permadeath","snapshot":true,"ts":"..."}
{"event":"snapshot_end","entity_count":147,"ts":"..."}
```

### Event fields

| Field | Type | Present On | Description |
|-------|------|-----------|-------------|
| `event` | string | all | `init`, `created`, `updated`, `deleted`, `linked`, `unlinked`, `snapshot_start`, `snapshot_end` |
| `ref` | string | entity events | Entity reference in `type:id` format |
| `type` | string | entity events | Schema type name |
| `name` | string | created, updated | Entity name attribute |
| `changed` | string[] | updated | Attribute names and/or `"relations"` |
| `from` | string | relation events | Source entity ref |
| `rel` | string | relation events | Relation name |
| `to` | string | relation events | Target entity ref |
| `snapshot` | bool | created (snapshot) | `true` during initial snapshot |
| `ts` | string | all | ISO 8601 timestamp |

### Human-readable output

When outputting to a TTY (or with `--human`):

```
[21:40:00] init: 147 entities across 11 types
[21:40:01] + mechanic:wall-jump (mechanic) "Wall Jump"
[21:40:05] ~ game:hades (game) "Hades" [description, relations]
[21:40:06] → game:hades --uses_mechanic--> mechanic:wall-jump
[21:41:00] - mechanic:wall-jump (mechanic)
[21:41:01] ✕ game:hades --uses_mechanic--> mechanic:wall-jump
```

### Filesystem watching (default mode)

1. Use `fsnotify` to watch `.kg/entities/` recursively.
2. On create/modify/delete, read YAML to determine entity type, id, name.
3. For `updated`, compare old and new YAML to populate `changed` (cache
   previous state in memory).
4. For relation events, diff the `relations` list before/after.
5. Ignore `.kg/.cache/` changes to avoid noise from `kg sync`.

### Daemon mode

With `--daemon`, connect to a running `kg daemon` (via unix socket) and
subscribe to its internal change stream instead of running a second
filesystem watcher. Error if daemon is not running.

### Debouncing

With `--debounce <dur>`, buffer and coalesce:
- Multiple `updated` for same entity → single event, merged `changed`
- `created` then `updated` → single `created`
- `created` then `deleted` → no event

Useful during bulk ingestion.

### Exit behavior

- Runs until SIGINT/SIGTERM.
- Exit 0 on clean shutdown, exit 1 on error.
- Flushes stdout before exiting.

Consequences
------------
- kg becomes composable in Unix pipelines: dashboards, CI triggers,
  changelogs, and integrations can subscribe with zero custom logic.
- Adds `fsnotify` as a dependency.
- `kg watch` and `kg daemon` serve different purposes: daemon maintains
  index consistency, watch exposes changes externally. They coexist
  cleanly via `--daemon` mode.
- Snapshot mode lets consumers bootstrap state without a separate API
  call.
