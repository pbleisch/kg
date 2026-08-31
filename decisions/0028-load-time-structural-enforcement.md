# 0028 — What a Load Refuses, and What It Merely Reports

Status: Decided

Date: 2026-08-31

Authors: Paul

Amends: the *check* half of P3 in `docs/PRINCIPLES.md`; corrects the
done-condition of increment 4 in `docs/increments.md`. Related: 0018 (error
handling), 0016 (ids and naming), 0029 (path-derived identity).

Context
-------
P3 says *structure is enforced; content is advised*, and increment 4 turns on
its load half. Turning it on required settling a contradiction the repository
was carrying in two places at once.

`docs/PRINCIPLES.md` P3 states its structural invariants as "identity is
unique, an entity's declared identity agrees with where it is stored, **a
relation points at something that exists**", and then its own check says:

> A dangling relation target is **reported**.

`docs/increments.md` §4 carried the same split down the middle. Its deliverable
said "a dangling target is **reported**"; its done-condition said a seeded
"relation pointing at nothing" **fails the load**.

Reported and fails-the-load are not the same rule, and a repository that holds
both settles nothing. Worse, the project has already decided to adopt OKF in
practice (increment 7), and OKF §11 requires that a consumer *"MUST NOT reject
a bundle because of… broken cross-links"*, §6.1 that *"Consumers MUST tolerate
broken links."* Making a dangling target a hard load failure would put kg in
head-on conflict with the format it is about to write, on a point where kg
gains nothing by disagreeing.

Decision
--------
**A load refuses on identity and on grammar. It reports everything else,
including a dangling relation target.** The write boundary keeps refusing a
dangling target, because a producer asserting an edge is a different act from a
reader finding one.

Three severities over two moments. This is the rule:

| class | what it is a property of | at the write boundary | at load |
|---|---|---|---|
| **Identity** — the identity is unique; the declared `id`/`type` agree with the file's location | a file's *place in the corpus*; checkable against the paths alone | refused | **refused** |
| **Grammar** — an attribute name matches `[a-z][a-z0-9_]*`; an id parses as `type:local`; a relation name is non-empty; the file parses at all | a file's *own bytes*, independent of every other file | refused | **refused** |
| **Content** — which attributes exist, their types, cardinality, whether a relation target resolves | *meaning*, or the corpus at one instant | schema: warned. **Dangling: refused** | **reported** |

### Why identity and grammar fail

Both are decidable from one file plus the paths, both are repairable by editing
that one file, and both decide whether a fact is reachable at all. Two files
claiming one identity means one of them is unreachable by every query kg has —
the corpus silently holds 265 files no `kg get` will ever return. A file whose
declared identity disagrees with its location is reachable by neither the
identity it claims nor the one it has. Those are not opinions about content;
they are the artifact failing to be a graph.

An illegal attribute name is in the same class for a different reason: the
*write boundary already refuses it* (`pkg/batch/validate.go`,
`illegal_attribute`). A rule that binds on the way in and shrugs on the way out
is not a rule — it produces a corpus kg refuses to write and cheerfully loads,
which is exactly the state 2,118 entities carrying an attribute named `--dir`
were found in. Note what this does *not* say: the grammar decides what may be
an attribute *name*; the schema decides which attributes are *expected*. An
unknown attribute is content and stays a warning.

### Why a dangling target is reported, not refused

Four reasons, in the order they weigh:

1. **It is not a property of the file being read.** Identity and grammar are
   decidable from one file. Whether `concept:foo`'s relation resolves depends
   on a *different* file, which an unrelated, legitimate `kg delete` can remove
   at any time. A rule that makes reading one file fail because of a file it is
   not reading is not an invariant of the artifact; it is a property of one
   snapshot, and it flips back and forth as unrelated files come and go.
2. **Refusing it makes the corpus unrepairable by the tool that found the
   problem.** GDKB holds 811 dangling occurrences over 282 distinct targets
   before the repair, and 741 after it. If a dangling target failed the load,
   `kg validate` could not report the other 2,550 violations, `kg back` and
   `kg stats` could not run, and the corpus would be readable only by not using
   kg — which is P1 working as designed and the tool failing.
