# kg — Local Knowledge Graph for Agents

A git-friendly knowledge graph system designed for LLM agents and CLI workflows. Store entities and relationships as YAML files, read them with nothing running, and get token-efficient TOON output optimized for agent consumption.

## Design Goals

- **Git-friendly**: Source of truth is human-readable YAML files that diff and merge cleanly
- **Agent-optimized**: Default output in TOON format minimizes tokens while maximizing LLM comprehension
- **No service required**: a read opens files — no daemon, no index, no database. A single-entity read is ~3 ms whatever the corpus size; see [decisions/0026](../decisions/0026-read-budget-and-scale-ceiling.md)
- **Simple mental model**: Entities have types, attributes, and relations to other entities
- **Files as API**: Schemas and entities are just YAML files—agents can read and write them directly
- **Two modes**: CLI for direct use, MCP for agent integration

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        kg CLI / MCP                         │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│   COMMANDS (writes)                 QUERIES (reads)         │
│   ─────────────────                 ─────────────────       │
│   kg create ...                     kg get ...              │
│   kg set ...                        kg find ...             │
│   kg delete ...                     kg path ...             │
│   kg link ...                       kg back ...             │
│   kg unlink ...                     kg types / describe     │
│         │                                 │                 │
│         ▼                                 ▼                 │
│   ┌───────────┐                    ┌───────────┐           │
│   │   YAML    │◀─── read directly ─│  the same │           │
│   │  (truth)  │                    │   files   │           │
│   └─────┬─────┘                    └───────────┘           │
│         │                                                   │
│         │  kg sync                  kg similar              │
│         ▼                                 │                 │
│   ┌──────────────────┐                    │                 │
│   │ .kg/.cache/      │◀───────────────────┘                 │
│   │ vectors.bin/.idx │  (derived, gitignored)               │
│   └──────────────────┘                                      │
└─────────────────────────────────────────────────────────────┘
```

### Storage Model

The YAML files are the graph. Reads open them: one file for a point read, one directory
for a typed find, and a whole-graph walk only for the reads that need every entity
(`back`, `stats`, `validate`).

The one derived artifact is the vector sidecar, which `kg similar` scans and `kg sync`
rebuilds. Delete `.kg/.cache/` and `git status` stays clean.

```
.kg/
├── entities/
│   ├── person/
│   │   ├── alice.yaml
│   │   └── bob.yaml
│   └── project/
│       └── atlas.yaml
├── schema/
│   ├── person.yaml
│   └── project.yaml
├── morphisms.yaml             # reusable path patterns
├── config.yaml                # travels with the graph
├── embedder.yaml              # gitignored, per checkout — names the embedding command
└── .cache/                    # gitignored, derived, rebuilt by `kg sync`
    ├── vectors.bin            # packed float64 rows
    ├── vectors.idx            # id→row, entity type, source-text checksum
    └── apply/<key>.json       # batch idempotency ledger
```

### Entity File Format

```yaml
# .kg/entities/person/alice.yaml
id: person:alice
type: person
attributes:
  name: Alice Chen
  role: Engineer
  email: alice@example.com
relations:
  worksOn:
    - project:atlas
  reportsTo:
    - person:bob
```

### Schema File Format

Schemas provide structure hints for agents and enable validation/completion. They are guidance, not enforcement.

```yaml
# .kg/schema/person.yaml
type: person
description: A person in the organization
attributes:
  name:
    type: string
    required: true
  role:
    type: string
  email:
    type: string
relations:
  worksOn:
    target: project
    cardinality: many
  reportsTo:
    target: person
    cardinality: one
  memberOf:
    target: team
    cardinality: many
```

### Schema Management

**Schemas are just YAML files.** There are no special commands for schema management—the filesystem is the API.

| Action | Method |
|--------|--------|
| Create type | Write `.kg/schema/<type>.yaml` |
| Modify type | Edit the YAML file |
| Delete type | Delete the file |
| List types | `kg types` |
| View type | `kg describe <type>` |

This means agents with filesystem access can evolve the schema by reading an existing schema file, understanding the pattern, and creating new ones:

```bash
# Agent reads existing schema to learn the format
$ cat .kg/schema/person.yaml

# Agent creates a new schema file
$ cat > .kg/schema/vehicle.yaml << 'EOF'
type: vehicle
description: A vehicle in the fleet
attributes:
  make:
    type: string
    required: true
  model:
    type: string
  year:
    type: number
  vin:
    type: string
