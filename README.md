# kg — Knowledge Graph CLI

A Git-friendly knowledge graph stored as YAML entities with a fast local
index. Designed for humans and AI agents alike.

## Quick Start

```bash
# Initialize a knowledge graph
kg init

# Create entities
kg create person alice name=Alice role=engineer
kg create project kg name="Knowledge Graph CLI"

# Link them
kg link person:alice works_on project:kg

# Query
kg get person:alice
kg find person name~Ali
kg path person:alice -> works_on
kg back project:kg
```

## Install

### From source

```bash
git clone https://github.com/pbleisch/kg.git
cd kg
make build          # builds to bin/kg
make install        # installs to $GOPATH/bin
```

### From release

Download the latest binary from
[Releases](https://github.com/pbleisch/kg/releases) (Linux and macOS,
amd64 and arm64).

Two variants are available:

- **`kg`** — Pure Go, no external dependencies (default)
- **`kg-ladybug`** — Includes [LadybugDB](https://ladybugdb.com) graph
  index backend for richer query capabilities (requires CGO)

## Commands

| Command | Description |
|---------|-------------|
| `kg init` | Scaffold a new `.kg/` directory |
| `kg create <Type> <id> [attrs...]` | Create an entity |
| `kg get <type:id>` | Get an entity by ID |
| `kg find <Type> [filters...]` | Find entities by type |
| `kg set <type:id> key=value` | Set attributes |
| `kg delete <type:id>` | Delete an entity |
| `kg link <from> <rel> <to>` | Create a relation |
| `kg unlink <from> <rel> <to>` | Remove a relation |
| `kg path <id> -> <rel>` | Traverse forward relations |
| `kg back <id>` | Find reverse references |
| `kg types` | List schema types |
| `kg describe <Type>` | Show type schema |
| `kg validate` | Validate entities against schemas |
| `kg sync` | Sync YAML to the index |
| `kg daemon start\|stop\|status` | Background index watcher |
| `kg watch` | Stream entity/relation change events |
| `kg morph` | Define and use reusable path patterns |
| `kg mcp serve` | Start MCP server (stdio) |
| `kg agent status\|schema\|skill\|examples\|mcp-config\|check` | Agent integration tools |

## Output Formats

Default output uses **TOON** (Token-Optimized Object Notation) — ~40%
fewer tokens than JSON, ideal for AI agents. Alternatives:

```bash
kg get person:alice --json    # JSON
kg get person:alice --human   # Human-readable
```

## Watching for Changes

`kg watch` streams entity and relation change events as NDJSON:

```bash
kg watch                             # human-readable (TTY) or NDJSON (piped)
kg watch --json                      # force NDJSON output
kg watch --json --snapshot           # emit all entities first, then stream
kg watch --json --type Game,Mechanic # filter by entity type
kg watch --json --debounce 100ms     # coalesce rapid changes
kg watch --daemon                    # subscribe to running kg daemon
```

Events: `init`, `created`, `updated`, `deleted`, `linked`, `unlinked`.

## Storage Model

- **Source of truth:** YAML files in `.kg/entities/<type>/<id>.yaml`
- **Schemas:** YAML files in `.kg/schema/<type>.yaml`
- **Index:** BoltDB or LadybugDB cache in `.kg/.cache/` (gitignored, rebuilt with `kg sync`)
- **Config:** `.kg/config.yaml`

The YAML files are designed to be committed to Git. The index is a local
cache that accelerates queries.

### Index Backend

The index backend can be configured in `.kg/config.yaml`:

```yaml
backend: auto  # auto (default), bolt, or ladybug
```

- **auto** — Uses LadybugDB if compiled in, otherwise BoltDB
- **bolt** — BoltDB (pure Go, always available)
- **ladybug** — LadybugDB (requires the `kg-ladybug` binary)

## Agent Integration

`kg agent` provides onboarding and discovery for AI coding agents:

```bash
kg agent status      # Graph state: entity counts, schema types, index health
kg agent schema      # Full schema dump in agent-readable format
kg agent skill       # Generate a SKILL.md for agent platforms
kg agent examples    # Example commands using your actual schema types
kg agent mcp-config  # Ready-to-paste MCP server config
kg agent check       # Health check with exit codes
```

Platform-specific output with `--github` or `--claude` flags:

```bash
kg agent skill --github       # writes .github/skills/kg/SKILL.md
kg agent mcp-config --claude  # emits claude_desktop_config.json format
```

## MCP Server

kg exposes its operations via the [Model Context Protocol](https://modelcontextprotocol.io/):

```bash
kg mcp serve    # starts on stdio
```

Configure your agent platform with `kg agent mcp-config`.

## Development

```bash
make build       # compile to bin/kg
make test        # run tests
make test-race   # run tests with race detector
make lint        # go vet
make check       # lint + test
make cover       # test coverage report
make local       # build and copy to .kg/bin/kg-<os>
```

### Building with LadybugDB

```bash
make build-ladybug   # compile to bin/kg-ladybug (requires CGO + native libs)
make test-ladybug    # run tests with ladybug backend
make check-ladybug   # lint + test with ladybug
```

Requires Go 1.25+.

## Architecture Decisions

Design decisions are recorded in `decisions/` as lightweight ADRs. Key
decisions include storage and indexing (0001), data model (0002), CLI
grammar (0004), MCP server (0007), daemon (0008), agent integration
(0021), and LadybugDB index backend (0022).

## License

See [LICENSE](LICENSE) for details.
