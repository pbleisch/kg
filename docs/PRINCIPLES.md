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
| **B. The interface** | P5, P6, P7 | How other programs reach kg — what may be written in, what comes out, and how a caller reaches it at all, whether by linking it or by running it | …changes a command, an output shape, a protocol, an exported library signature, or the way a producer submits facts |
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
| **A. The graph on disk** | **Yes — entirely, and this is where the decision is made.** P1 is satisfied by construction: OKF's stated design goals restate it. P2 is satisfied trivially, there being no derived layer to misplace. P3 looked refused outright — OKF §11 requires that consumers *"MUST NOT reject a bundle because of… broken cross-links"* and §6.1 that *"Consumers MUST tolerate broken links."* **[ADR-0028](../decisions/0028-load-time-structural-enforcement.md) found the conflict is narrower than that**: it is only about broken links, kg's load reports rather than refuses them, and the strictness worth keeping — identity, and the typed write boundary — is untouched by the specification. The remaining disagreement is deliberate and stated (increment 7). |
| **B. The interface** | **No.** OKF ships no reader, validator, or query tool, so there is nothing for P5–P7 to bind to. kg's write boundary, output contract and invocation contract survive the swap unchanged. |
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
specification. Five of the nine recorded violations (#1, #3, #4, #6, #8) land here.
**#1 and #8 were fixed in increment 3, #3 and #4 in increment 4**; #6 remains, so P1 is
still violated on the atomicity of writes. P3 now holds in kg — the corpus that
violated it is in another repository and is repaired there.

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
identity agrees with where it is stored, and its names are legal names. Those are hard
failures that name the offending records. **Content** — which attributes exist, their
types, cardinality, whether a relation target resolves — is advisory guidance for
agents, reported and not blocking, because a graph under construction is legitimately
incomplete.

**The severity depends on the moment.** A producer is refused for what it asserts; a
reader is not punished for what someone else deleted. So the *write boundary* also
refuses a relation whose ends do not resolve — at submission the batch contains the
edge and the producer can fix it — while a *load* reports one. That asymmetry is the
one thing this principle used to be silent about, and being silent about it is how the
repository came to carry two contradictory readings of the same rule.
[ADR-0028](../decisions/0028-load-time-structural-enforcement.md) settles it and states
the three classes in full.

**Check.** Two files claiming one identity fail the load, naming both. A file whose
declared identity disagrees with its location fails. An illegal attribute name fails.
A dangling relation target is refused at the write boundary and **reported** by a load.
An unknown attribute is a warning, and a graph full of warnings still loads.

**Cost.** Ingestion that used to succeed now fails, and existing corpora will not load
until they are repaired — measured on GDKB, 2,533 of 27,158 files. Being strict about
identity means a rename is a real migration, not a `mv`. And one read has to be exempt:
a corpus that will not load must still be walkable by the tool that repairs it, so
`store.ListEntityFiles` enforces nothing by design, and every whole-corpus rewrite has
to use it rather than a load.

## Area B — The interface: how other programs reach kg

*What may be written in, what comes out, and how a caller reaches kg at all.* Three
principles, one for each direction: P5 fixes the shape of what enters, P6 the shape of
what leaves, P7 who is allowed to reach either without changing language. This is kg's
least-contested ground — nothing comparable in the field offers a typed, enforced write
boundary — and the only recorded violation here (#5) was committed by a consumer that
skipped the boundary rather than by kg. P6 and P7 have no recorded violation, which is
partly that they hold and partly that nothing has stressed them.

**P7 was revised on 2026-08-29**, on Paul's decision: kg becomes a library with a thin
CLI over it. That is the one principle change this document has carried, and it did not
overturn its neighbours — it forced them to be more specific. With two ways in, P5 has
to say which side of the library the write boundary sits on, and P6 has to say which
exit determinism is a property of. Both clarifications are marked below.

### P5 — Every write crosses a typed boundary

Nothing becomes an entity by being parsed out of prose or a command line. Writes take
structured records — typed fields, versioned envelope, an idempotency key for batches —
so that a malformed producer fails at the boundary instead of writing nonsense that
validates.

**There is one such boundary, and P7 puts it in the library.** The manifest, the CLI and
the MCP tools are producers that enter through it, not boundaries of their own. A Go
caller gets the *typed* half from the compiler — a struct literal is more typed than a
manifest, not less — but the compiler supplies neither the versioned envelope nor the
idempotency key, and those were always the parts doing the work. They have to be in the
API's signature, refused at compile time, rather than assumed of a caller who now has no
parser standing between them and the store.

**Check.** No code path constructs an entity by string-splitting a command. Feed the
write boundary a record with an unknown field and it is rejected, not stored. Apply the
same batch twice and the graph is unchanged the second time. Nothing reachable from the
library writes an entity without crossing the same boundary the manifest path crosses,
and no exported signature lets a caller write without supplying the envelope.

**Cost.** Producers must emit structured data. The convenient shortcut — prompting a
model for CLI text and scraping it — is exactly what this forbids, and it is genuinely
easier to prompt for. This principle is the one most likely to be violated under
deadline, and §"Violations" shows what it costs when it is.

### P6 — Output is machine-first and deterministic by construction

The default output is for a program, not a terminal: structured, complete, stable field
order, token-efficient. Human formatting is a flag, not the baseline. Errors are
structured too — a machine-readable code, a message, and where possible a suggestion.

**Determinism binds the library, not the printer.** P7 gives results two exits, and "by
construction" means the order is already fixed when a result leaves the library; the CLI
inherits it and adds only presentation. A CLI that sorts on its own way out satisfies
every check below except the last one, and hands a linking caller a different answer.

**Check.** Run the same query twice on unchanged data and the bytes are identical. Pipe
any command's output into a parser without a regex. Trigger every error class and get
back structure, not prose on stderr. The same query answered through the library and
through the CLI's machine format carries the same records in the same order.

**Cost.** No per-caller output tuning and no adaptive prettiness. Deterministic ordering
constrains internal data structures — maps must be sorted before a result leaves the
library, everywhere, forever.

### P7 — No consumer is required to share kg's language

**kg is a library, and the CLI is a thin, complete cover over it.** Behaviour lives in
the library. A Go consumer links it; every other consumer runs it — a binary on `PATH`,
or a stdio protocol — and that path is not a reduced one. The guarantee is not the
process boundary. The process boundary was the *means*; the guarantee is that no
consumer is ever **required** to link kg, share its memory, or match its runtime. That
is why the only real consumer to date, written in Python and Node, could use a Go tool
at all, and it has to stay true now that a Go caller has a shorter route.

**"Thin" is a rule, not an adjective.** The CLI parses arguments, calls the library, and
formats the result. It does not validate what the library does not validate, does not
apply a default the library does not apply, and does not normalize an input the library
accepts as it stands. The failure this forbids is not an ugly CLI; it is a CLI that
accumulates behaviour of its own until two consumers of the same graph — one linking,
one invoking — disagree about what that graph contains, with neither able to see the
disagreement.

**Check.** *Language independence:* a consumer in a language kg is not written in can
perform every supported operation using only a binary and a pipe; nothing in the
contract requires an FFI, a shared object, or a matching runtime. *Completeness:* every
operation the library exports is reachable from the CLI — a library that can do things
the CLI cannot re-creates precisely the exclusion this principle exists to prevent, and
is the way the guarantee is most likely to be lost. *Thinness:* the same inputs applied
through the library and through the CLI leave a byte-identical graph and produce
byte-identical machine output, and the CLI layer holds no validation, defaulting, or
normalization that is not a call into the library.

**Cost.** Two public surfaces where there was one. The Go API acquires its own
compatibility obligation — argv plus bytes on stdout was the whole contract before, and
now every exported type and signature is contract too, breakable in a way that stops a
caller compiling rather than merely surprising a script. And **the library path can hold
state the CLI path cannot**: an open handle, a warm index, a graph resident across
calls. That is a correctness hazard the process boundary made impossible by
construction — a bug that reproduces in-process and not through the binary, or the
reverse — and it is now maintained by discipline instead. Correctness has to be
established at the library, with the CLI's agreement with it tested rather than assumed.

**Why this was revisable at all.** The previous P7 forbade the library outright, and its
own cost paragraph carried the assumption that made that affordable: no shared in-memory
state between calls, so *anything expensive to construct must be either cheap enough to
rebuild or persisted as derived state (P2)*. That was written against a read path where
construction was ruinous — a 1.34 GB index over 16 MB of content, 22 s warm and
daemonless, 116 s cold (violation #2). Throwing that away on every call is a real cost,
and a boundary that imposes it is worth promoting from *choice* to *principle* only
because the alternative — letting expensive state leak into the contract — is worse. If
the read path becomes the ~120-line cold walk the 2026 prior-art review measured, there
is nothing expensive left to amortize and that argument largely evaporates, leaving the
guarantee it was protecting, which is what P7 now states. **This is a principle that
changed because the world underneath it changed, not because it became inconvenient** —
and it rests on a number that is not settled yet. If open question 2 is answered
"millions" rather than "~100k", construction becomes expensive again and the case for
the old P7 comes back with it.

## Area C — The runtime: what has to be running

*What kg requires to be alive, and what kg is allowed to run on your behalf.* Both
principles here say the same thing in opposite directions: kg owns no resident process
of its own (P4) and hosts no model of anyone else's (P8). The cost of both lands on the
operator, and both are checkable by turning things off. P8 holds. **P4 held again from
increment 3**: the index and the daemon are deleted, a cold single-entity read is 3 ms,
and ADR-0026 states the budget and the corpus size it holds for — the number P4's own
check has always demanded and never had.

### P4 — Reading needs no running service

A read is: open files, answer. No daemon, no server, no socket, no background process
on the critical path. A service may exist as an *optimization*, never as a
*requirement*, and the no-service path must be fast enough that nobody is tempted to
route around the tool.

**Check.** With every background process disabled and every cache cold, a single-entity
read, a typed find, and a full-graph traversal all complete inside a stated budget.
State the budget and the corpus size it holds for. If the honest answer to "how long
without the daemon?" is measured in tens of seconds, this principle is broken.

**The budget and the ceiling are proposed in
[ADR-0026](../decisions/0026-read-budget-and-scale-ceiling.md) and are not yet
ratified** — open questions 1 and 2 below. Measured at 27,158 entities with every cache
cold and nothing running: single-entity read 3 ms, typed find 0.61 s over 20,704 of one
type, full-graph traversal 0.81 s, similarity search 0.10 s.

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
| ~~LadybugDB, Cypher, CGO, the bundled native library~~ | 0010, 0022 | "Reads are fast" (P4, area C) | **Discarded in increment 3** (ADR-0027) for a cold walk plus a flat vector sidecar |
| ~~The daemon, `net/rpc`, Unix sockets, auto-start~~ | 0008 | "Reads are fast" (P4, area C) — it existed *only* because opening the index cost 22 s | **Discarded in increment 3** (ADR-0027). The cold path is fast enough: 3 ms |
| YAML | 0002 | "Plain, diffable, human-readable" (P1, area A) | JSON, TOML, Markdown + frontmatter |
| One file per entity, `.kg/entities/<type>/<id>.yaml` | 0002, 0016 | "Independently addressable and diffable" (P1, area A) | Any layout that keeps entities separately diffable |
| TOON | 0006 | "Machine-first, deterministic, token-efficient" (P6, area B) | JSON, NDJSON, anything stable and compact |
| MCP | 0007 | "Full access without linking, from any language" (P7, area B) | Any typed stdio protocol |
| snake_case normalization | 0016 | "IDs are stable and filesystem-safe" (P3, area A) | Any rule that is total, reversible, and enforced |
| Go, cobra, the ~30-command CLI shape | 0010, 0004 | P7, area B. The command *count* is a choice; the CLI's **coverage** of the library is not | Any language that can be both linked and run |
| Morphism DSL (`kg morph`) | 0005 | Nothing. A feature. | Deletion |
| 384-dim all-MiniLM embeddings, cosine, HNSW | 0024, 0014 | "Semantic search exists" | Any embedding, any metric, brute force |
| Optimistic concurrency, no locks, no auto-commit | 0009 | "Git is the concurrency model" (P1, area A) | Locks, if single-writer stops holding |
| LICENSE, CODE_OF_CONDUCT, SECURITY, goreleaser, multi-platform install | — | **Deliberate intent**, not over-engineering: kg is meant to be published if it proves useful. Not a principle, but not waste either. | — |

The single most important row was the second. **The daemon was not a feature; it was the
cost of a choice made in ADR-0022.** Removing the heavy index removed the daemon, the
daemon-status bug, the stale-PID bug, the second-daemon-clobbers-the-first bug and the
socket-path limit with it — which is what increment 3 did, and what the first two rows
now record.

---

## Where kg violates its own principles

Every row is verified in this repository or measured on the one real corpus
(27,157 entity files, 26,892 distinct IDs).

| # | Violation | Evidence | Principle |
|---|---|---|---|
| 1 | ~~**Embeddings are written into the entity files.**~~ **FIXED, increment 3.** | Was: `pkg/store/embed.go:199-201`, **643.5 MB of 659.6 MB (97.55%)** embedding floats against 16.2 MB of content; a re-embed rewrote 8,981 files. Now: vectors go to `.kg/.cache/vectors.bin`, gitignored. Migrating the real corpus moved 54,148 (entity, field) pairs into 53,630 sidecar rows and took the entity files to **14.92 MB** and the packed repository from 229 MB to **13 MB**. A model change now re-embeds every vector and leaves `git status` clean. | **P2**, held. ADR-0027; `docs/embedding.md` rewritten to match. |
| 2 | ~~**The read path requires the daemon in practice.**~~ **FIXED, increment 3.** | The recorded 116 s cold was optimistic: re-measured on the same corpus it is **783.8 s**. Now, with no index and nothing running, `kg get` is **3–4 ms** cold, a whole-graph walk 0.82 s, `kg similar` 0.05–0.12 s. ADR-0026 has the table. The consumer scripts that bypassed kg no longer have a reason to. | **P4**, held. ADR-0008 and ADR-0022 superseded by ADR-0027. |
| 3 | ~~**Identity is derived from the ID but never enforced.**~~ **FIXED in kg, increment 4.** | Was: `EntityPath` computed the path from the ID and `walkEntities` read whatever it found, comparing them never. Measured on the corpus: **265 files whose `id` disagrees with `<dir>:<basename>`**, 162 whose directory disagrees with their `type:`, and **267 declared ids claimed by two files each, over 534 files** — the same 534 increment 3's embedding pass counted from the other side. Now: [ADR-0029](../decisions/0029-path-derived-identity.md) settles identity as **path-derived**, `id:`/`type:` are a checked echo, and a disagreement or a collision refuses the load naming every file. `kg validate --fix` repairs 2,528 of GDKB's 2,533 offending files in 1.8 s and **moves none** — the old `--fix` moved them to `EntityPath(id)`, which is the overwrite this row warned about, performed deliberately. | **P3**, held in kg. The GDKB repair is a separate change in a separate repository. |
| 4 | ~~**Validation is silent about exactly what matters.**~~ **FIXED, increment 4.** | Was: unknown attributes skipped (`pkg/schema/schema.go`, `if !ok \|\| val == nil { continue }`), entities with no schema skipped entirely, relation targets never checked — so **2,118 entities (7.8%) carrying an attribute literally named `--dir` with the value `.kg`** drew "all entities valid". Now `kg validate` reports both halves separately and says which is which: on the unrepaired corpus, **2,550 structural errors and 7,343 advisories**, in under a second. The `--dir` attributes are a structural refusal, because the write boundary already refuses that name and a rule that binds on the way in and not on the way out is not a rule (ADR-0028). | **P3**, held. The open-world *content* rule is intended and is now reported rather than silent. |
| 5 | **The corruption in #4 entered through an untyped write boundary.** A model was prompted to emit `kg create …` shell text; a pipeline regex-scraped it, `shlex`-split it, and treated "anything containing `=`" as an attribute. | The typed boundary already existed and was skipped: `cmd/kg/apply.go:18-36` defines a versioned manifest with an idempotency key, and the MCP tools take typed objects. | **P5**, violated by the consumer — but kg accepted the result without complaint (#4), which is kg's failure. |
| 6 | **Writes to the source of truth are not atomic; writes to the disposable index are.** `WriteEntity` truncates in place with `os.WriteFile`. | `pkg/store/yaml.go:72`, against ADR-0008's temp-file → fsync → rename for the *manifest*. The durability care is on the artifact that can be rebuilt, not on the one that cannot. | **P1** — if the files are the graph, they deserve at least the index's care. |
| 7 | **No code written after v0.9.3 has ever passed CI, and CI cannot run at all right now.** | Two separate faults. (a) The last green run on `main` is the v0.9.3 commit; the **ten runs since then all failed at the `Test` step** — checkout, ladybug download and `go vet` all pass, tests fail, everything after is skipped. Run logs have expired (HTTP 410), so the specific failure is unrecorded, and the same suite passes locally on macOS, so it is Linux-CI-specific. (b) **GitHub Actions now refuses to start jobs on this account**: a run triggered 2026-08-29 died in 2 s with *"The job was not started because recent account payments have failed or your spending limit needs to be increased."* Separately, `origin/main` is two commits behind local `main` — `48a0e04` and `77d87ff` were never pushed and have never been built. `CONTRIBUTING.md` still describes a separate CGO job from the removed dual-binary era. | **P9** — ADR-0011 records a gate that is now decorative, and (b) must be cleared before anyone can even find out why (a) happens. |
| 8 | ~~**An 11.7 MB compiled binary is tracked at the repository root.**~~ **FIXED, increment 3.** `kg-ladybug` deleted. | The name it carried is gone too: there is one `kg` binary, pure Go, and no variant to alias. | **P2**, held. |
| 9 | **kg does not dogfood itself.** There is no `.kg/` in this repository. | A knowledge-graph tool with no knowledge graph has no daily proof its own claims hold. | — |

**By area.** *A, the graph on disk:* #1, #3, #4, #6, #8 — five of the eight attributed
rows, and all three of the area's principles. *B, the interface:* #5 alone, and that one
was the consumer's failure to use the boundary, not the boundary's. *C, the runtime:* #2.
*D, the record:* #7. Row #9 is attributed to no principle.

**Status after increment 4.** #1, #2, #3, #4 and #8 are fixed; areas A's identity and
validation halves and area C hold outright. Area A is down to **#6 alone** — writes to
the source of truth are still not atomic — and P3's hold is conditional in one honest
way: kg now refuses a corpus that violates it, and the corpus that does is in another
repository and is not repaired yet. #5, #7 and #9 are untouched.

Two things fall out of that split. **The area kg is least distinctive in is the area it
holds worst** — area A's position is the one the field has since standardised, and it is
where every structural violation lives. And **the area that distinguishes kg is the one
it mostly holds**: area B's typed write boundary was built correctly and then bypassed by
a caller, which is a different and cheaper problem than not having built it.

*Originally noted, not fixed — this document changes no code. Increments 3 onward do;
each fixed row says which one, and cites the ADR.* #7 remains broken and is not this
repository's to fix.

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
   **STILL OPEN.** [ADR-0026](../decisions/0026-read-budget-and-scale-ceiling.md)
   proposes an answer — yes as an optimization, never as a requirement, with a four-row
   budget to turn it off against — and is deliberately left unratified. Increment 3 was
   built to that budget because an implementation must choose something; that must not
   be mistaken for the question being settled.

2. **What is the stated scale ceiling?** ADR-0014 says 100k entities. The measured
   corpus is 27k and walks cold in under a second once derived data is evicted. If the
   ceiling is ~100k, reading everything is a legitimate architecture and most of the
   index disappears. If the ambition is millions, P4 needs rewriting first. These are
   different systems; pick one. **P7's revision now leans on this answer too**: the case
   for a library rests on construction being cheap enough that nothing needs amortizing
   across calls, and at millions it is not.
   **STILL OPEN.** ADR-0026 proposes **100,000 entities**, with the measurement it
   projects from: 27,158 entities of 14.92 MB walk in 0.81 s, so 100k is roughly 55 MB
   and roughly 3 s. At a million it is ~30 s and `back`, `stats` and `validate` would
   need a persistent reverse index — which is the artifact that needed keeping warm.
   Unratified.

3. **Where does identity live — in the file or in the path?** Both are defensible. A
   slug in the file makes renames free and collisions detectable. A path-derived ID
   makes lookup O(1) with no index. The answer was *both, unenforced*, which is the one
   option that is definitely wrong (#3).
   **CLOSED, increment 4: the path.**
   [ADR-0029](../decisions/0029-path-derived-identity.md) records it. `id:` and `type:`
   stay in the file as a checked echo, because P1 wants a file to say what it is; a
   disagreement refuses the load. The deciding argument was that a file-authoritative
   identity needs an id→path map to answer the cheapest question kg has, and ADR-0027
   has just deleted the index and the daemon that kept one warm.

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
   stranger must install and run kg in one command. **The blocker is gone as of increment
   3**: no CGO, no bundled native library, no rpath, no LadybugDB pin — one static binary
   per platform from one cross-compiling job. The question is now only *when*, not
   *whether it is possible*.

7. **Is P3 one principle or two?** Grouping placed it in area A because its enforced
   half — identity is unique, declared identity agrees with location, a relation points
   at something that exists — is an invariant of the artifact, checkable on a corpus that
   no one is writing to. Its advisory half is guidance to a producer and belongs with
   area B. They are stated as one rule precisely so that nobody gives them one setting,
   which is a good reason to keep them together and not an argument that they are one
   property. Splitting them would cost a stable identifier; that trade is unsettled.
   **STILL OPEN, and increment 4 made the case for keeping it whole stronger.**
   [ADR-0028](../decisions/0028-load-time-structural-enforcement.md) found that P3 is
   one principle over **three** classes — identity, grammar, content — at **two**
   moments, and that the classes do not partition the same way at both: a dangling
   target is refused at the write boundary and reported by a load. Split P3 in two and
   the split has to be made twice, differently. The question the split was meant to
   answer — which area each half belongs to — turns out to have no single answer.

8. **Area B names three shapes and no promise.** P5 fixes what may be written in, P6 what
   comes out, P7 how a caller reaches kg — and none of them says what kg promises *not to
   break*, or over what horizon. Every other area's principles carry a durable guarantee;
   the interface carries none. Whether compatibility is a missing principle or a
   deliberately withheld one is undecided, and question 5 is the first place the absence
   bites: an export path with no stability guarantee is not an answer to bulk extraction.
   **P7's revision sharpens this question rather than answering it.** There are now two
   surfaces to promise about, not one, and P7's cost paragraph names the Go API's
   compatibility obligation without attaching a horizon to it — which is exactly the
   missing promise, now owed twice.

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

***Updated 2026-08-30 for increment 3.*** Violations #1, #2 and #8 are fixed and say so;
question 6's blocker is gone; questions 1 and 2 have a **proposed and unratified** answer
in ADR-0026 and are still open. **No principle's statement, number, check or cost
changed** — the point of the increment was to stop violating three of them, not to move
them.

***Updated 2026-08-31 for increment 4.*** Violations #3 and #4 are fixed and say so;
question 3 is **closed** — identity is path-derived, ADR-0029 — and question 7 has its
second piece of evidence and is still open. **P3's statement, check and cost were
edited**, and this is the one place a principle's text moved in this increment, so it
is called out: the statement and the check disagreed with each other about whether a
dangling relation target fails a load or is reported, and `docs/increments.md` §4
carried the same contradiction between its deliverable and its done-condition. The
enforced/advised split, P3's number and its area are unchanged; what was added is
**which moment each half binds at**, which is the thing the rule was silent about and
the reason it could be read two ways.
[ADR-0028](../decisions/0028-load-time-structural-enforcement.md) has the argument, the
alternatives, and what Paul would be overruling to decide it the other way.

***P7 revised 2026-08-29***, on Paul's decision — *"I like the library plus CLI model.
Feels more useful."* — from **the integration boundary is a process, not a library** to
**no consumer is required to share kg's language**. The old statement's last clause was
already the real principle; the process boundary was how it was kept. P5 and P6 carry
clarifications forced by that change and are otherwise unaltered. The other six
principles, all nine numbers, and the four areas are unchanged. Area A's serialization
question is untouched by this and remains open (question 4). *P9 wants this in
`decisions/` as well; that ADR is owed and not yet written.*
