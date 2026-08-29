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

### macOS and Linux

```bash
VER=$(curl -fsSL https://api.github.com/repos/pbleisch/kg/releases/latest \
  | grep '"tag_name"' | cut -d'"' -f4) && \
OS=$(uname -s | tr '[:upper:]' '[:lower:]') && \
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
curl -fsSL "https://github.com/pbleisch/kg/releases/download/${VER}/kg_${VER}_${OS}_${ARCH}.tar.gz" \
  | sudo tar xzf - -C /usr/local/bin
```

The release archive contains the binary and a `lib-ladybug/` directory with
the [LadybugDB](https://ladybugdb.com) native library (bundled, no separate
install needed). Extracting them together keeps the native library discoverable
at runtime. If you later move only the binary, copy `lib-ladybug/` alongside
it or install the library system-wide
(`sudo cp lib-ladybug/liblbug.* /usr/local/lib/ && sudo ldconfig`).

### Windows

```powershell
$ver = (Invoke-RestMethod https://api.github.com/repos/pbleisch/kg/releases/latest).tag_name
Invoke-WebRequest "https://github.com/pbleisch/kg/releases/download/$ver/kg_${ver}_windows_amd64.zip" `
  -OutFile kg.zip
Expand-Archive kg.zip . -Force; Remove-Item kg.zip
# Move kg.exe and lbug_shared.dll to the same directory in your PATH
```

### From source

Requires CGO and a C compiler (for LadybugDB native bindings):

```bash
git clone https://github.com/pbleisch/kg.git
cd kg
mage build          # builds to bin/kg (downloads native lib if needed)
mage install        # installs kg + lib-ladybug/ to $GOPATH/bin
```

The native library is downloaded automatically on first build to
`lib-ladybug/` and installed alongside the binary in `$GOPATH/bin/lib-ladybug/`
so the rpath resolves correctly.

## Commands

| Command | Description |
|---------|-------------|
| `kg init` | Scaffold a new `.kg/` directory |
| `kg create <type> <id> [attrs...]` | Create an entity |
| `kg get <type:id>` | Get an entity by ID (strips embeddings; `--include-embeddings` to keep) |
| `kg find <type> [filters...]` | Find entities by type (`--limit`, `--offset`, `--include-embeddings`, `--include-relations`) |
| `kg similar <type> --field <f> --vector\|--like\|--vector-file` | Find similar entities by vector (strips embeddings; `--include-embeddings` to keep) |
| `kg set <type:id> key=value` | Set attributes |
| `kg delete <type:id>` | Delete an entity |
| `kg link <from> <rel> <to>` | Create a relation |
| `kg unlink <from> <rel> <to>` | Remove a relation |
| `kg path <id> -> <rel>` | Traverse forward relations |
| `kg back <id>` | Find reverse references (strips embeddings; `--include-embeddings` to keep) |
| `kg types` | List schema types |
| `kg describe <type>` | Show type schema |
| `kg merge <source> <target>` | Merge two entities |
| `kg dedup <type> --threshold --fields` | Find/merge duplicate entities by similarity |
| `kg stats` | Graph metrics |
| `kg validate` | Validate entities against schemas |
| `kg sync` | Sync YAML to the index |
| `kg daemon start\|stop\|status` | Background index server (auto-starts) |
| `kg watch` | Stream entity/relation change events |
| `kg morph` | Define and use reusable path patterns |
| `kg mcp serve` | Start MCP server (stdio) |
| `kg agent status\|schema\|skill\|examples\|mcp-config\|check` | Agent integration tools |

Examples with new flags:

```bash
kg find person name~Ali --limit 10          # paginated results
kg find person --include-relations          # include relations in output
kg get person:alice --include-embeddings    # include embedding vectors
```

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
kg watch --json --type game,mechanic # filter by entity type
kg watch --json --debounce 100ms     # coalesce rapid changes
kg watch --daemon                    # subscribe to running kg daemon
```

Events: `init`, `created`, `updated`, `deleted`, `linked`, `unlinked`.

## Daemon Mode

The daemon holds the index open and serves queries via RPC, eliminating
lock contention and repeated index open/close overhead:

```bash
kg daemon start     # start manually (foreground)
kg daemon stop      # stop the daemon
kg daemon status    # check if running
```

**Auto-start:** The first `kg` command automatically forks a daemon in
the background. Subsequent commands connect via RPC instead of opening
the index directly. The daemon exits after 30 minutes of inactivity.

**Opt-out:** Set `KG_NO_DAEMON=1` to disable auto-start.

## Storage Model

