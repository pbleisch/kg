# kg — Knowledge Graph CLI

A Git-friendly knowledge graph stored as YAML entities. Reading it needs
nothing running — no daemon, no index, no database. Designed for humans and AI
agents alike.

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
curl -fsSL "https://github.com/pbleisch/kg/releases/download/${VER}/kg_${VER#v}_${OS}_${ARCH}.tar.gz" \
  | sudo tar xzf - -C /usr/local/bin kg
```

The archive holds one static binary, plus the LICENSE and README; the command
above extracts only `kg`. kg is pure Go: no native library, no runtime
dependency, nothing to keep beside it.

### Windows

```powershell
$ver = (Invoke-RestMethod https://api.github.com/repos/pbleisch/kg/releases/latest).tag_name
Invoke-WebRequest "https://github.com/pbleisch/kg/releases/download/$ver/kg_$($ver.TrimStart('v'))_windows_amd64.zip" `
  -OutFile kg.zip
Expand-Archive kg.zip . -Force; Remove-Item kg.zip
# Move kg.exe somewhere on your PATH
```

### From source

Go 1.25 or newer. No CGO, no C compiler, nothing to download:

```bash
git clone https://github.com/pbleisch/kg.git
cd kg
mage build          # builds to bin/kg
mage install        # installs kg to $GOPATH/bin
```

## Commands

| Command | Description |
|---------|-------------|
| `kg init` | Scaffold a new `.kg/` directory |
| `kg create <type> <id> [attrs...]` | Create an entity |
| `kg get <type:id>` | Get an entity by ID (strips embeddings; `--include-embeddings` to keep) |
| `kg find <type> [filters...]` | Find entities by type (`--limit`, `--offset`, `--include-embeddings`, `--include-relations`) |
| `kg similar <type> --field <f> --vector\|--like\|--vector-file` | Find similar entities by vector (strips embeddings; `--include-embeddings` to keep) |
| `kg set <type:id> key=value` | Set attributes |
| `kg unset <type:id> <attr...>` | Remove attributes, including an item no contributor will replace |
| `kg delete <type:id>` | Delete an entity |
| `kg link <from> <rel> <to>` | Create a relation |
| `kg unlink <from> <rel> <to>` | Remove a relation |
| `kg path <id> -> <rel>` | Traverse forward relations |
| `kg back <id>` | Find reverse references (strips embeddings; `--include-embeddings` to keep) |
| `kg types` | List schema types |
| `kg describe <type>` | Show type schema |
| `kg apply <manifest>` | The write boundary: a batch of typed records under one idempotency key |
| `kg merge <source> <target>` | Merge two entities, through that same boundary |
| `kg resolve` | Report every pair in the corpus that may be one entity (merges nothing) |
| `kg stats` | Graph metrics |
| `kg validate` | Check the graph: structure fails, content warns |
| `kg validate --fix` | Repair `id:`/`type:` to agree with the path, drop illegal attribute names |
| `kg sync [entity-id...]` | Rebuild derived data — the vector sidecar (`--init`, `--show`, `--rebuild`, `--dry-run`, `--no-embed`) |
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

## Storage Model

- **Source of truth:** YAML files in `.kg/entities/<type>/<id>.yaml`
- **Schemas:** YAML files in `.kg/schema/<type>.yaml`
- **Derived data:** `.kg/.cache/` — gitignored, rebuilt by `kg sync`
- **Config:** `.kg/config.yaml` (travels with the graph)
- **Embedder:** `.kg/embedder.yaml` (gitignored, per checkout)

The YAML files are committed to Git. Everything under `.kg/.cache/` is
recomputable from them and nothing else is.

### Reading needs nothing running

There is no index and no daemon. A read opens files:

- a single-entity read is one file — about 3 ms, whatever the corpus size;
- a typed find reads one directory;
- only the reads that genuinely need every entity — `kg back`, `kg stats`,
  `kg validate` — walk the whole graph, about 0.8 s over 27,000 entities.

[decisions/0026](decisions/0026-read-budget-and-scale-ceiling.md) states the
latency budget and the corpus size it holds for, with the measurements behind
both. [decisions/0027](decisions/0027-delete-index-and-daemon.md) records why
the LadybugDB index and the daemon were deleted.

### Identity is where the file sits

A file at `.kg/entities/<type>/<local-id>.yaml` **is** `<type>:<local-id>`. The
`id:` and `type:` lines inside stay, so a file read on its own still says what
it is, but they are a checked echo, not the source — when they disagree with the
path the graph does not load. See
[decisions/0029](decisions/0029-path-derived-identity.md).

### Structure is enforced; content is advised

`kg validate` reports both and treats them differently. **Structure fails**, and
no flag suppresses it: an identity two files claim (named, both of them), a
declared `id:` or `type:` that disagrees with the file's location, an attribute
name outside `[a-z][a-z0-9_]*`, a file that will not parse. **Content warns**,
and the graph still loads: an unknown attribute, a schema violation, a type with
no schema, a relation target that names nothing.

```bash
kg validate          # every finding, both kinds; exit 1 only on structure
kg validate --fix    # correct id:/type: to the path, drop illegal names
```

`--fix` never moves a file. Where two files claim one identity it reports and
refuses to choose, because choosing which one is the entity is not kg's call.
[decisions/0028](decisions/0028-load-time-structural-enforcement.md) states the
three classes and why a dangling relation target is refused by the *write
boundary* but only reported by a *load*.

### Embeddings

kg generates vector embeddings for entity attributes by running a command you
configure, and searches them with `kg similar`. kg never chooses, hosts or calls
a model itself.

```bash
kg sync --init                           # scaffold a script and .kg/embedder.yaml
pip install -r .kg/scripts/requirements.txt
kg sync --show                           # what kg resolved, and from where
kg sync                                  # build the vector sidecar
kg similar person --field name --like person:alice
```

The vectors go to `.kg/.cache/vectors.bin` with an id-to-row index beside it,
never into the entity files. Changing the model changes not one entity file.

The embedding command lives in `.kg/embedder.yaml` (gitignored, per checkout,
absolute paths) or in `KG_EMBED_COMMAND`. It does not live in `.kg/config.yaml`,
because that file travels with the graph and an interpreter path does not —
see [decisions/0025](decisions/0025-embedder-configuration.md).

Migrating a corpus written before vectors moved out:

```bash
kg sync --migrate-vectors    # harvest *_embedding attributes into the sidecar
```

See [docs/embedding.md](docs/embedding.md) for the full guide.

## Agent Integration

`kg agent` provides onboarding and discovery for AI coding agents:

```bash
kg agent status      # Graph state: entity counts, schema types, vector sidecar
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
mage lint        # go vet + gofmt check
mage check       # lint + tests + integration tests
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

The release produces one static `kg` binary per platform, cross-compiled from a
single job. There is no native library to bundle.

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
mage build           # compile to bin/kg
mage test            # run tests
mage check           # lint + tests + integration tests
```

Requires Go 1.25+. No CGO.

## Principles and Architecture Decisions

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) records the properties any rewrite of kg
must preserve, what each one costs, and — deliberately — which current choices are
*not* principles and may be discarded freely. Read it before making a structural
change.

Individual design decisions are recorded in `decisions/` as lightweight ADRs. Key
decisions include storage (0001), data model (0002), CLI grammar (0004), MCP
server (0007), agent integration (0021), where the embedding command is
configured (0025), the read budget and scale ceiling (0026, **unratified**), and
the deletion of the index and the daemon (0027, superseding 0008 and 0022).

## License

See [LICENSE](LICENSE) for details.
