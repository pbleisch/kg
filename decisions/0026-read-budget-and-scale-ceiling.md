# 0026 — The Read Budget and the Scale Ceiling

Status: **PROPOSED — NOT RATIFIED.** The numbers below are a recommendation
with the evidence for them. Open questions 1 and 2 in `docs/PRINCIPLES.md`
remain open until Paul settles them.

Date: 2026-08-30

Authors: drafted for ratification

Context
-------
P4 says a read is "open files, answer" and that a service may exist as an
optimization but never as a requirement. Its own check demands a number:

> With every background process disabled and every cache cold, a single-entity
> read, a typed find, and a full-graph traversal all complete inside a stated
> budget. **State the budget and the corpus size it holds for.**

No budget was ever stated. Without one, "no service required" cannot be
falsified, and the daemon comes back the first time something feels slow — which
is how it arrived. Two questions have to be answered together, because neither
means anything alone:

1. **Is a resident service permitted at all?** Only answerable as a latency.
2. **What is the stated scale ceiling?** ADR-0014 says 100k entities. The one
   real corpus is 27k.

This ADR proposes both, and records the measurements they rest on.

Evidence
--------
All figures measured on the GDKB corpus — **27,158 entity files, 26,892 distinct
ids, 53,630 stored vectors** of 384 dimensions (two embedded fields per entity,
`name` and `description`) — on an Apple M5 Max, macOS 15 (Darwin 25.3.0), APFS
on internal SSD, Go 1.26.1.

The corpus yields 54,148 (entity, field) pairs but only 53,630 rows: 534 of them
collapse because 265 files share an id with another file, so one of each pair is
unreachable. That is PRINCIPLES.md violation #3 showing through the embedding
pass, and `kg sync` now says so.

*Method.* "Cold" means kg's own derived state is absent: `.kg/.cache/` deleted,
and — before — no daemon, with `KG_NO_DAEMON=1` set. The OS page cache could not
be dropped (macOS `purge` needs root), so both the before and after runs were
taken with the corpus recently read. That inflates the *after* numbers less than
the *before* ones, since the after path reads 15 MB and the before path built a
1.3 GB index; the comparison is if anything conservative. Each figure is the
range over three consecutive runs.

### Before — LadybugDB index, daemon available

| | |
|---|---|
| entity YAML on disk | **659.7 MB**, of which 643.5 MB (97.55%) is float lines |
| packed git, tip only, after `git gc --aggressive` | **229 MB** (217.4 MiB) |
| LadybugDB index | **1.3 GB** |
| `kg get`, cold, no daemon | **783.8 s** |
| `kg get`, index present, every mtime new (a fresh clone) | 4.2 s |
| `kg get`, index present, mtimes unchanged | 0.29 s |
| `kg similar game --field description --top 10`, HNSW warm | 0.52 s |
| `kg similar concept …` (20,704 of the type), HNSW warm | 0.51 s |

Two of these disagree with `docs/PRINCIPLES.md`, which records 116 s cold and
22 s warm-but-daemonless. **Cold is far worse than recorded — 783.8 s, 6.8×** —
and the 22 s figure did not reproduce at all: the daemonless steady state is
0.29 s with mtimes unchanged and 4.2 s in the fresh-clone shape, where the
manifest forces a re-checksum of all 659.7 MB. Both corrections point the same
way. The recorded 116 s was a floor, and the thing the daemon was hiding was the
first read after a clone, which on this machine today is thirteen minutes.

### After — no index, no daemon, gitignored sidecar

| | |
|---|---|
| entity YAML on disk | **14.92 MB** (44× smaller) |
| packed git, same commit, same `gc` | **13 MB** (9.4 MiB), 17× smaller |
| `.kg/.cache/vectors.bin` | 164.7 MB, gitignored |
| `.kg/.cache/vectors.idx` | 10.3 MB, gitignored |
| **`kg get`** (cold, no `.kg/.cache` at all) | **3–4 ms** |
| `kg path <id> -> <rel>` | 3 ms |
| `kg path <id> ->* <rel>` (recursive) | 3 ms |
| `kg find genre` (513 of the type) | 16–17 ms |
| **`kg find concept`** (20,704 of the type) | **608–675 ms** |
| **`kg back <id> via <rel>`** (whole-graph walk) | **817–840 ms** |
| `kg stats` (whole-graph walk) | 808–826 ms |
| `kg validate` | 772–784 ms |
| **`kg similar`**, end to end | **49–119 ms** (the spread is page-cache warmth on the 165 MB bin) |
| a single write (`create` / `set` / `delete`) | 190–222 ms, dominated by rewriting the 165 MB sidecar |
| — of which the brute-force scan | 9.1 ms |
| — of which mapping the sidecar in | 52.9 ms |
| `kg sync`, full rebuild from nothing | 51.4 s (the embedder subprocess) |
| `kg sync`, nothing changed | 4.0 s |
| `kg sync --migrate-vectors`, one-time | 46 s |

