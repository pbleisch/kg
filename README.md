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

The `kg-ladybug` binary links against native LadybugDB libraries. Developers building locally should use the project downloader (below). Users who installed a prebuilt `kg-ladybug` release and see a runtime error like:

  dyld: Library not loaded: @rpath/liblbug.dylib

should install the native runtime libraries on their machine so the dynamic linker can find liblbug.

Developer build (fetch native libs):

```bash
# get the project-provided downloader and extract platform libs to ./lib-ladybug
curl -sL https://raw.githubusercontent.com/LadybugDB/go-ladybug/refs/heads/master/download_lbug.sh -o download_lbug.sh
chmod +x download_lbug.sh
# replace -platform with linux|darwin|windows as appropriate
./download_lbug.sh -out lib-ladybug -platform darwin
```

For end users (runtime installation) — preferred approaches by OS:

macOS (user-facing fixes)

Option A (recommended): install the dylib into a standard library path so dyld can find it:

```bash
# assuming you've run the downloader above
sudo mkdir -p /usr/local/lib
sudo cp lib-ladybug/lib/dynamic/darwin/liblbug.dylib /usr/local/lib/
sudo chmod 644 /usr/local/lib/liblbug.dylib
# if you installed to Homebrew prefix (Apple Silicon):
# sudo cp lib-ladybug/lib/dynamic/darwin/liblbug.dylib /opt/homebrew/lib/
```

Option B (works for packaged / single-binary installs): place the runtime next to the installed binary — the release binary looks in a few sibling locations (the uploader uses one of these in CI):

```bash
sudo mkdir -p /usr/local/bin/lib-ladybug
sudo cp lib-ladybug/lib/dynamic/darwin/liblbug.dylib /usr/local/bin/lib-ladybug/
```

Verification and troubleshooting on macOS:

```bash
# show what the binary references
otool -L /usr/local/bin/kg
# clear quarantine if macOS blocks the file
sudo xattr -r -d com.apple.quarantine /usr/local/bin/kg /usr/local/bin/lib-ladybug/liblbug.dylib || true
# ad-hoc codesign if Gatekeeper prevents loading
sudo codesign --force --sign - /usr/local/bin/kg /usr/local/bin/lib-ladybug/liblbug.dylib || true
```

Linux

```bash
# after running the downloader with -platform linux
sudo cp lib-ladybug/lib/dynamic/linux/liblbug.so /usr/local/lib/
sudo chmod 644 /usr/local/lib/liblbug.so
sudo ldconfig
# or, for a single-user install, copy the .so next to the binary and run:
# LD_LIBRARY_PATH=/path/to/lib-ladybug/lib/dynamic/linux /usr/local/bin/kg
# verify
ldd /usr/local/bin/kg | grep liblbug
```

Windows

```powershell
# after running the downloader (or obtaining the windows dll)
# place the DLL next to the kg executable or add its folder to PATH
Copy-Item .\lib-ladybug\lib\dynamic\windows\liblbug.dll -Destination "C:\Program Files\kg\"
# or next to kg.exe
# then run kg.exe normally
```

Notes and tips

- The CI release attaches the Ladybug binaries and arranges their native libs so the uploaded artifacts are runnable on the target runner; reproducing the CI layout locally with the downloader above is the fastest way to resolve missing-library errors.
- If you still get "Library not loaded: @rpath/liblbug.dylib", prefer Option A (install into /usr/local/lib or Homebrew lib) or Option B (place lib-ladybug next to the kg binary) above — both match paths the release binary will search.
- If platform protections (Gatekeeper / codesign) block loading on macOS, removing quarantine attributes and ad-hoc codesigning (examples above) typically resolves it for local installs.
- For support, include the full dyld or ldd output and the path where you placed liblbug in your issue so we can suggest the correct placement.

## Architecture Decisions

Design decisions are recorded in `decisions/` as lightweight ADRs. Key
decisions include storage and indexing (0001), data model (0002), CLI
grammar (0004), MCP server (0007), daemon (0008), agent integration
(0021), and LadybugDB index backend (0022).

## License

See [LICENSE](LICENSE) for details.
