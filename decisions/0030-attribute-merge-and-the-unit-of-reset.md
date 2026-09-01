# 0030 — What Upsert Does When Two Records Claim One Identity

**AMENDED 2026-09-01 by [0031](0031-cli-surface-reduction.md).** Two callers named below
are gone. `kg dedup` was deleted, so `command.Merge` is now reached only by `kg merge`
and the `merge` op — and `kg merge` reaches it *through* `batch.Apply`, so the one merge
rule this ADR states is also the one write path. `kg import` was deleted, which closes
the data-loss consequence recorded at the end of this document rather than leaving it
open.

Status: **PROPOSED — NOT RATIFIED.** The implementation is on `main` because
increment 5 could not start without it, and choosing an implementation must not
be allowed to answer a question that belongs to the project. Two of the
decisions below are Paul's and are recorded as such under *Open questions*.

Date: 2026-09-01

Authors: drafted for ratification, from the measured design in
`kg-upsert-merge-design` (2026-08-31, 833 lines, measured on the real corpus)

Amends: the *cost* paragraph of P3 in `docs/PRINCIPLES.md` — the schema now
decides what a write does. Related: 0028 (what a load refuses), 0029
(path-derived identity), 0026 (the read budget), 0017 (provenance and audit).

Context
-------
`kg apply` refused every multi-batch document, and the refusal cost the whole
document.

A source is extracted in sections. Its subject recurs across them, the
sections' records are concatenated, and two records of one submission end up
naming one entity — which is the normal case, not an error. The boundary
refused the batch:

```
batch refused: 8 structural error(s): upsert concept:kind_games: identity
"concept:kind_games" is declared by record 0 (upsert concept:kind_games) and
record 25 (upsert concept:kind_games) (entity.id); … (7 more)
```

Measured on the rebuild pilot: **three of three multi-batch documents lost 100%
of their extraction**, after the model had been paid for, and **19 of the 40
documents in the acquisition cache (48%) are multi-batch**. Weighting the
1,252-source manifest by type gives ~3,162 batches, so the majority of the
~1,144-source rebuild was exposed. Increment 5 could not start.

**The refusal is the loud half of one defect whose quiet half was already in
production.** The boundary already combined two claims on one identity — every
time an upsert landed on an existing entity — using a rule it declined to apply
across the records of one batch. And that rule lost data silently: batch 2's
`description` replaced batch 1's with no warning, which is why **1,610 entities
(6.03% of the corpus) carry two or more `sourced_from` targets and exactly one
description**. Whatever the other 2,556 source attributions had to say was
overwritten by a rule nobody had written down.

kg held **three** different answers to "a record claims an existing identity",
two of them exact opposites:

| operation | attributes | relations |
|---|---|---|
| `command.UpsertEntity` | **incoming wins** | union |
| `batch.applyCreate`, on-conflict=merge | incoming wins (the same rule, written a second time) | union |
| `command.Merge` (`kg merge`, the `merge` op, `kg dedup`) | **target wins**; `--prefer-source` flipped it | union |

180 entities were merged with `kg merge` in increment 4 under target-wins, and
every `upsert` since has run under incoming-wins. Two rules in one tool is a
defect whether or not either is the right rule.

A fourth thing was true and unnoticed: **`type: list` was in the schema
language, declared on five real fields over 34,091 attribute instances, and
nothing implemented it.** Both places that would act on it — `checkAttrType` and
`parseAttrValue` — fell through to a default meaning "anything goes".

Decision
--------

### Upsert merges by field kind, and the schema declares the kind

**Relations union**, as they do today, and correctly: a relation item's
identity *is* its target, so re-submitting `sourced_from: [source:mda]` is a
no-op by construction. That, and not taste, is why one kind unions and another
cannot — attribute values carry no identity of their own.

**A scalar attribute takes the incoming value**, as it does today, and emits a
`content`-class warning (`attribute_overwritten`) when it overwrites a
*different* one. One slot holds one value; last-wins is at least defined, and
the warning turns a silent loss into a reported one, which is what P3 says a
content-class problem gets.