The `kg similar` figures are with a stand-in embedder; the *search* cost does not
depend on which embedder produced the vectors. Reproduce the scan measurement
with `KG_BENCH_SIDECAR=<path>/.kg/.cache go test ./pkg/vector -bench Corpus`.

### The sidecar returns the index's answer

`kg similar` on the sidecar was compared against the same query on the LadybugDB
HNSW index, on the same corpus, before the index was deleted:

- **`game`** (3,031 of the type): identical ten ids, identical order, scores
  agreeing to five decimal places — one rank differs in the sixth printed
  decimal (0.677531 against 0.677530).
- **`concept`** (20,704 of the type, searched over all 53,630 vectors):
  identical ten ids, identical order, **identical scores** at six printed
  decimals.

`TestSidecarMatchesInFileVectors` in `pkg/store` pins the invariant permanently:
it builds a corpus with the vectors where they used to live, searches both ways,
and asserts the top-10 match in order with **bit-identical** scores — which they
are, because the sidecar stores the same float64s and does the same arithmetic
in the same order.

Decision (proposed)
-------------------

### The stated scale ceiling is 100,000 entities

Same as ADR-0014, now with evidence rather than an assumption. The measured
corpus is 27,158 entities of 14.92 MB — about 550 bytes of content each — and a
whole-graph walk costs 0.81 s. That is linear in bytes, so 100,000 entities is
roughly 55 MB and roughly 3 s.

Above that the shape changes. At a million entities a whole-graph walk is ~30 s,
which is not a budget anyone would accept, and `back`, `stats` and `validate`
would need a persistent reverse index — which is the artifact that needed
keeping warm, which is what the daemon was for. **If the ambition is millions,
P4 has to be rewritten before anything is built, not after.**

### The budget

With every background process disabled and every kg cache cold, on a corpus at
or under the ceiling:

| operation | budget | measured at 27,158 | projected at 100,000 |
|---|---|---|---|
| single-entity read | **50 ms** | 3–4 ms | 3–4 ms (independent of corpus size) |
| typed find | **5 s** | 0.61–0.68 s (20,704 of one type) | ~3 s |
| full-graph traversal | **5 s** | 0.82–0.84 s | ~3 s |
| similarity search | **1 s** | 0.05–0.12 s | ~0.3 s |

Measured on a freshly migrated corpus, against the shipped binary, with
`.kg/.cache/` deleted for the first three rows and present for the fourth —
there is no background process to disable, `kg daemon` not being a command.

The budgets are about 6× the measured value at 27k, which is about 1.7× the
linear projection at 100k. The headroom is for slower disks, spinning media, and
corpora with more content per entity — not for regressions. A change that
consumes it is a regression whether or not it stays inside.

### Restated for per-item source attribution (ADR-0030, 2026-09-01)

ADR-0030 makes an attribute the schema declares `type: list` multi-valued, with
each item carrying `{text, source}`. **That moves the numbers above, and the
number is restated here rather than quietly exceeded.**

Measured on a real copy of the corpus (26,718 files, 14,644,671 bytes):

| corpus state | bytes | delta | whole-graph walk | projected at 100k |
|---|---|---|---|---|
| today | 14,644,671 | — | 0.82 s | ~3 s |
| `description` → attributed `details` | 16,641,649 | +13.64% | 0.86 s | ~3.2 s |
| **both fields fully attributed** | **21,777,170** | **+48.7%** | **0.93 s** | **~3.5 s** |

