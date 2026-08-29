# kg Core Principles

kg is an experiment in an **in-repo, agent-friendly knowledge graph**. Nothing in the
current implementation is sacred — not the storage format, not the index, not the
language. This document records the small set of properties that *are*, so that a
rewrite has something to be judged against.

**A principle is a property a rewrite must preserve. A choice is anything else.**
The separation is this document's only real job. A principles list that smuggles in
today's implementation blocks the rewrite; one that is all slogans permits anything.

**The test for admission: can you state how you would check it?** "The graph should be
agent-friendly" is a slogan. "Every entity is readable by a program that is not kg,
with no running service" is a principle, because a rewrite either satisfies it or does
not. Every principle below carries a check and a cost. A principle with no cost is not
constraining anything.

Discarding a **choice** needs no permission. Discarding a **principle** needs Paul's.

---

## The four areas

The nine principles govern four different parts of the system, and a proposed change
almost never lands on all four. Grouping them by area is what makes a change's **blast
radius** visible: find the area the change touches, and the principles in play are the
two or three in that group, not all nine.

| Area | Principles | What it governs | A change lands here when it… |
|---|---|---|---|
| **A. The graph on disk** | P1, P2, P3 | The bytes that *are* the knowledge: what a file contains, what belongs outside it, and what makes a set of files a valid graph rather than a pile | …changes the format, the layout, what is stored, or what a load is allowed to accept |
| **B. The interface** | P5, P6, P7 | How other programs reach kg — what may be written in, what comes out, and how a caller invokes it at all | …changes a command, an output shape, a protocol, or the way a producer submits facts |
| **C. The runtime** | P4, P8 | What has to be running, and what kg is allowed to run. kg owns no resident process and no model | …adds a daemon, a service dependency, an SDK, an API key, or a network call |
| **D. The record** | P9 | How kg's own design decisions are captured, so a settled argument is not re-derived | …invalidates, supersedes, or quietly departs from a written decision |

**Numbers are identifiers, not an ordering.** P1–P9 are cited throughout this document
and by reviews written against it, so they are unchanged; a stable identifier is worth
more than a tidy sequence. Within an area they run in ascending order, and the areas
therefore do not run 1–9. A principle whose number and whose area disagree is not a
defect.

### Worked example: what "adopt OKF as the on-disk format" touches

Google's Open Knowledge Format is a live proposal for kg's serialization — markdown
with YAML frontmatter, path-derived identity, no schema registry. In a flat list of
nine, answering it means checking nine principles one at a time. By area:

| Area | Does the proposal touch it? |
|---|---|
| **A. The graph on disk** | **Yes — entirely, and this is where the decision is made.** P1 is satisfied by construction: OKF's stated design goals restate it. P2 is satisfied trivially, there being no derived layer to misplace. **P3 is refused outright** — OKF §11 requires that consumers *"MUST NOT reject a bundle because of… broken cross-links"* and §6.1 that *"Consumers MUST tolerate broken links."* |
| **B. The interface** | **No.** OKF ships no reader, validator, or query tool, so there is nothing for P5–P7 to bind to. kg's write boundary, output contract and process boundary survive the swap unchanged. |
| **C. The runtime** | **No.** A format requires nothing running, so it satisfies P4 and P8 *vacuously* rather than being tested by them. Compatibility here is not evidence of fit. |
| **D. The record** | **Only in the ordinary way.** Adopting it is a decision P9 requires be written down before it is load-bearing, and it answers open question 4. |

So the format question is an **Area A** question. It is decidable on three principles,
and the one that decides it is **P3** — whether kg's strictness survives adopting a
format that forbids strictness in consumers. Areas B, C and D do not constrain the
answer, and a swap that leaves them alone is a smaller change than a flat list makes it
look.

---

## Area A — The graph on disk