**An attribute the type's schema declares `type: list` is multi-valued.** Its
items carry `{text, source}`, and an upsert **replaces the items written by its
own batch's contributor and leaves every other contributor's items standing.**
A scalar arriving at a list-declared field is a one-element list — OKF §5.2
sets that precedent normatively — and **a string is never split on a
separator**: `categories: "design,mechanic"` is one item holding that string,
which is exactly what that value means today, and splitting it would silently
shred every value that legitimately contains a comma.

Three candidate rules for "what makes a field a list" were considered and two
rejected. *The value's runtime type* — a YAML list is a list — would make the
merge rule depend on what a producer happened to send, so the same field flips
rules between writes and two producers disagree about what the graph holds;
P7's thinness rule exists to prevent exactly that. *A naming convention* is not
checkable, and English lies: `categories` and `tags` are plural and were stored
as strings. **The schema is one place, checkable, already in the language, and
already declared on five real fields.**

The rule is stated in one place — `pkg/merge` — and both verbs that combine two
claims call it. `kg merge` no longer states a rule of its own.

### Two records claiming one identity are folded, not refused

For `upsert`, and for `create` under `on-conflict=merge` or `skip`. `create`
under `on-conflict=fail` still refuses: a create asserts novelty, and that
assertion is the producer's to keep.

The record-level `identity_collision` check **was never the identity invariant
ADR-0028 names.** That invariant is about two *files*, one of which becomes
unreachable by every query kg has — the corpus silently held 265 such files. A
fold produces one file and nothing becomes unreachable. The check was a
placeholder standing in for a merge rule the boundary did not have.

The folded record sits at the position of the *first* record claiming the
identity, so entities still precede the links that point at them, and it keeps
that record's op. Within a fold, items **concatenate** rather than replace: two
records of one batch are two halves of one contributor's account, and
replace-by-contributor runs once, later, when the folded record meets the file.
That is precisely why the second batch of a document no longer deletes the
first batch's items.

### The unit of reset is a contributor's own contributions

This is not a preference. **Union without a per-item key is not idempotent**,
and the idempotency ledger does not cover a re-ingest: its key is a SHA-256 of
the manifest bytes, so a re-extraction of one document produces a new key and
applies again. Measured:

```
kg apply k1.yaml   → manifest:215b0b2d…  applied
kg apply k1.yaml   → same key         → "Already applied under this key"
kg apply k2.yaml   → manifest:543c8ee9…  applied   # same document, one word changed
```

The ledger dedupes retries of one submission. It does nothing about a
re-ingest, which is the whole point of increment 5. Under
replace-by-contributor a re-applied batch yields a byte-identical file with or
without the ledger, which is **P5's own check** — *apply the same batch twice
and the graph is unchanged the second time.*

**The contributor is carried by the envelope**: one document, one batch, one
source. `batch.NewEnvelopeFrom` names it; a manifest spells it `contributor:`.
It is validated as grammar — it must parse as `type:local` — and not as
content: whether it resolves is not this rule's business.

**A batch with no contributor writes.** Its multi-valued items land carrying no
source, and an item with no source is never replaced by any contributor. That
is the right behaviour for a `kg create` or `kg set` typed by a person: the
edit survives every subsequent re-ingest. It is **reported, not refused**.

**Items are ordered by (source, text) and deduplicated on the same pair.**
Append order follows ingest order, which is not stable, and the rebuild's whole
purpose is comparison against the repaired corpus.

### `unset` is a new op

Contributor-scoped replace covers a re-ingest. It does not cover "an operator
wants this field emptied", and **kg could not do that at any granularity**:
there was no `unset` op, `kg set` only added and overwrote, and `kg delete`
removed the whole entity.

`unset` takes a target and attribute names, and is reachable as
`kg unset <type:id> <attr>...`, through `batch.UnsetAttrs`, and as the MCP
`kg_unset` tool. A producer wanting wipe-then-write emits `unset` then `upsert`
in one manifest, which is atomic under the default `--on-error=fail`. It is the
first way kg has ever had to remove an attribute, and it closes a P7
completeness gap that existed independently of this design.

