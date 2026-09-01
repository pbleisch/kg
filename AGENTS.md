# Agent Instructions

> **Note for human contributors:** This file provides instructions for AI
> coding agents (GitHub Copilot, Claude, etc.) that work on this repository.
> If you're a human contributor, see [CONTRIBUTING.md](CONTRIBUTING.md) instead.

## Before a structural change

[docs/PRINCIPLES.md](docs/PRINCIPLES.md) records the properties any rewrite of kg must
preserve, what each costs, and which current choices (YAML, TOON, MCP) are explicitly
*not* principles and may be discarded freely — LadybugDB and the daemon were two such
choices and are gone. It also lists
where the code currently violates its own principles. The nine principles are grouped
into four areas — the graph on disk, the interface, the runtime, the record — so find
the area your change lands on first: the principles in play are that group's two or
three, not all nine. Read it before changing storage, the index, the write boundary, the
library API, or the CLI surface; individual decisions live in `decisions/` as ADRs.

P7 was revised on 2026-08-29: kg is a library with a **thin, complete** CLI over it. The
CLI composes library calls and adds only presentation — it must not hold validation,
defaults, or normalization the library lacks, and it must expose every operation the
library does. Read P7 before adding behaviour under `cmd/`.

P3's two halves were settled on 2026-08-31: [ADR-0028](decisions/0028-load-time-structural-enforcement.md)
says what a load refuses and what it merely reports, and
[ADR-0029](decisions/0029-path-derived-identity.md) settles identity as path-derived.
Read both before touching a read path, the write boundary, or anything that walks the
corpus.

[ADR-0030](decisions/0030-attribute-merge-and-the-unit-of-reset.md) (2026-09-01,
**unratified**) is what happens when two claims on one identity have to become one
entity, and it makes `.kg/schema/` load-bearing: an attribute the schema declares
`type: list` is multi-valued, and that decides whether a second document's items are
kept or destroyed. Read it before touching a merge, the fold, or anything that reads an
attribute value. It also restates ADR-0026's number.

[docs/increments.md](docs/increments.md) is the current plan — numbered increments with
goals, deliverables and done-conditions, covering both kg and the GDKB ingestion
pipeline, plus what is deliberately not being built and which increment forces each of
PRINCIPLES.md's open questions. Read it before starting work that sounds like a
milestone; it is edited as increments land, so it is a plan and not an ADR.

Build and test are plain Go — `mage build`, `mage check`. No CGO, no native library:
increment 3 deleted LadybugDB, the index and the daemon (ADR-0027), and CI builds with
`CGO_ENABLED=0` so a C dependency cannot creep back in. **Verify locally — CI is not a
usable signal.** Every run on `main` since v0.9.3 fails at the `Test` step (Linux-only;
the suite passes on macOS), and as of 2026-08 GitHub Actions will not start jobs on this
account at all for billing reasons.

## Identity, and what a load refuses

**A file's location is its identity** (ADR-0029): `entities/<type>/<local>.yaml` is
`<type>:<local>`, percent-decoded. `id:` and `type:` stay in the file as a *checked
echo* — a loader fills them in when they are absent and refuses when they disagree.
`store.IdentityAt` derives one; `store.CheckEntityFile` and `store.CheckPath` check one.

Every read enforces P3's load half and returns a `*store.LoadError` naming every
offending file — use `store.LoadRefused(err)` to branch on it, and never treat a refusal
as a miss. **`store.ListEntityFiles` is the one read that enforces nothing**, paired with
`store.RewriteEntityFile`: a corpus that will not load must still be walkable by the
tool that repairs it, and by `query.Validate`, which is the report that says what is
wrong with it. `store.WriteEntity` and `store.WriteEntityAt` refuse what a load refuses,
so kg cannot write a file it will not read back.

What fails: an identity two files claim, a declared `id:`/`type:` that disagrees with
the path, an attribute name outside `[a-z][a-z0-9_]*`, a filename kg would not have
written, a file that will not parse. What is reported: unknown attributes, schema
violations, and **dangling relation targets** — refused at the write boundary, reported
by a load, and ADR-0028 has the argument for the asymmetry. There is no flag that moves
a rule between the two; P3 forbids them sharing a setting.

## Reading, and the derived data

There is no index and no daemon. A read opens files, and which files depends on the
read: `store.ReadEntity` for a point read, `store.FindByType` for a typed find,
`store.Loader` for traversal (reads as it reaches, caches what it read), and
`store.LoadGraph` — a whole-corpus walk building ids, types and the reverse-edge map —
only for the reads that genuinely need every entity: `back`, `stats`, `resolve`, merge.
Do not reach for `LoadGraph` in a read that does not need it; that is how a cheap read
stops being cheap.

Vectors live in a gitignored sidecar, `pkg/vector`: `.kg/.cache/vectors.bin` plus
`vectors.idx`. Never in an entity file — `pkg/batch` strips `*_embedding` attributes at
the write boundary, with a warning. `kg similar` scans the sidecar by brute force.

The embedding command is **operator configuration, not graph content**: `KG_EMBED_COMMAND`
or the gitignored `.kg/embedder.yaml`, absolute paths only, and kg refuses an
`embedding:` block in `.kg/config.yaml` with the migration in the error text (ADR-0025).
`kg sync` is the one verb that rebuilds it — `--init` scaffolds an embedder, `--show`
says which one resolved, and named entity ids restrict what is re-embedded. `kg embed`
was the same call under a second name and is gone (ADR-0031).