*What the bytes are, what is allowed among them, and what makes them a graph.* This is
kg's storage position — plain files, derived data outside, portable without the tool —
and a 2026 prior-art review found it is now also the industry's, published as a
specification. **Every principle in this area is currently violated somewhere**: five of
the nine recorded violations (#1, #3, #4, #6, #8) land here. It is at once the area kg
is least distinctive in and the area it holds worst.

### P1 — The files are the graph, and a graph travels without the tool

Entities live in plain, human-readable text, one entity per file, in the repository
they describe. The file *is* the entity — not a cache of a row somewhere. A graph is a
portable artifact: someone who has the files and none of the software still has the
knowledge, can diff it, review it, branch it, and read it with fifty lines of any
language.

**Check.** Delete every derived artifact; the graph is intact and every fact is still
reachable. Clone a graph repository that contains no binary and no tool, and read the
whole thing with a script in a different language. Diff a real content change and have
it be reviewable by a human.

**Cost.** The on-disk layout becomes a public contract you cannot change quietly. There
are no server-side joins, no view maintained for you; every consumer re-derives what it
needs. Above some corpus size a full walk stops being cheap, and that ceiling must be
stated (P4) rather than discovered.

### P2 — Derived data never lives in the source of truth

Anything recomputable from the files — indexes, embeddings, caches, manifests,
statistics — lives outside them, is gitignored, and is rebuildable by one command. The
source of truth contains only what a human or an agent authored.

**Check.** Rebuild every derived artifact from scratch and `git status` is clean.
Change the embedding model and not one entity file changes. Total entity-store bytes
are within a small factor of the bytes a human would call content.

**Cost.** Two artifacts to keep in sync instead of one, and a rebuild step before a
fresh clone can answer a semantic query. Derived state can go missing or stale, so
every consumer needs a defined behaviour when it is absent.

### P3 — Structure is enforced; content is advised

The two are not the same rule and must not share a setting. **Structural** invariants
decide whether a fact is *reachable at all* — identity is unique, an entity's declared
identity agrees with where it is stored, a relation points at something that exists.
Those are hard failures that name the offending records. **Content** — which attributes
exist, their types, cardinality — is advisory guidance for agents, reported and not
blocking, because a graph under construction is legitimately incomplete.

**Check.** Two files claiming one identity fail the load, naming both. A file whose
declared identity disagrees with its location fails. A dangling relation target is
reported. An unknown attribute is a warning, and a graph full of warnings still loads.

**Cost.** Ingestion that used to succeed now fails, and existing corpora will not load
until they are repaired. Being strict about identity means a rename is a real
migration, not a `mv`.

## Area B — The interface: how other programs reach kg

*What may be written in, what comes out, and how a caller invokes kg at all.* Three
principles, one for each direction: P5 fixes the shape of what enters, P6 the shape of
what leaves, P7 the mechanism in between. This is kg's least-contested ground — nothing
comparable in the field offers a typed, enforced write boundary — and the only recorded
violation here (#5) was committed by a consumer that skipped the boundary rather than by
kg. P6 and P7 have no recorded violation, which is partly that they hold and partly that
nothing has stressed them.

### P5 — Every write crosses a typed boundary

Nothing becomes an entity by being parsed out of prose or a command line. Writes take
structured records — typed fields, versioned envelope, an idempotency key for batches —
so that a malformed producer fails at the boundary instead of writing nonsense that
validates.

**Check.** No code path constructs an entity by string-splitting a command. Feed the
write boundary a record with an unknown field and it is rejected, not stored. Apply the
same batch twice and the graph is unchanged the second time.

**Cost.** Producers must emit structured data. The convenient shortcut — prompting a
model for CLI text and scraping it — is exactly what this forbids, and it is genuinely
easier to prompt for. This principle is the one most likely to be violated under
deadline, and §"Violations" shows what it costs when it is.

### P6 — Output is machine-first and deterministic by construction

The default output is for a program, not a terminal: structured, complete, stable field
order, token-efficient. Human formatting is a flag, not the baseline. Errors are
structured too — a machine-readable code, a message, and where possible a suggestion.

**Check.** Run the same query twice on unchanged data and the bytes are identical. Pipe
any command's output into a parser without a regex. Trigger every error class and get
back structure, not prose on stderr.

**Cost.** No per-caller output tuning and no adaptive prettiness. Deterministic ordering
constrains internal data structures — maps must be sorted on the way out, everywhere,
forever.

### P7 — The integration boundary is a process, not a library

Consumers reach kg by running it: a binary on `PATH`, or a stdio protocol. They do not
link it, do not share its memory, and are not required to share its language. This is
why the only real consumer to date, written in Python and Node, could use a Go tool at
all.

**Check.** A consumer in a language kg is not written in can perform every supported
operation using only a binary and a pipe. Nothing in the contract requires an FFI, a
shared object, or a matching runtime.

**Cost.** Process startup and serialization on every call, which tightens P4's budget
rather than relaxing it. No shared in-memory state between calls, so anything expensive
to construct must be either cheap enough to rebuild or persisted as derived state (P2).

## Area C — The runtime: what has to be running

*What kg requires to be alive, and what kg is allowed to run on your behalf.* Both
principles here say the same thing in opposite directions: kg owns no resident process
of its own (P4) and hosts no model of anyone else's (P8). The cost of both lands on the
operator, and both are checkable by turning things off. P8 holds. P4 is violated in
practice (#2): the daemon is required in all but name, which is the single largest gap
between what this document claims and what the binary does.

### P4 — Reading needs no running service

A read is: open files, answer. No daemon, no server, no socket, no background process
on the critical path. A service may exist as an *optimization*, never as a
*requirement*, and the no-service path must be fast enough that nobody is tempted to
route around the tool.

**Check.** With every background process disabled and every cache cold, a single-entity
read, a typed find, and a full-graph traversal all complete inside a stated budget.
State the budget and the corpus size it holds for. If the honest answer to "how long
without the daemon?" is measured in tens of seconds, this principle is broken.

**Cost.** Query power is capped at what can be computed from a cold walk plus cheap
derived state: no Cypher, no query planner, no index you cannot rebuild in seconds.
There is a real scale ceiling, and P4 requires naming it rather than hiding it behind a
resident process.

### P8 — kg never chooses, hosts, or calls a model

Embedding and extraction are external commands that the operator configures — a
subprocess with a documented JSON protocol on stdin and stdout. kg holds no provider
SDK, no API key, no model name, no network client. Which model runs, where it runs, and
what it costs are the operator's decisions.

**Check.** Grep the source for provider SDKs and outbound HTTP in the product path;
the answer is zero. Switching from a hosted model to a local one is a config edit and
no code change. kg with no network access still does everything except generate new
vectors.

**Cost.** kg cannot embed out of the box; every user must supply a command, and the
scaffolding for that is permanent maintenance. Dimension and quality mismatches are the
operator's problem, and kg cannot diagnose a bad model for them.

## Area D — The record: how kg's own decisions are kept

*How a settled argument stays settled.* One principle, and the only one that governs the
project rather than the software. It is what makes the other three areas auditable — an
area's principles are worth nothing if the decisions that implement them have drifted
out of date without saying so. Violation #7 lands here, and P9's own cost paragraph
names five ADRs that have already gone stale.

### P9 — Design decisions are written down before they are load-bearing

`decisions/` holds the reasoning, not just the outcome. A principle argued once should
not be re-derived from scratch; a decision that turned out wrong is a finding worth
keeping, not deleting. This document exists because 24 ADRs did.

**Check.** Every non-obvious property of the system traces to a written decision. A
change that invalidates a decision updates it in the same commit. An ADR that no longer
matches the code is a defect, not history.

**Cost.** Friction on every design change, and 24 decisions are also 24 things that can
go stale. Five already have: 0011 records Prometheus metrics, 0017 an audit log at
`.kg/.cache/audit.log`, and 0012 a `kg migrate` command — none of the three exists in
the source. 0001's Decision block still names a backend removed in 2026-04. 0024 is
still `Status: Proposed` with an unfilled `[Your Name]` placeholder while `kg similar`
has shipped. A stale ADR is worse than none, because it is cited.

---

## The agentic surface

Making the pattern useful to agentic consumers is the point of the experiment, so it is
worth naming what an agent needs that a human interface does not, and whether kg
supports it by design or by accident.

| An agent needs | Why a human interface does not | Status |
|---|---|---|
| **Byte-identical output for identical input** | Humans re-read; agents cache, diff, and put output in prompts, where instability is invisible corruption | **By design** (P6). Output is sorted before serialization. |
| **Cheap bulk reads of the whole corpus** | Humans page through; an agent extracting a training set or building a view reads *everything*, repeatedly | **By accident.** P1 makes it possible — the files are right there — but nothing in kg is aimed at it, and today the corpus is 97% derived floats, so the whole-graph read that fine-tuning needs pays a ~30× tax it should not. There is no bulk-export command. |
| **A typed write boundary with idempotency** | A human notices they ran it twice; an agent retries silently and a partial batch must converge | **By design** (P5). Versioned manifest, idempotency key, batch conflict policy. Correct, and the corruption below happened upstream of it. |
| **Self-description at runtime** | Humans read docs once; an agent lands cold in a workspace and must discover the schema, the state, and the vocabulary in one call | **By design.** `kg agent` and the MCP tool definitions exist for exactly this. It has no in-repo callers, and that is the intended audience, not dead code — the audience is the agent, not the pipeline. |
| **Failure it can act on** | A human reads the error; an agent must branch on it | **Partial.** Structured errors are decided (ADR-0018); the checks that would produce the most useful ones do not run (P3). |

The gap worth naming: **bulk extraction is the agentic use kg supports least
deliberately**, and it is the one Paul has actually used the graph for. P1 and P2
together are what make it cheap; P2 is currently violated, which is why it is not.

---

## What is *not* a principle

These are choices. Any of them may be discarded without asking. Several are currently
mistaken for principles because they appear in an ADR — an ADR records a decision, and
a decision is not a principle.

| Choice | Recorded in | The principle it was serving (and its area) | Freely replaceable by |
|---|---|---|---|
| LadybugDB, Cypher, CGO, the bundled native library | 0010, 0022 | "Reads are fast" (P4, area C) | SQLite, an in-memory map, a flat sidecar, nothing at all |
| The daemon, `net/rpc`, Unix sockets, auto-start | 0008 | "Reads are fast" (P4, area C) — it exists *only* because opening the index costs 22 s | Deleting it, if the cold path is fast enough |
| YAML | 0002 | "Plain, diffable, human-readable" (P1, area A) | JSON, TOML, Markdown + frontmatter |
| One file per entity, `.kg/entities/<type>/<id>.yaml` | 0002, 0016 | "Independently addressable and diffable" (P1, area A) | Any layout that keeps entities separately diffable |
| TOON | 0006 | "Machine-first, deterministic, token-efficient" (P6, area B) | JSON, NDJSON, anything stable and compact |
| MCP | 0007 | "Language-agnostic process boundary" (P7, area B) | Any typed stdio protocol |
| snake_case normalization | 0016 | "IDs are stable and filesystem-safe" (P3, area A) | Any rule that is total, reversible, and enforced |
| Go, cobra, the ~30-command CLI shape | 0010, 0004 | P7, area B | Any language producing a runnable binary |
| Morphism DSL (`kg morph`) | 0005 | Nothing. A feature. | Deletion |
| 384-dim all-MiniLM embeddings, cosine, HNSW | 0024, 0014 | "Semantic search exists" | Any embedding, any metric, brute force |
| Optimistic concurrency, no locks, no auto-commit | 0009 | "Git is the concurrency model" (P1, area A) | Locks, if single-writer stops holding |
| LICENSE, CODE_OF_CONDUCT, SECURITY, goreleaser, multi-platform install | — | **Deliberate intent**, not over-engineering: kg is meant to be published if it proves useful. Not a principle, but not waste either. | — |

The single most important row is the second. **The daemon is not a feature; it is the
cost of a choice made in ADR-0022.** A rewrite that removes the heavy index removes the
daemon, the daemon-status bug, the stale-PID bug, and the socket-path limit with it.

---

## Where kg violates its own principles

Every row is verified in this repository or measured on the one real corpus
(27,157 entity files, 26,892 distinct IDs).

| # | Violation | Evidence | Principle |
|---|---|---|---|
| 1 | **Embeddings are written into the entity files.** `s.entity.Attributes[field+"_embedding"] = vec` then `WriteEntity`. | `pkg/store/embed.go:199-201` and `:231-233`. On the real corpus: **643.5 MB of 659.6 MB (97.55%) is embedding floats**; content is 16.2 MB. A re-embed rewrote 8,981 files, +3.07M/−2.93M lines. **99.05%** of a typical data commit is float churn. | **P2.** ADR-0022 says the index is "a disposable cache rebuilt from YAML"; `docs/embedding.md` documents writing vectors back into YAML as intended behaviour. The two contradict each other and the code follows the doc. |
| 2 | **The read path requires the daemon in practice.** | Measured: `kg get` cold **116 s** (building a 1.34 GB index over 16 MB of content), warm-but-daemonless **22 s**, daemon-served ~20 ms. Consequence: every non-trivial consumer script bypasses kg and parses the YAML directly — including two that regex embeddings out of the files rather than call `kg similar`. | **P4**, and ADR-0008's own claim that "the daemon is never required." The bypassing is P1 working as intended; the *reason* for it is P4 broken. |
| 3 | **Identity is derived from the ID but never enforced.** `EntityPath` computes the path from the ID; `walkEntities` reads whatever it finds and never checks for collisions. | `pkg/store/yaml.go:29`, `:104`. Measured: **265 duplicate IDs shadowing 265 files that no query can ever reach**, 162 files whose directory disagrees with their `type:`, 269 whose `id` ≠ `<dir>:<basename>`. `kg stats` reports 26,891 entities over 27,157 files and no one notices. | **P3.** |
| 4 | **Validation is silent about exactly what matters.** Unknown attributes are skipped; entities with no schema are skipped entirely; relation targets are never checked. | `pkg/schema/schema.go:103` (`if !ok \|\| val == nil { continue }`), `cmd/kg/validate.go:44`. Result: **2,118 entities (7.8%) carry an attribute literally named `--dir` with the value `.kg`** — a CLI flag that became knowledge — and `kg validate` prints "all entities valid". | **P3.** The open-world *content* rule is intended; the structural silence is not. |
| 5 | **The corruption in #4 entered through an untyped write boundary.** A model was prompted to emit `kg create …` shell text; a pipeline regex-scraped it, `shlex`-split it, and treated "anything containing `=`" as an attribute. | The typed boundary already existed and was skipped: `cmd/kg/apply.go:18-36` defines a versioned manifest with an idempotency key, and the MCP tools take typed objects. | **P5**, violated by the consumer — but kg accepted the result without complaint (#4), which is kg's failure. |
| 6 | **Writes to the source of truth are not atomic; writes to the disposable index are.** `WriteEntity` truncates in place with `os.WriteFile`. | `pkg/store/yaml.go:72`, against ADR-0008's temp-file → fsync → rename for the *manifest*. The durability care is on the artifact that can be rebuilt, not on the one that cannot. | **P1** — if the files are the graph, they deserve at least the index's care. |
| 7 | **No code written after v0.9.3 has ever passed CI, and CI cannot run at all right now.** | Two separate faults. (a) The last green run on `main` is the v0.9.3 commit; the **ten runs since then all failed at the `Test` step** — checkout, ladybug download and `go vet` all pass, tests fail, everything after is skipped. Run logs have expired (HTTP 410), so the specific failure is unrecorded, and the same suite passes locally on macOS, so it is Linux-CI-specific. (b) **GitHub Actions now refuses to start jobs on this account**: a run triggered 2026-08-29 died in 2 s with *"The job was not started because recent account payments have failed or your spending limit needs to be increased."* Separately, `origin/main` is two commits behind local `main` — `48a0e04` and `77d87ff` were never pushed and have never been built. `CONTRIBUTING.md` still describes a separate CGO job from the removed dual-binary era. | **P9** — ADR-0011 records a gate that is now decorative, and (b) must be cleared before anyone can even find out why (a) happens. |
| 8 | **An 11.7 MB compiled binary is tracked at the repository root.** `kg-ladybug`, mode 100755. `.gitignore` covers `/kg` and `/bin/` but not this. | `git ls-files kg-ladybug`. The same class of leak put a stale tracked binary into the consumer repo, where it made a fresh clone unrunnable. | **P2.** |
| 9 | **kg does not dogfood itself.** There is no `.kg/` in this repository. | A knowledge-graph tool with no knowledge graph has no daily proof its own claims hold. | — |

**By area.** *A, the graph on disk:* #1, #3, #4, #6, #8 — five of the eight attributed
rows, and all three of the area's principles. *B, the interface:* #5 alone, and that one
was the consumer's failure to use the boundary, not the boundary's. *C, the runtime:* #2.
*D, the record:* #7. Row #9 is attributed to no principle.

Two things fall out of that split. **The area kg is least distinctive in is the area it
holds worst** — area A's position is the one the field has since standardised, and it is
where every structural violation lives. And **the area that distinguishes kg is the one
it mostly holds**: area B's typed write boundary was built correctly and then bypassed by
a caller, which is a different and cheaper problem than not having built it.

Noted, not fixed — this document changes no code. #7 and #8 are trivially and obviously
broken.

---

## Open questions to settle before a rewrite starts

Questions 1–6 predate the grouping. Questions 7–9 came out of it — grouping the
principles by area exposed one that may be two, one area that names no promise, and a
concern no area governs at all. They are recorded here rather than fixed, because
regrouping is not licence to add, remove, or soften a principle.

1. **Is a resident service permitted at all?** P4 says a service may optimize but never
   be required. That is only meaningful with a number attached: *what read latency, on
   what corpus size, with everything cold, counts as passing?* Until that is fixed, "no
   service required" is unfalsifiable and the daemon comes back.

2. **What is the stated scale ceiling?** ADR-0014 says 100k entities. The measured
   corpus is 27k and walks cold in under a second once derived data is evicted. If the
   ceiling is ~100k, reading everything is a legitimate architecture and most of the
   index disappears. If the ambition is millions, P4 needs rewriting first. These are
   different systems; pick one.

3. **Where does identity live — in the file or in the path?** Both are defensible. A
   slug in the file makes renames free and collisions detectable. A path-derived ID
   makes lookup O(1) with no index. The current answer is *both, unenforced*, which is
   the one option that is definitely wrong (#3).

4. **Is the on-disk format the product?** If more kg graphs should exist as examples,
   the durable artifact is the *format*, and it needs a versioned specification a second
   implementation could be written against — separate from this Go program. If the
   binary is the product, the format can keep changing quietly. This decides whether a
   rewrite is free to change the layout.

5. **Is bulk extraction a first-class command or a documented pattern?** Fine-tuning is
   a stated use and it needs the whole corpus in a training-shaped format. Either kg
   grows an export path with a stability guarantee, or P1 is the answer and the promise
   is "read the files yourself, here is the spec" — in which case #4 above is a
   precondition, not an option.

6. **Publish when?** Open-sourcing is stated intent, and it constrains the rewrite: a
   stranger must install and run kg in one command. Today that means CGO, a bundled
   native library, rpath resolution, and a release pinned to LadybugDB v0.13.1 because
   newer upstream releases ship no assets. If publication is near, "installs from a
   single static binary" is a hard constraint on the index choice. If it is far, it is
   only a preference.

7. **Is P3 one principle or two?** Grouping placed it in area A because its enforced
   half — identity is unique, declared identity agrees with location, a relation points
   at something that exists — is an invariant of the artifact, checkable on a corpus that
   no one is writing to. Its advisory half is guidance to a producer and belongs with
   area B. They are stated as one rule precisely so that nobody gives them one setting,
   which is a good reason to keep them together and not an argument that they are one
   property. Splitting them would cost a stable identifier; that trade is unsettled.

8. **Area B names three shapes and no promise.** P5 fixes what may be written in, P6 what
   comes out, P7 how a caller reaches kg — and none of them says what kg promises *not to
   break*, or over what horizon. Every other area's principles carry a durable guarantee;
   the interface carries none. Whether compatibility is a missing principle or a
   deliberately withheld one is undecided, and question 5 is the first place the absence
   bites: an export path with no stability guarantee is not an answer to bulk extraction.

9. **No area governs provenance or trust.** Nothing here says where a fact came from, who
   or what asserted it, how well corroborated it is, or when it goes stale. In a flat list
   that absence is invisible; grouped, it is the question area A cannot answer — area A
   guarantees a file's *structure* and says nothing about the standing of its *content*.
   OKF v0.2 has already designed this layer (`sources` with credibility signals,
   `generated`/`verified` actors, `status`, `stale_after`) and kg has none of it, while
   corroboration is the measured weak point of the one real corpus. Whether that is a
   principle kg is missing or a feature it has not built is the question, and it should be
   settled before question 4 is, because it changes what adopting a format buys.

---

*Written 2026-08-29 against `dcafd83`; regrouped by area the same day, with the nine
principles, their numbers, checks and costs unchanged. Corpus measurements are from the
GDKB/kg state investigation of the same date.*