`set` remains an assignment rather than a merge — that is what the verb means,
and `unset` is its inverse — but the items it writes carry the batch's
contributor, because whose account a write is belongs to the batch and not to
the verb, and it reports (`attribution_discarded`) when it replaces items other
contributors wrote.

### `kg merge`'s scalar default flips to incoming-wins

One merge model. `--prefer-source` is replaced by `--prefer source|target`,
defaulting to `source`, and the preference **narrows to scalars** — a
multi-valued attribute keeps both accounts and a relation unions, so neither
has a side to prefer. This is retroactively the right shape for the flag: a
preference was only ever meaningful on a one-slot field.

**This changes what `kg merge` does to the successors of the 180 entities
merged in increment 4.** Merging into one of them now takes the incoming
entity's scalars where it used to keep theirs. Nothing already written changes;
what changes is the next merge.

### Cardinality remains content

Nothing is refused on it: a scalar at a list-declared field is coerced, a list
at a scalar-declared field is stored and reported. P3 and ADR-0028 hold, and
`kg validate` on the real corpus emits **byte-identical output** before and
after — 4,934 advisories either way — because a bare scalar at a list-declared
field is a legal one-element list rather than a violation.

**What this ADR is admitting** is that the schema now decides what a *write*
does — whether a second document's evidence is kept or destroyed. That is not
enforcement, nothing is refused, and P3's enforced/advised split is unchanged.
But it makes `.kg/schema/` **load-bearing**, and something a rewrite must
preserve, which is an Area A consequence P3's cost paragraph does not
anticipate.

Evidence
--------
Measured on the real corpus (26,718 entity files, 14,644,671 bytes of content,
0 structural errors) and on a real multi-batch document recovered from the
pipeline's own `.kg/.cache/recovery/` — *Game Engine Architecture*, 4,138
records the model actually emitted.

**The blocker, on the real document.** 1,559 upserts over 1,299 distinct
identities; **174 identities claimed more than once**, the most claimed 15
times.

| | before (`c5fe78a`) | after |
|---|---|---|
| `kg apply` | **refused: 260 structural errors** | applied |
| entity files written | **0** | 1,299 |
| links | 0 | 2,512 |
| wall clock | — | 0.40 s |
| re-applied under a different key | — | **byte-identical graph** |

**Encoding cost.** Three encodings were measured, each carrying one attributed
item per entity (26,642 items):

| encoding | corpus bytes | delta | overhead/item |
|---|---|---|---|
| `list` — OKF-shaped, **chosen** | 16,641,649 | **+13.64%** | 75.0 B |
| `map` — source id as key | 16,014,566 | +9.35% | 51.4 B |
| `ref` — index into `sourced_from` | 15,345,160 | +4.78% | 26.3 B |

`list` is the most expensive and is chosen on two grounds that are not this
ADR's. **OKF §5.1 refuses `ref` by its own argument** — *"Labels are keyed
rather than positional (`sources[0]`) because agents constantly rewrite these
documents: a positional index misattributes silently the moment the list is
reordered, whereas a stable `id` survives reordering"* — which is kg's
situation precisely. And `map` puts a `type:id` in a key position where kg's
own grammar stops binding (`store.ValidateAttributeNames` checks top-level
names only), forbids one source contributing two items to one field, and does
not map onto OKF's `sources[]` at increment 7.

**`evidence` costs nothing to migrate**: measured, **0 of 26,718 entities carry
an `evidence` attribute today**. It is a field the pipeline emits and the
corpus has never held.

**The read budget is unchanged by this change.** Measured on the real corpus
with the binary before and after, three runs each: `kg stats` 0.84–0.91 s → 
0.83–0.85 s, `kg validate` 0.87–0.92 s → 0.82–0.85 s, `kg find concept` 0.65 s
→ 0.65 s, `kg get` 17 ms → 16 ms. What the *design* costs when the rebuild
writes both fields attributed is restated in ADR-0026.

Consequences
------------
- **Positive.** The pilot's blocker is gone and increment 5 can start. A
  document whose batches name one entity applies, and re-ingesting it converges
  instead of accumulating.