So at the proposed 100,000-entity ceiling a whole-graph traversal projects to
**about 3.5 s**, against the ~3 s this ADR projected and inside its stated 5 s
budget — but the headroom this ADR said was *"for slower disks, spinning media,
and corpora with more content per entity — not for regressions"* is now partly
spent on content instead. That is a deliberate trade: the bytes buy provenance
per item, which is what makes a re-ingest converge (P5) and what increment 7's
OKF `sources[]` renders. **The ceiling and the budgets are unchanged. What
changes is how much of the headroom is already committed.**

Two things this restatement is careful about:

- **ADR-0030's implementation costs nothing today.** Measured with the binary
  before and after, three runs each on the same corpus: `kg stats`
  0.84–0.91 s → 0.83–0.85 s, `kg validate` 0.87–0.92 s → 0.82–0.85 s,
  `kg find concept` 0.65 s → 0.65 s, `kg get` 17 ms → 16 ms, and `kg validate`
  emits byte-identical output. The corpus carries no attributed items yet. The
  21.78 MB figure is what the **rebuild** will write, not what this change
  wrote.
- **The `description` → `details` row is not scheduled.** ADR-0030 leaves that
  field to increment 5's new graph rather than migrating the repaired one. The
  row is here because the budget has to be stated against the design's full
  extent, not against the half of it that has landed.

This ADR stays **unratified**. Open questions 1 and 2 below are still Paul's,
and this restatement does not answer either.

### A resident service is permitted, and is never required

P4 stands as written. A service may exist as an optimization. It may never be on
the critical path of a read, and the check that it is not is that **with it off,
the budget above still holds**. If a future service is added, this table is what
it must be turned off against.

**If a read exceeds its budget on a corpus within the ceiling, the answer is to
make the cold path faster or to lower the ceiling — never to add a resident
process.** That sentence is the whole point of writing the numbers down.

Consequences
------------
- **Positive:** P4 becomes falsifiable. There is a table to measure against and
  a rule about what to do when it fails.
- **Positive:** The daemon's deletion (ADR-0027) rests on measurements rather
  than on the hope that files are fast.
- **Positive:** Two known costs are now visible rather than latent: mapping the
  sidecar in is 53 ms of JSON parsing, over five times the 9 ms scan it enables,
  and it grows with the corpus. At the ceiling it is ~200 ms — inside the 1 s
  budget, but it is the first thing to fix if that budget tightens, and a binary
  index is the obvious fix.
- **Negative:** A ceiling is a promise not to grow past it quietly. Crossing
  100k entities is a design change, not a data change.
- **Negative:** The budgets are generous enough that a real 3× regression could
  hide inside them. The measurements above, not the budgets, are the baseline to
  compare against.
- **Negative:** Everything here is one machine, one filesystem, one corpus. A
  spinning disk or a network filesystem may not meet these numbers, and nobody
  has checked.
- **Negative:** A write costs ~200 ms on this corpus, because the sidecar is
  rewritten whole. That is the price of a format with one state instead of two,
  and it is paid per *batch*, so bulk ingestion through `kg apply` pays it once.
  A single interactive `kg set` pays it alone. At the ceiling it is ~700 ms, and
  a row-level in-place update is the fix if that becomes the complaint.

Open questions this does *not* settle
-------------------------------------
This ADR is **unratified on purpose**. Increment 3 was built to it because the
implementation had to choose something, but choosing an implementation must not
be allowed to answer a question that belongs to the project:

1. **Is a resident service permitted at all?** Proposed: yes as an optimization,
   never as a requirement, checked by turning it off against the table above.
   Open until ratified.
2. **What is the stated scale ceiling?** Proposed: 100,000 entities. Open until
   ratified. If the answer is millions, P4 needs rewriting first and the
   architecture increment 3 landed is the wrong one.

Related
-------
- P4 (reading needs no running service) — the principle this makes checkable.
- P1 (the files are the graph) — its cost paragraph says the ceiling "must be
  stated rather than discovered". This states it.
- ADR-0014 (performance and scaling) — asserted 100k without evidence.
- ADR-0027 (deleting LadybugDB, the index and the daemon) — the change these
  measurements justify.
- ADR-0008 (daemon) — superseded.
- ADR-0030 (attribute merge and the unit of reset) — the change whose byte and
  latency cost is restated above.
