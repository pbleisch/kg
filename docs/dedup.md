# Deduplication Guide

kg supports finding and merging duplicate entities using vector similarity
on embedding fields. Two complementary tools:

- **`kg dedup`** — find and merge duplicates in an existing graph
- **`kg apply --on-conflict dedup`** — deduplicate during manifest ingest

Both require [embeddings](embedding.md) to be configured and generated.

## Quick Start

```bash
# Find duplicates (dry-run by default — safe to explore)
kg dedup concept --threshold 0.85 --fields name,description

# Actually merge them
kg dedup concept --threshold 0.85 --fields name --apply

# Deduplicate during ingest
kg apply manifest.yaml --on-conflict dedup --dedup-fields name --dedup-threshold 0.85
```

## `kg dedup`

Scans entities of a given type and clusters duplicates by vector similarity.

### Usage

```bash
kg dedup <type> --threshold <0.0-1.0> --fields <field1,field2> [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--threshold` | 0.85 | Similarity threshold (entities above this are considered duplicates) |
| `--fields` | — | Comma-separated embedding fields to compare (required) |
| `--apply` | false | Merge duplicates (default is dry-run) |
| `--prefer-source` | false | Source attributes win on merge conflicts |
| `--cross-type` | false | Compare across entity types |
| `--json` | false | JSON output |

### How It Works

1. Loads all entities of the specified type
2. Extracts `<field>_embedding` vectors for each entity
3. Computes pairwise cosine similarity across all entity pairs
4. Groups duplicates into clusters using union-find (transitive: if A≈B and B≈C, all three cluster together)
5. Picks the canonical entity per cluster (lowest ID lexicographically)
6. With `--apply`: merges non-canonical entities into the canonical using `kg merge` semantics

### Examples

```bash
# Dry-run: see what would be merged
kg dedup concept --threshold 0.85 --fields name,description
# Output:
#   concept:game_feel_design → concept:game_feel (0.91, name)
#   concept:juiciness → concept:game_feel (0.87, description)
#   2 duplicates found, 1 clusters across 11063 entities

# Apply merges
kg dedup concept --threshold 0.85 --fields name --apply
# Output:
#   concept:game_feel_design → concept:game_feel (0.91, name)
#   1 duplicates merged into 1 clusters across 11063 entities

# JSON output for pipeline integration
kg dedup concept --threshold 0.90 --fields name --json

# Cross-type dedup (e.g., find concept/genre duplicates)
kg dedup concept --threshold 0.85 --fields name --cross-type
```

### Choosing a Threshold

| Threshold | Behavior |
|-----------|----------|
| 0.95+ | Very strict — only near-identical entities |
| 0.85–0.95 | Good default range — catches most duplicates |
| 0.75–0.85 | Aggressive — may merge loosely related entities |
| < 0.75 | Too aggressive for most use cases |

Always run without `--apply` first to review matches.

## `kg apply --on-conflict dedup`

Similarity-aware ingest: checks incoming entities against existing ones
before creating them.

### Usage

```bash
kg apply manifest.yaml --on-conflict dedup --dedup-fields <fields> [--dedup-threshold <0.0-1.0>]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--on-conflict dedup` | — | Enable similarity-aware conflict resolution |
| `--dedup-fields` | — | Embedding fields to compare (required with dedup) |
| `--dedup-threshold` | 0.85 | Similarity threshold |

### How It Works

For each `create` or `upsert` action in the manifest:

1. Extracts or computes embeddings for the new entity's dedup fields
2. Compares against all existing entities of the same type
3. If a match above threshold is found: merges the new entity into the existing one
4. If no match: creates normally
5. Rewrites any subsequent `link` actions in the same manifest to use the deduplicated ID

This lets LLM pipelines ship raw output directly to `kg apply` without a
separate resolve/dedup phase.

### Example

```yaml
# manifest.yaml
version: 1
actions:
  - action: upsert
    entity:
      id: concept:game_feel_design
      type: concept
      attributes:
        name: "Game Feel in Design"
        description: "The tactile quality of game interactions"
  - action: link
    from: concept:game_feel_design
    rel: related_to
    to: concept:player_experience
```

```bash
kg apply manifest.yaml --on-conflict dedup --dedup-fields name,description --dedup-threshold 0.85
```

If `concept:game_feel` already exists with similarity 0.91, the output:
```
  dedup: concept:game_feel_design → concept:game_feel
1 action(s) deduped
```

The link action is automatically rewritten to use `concept:game_feel` instead.

## Merge Semantics

Both tools use `kg merge` internally:

- Target (canonical) entity attributes are preserved; source attributes fill gaps
- Outgoing relations are unioned (no duplicates)
- All incoming links from other entities are rewritten to point to the target
- Source entity is deleted after merge
- Use `--prefer-source` to let the incoming entity's attributes win