relations:
  assignedTo:
    target: person
    cardinality: one
  maintainedBy:
    target: team
    cardinality: one
EOF

# Next kg command picks up the new schema
$ kg describe vehicle
```

Schema changes are automatically picked up on the next `kg` operation (or explicitly via `kg sync`).

## CLI Reference

### Query Commands (read-only)

All queries read from the local index. Default output is TOON format.

#### `kg get <type:id>`

Retrieve a single entity with all attributes and relations.

```bash
$ kg get person:alice
```
```toon
id: person:alice
type: person
name: Alice Chen
role: Engineer
worksOn[1]: project:atlas
reportsTo[1]: person:bob
```

#### `kg find <type> [where <attr><op><val>, ...]`

Search for entities by type and optional attribute filters.

```bash
$ kg find person where role=Engineer
```
```toon
results[3]{id,name,role}:
  person:alice,Alice Chen,Engineer
  person:dave,Dave Park,Engineer
  person:eve,Eve Wilson,Engineer
count: 3
```

```bash
$ kg find person where salary>=100000, role~=Engineer
```
```toon
results[2]{id,name,role,salary}:
  person:alice,Alice Chen,Senior Engineer,150000
  person:bob,Bob Smith,Staff Engineer,180000
count: 2
```

Supported operators:

| Operator | Meaning |
|----------|---------|
| `=` | Equals |
| `!=` | Not equals |
| `~` | Contains (substring) |
| `>` | Greater than |
| `<` | Less than |
| `>=` | Greater than or equal |
| `<=` | Less than or equal |

#### `kg path <id> -> <rel> [-> <rel> ...]`

Traverse relations forward from an entity.

```bash
$ kg path person:alice -> reportsTo -> worksOn
```
```toon
from: person:alice
path[2]: reportsTo,worksOn
results[2]: project:atlas,project:beacon
```

**Recursive traversal** with `->*` follows a relation transitively until no more matches:

```bash
$ kg path person:alice ->* reportsTo
```
```toon
from: person:alice
path[1]: reportsTo*
results[3]: person:bob,person:carol,person:ceo
```

This is useful for hierarchies (org charts, dependencies, taxonomies).

**Verbose output** with `--verbose` shows the full traversal with intermediate nodes:

```bash
$ kg path person:alice -> reportsTo -> worksOn --verbose
```
```toon
steps[3]{depth,via,id,name}:
  0,,person:alice,Alice Chen
  1,reportsTo,person:bob,Bob Smith
  2,worksOn,project:atlas,Atlas
  2,worksOn,project:beacon,Beacon
```

#### `kg back <id> [via <rel>]`

Find entities that point to this entity (reverse lookup).

```bash
$ kg back project:atlas via worksOn
```
```toon
results[3]{id,name,type}:
  person:alice,Alice Chen,person
  person:bob,Bob Smith,person
  person:charlie,Charlie Davis,person
count: 3
```

Supports recursive reverse traversal with `<-*`:

```bash
$ kg path project:atlas <-* dependsOn
```
```toon
from: project:atlas
path[1]: dependsOn*
results[5]: project:api,project:web,project:mobile,project:sdk,project:cli
```

#### `kg morph <name> [= <path>]`

Define or use reusable path patterns (morphisms).

**Define a morphism:**

```bash
$ kg morph teammates = -> worksOn <- worksOn
```
```toon
created: teammates
pattern: -> worksOn <- worksOn
```

**List morphisms:**

```bash
$ kg morph
```
```toon
morphisms[3]{name,pattern}:
  teammates,-> worksOn <- worksOn
  management,->* reportsTo
  dependencies,<-* dependsOn
```

**Use a morphism in a path:**

```bash
$ kg path person:alice @teammates
```
```toon
from: person:alice
path[1]: @teammates
expanded: -> worksOn <- worksOn
results[4]: person:bob,person:charlie,person:dave,person:eve
```

Morphisms are stored in `.kg/morphisms.yaml` and can be version-controlled.

```yaml
# .kg/morphisms.yaml
teammates:
  pattern: -> worksOn <- worksOn
  description: People who work on the same projects
management:
  pattern: ->* reportsTo
  description: Full management chain
dependencies:
  pattern: <-* dependsOn
  description: All transitive dependents
```

#### `kg types`

List all entity types defined in the schema.

```bash
$ kg types
```
```toon
types[3]: person,project,team
```

#### `kg describe <type>`

Show schema details for a type.

```bash
$ kg describe person
```
```toon
type: person
description: A person in the organization
attributes[3]{name,type,required}:
  name,string,true
  role,string,false
  email,string,false