- **Source of truth:** YAML files in `.kg/entities/<type>/<id>.yaml`
- **Schemas:** YAML files in `.kg/schema/<type>.yaml`
- **Index:** LadybugDB cache in `.kg/.cache/` (gitignored, rebuilt with `kg sync`)
- **Config:** `.kg/config.yaml`

The YAML files are designed to be committed to Git. The index is a local
cache that accelerates queries.

### Index Backend

The index backend is configured in `.kg/config.yaml`:

```yaml
backend: ladybug
```

LadybugDB is the only supported backend. The `backend:` field is retained
for forward compatibility but only `ladybug` is accepted.

When the daemon is running, CLI commands connect to it via RPC instead
of opening the index file directly. This eliminates lock contention and
keeps the index hot in memory. See [Daemon Mode](#daemon-mode).

### Embeddings

kg can automatically generate vector embeddings for entity attributes,
enabling semantic similarity search with `kg similar`.

```bash
kg embed --init                          # scaffold embedding script
pip install -r .kg/scripts/requirements.txt
kg embed                                 # generate vectors
kg similar person --field name --like person:alice
```

See [docs/embedding.md](docs/embedding.md) for the full guide.

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

This project uses [Mage](https://magefile.org) as its build tool. Install it with
`go install github.com/magefile/mage@latest`.

```bash
mage build       # compile to bin/kg
mage test        # run tests
mage testRace    # run tests with race detector
mage lint        # go vet
mage check       # lint + test
mage cover       # test coverage report
mage local       # build and copy to .kg/bin/kg-<os>
```

## Releasing

Releases are produced with goreleaser (recommended) or can be done manually. Before releasing, make sure the working tree is clean and checks pass:

```bash
mage check
git pull --rebase
```

Recommended: use goreleaser which will build binaries and generate a changelog using the project's .goreleaser.yaml (it contains changelog.filters.exclude, e.g. `^docs:`, `^chore:`, `^test:`):

```bash
goreleaser release --rm-dist
```

The release produces a single `kg` binary linked against LadybugDB. The
archive includes the binary and the `lib-ladybug/` native library directory.
(`kg-ladybug` is a legacy alias — both binaries are identical.)

Manual changelog generation (if not using goreleaser):

```bash
# choose the previous tag, then list commits up to HEAD excluding internal prefixes
prev=$(git describe --tags --abbrev=0 $(git rev-list --tags --max-count=1)^)
# generate notes while filtering out docs/chore/test commits
git log --no-merges --pretty=format:'- %s (%h)' "$prev..HEAD" | grep -Ev '^(docs:|chore:|test:)' > docs/release-notes/vX.Y.Z.md
# publish the release with GH CLI
gh release create vX.Y.Z -F docs/release-notes/vX.Y.Z.md
```

Notes on good release notes:

- Focus on user-visible changes: features, breaking changes, bug fixes, and upgrade guidance.
- Exclude internal workflow noise — when generating notes manually, use the grep filter above.
- Keep entries short and link to PRs or issues when helpful.

### Building

```bash
mage build           # compile to bin/kg (requires CGO + native libs)
mage test            # run tests
mage check           # lint + test
```

Requires Go 1.25+.

### Installing Ladybug native libraries

Release archives already bundle the native library. When you extract the
archive, the `lib-ladybug/` directory is placed next to the binary and the
binary finds it automatically via its embedded rpath — no extra steps needed.

```
kg_vX.Y.Z_linux_amd64.tar.gz
  kg                   ← binary
  lib-ladybug/
    liblbug.so         ← native lib, found automatically at runtime
```

macOS and Windows archives follow the same layout (`.dylib` / `.dll`).

**Only needed if you install the binary system-wide** (e.g., move `kg` to
`/usr/local/bin/` without `lib-ladybug/` beside it). In that case, copy the
lib to a standard library path:

```bash
# Linux
sudo cp lib-ladybug/liblbug.so /usr/local/lib/ && sudo ldconfig

# macOS
sudo cp lib-ladybug/liblbug.dylib /usr/local/lib/
```

Or keep the extracted directory together and add it to `PATH` instead of
moving the binary. For more options see the official Ladybug installation
guide: https://docs.ladybugdb.com/installation/

## Principles and Architecture Decisions

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) records the properties any rewrite of kg
must preserve, what each one costs, and — deliberately — which current choices are
*not* principles and may be discarded freely. Read it before making a structural
change.

Individual design decisions are recorded in `decisions/` as lightweight ADRs. Key
decisions include storage and indexing (0001), data model (0002), CLI
grammar (0004), MCP server (0007), daemon (0008), agent integration
(0021), and LadybugDB index backend (0022).

## License

See [LICENSE](LICENSE) for details.