[ADR-0026](decisions/0026-read-budget-and-scale-ceiling.md) states the read budget and
the scale ceiling and is **deliberately unratified** — PRINCIPLES.md open questions 1
and 2 are Paul's. Cite it for the numbers; do not treat it as settled.

## The write boundary

`pkg/batch` is the only way anything becomes an entity. `batch.Apply` takes an
[`Envelope`](pkg/batch/envelope.go) — version, idempotency key and contributor,
buildable only through `NewEnvelope` / `NewEnvelopeFrom` — and typed records.
`kg apply`, the write commands (`create`, `set`, `unset`, `link`, `unlink`,
`delete`, `merge`) and the MCP write tools are producers that enter through it; none
holds validation, defaulting or normalization of its own (P7).

Structure is refused, content is warned (P3): illegal attribute names, a type
that disagrees with its id, a relation whose end resolves to nothing. **A
refused batch writes nothing at all** — validation runs over the whole batch
first, and while records apply a `store.Journal` remembers what each file held
so `--on-error=fail` can put the graph back.

Two behaviours changed with it and will surprise: `command.Link` no longer
mints a typeless stub for a missing source, and `kg apply` defaults to
`--on-error=fail`. Both were how entities nobody submitted got into the graph.

**Entity resolution is not part of a write.** `--on-conflict dedup`, `--dedup-fields`
and `--dedup-threshold` were deleted in increment 4: they embedded on the critical path
of every write and compared against a sidecar `kg sync` refreshed *after* the apply, so
resolution was permanently one document behind. `command.Resolve` / `kg resolve` is one
whole-corpus pass over a **complete** index — it refuses a corpus that does not load and
refuses an entity whose text has no vector — and it reports pairs and merges nothing.

Writes no longer sync anything — there is nothing to sync. A batch does refresh the
vector sidecar for the entities it touched, when an embedder is configured.

Applying one idempotency key twice applies it once — the ledger is
`.kg/.cache/apply/<key>.json`. It does **not** cover a re-ingest: the key is a
hash of the manifest bytes, so a re-extracted document is a new key.
Replace-by-contributor is what makes that converge.

## One merge rule, in one place

`pkg/merge` states what happens when two claims on one identity become one
entity, and it is the **only** place that states it (ADR-0030). Both verbs call
it: `command.UpsertEntity` and `command.Merge`. Do not restate it in a caller —
`pkg/batch` held a second copy of upsert's rule and `kg merge` held an opposite
one, which is the defect the ADR names.

- **scalar**: the incoming value wins, and overwriting a *different* one warns.
- **`type: list`**: multi-valued. Items are `{text, source}`; a write replaces
  the items **its own contributor** wrote and leaves every other one's standing.
  A scalar at such a field is a one-element list; **a string is never split on a
  separator**.
- **relation**: union.

Two records of one batch claiming one identity are **folded into one** before
validation (`pkg/batch/fold.go`), not refused — that refusal cost the pilot
three of three multi-batch documents. A `create` under `--on-conflict=fail` is
still refused; that is all `identity_collision` means now.

An item carrying no source is never replaced by anybody, so a hand-authored edit
survives every re-ingest and only `kg unset` removes it. That is why `unset`
exists.

Never read an attribute value with `fmt.Sprintf("%v", …)`. Use
`store.AttrText` (one string) or `store.AttrTexts` (one per item): `%v` renders
a multi-valued field as `[map[source:… text:…]]`, and that is the string every
vector would be built from.

## The CLI surface, and both halves of P7

[ADR-0031](decisions/0031-cli-surface-reduction.md) deleted `import`, `morph`, `dedup`,
`watch` and `embed` (2026-09-01) and says why each one had to go. Before proposing
another deletion, read P7's **count** clause: *"the command count is a choice; the CLI's
coverage of the library is not"*. A thin wrapper over one library call is P7 working, not
P7 violated — `get`, `find`, `back`, `path`, `stats`, `describe` and everything under
`kg agent` are the coverage half and are not candidates. What convicts a command is
holding validation, defaulting or normalization the library lacks, writing outside
`pkg/batch`, or being a second name for a call another verb makes.

The same principle runs the other way, and that direction is the one PRINCIPLES.md says
is likeliest to be lost: **a command must not hold behaviour no library call exposes.**
Scaffolding is a library call — `store.Init` and `store.ScaffoldEmbedder` — because
`kg init` and `kg embed --init` used to hold all of it in `cmd/`, so a Go consumer could
read and write a graph but not create one.

Every deleted verb has a test asserting it stays deleted
(`TestRemovedCommandsAreGone`, the `TestDaemonCommandIsGone` shape), and
`TestGeneratedSkillNamesOnlyRealCommands` fails if the generated `SKILL.md` names a
command kg does not have. **The agent surface must report what the human surface
reports:** `kg agent check` and `kg agent status` once said `healthy: true` and
`entity_count: 0` on a corpus `kg validate` refuses with exit code 2, because both
dropped a `*store.LoadError` and read the empty list as an empty graph. `check` calls
`query.Validate` now. Never branch on `err == nil` after a read; branch on
`store.LoadRefused`.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create GitHub issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
4. **Clean up** - Clear stashes, prune remote branches
5. **Verify** - All changes committed AND pushed
6. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.

One trap worth knowing: 265 files in the real corpus declare an `id` that disagrees
with their path. Anything that walks the corpus and rewrites what it finds must write
back to the file it read — `store.ListEntityFiles` and `store.RewriteEntityFile` — not
to `EntityPath(id)`, which skips one file and overwrites another. `kg validate --fix`
does this correctly and moves no file; the version before increment 4 moved all 265,
which is the trap performed deliberately.