relations[3]{name,target,cardinality}:
  worksOn,project,many
  reportsTo,person,one
  memberOf,team,many
```

### Mutation Commands (writes)

All mutations write to YAML files first, then sync to the index.

#### `kg create <type> <id> [attr=val ...]`

Create a new entity.

```bash
$ kg create person charlie name="Charlie Davis" role="Designer"
```
```toon
created: person:charlie
```

#### `kg set <type:id> <attr>=<val> [, ...]`

Update attributes on an existing entity.

```bash
$ kg set person:alice role="Senior Engineer", email="alice@newco.com"
```
```toon
updated: person:alice
changed[2]: role,email
```

#### `kg delete <type:id>`

Delete an entity and its relations.

```bash
$ kg delete person:charlie
```
```toon
deleted: person:charlie
relationsRemoved: 2
```

#### `kg link <type:id> <relation> <type:id>`

Create a relation between two entities.

```bash
$ kg link person:charlie worksOn project:atlas
```
```toon
linked: person:charlie -> worksOn -> project:atlas
```

#### `kg unlink <type:id> <relation> <type:id>`

Remove a relation between two entities.

```bash
$ kg unlink person:charlie worksOn project:atlas
```
```toon
unlinked: person:charlie -> worksOn -> project:atlas
```

### Maintenance Commands

#### `kg sync`

Rebuild the index from YAML files. Run after `git pull` or manual edits.

```bash
$ kg sync
```
```toon
synced: true
entities: 47
relations: 123
duration: 45ms
```

#### `kg stats`

Show knowledge graph statistics (entity/relation counts by type).

```bash
$ kg stats
```

#### `kg similar <type:id> [--limit N]`

Find entities similar to the given entity using vector embeddings.

```bash
$ kg similar person:alice --limit 5
```

#### `kg dedup <type>`

Find potential duplicates within a type using embedding similarity.

```bash
$ kg dedup person
```

#### `kg embed [type]`

Generate or regenerate embeddings for entities.

```bash
$ kg embed person     # embed all person entities
$ kg embed            # embed all entities
```

#### `kg validate`

Check YAML files against schemas.

```bash
$ kg validate
```
```toon
valid: true
entities: 47
warnings[1]: person:dave missing recommended attribute 'email'
```

### Output Formats

```bash
kg get person:alice              # TOON (default, agent-optimized)
kg get person:alice --json       # JSON (for jq, scripts)
kg get person:alice --human      # Human-readable formatted text
```

### Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |
| `--human` | Output as human-readable text |
| `--dir <path>` | Use knowledge graph at path (default: `.kg`) |
| `--quiet` | Suppress non-essential output |
| `--dry-run` | Show what would happen without making changes |
| `--include-embeddings` | Include embedding vectors in output (stripped by default) |
| `--include-relations` | Include relations in find results |
| `--limit N` | Limit number of results |
| `--offset N` | Skip first N results |

## Output Format: TOON

Default output uses TOON (Token-Oriented Object Notation) for minimal token usage with high LLM comprehension.

### Why TOON?

- ~40% fewer tokens than JSON on typical knowledge graph queries
- Tabular format for uniform arrays (query results)
- YAML-like nested format for single entities
- Explicit array lengths help LLMs track structure
- Lossless conversion to/from JSON

### Format Examples

**Single entity (nested):**
```toon
id: person:alice
type: person
name: Alice Chen
role: Engineer
```

**Query results (tabular):**
```toon
results[3]{id,name,role}:
  person:alice,Alice Chen,Engineer
  person:bob,Bob Smith,Manager
  person:charlie,Charlie Davis,Designer
count: 3
```

**Errors:**
```toon
error: entity_not_found
message: No entity with id 'person:notexist'
suggestions[2]: person:alice,person:bob
```

## Operational Modes

### 1. CLI Mode (default)

Direct command-line usage for humans and agents.

```bash
$ kg find person where role=Engineer
```

### 2. MCP Mode (planned)

Model Context Protocol server for structured agent integration.

```bash
$ kg mcp serve
```

Exposes tools:
- `kg_get` — retrieve entity
- `kg_find` — search entities
- `kg_path` — traverse relations
- `kg_back` — reverse lookup
- `kg_create` — create entity
- `kg_set` — update entity
- `kg_link` — create relation
- `kg_unlink` — remove relation

### 3. Library

`pkg/store`, `pkg/query`, `pkg/batch` and `pkg/vector` are the same operations the CLI
composes, linkable from any Go program. The CLI adds presentation and nothing else (P7).

There is no daemon. There used to be one, holding a LadybugDB index open over `net/rpc`;
it existed only because opening that index cost tens of seconds, and the index was
expensive only because 97.55% of the entity store was embedding floats. Moving the
vectors to the sidecar took the first cold read from 783.8 s to 3 ms and left the daemon
nothing to amortize. See
[decisions/0027](../decisions/0027-delete-index-and-daemon.md).

## Agent Skill Description

For MCP tool descriptions or system prompts:

```markdown
## Knowledge Graph (kg)

