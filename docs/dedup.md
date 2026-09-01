# Entity Resolution and Deduplication Guide

Finding entities that are the same thing, and merging the ones that are. Two
commands, a human decision between them, and the order matters:

- **`kg resolve`** — one whole-corpus pass over a complete index, reporting every
  pair that may be one entity. It merges nothing.
- **`kg merge`** — fold one named entity into another, through the write
  boundary.

`kg resolve` needs [embeddings](embedding.md) configured and a sidecar built by
`kg sync`; `kg merge` needs neither, because by then the decision is made.

**`kg dedup` is gone.** It clustered one type by similarity and, with
`--apply`, merged — which is resolution and merging fused into one command, with
nobody looking in between. It reported "no duplicates found" and exited 0 on a
corpus where it had compared nothing, because a missing sidecar was a warning on
stderr rather than a refusal; `kg resolve` was built to refuse instead. It also
merged in a loop outside `pkg/batch`, walking the whole corpus once per
duplicate. Run `kg resolve`, adjudicate, then `kg merge`.

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

# Fold one adjudicated pair into one entity
kg merge concept:game_feel_design concept:game_feel --dry-run
kg merge concept:game_feel_design concept:game_feel
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
cross-type ones were never in the reach of the per-type `kg dedup` that used
to be here at all.

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

`kg merge` is what you run by hand on a pair `kg resolve` reported and a human
adjudicated. It is the one merge rule, stated once in `pkg/merge` and used by
both verbs that combine two claims (ADR-0030):

- A **scalar** takes the incoming (source) value; overwriting a *different* one
  is reported as a warning.
- An attribute the schema declares **`type: list`** keeps both accounts: the two
  item sets concatenate, deduplicated on (source, text). Two entities being
  merged genuinely were written by different sources and both are true.
- **Relations** union, outgoing and incoming alike: every link pointing at the
  source is rewritten to point at the target.
- The source entity is deleted last.
- `--prefer target` keeps the target's scalars instead. It applies to scalars
  only — a list keeps both sides and a relation unions, so neither has a side to
  prefer. The default flipped with ADR-0030; it used to be target-wins, which
  made `kg merge` and `kg apply` give opposite answers to the same question.
- `--cross-type` allows two ends that disagree about type. Without it the merge
  is refused, which is what catches an interactive typo.

It goes through `batch.Apply`, so it carries an envelope, an idempotency key and
a journal: a merge that dies halfway is rolled back rather than leaving half the
referrers pointing at an entity that no longer exists. `kg apply` reaches the
same code with an `action: merge` record.
