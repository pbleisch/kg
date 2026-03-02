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
  index backend for richer query capabilities. The release archive
  bundles the native LadybugDB library alongside the binary — just
  extract and run, no separate library install required.

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

## Releasing

Releases are produced with goreleaser (recommended) or can be done manually. Before releasing, make sure the working tree is clean and checks pass:

```bash
make check
git pull --rebase
```

Recommended: use goreleaser which will build binaries and generate a changelog using the project's .goreleaser.yaml (it contains changelog.filters.exclude, e.g. `^docs:`, `^chore:`, `^test:`, `^bd sync:` to avoid including beads sync commits in generated notes):

```bash
goreleaser release --rm-dist
```

This project publishes two related artifacts per release: a default pure-Go `kg` binary (no CGO, small and portable) and an optional `kg-ladybug` binary that is linked against the native LadybugDB libraries to enable the Ladybug index backend. CI is configured to build both artifacts within a single goreleaser pipeline so releases are produced together while still delivering two separate binaries for users to choose from. Shipping two artifacts keeps the default binary lightweight and maximizes portability for most users, while the Ladybug-enabled variant provides richer query capabilities for users who need it; see "Building with LadybugDB" below for runtime dependencies and build targets.

Manual changelog generation (if not using goreleaser):

```bash
# choose the previous tag, then list commits up to HEAD excluding internal prefixes
prev=$(git describe --tags --abbrev=0 $(git rev-list --tags --max-count=1)^)
# generate notes while filtering out docs/chore/test and bd sync commits
git log --no-merges --pretty=format:'- %s (%h)' "$prev..HEAD" | grep -Ev '^(docs:|chore:|test:|bd sync:)' > docs/release-notes/vX.Y.Z.md
# publish the release with GH CLI
gh release create vX.Y.Z -F docs/release-notes/vX.Y.Z.md
```

Notes on good release notes:

- Focus on user-visible changes: features, breaking changes, bug fixes, and upgrade guidance.
- Exclude internal workflow noise (beads/bd sync commits) — the goreleaser config already filters `^bd sync:`; when generating notes manually, use the grep filter above.
- Keep entries short and link to PRs or issues when helpful.

### Building with LadybugDB

```bash
make build-ladybug   # compile to bin/kg-ladybug (requires CGO + native libs)
make test-ladybug    # run tests with ladybug backend
make check-ladybug   # lint + test with ladybug
```

Requires Go 1.25+.

### Installing Ladybug native libraries

Release archives for `kg-ladybug` already bundle the native library. When
you extract the archive, the `lib-ladybug/` directory is placed next to the
binary and the binary finds it automatically via its embedded rpath — no
extra steps needed.

```
kg-ladybug_vX.Y.Z_linux_amd64.tar.gz
  kg-ladybug          ← binary
  lib-ladybug/
    liblbug.so        ← native lib, found automatically at runtime
```

macOS and Windows archives follow the same layout (`.dylib` / `.dll`).

**Only needed if you install the binary system-wide** (e.g., move
`kg-ladybug` to `/usr/local/bin/` without `lib-ladybug/` beside it). In
that case, copy the lib to a standard library path:

```bash
# Linux
sudo cp lib-ladybug/liblbug.so /usr/local/lib/ && sudo ldconfig

# macOS
sudo cp lib-ladybug/liblbug.dylib /usr/local/lib/
```

Or keep the extracted directory together and add it to `PATH` instead of
moving the binary. For more options see the official Ladybug installation
guide: https://docs.ladybugdb.com/installation/

## Architecture Decisions

Design decisions are recorded in `decisions/` as lightweight ADRs. Key
decisions include storage and indexing (0001), data model (0002), CLI
grammar (0004), MCP server (0007), daemon (0008), agent integration
(0021), and LadybugDB index backend (0022).

## License

See [LICENSE](LICENSE) for details.