Query and manage a local knowledge graph of entities and relationships.

### Query Commands (read-only)
- `kg get <type:id>` — Get entity with attributes and relations
- `kg find <type> [where <attr><op><val>]` — Search entities by type/attributes
  - Operators: `=`, `!=`, `~` (contains), `>`, `<`, `>=`, `<=`
- `kg path <id> -> <rel> [-> <rel>...]` — Traverse relations forward
  - Use `->*` for recursive traversal (e.g., `->* reportsTo` for full chain)
  - Use `--verbose` to see intermediate nodes
- `kg back <id> [via <rel>]` — Find entities pointing here (reverse)
  - Use `<-*` for recursive reverse traversal
- `kg types` — List all entity types
- `kg describe <type>` — Show type schema
- `kg morph` — List reusable path patterns
- `kg morph <name> = <pattern>` — Define a morphism

### Mutation Commands (writes)
- `kg create <type> <id> [attr=val...]` — Create entity
- `kg set <id> attr=val [, ...]` — Update attributes
- `kg delete <id>` — Delete entity
- `kg link <id> <rel> <target_id>` — Create relation
- `kg unlink <id> <rel> <target_id>` — Remove relation

### Schema Management
Schemas are YAML files in `.kg/schema/`. To create a new type:
1. Read an existing schema to learn the format: `cat .kg/schema/person.yaml`
2. Create a new file following the same pattern: `.kg/schema/<typename>.yaml`
3. Run `kg sync` or any kg command to pick up changes

Schema file format:
- `type`: snake_case type name (must match filename)
- `description`: Human-readable description  
- `attributes`: Map of name → {type, required?}
  - type: string | number | boolean | date | datetime
- `relations`: Map of name → {target, cardinality}
  - cardinality: one | many

### Output
Default: TOON format (token-efficient). Use `--json` for JSON output.

### Examples
- Find highly-paid engineers: `kg find person where role~=Engineer, salary>=100000`
- Who works on Atlas: `kg back project:atlas via worksOn`
- Alice's full management chain: `kg path person:alice ->* reportsTo`
- Alice's teammates: `kg path person:alice @teammates` (using morphism)
- Detailed traversal: `kg path person:alice -> reportsTo -> worksOn --verbose`
```

## Data Model

### Entity IDs

Format: `<type>:<localId>` (lowercase type)

Examples: `person:alice`, `project:atlas`, `team:platform`

### Attribute Types

| Type | Description |
|------|-------------|
| `string` | Text value |
| `number` | Integer or float |
| `boolean` | true/false |
| `date` | ISO 8601 date |
| `datetime` | ISO 8601 datetime |

### Relation Cardinality

| Cardinality | Description |
|-------------|-------------|
| `one` | At most one target |
| `many` | Zero or more targets |

## Derived Data

The entity files are the whole graph; nothing is kept in sync with them. The one derived
artifact is the vector sidecar, which exists so `kg similar` has something to search.

### The sidecar

```
.kg/.cache/vectors.bin    packed rows, `dim` float64s each, row i at byte offset i*dim*8
.kg/.cache/vectors.idx    JSON: dim, row count, host byte order, embedder digest,
                          and one entry per (entity, field) — row, entity type, and the
                          SHA-256 of the text it was built from