- **Positive.** kg holds one merge rule instead of three. `pkg/batch`'s
  duplicate of upsert's rule is deleted and `kg merge` calls the same function.
- **Positive.** Two silent losses became reported ones:
  `attribute_overwritten` on a scalar and `attribution_discarded` on a `set`.
  On the real document that is **327 scalar overwrites and 12 discarded
  attributions** nobody could previously see, out of 342 warnings total.
- **Positive.** `type: list` does something, in both places that fell through,
  on 34,091 real attribute instances.
- **Negative.** `.kg/schema/` is load-bearing. A graph whose schema is lost or
  edited merges differently, and no error says so.
- **Negative.** `NewEnvelope` gained a sibling and `MergeOptions.PreferSource`
  became `MergeOptions.Prefer`. Both are Area B compatibility events on the
  exported Go API, which PRINCIPLES.md **open question 8** says has no stated
  horizon. This is the second time that question has been asked to pay.
- **Negative.** The CLI's `--prefer-source` flag is replaced by `--prefer`,
  whose default inverts what the old flag's absence meant. A script passing
  `--prefer-source` now fails rather than silently changing meaning, which is
  the better of the two failures available.
- **Negative.** Making an *embedded* field multi-valued needs a text
  projection, or the embedder receives Go's `%v` rendering of the items and
  every vector is poisoned silently. `store.AttrText` is that projection and is
  now on all three paths that needed it (`embed`, the legacy checksum,
  `resolve`) — but the projection is a minimum, not the schema-driven one
  `description` → `details` will need.
- **Negative.** `kg import` is not covered by any of this, because it does not
  cross the write boundary at all. It calls `store.WriteEntity` directly, and
  it still destroys everything not in the file.

Open questions this does *not* settle
-------------------------------------
This ADR is **unratified on purpose**. Two of its decisions are Paul's:

1. **Does the schema get to decide what a write does?** Proposed: yes. Nothing
   is refused, so P3 holds — but `.kg/schema/` becomes load-bearing and
   something a rewrite must preserve, which P3's cost paragraph does not
   anticipate. Open until ratified.
2. **The encoding.** Proposed: `list` at +13.64%, OKF-shaped. `map` at +9.35%
   is 4.3 percentage points cheaper and is the fallback if bytes turn out to
   matter more than the OKF mapping. `ref` at +4.78% is refused by OKF §5.1's
   own argument. Open until ratified.

Deliberately out of scope, and named so they are not lost:

- **`description` → `details`.** It is born in increment 5's new graph, not
  migrated into the repaired one. Migrating would have to **fabricate
  provenance** for 1,610 entities (6.03%) whose surviving description's author
  was destroyed by last-wins years ago, and fabricated attribution is a worse
  artifact than none.
- **Relation-level evidence** is unrepresentable — `store.Entity.Relations` is
  `map[string][]string` — and is being discarded on every write. It is the same
  provenance question one level down, and increment 7's OKF mapping has to
  answer it.
- **`kg import`'s data loss**, above.
- **The tabular TOON writer does not escape its own separator**
  (`pkg/output/toon.go`), so `kg find` emits rows with more fields than
  declared columns whenever a value contains a comma — which `categories` does
  on 18,096 entities. Pre-existing, independent of this design. List *items*
  are quoted here; scalar values are not, and that inconsistency is the
  pre-existing bug showing through.

Related
-------
- P3 (structure is enforced, content is advised) — cardinality stays content;
  the cost paragraph is amended.
- P5 (every write crosses a typed boundary) — its idempotency check is the
  reason the unit of reset is the contributor.
- P7 (thin, complete CLI) — `unset` closes a completeness gap; the fold and the
  merge rule live in the library, and the CLI holds none of them.
- ADR-0028 — the three-severity table this change does not move.
- ADR-0029 — path-derived identity, which a fold preserves and a refusal was
  not protecting.
- ADR-0026 — the read budget, restated for what this design costs at full
  extent.
- ADR-0017 (provenance and audit) — per-item source attribution is the first
  half of PRINCIPLES.md open question 9's answer, arriving at increment 5
  rather than increment 7.