3. **OKF §11 and §6.1 forbid it**, and increment 7 adopts OKF. kg is
   deliberately a *stricter* tool over OKF bundles and does not trade that for
   a conformance badge — but the strictness worth keeping is the strictness
   nothing else offers, which is the typed, enforced write boundary. Being
   strict on the read side about the one thing the specification names is
   incompatibility bought for nothing.
4. **The boundary already catches it at the only moment a producer can fix
   it.** `batch.Apply` refuses `dangling_relation` today: at submission the
   batch itself contains the edge, the referent either exists or is declared by
   the same batch, and the producer is the party that can act. That asymmetry
   is the whole point — *a producer is refused for what it asserts; a reader is
   not punished for what someone else deleted.*

A reported dangling target is not a shrug. `kg validate` names every one, with
the source entity, the relation and the target, and says how many there are.
It exits 0, because a graph under construction is legitimately incomplete.

### What this does not change

P3's statement, number, check and cost are unchanged in substance. The
enforced/advised split is unchanged. What changes is that P3 now says at *which
moment* each half binds, which is the thing it was silent about and the reason
it could be read two ways. `docs/increments.md` §4's done-condition is
corrected to match its own deliverable.

Consequences
------------
- `store.LoadGraph`, `store.ReadEntity`, `store.ListEntities` and
  `store.FindByType` refuse a corpus that breaks an identity or grammar rule,
  returning a `*store.LoadError` that names every offending file. GDKB does not
  load until it is repaired. That is P3's stated cost, arriving.
- `store.ListEntityFiles` deliberately does **not** enforce. It is the read the
  repair uses, and it is the only one: a corpus that will not load must still
  be walkable by the tool that fixes it. Anything that walks and rewrites uses
  it, with `store.WriteEntityAt`.
- `kg validate` exits non-zero only on identity and grammar. Content — unknown
  attributes, schema violations, dangling targets, entity types with no schema
  — is reported and exits 0. There is no flag that moves a rule between the two
  columns; P3 forbids them sharing a setting, and a flag is a shared setting.
  Measured on the corpus: **2,550 structural findings over 2,533 files, and
  7,343 advisories**, in under a second; after `kg validate --fix` and three deletions
  a human has to choose, 0 and 5,225, exit 0.
- The seeded-violation test set is: two files claiming one identity → fails,
  naming both; a file whose declared identity disagrees with its location →
  fails, naming the file, the declared identity and the derived one; an
  attribute named `--dir` → fails, naming the entity and the attribute; a
  relation pointing at nothing → **reported**, exit 0.

Alternatives considered
-----------------------
**Make a dangling target fail the load.** This is what increment 4's
done-condition said. Rejected on all four grounds above; the deciding one is
(1), that it is not a property of the artifact but of a snapshot of it.

**Make a dangling target fail only when a flag is set.** Rejected: P3's one
explicit prohibition is that structure and content must not share a setting,
and a severity flag is precisely a shared setting. It also just moves the
argument to the flag's default.

**Let an illegal attribute name be a warning at load, matching "content".**
Rejected: it is a *name-grammar* rule, not a schema rule, and the write
boundary already refuses it. Accepting on load what the boundary refuses on
write is how a corpus drifts into a state its own tool cannot produce.

**Leave the write boundary and the load agreeing on dangling — both refuse, or
both report.** Rejected in both directions. Both-refuse is alternative 1. Both-
report gives up the one check that stops a producer inventing referents, which
is how `command.Link` used to mint typeless stubs for entities nobody
submitted.

Open
----
This contributes the second half of the evidence for PRINCIPLES.md open
question 7, *is P3 one principle or two?* The answer this ADR implies is that
it is one principle over **three** severities and **two** moments, and that the
severities do not partition the same way at both moments — dangling is refused
at the boundary and reported at the load. That is a stronger reason to keep P3
whole than the question assumed: split it in two and the split has to be made
twice, differently.