```

The index doubles as the staleness record: text whose checksum still matches is carried
across a rebuild untouched, so `kg sync` only pays the embedder for what changed. It also
records a digest of the embedder — the command plus the contents of any script it names —
so changing the model invalidates vectors whose text has not changed.

### Rebuild triggers

| Trigger | Command | Use case |
|---------|---------|----------|
| Incremental | `kg sync` | after editing entities, after `git pull` |
| Full | `kg sync --rebuild` | after changing embedders, or to recover a damaged sidecar |
| Report only | `kg sync --dry-run` | see what would be embedded |
| No vectors wanted | `kg sync --no-embed` | this graph has none, deliberately |
| One-way migration | `kg sync --migrate-vectors` | harvest `*_embedding` attributes out of a pre-sidecar corpus |

A graph with no embedder configured has no derived data, and `kg sync` exits non-zero
saying so rather than doing nothing quietly.

## Implementation Notes

### Go Package Structure

```
kg/
├── cmd/
│   └── kg/
│       └── main.go           # CLI entry point
├── pkg/
│   ├── store/
│   │   ├── yaml.go           # YAML file operations, path-aware walks
│   │   ├── graph.go          # whole-corpus in-memory graph (reverse edges, stats)
│   │   ├── embedder_config.go # resolving the operator's embedding command
│   │   ├── embed.go          # embedding, and the migration off in-file vectors
│   │   └── sync.go           # rebuilding the derived data
│   ├── query/
│   │   ├── get.go
│   │   ├── find.go
│   │   ├── path.go
│   │   └── back.go
│   ├── command/
│   │   ├── create.go
│   │   ├── set.go
│   │   ├── delete.go
│   │   ├── link.go
│   │   └── unlink.go
│   ├── vector/
│   │   ├── store.go          # mmapped sidecar, brute-force search
│   │   ├── writer.go         # atomic whole-file writes
│   │   └── index.go          # vectors.idx format
│   ├── batch/                # the write boundary
│   ├── schema/
│   │   └── schema.go         # Schema loading/validation
│   └── output/
│       ├── toon.go           # TOON formatting
│       ├── json.go           # JSON formatting
│       └── human.go          # Human-readable formatting
├── internal/
│   ├── mcp/                  # MCP server
│   └── watch/                # filesystem event stream
└── go.mod
```

### Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/fsnotify/fsnotify` | `kg watch` |
| `golang.org/x/term` | TTY detection for output format |
| `gopkg.in/yaml.v3` | YAML parsing |

No CGO, no database, no native library. The whole binary is static.

## Design Space: Labeled Facts

*Written when kg had a property-graph index that could carry edge properties. That index
is gone (ADR-0027), so the storage question below is now open rather than answered: a
labeled fact would need a representation in the YAML, not in a database. The design
space is unchanged; the implementation sketch is not.*

Each fact could carry additional context (provenance, confidence, temporal markers)
alongside the relationship, in the manner of RDF quads.

### What Quads Enable

A triple: `(subject, predicate, object)`
A quad: `(subject, predicate, object, label)`

The label contextualizes the fact without changing it.

### Potential Use Cases

| Label Pattern | Purpose | Example |
|---------------|---------|---------|
| `source:*` | Provenance | Where did this fact come from? |
| `confidence:*` | Certainty | How sure are we? |
| `asof:*` | Temporal | When was this true? |
| `view:*` | Perspectives | Different interpretations |
| `tenant:*` | Multi-tenancy | Separate knowledge spaces |

### Example: Provenance Tracking

```yaml
relations:
  worksOn:
    - target: project:atlas
      label: source:hr-system
    - target: project:beacon
      label: source:inferred
```

Internally stored as:
```
(person:alice, worksOn, project:atlas, source:hr-system)
(person:alice, worksOn, project:beacon, source:inferred)
```

Query possibilities:
```bash
# Only facts from HR system
kg find person where worksOn=project:atlas --label source:hr-system

# Exclude inferred facts
kg find person --exclude-label source:inferred
```

### Example: Temporal Snapshots

```
(person:alice, role, Engineer, asof:2023-01-01)
(person:alice, role, Senior Engineer, asof:2024-01-01)
```

Query: "What was Alice's role in 2023?"

### Example: Confidence Levels

```
(person:alice, worksOn, project:atlas, confidence:high)
(person:alice, worksOn, project:secret, confidence:low)
```

Query: "Only high-confidence facts"

### Current Decision: Deferred

We're deferring quad/label support until we have clearer use cases from actual usage. The YAML format and index can be extended later without breaking changes.

Questions to resolve:
- How should labels appear in YAML files?
- Should labels be queryable via `--label` flags, or a richer syntax?
- Do we need label hierarchies (`source:hr:employee-db`)?
- How do labels interact with morphisms and recursive traversal?

If you have a use case that needs quads, please document it.

## Prior Art & Inspiration

- **RDF/SPARQL**: Triple-store data model (simplified)
- **Obsidian**: Local-first, markdown files as source of truth
- **Logseq**: Graph-based knowledge management
- **LadybugDB**: Embedded property graph database (formerly Kuzu)
- **TOON**: Token-efficient serialization for LLMs
