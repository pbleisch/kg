# Entity Resolution and Deduplication Guide

Finding entities that are the same thing, and merging the ones that are. Three
commands, and the order matters:

- **`kg resolve`** — one whole-corpus pass over a complete index, reporting every
  pair that may be one entity. It merges nothing.
- **`kg dedup`** — cluster one type by similarity and, with `--apply`, merge.
- **`kg merge`** — fold one named entity into another.

All three need [embeddings](embedding.md) configured and a sidecar built by
`kg sync`.

**`kg apply --on-conflict dedup` is gone.** Resolution used to happen inside the
write boundary, once per batch, against whatever the vector sidecar held — which
was the state *before* the batch, because `kg sync` ran after the apply. It was
therefore permanently one document behind, it ran the embedding command on the
critical path of every write, and it merged two entities on a threshold with
nobody looking. Resolution is a separate pass now. A pipeline that passed
`--on-conflict dedup`, `--dedup-fields` or `--dedup-threshold` drops them and
runs `kg sync && kg resolve` after ingest instead.

## Quick Start

```bash
# Every pair in the whole corpus that may be one entity. Reports; merges nothing.
kg resolve

# The closest 50 above 0.95, as JSON, for an agent to adjudicate
kg resolve --threshold 0.95 --limit 50 --json

# Only pairs whose types differ — the ones a per-type dedup can never see
kg resolve --cross-type

# Cluster and merge within one type (dry-run by default)
kg dedup concept --threshold 0.85 --fields name,description
kg dedup concept --threshold 0.85 --fields name --apply
```

## `kg resolve`

One pass over the whole corpus, comparing every embedded field against every
other entity's *same* field, and reporting the pairs at or above a threshold.

### Usage

```bash
kg resolve [--threshold <0.0-1.0>] [--fields <f1,f2>] [--limit <n>]
           [--same-type | --cross-type]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--threshold` | 0.90 | Cosine score at or above which a pair is a candidate |
| `--fields` | the embedder's | Comma-separated embedded fields to compare |
| `--limit` | 0 (all) | Report at most this many pairs, highest score first |
| `--same-type` | — | Only pairs whose types agree |
| `--cross-type` | — | Only pairs whose types differ |

### What it refuses

`kg resolve` answers from a complete index or not at all, which is the whole
point of moving it out of the apply:

- **A corpus that does not load** is refused by the load, before any comparison.
  An identity two files claim has one vector row for two entities, and resolving
  against that is resolving against a corpus that is not the one on disk. Run
  `kg validate` and `kg validate --fix`.
- **An entity with text and no vector** is counted and named, and the pass
  refuses rather than quietly comparing the rest. Run `kg sync`.

### What it will not do

It does not merge. Deciding that two names are one thing is judgement, and a
threshold is not judgement — the corpus has `concept:match_three` and
`genre:match_three` scoring 1.0000, and whether those are one entity is a
question about the graph's ontology, not about cosine. The output is the list a
person or an agent adjudicates; `kg merge` applies what they decide.

### Cost

Exact, all-pairs, brute force, in pure Go — there is no CGO in kg and no
approximate index to keep in sync (P2, ADR-0027). Measured on the 27,158-file
GDKB corpus, one embedded field, 18 cores:

| | |
|---|---|
| whole-corpus load | 0.85 s |
| normalize and pack 27,079 vectors of 384 | 0.02 s |
| all-pairs cosine, 366M comparisons | 2.6 s |
| **end to end** | **3.9 s** |

It found 862 pairs at cosine ≥ 0.90 — 464 same-type and 398 cross-type. The
cross-type ones were never in the reach of a per-type `kg dedup` at all.

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
2. Reads each entity's vector for that field from the sidecar (`kg sync` builds it)
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

## `kg apply --on-conflict dedup` — removed

`kg apply` has no dedup mode, and `--dedup-fields` and `--dedup-threshold` are
gone with it. The write boundary writes; it does not decide that two entities
are one.

What it did, and why each part was a problem:

1. It embedded the incoming entities at the boundary — a subprocess on the
   critical path of every write, and 67.6 hours of the original corpus build.
2. It compared them against the vector sidecar *as it stood before the batch*,
   because `kg sync` ran after the apply. Every run was one document behind, and
   the pairs it could not see were the ones it had just created.
3. It merged on a threshold, silently, and rewrote later `link` actions in the
   same manifest to point at whatever it merged into.

**What to run instead.** Ingest, then resolve:

```bash
kg apply manifest.yaml            # writes; refuses a dangling relation as before
kg sync                           # brings the sidecar level
kg resolve --json > pairs.json    # every candidate, over the complete index
```

and adjudicate `pairs.json`, applying the decisions with `kg merge`.


## Merge Semantics

`kg dedup --apply` uses `kg merge` internally, and it is what you run by hand on
a pair `kg resolve` reported and a human adjudicated:

- Target (canonical) entity attributes are preserved; source attributes fill gaps
- Outgoing relations are unioned (no duplicates)
- All incoming links from other entities are rewritten to point to the target
- Source entity is deleted after merge
- Use `--prefer-source` to let the incoming entity's attributes win
