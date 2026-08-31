# 0029 — Identity Is Derived From the Path

Status: Decided

Date: 2026-08-31

Authors: Paul

Answers: `docs/PRINCIPLES.md` open question 3. Amends 0016 (ids and naming
conventions), 0002 (data model). Related: 0028 (what a load refuses).

Context
-------
PRINCIPLES.md open question 3 asks where identity lives — in the file or in the
path — and records the state of the answer bluntly:

> The current answer is *both, unenforced*, which is the one option that is
> definitely wrong.

Both are defensible on their own. A slug in the file makes a rename free and a
collision detectable; a path-derived id makes lookup O(1) with no index. What
is not defensible is holding both and checking neither, which is what
`pkg/store/yaml.go` did: `EntityPath` computed the path from the `id:` field,
`walkEntities` read whatever it found, and nothing compared them.

Measured on the GDKB corpus of 27,158 files (2026-08-31, this branch):

| | |
|---|---|
| files whose `id:` disagrees with `<dir>:<basename>` | **265** |
| declared ids claimed by more than one file | **267**, over **534** files |
| files whose `type:` disagrees with their directory | **162** |
| distinct declared ids | 26,891 |
| distinct path-derived ids | 27,156 |

Those two collision counts are the same defect seen from two sides, and the
arithmetic closes: of the 267 declared-id collisions, 265 have exactly one file
sitting somewhere other than where its declared id says. The remaining 2 are a
different animal — `source/an_introduction_to_game_studies_frans_mäyrä.yaml`
and `source/an_introduction_to_game_studies_frans_m%C3%A4yr%C3%A4.yaml`, one
file written with the raw UTF-8 name and one with the percent-encoding kg
produces, both decoding to one identity. Increment 3's embedding pass counted
the same 534 files independently, as (entity, field) pairs collapsing onto ids
two files both claim.

Increment 7 adopts OKF, whose serialization is path-derived. Repairing 265
disagreements under one rule and then changing the rule would be doing the work
twice; this pre-commits so that increment 7 is a change of bytes and not of
semantics.

Decision
--------
**A file's location is its identity.** For a file at
`<kgDir>/entities/<type>/<local>.yaml`, the identity is `<type>:<local>`, with
`<local>` percent-decoded — the exact inverse of the encoding `EntityPath`
applies when it writes.

`id:` and `type:` remain in the file. They are a **redundant echo**, not the
source:

- When present, they must agree with the path. A disagreement is a structural
  failure of the load, naming the file, the declared identity and the derived
  one (ADR-0028).
- When absent, the path supplies them and nothing is reported. A loader always
  returns an entity with `ID` and `Type` filled in from the path, so no caller
  has to know which of the two it got.

They are kept, rather than dropped, because P1 says a graph travels without the
tool: someone holding one file and no software still learns what it is without
having to reconstruct the convention from the directory tree. The cost of
keeping them is exactly the check above, and the check is cheap — it needs the
file and its own path, nothing else.

### What follows

**Duplicate identity becomes a filesystem question.** Two files can claim one
identity only when two paths derive one identity, which happens exactly when
the encoding is not injective over what is on disk — the mäyrä/façade pairs
above — or when the tree is nested more deeply than `<type>/<local>.yaml`. That
is a much smaller and much sharper failure than 267 files quietly agreeing on a
string, and it is why the load reports 4 files in that class where the old
declared-id view reported 534.

**A rename is a `git mv` plus the edges that name it.** Under path-derived
identity, moving the file *is* the rename; the `id:` line follows. That is the
cost open question 3 named for this option, and it lands on inbound relations,
which have to be rewritten either way.

**`ListEntityFiles` and `WriteEntityAt` are the whole-corpus pair.** Anything
that walks the corpus and rewrites what it finds must write back to the file it
read. `WriteEntity(kgDir, e)` derives the path from `e.ID`, which on a corpus
with 265 disagreements skips one file and overwrites another. `WriteEntityAt`
now refuses a write whose entity identity disagrees with the path it is being
written to, so the trap is closed by the API rather than by a comment.

**`kg validate --fix` reverses.** It used to treat the `id:` field as
authoritative and *move the file* to `EntityPath(id)` — the exact overwrite
above, applied deliberately, 265 times. It now treats the path as
authoritative, rewrites `id:` and `type:` to match, and never moves a file.

Consequences
------------
- Open question 3 is closed: **path**.
- ADR-0016's snake_case normalization is unaffected as a rule for *minting* an
  id, and is no longer load-bearing for *resolving* one. A file at
  `entities/concept/Not_Snake.yaml` has identity `concept:Not_Snake`, and its
  `id:` must say so; normalization is what a producer applies before choosing a
  path, not something a reader re-derives.
- Percent-encoding becomes part of the identity contract rather than an
  implementation detail of `EntityPath`, and its inverse is now a load-path
  operation. A corpus holding both encodings of one name has two files claiming
  one identity and does not load until one is removed. GDKB has two such pairs.
- kg does not renumber, re-slug or relocate anything on load. A load that finds
  a disagreement refuses and names it; deciding which of the two is right is
  the operator's, because for 265 of GDKB's files the `id:` field is the older
  and more-referenced name and for others the path is.

Alternatives considered
-----------------------
**Identity lives in the file; the path is a cache.** Renames are free and
collisions are detectable without touching the filesystem. Rejected because it
requires an index to answer "where is `concept:foo`?" — a map from id to path,
rebuilt by walking every file — and ADR-0027 has just deleted the index and the
daemon that kept it warm. It reintroduces exactly the derived artifact
increment 3 removed, to answer the cheapest question kg has.

**Drop `id:` and `type:` from the file entirely.** Cleanest by one measure: one
source of truth, no check needed, and closest to OKF frontmatter. Rejected on
P1 — a file that does not say what it is has moved part of the knowledge into
the directory tree, and the whole point is that someone with the files and none
of the software still has the knowledge.

**Keep "both" and enforce agreement without saying which wins.** This is
almost the current state and it is not an answer: when they disagree something
has to be authoritative, and refusing to say means the repair tool picks, which
is how `--fix` came to move 265 files.
