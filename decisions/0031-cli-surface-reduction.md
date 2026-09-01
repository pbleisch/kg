# 0031 — CLI Surface Reduction: five verbs out, two bodies into the library

Status: Decided

Date: 2026-09-01

Authors: Paul

Supersedes: 0005 (morphisms DSL), 0023 (kg watch event stream).
Amends: 0004 (CLI grammar), 0025 (embedder configuration — the scaffolding verb
is now `kg sync --init`).

Context
-------
kg had 28 top-level commands. A scout report measured every one of them against a
scratch graph — callers, contract surface, and what each duplicated — and found that
the count was not the problem. Five commands were: each either could not be used, wrote
outside the write boundary, or was a second name for a call another verb already made.

The temptation was to read P7 as "fewer commands is thinner". It says the opposite.
`docs/PRINCIPLES.md:363`: *"The command count is a choice; the CLI's coverage of the
library is not."* A thin wrapper over one library call is P7's **completeness** half
working correctly, and deleting one because a Go caller could link the library instead
is precisely the exclusion P7 exists to prevent. What P7's **thinness** half forbids is
a CLI that validates, defaults or normalizes where the library does not.

Both halves convicted something here, in opposite directions.

Decision
--------

**Deleted.**

- **`kg import`.** It parsed one file and wrote the whole entity with
  `store.WriteEntity`, so every relation absent from that file was destroyed. It never
  entered `batch.Apply` — no envelope, no idempotency key, no journal — held its own ID,
  type and relation normalization plus `schema.ValidateEntity` in `cmd/`, and made a
  schema violation a **hard error where the boundary warns**, which is a rule crossing
  the enforced/advised line P3 forbids sharing a setting. A fifth defect: the guard
  meant to let a type with no schema pass used `os.IsNotExist` on an error
  `schema.LoadSchema` wraps, and `os.IsNotExist` does not unwrap, so it never fired.
  `kg apply` with an `upsert` action does the job non-destructively.
- **`kg morph`** and `pkg/query/morphism.go`. See [0005](0005-morphisms-dsl.md).
- **`kg dedup`** and `pkg/command/dedup.go`. Increment 4 replaced its argument with
  `kg resolve`: it reported "no duplicates found" and exited 0 on a corpus where it had
  compared nothing, and merged in a loop outside `pkg/batch`, walking the whole corpus
  once per duplicate.
- **`kg watch`** and `internal/watch/`. See [0023](0023-kg-watch-event-stream.md).
- **`kg embed`**, folded into `kg sync`. `store.Embed` is `ResolveEmbedder` +
  `EmbedWith`; `store.Sync` is that plus a count. Neither command was a superset of the
  other. `sync` survives because it is the name the pipeline calls and the docs point
  at; `--init`, `--show` and optional entity ids came across with it.

None of the five had an MCP tool, a line in the generated `SKILL.md`, or a caller in the
pipeline. `kg watch` had two pipeline callers and one of them was already dead.

**Fixed, not deleted.**

- **`kg agent check` and `kg agent status`** reported a healthy graph on a corpus
  `kg validate`, `kg find` and `kg stats` all refuse with exit code 2. Both walked with
  `store.ListEntities`, dropped the `*store.LoadError` it returned, and read the empty
  list that came back as an empty graph. That is violation #4's exact class on the one
  surface whose audience is an agent that believes it. `check` now calls
  `query.Validate` — the same call `kg validate` makes, so the two cannot disagree
  again — and keeps P3's halves apart: structural findings refuse and exit 2,
  advisories are reported and refuse nothing.
- **`kg merge`** built its write by calling `command.Merge` directly, which writes with
  `store.WriteEntity` and `store.DeleteEntity`. It was the last verb outside the
  boundary: a merge that died halfway left referrers pointing at an entity that no
  longer existed. `batch.Apply` has had `OpMerge`, with the envelope, the ledger and the
  journal, since the boundary was built. `batch.MergeEntities` is the producer; the verb
  and its argv are unchanged.

**Moved into the library.**

- **`store.Init`** and **`store.ScaffoldEmbedder`**. `kg init` and `kg embed --init`
  held every byte of graph scaffolding in `cmd/`: the directory layout, the
  `config.yaml` text, both `.gitignore` files, the embedder script. A Go consumer that
  links kg could read and write a graph but could not create one. This is P7's
  completeness half failing in the CLI→library direction, which `PRINCIPLES.md:218-220`
  names as the way the guarantee is most likely to be lost, and it is the first time it
  has been caught. Both commands stay and hold only presentation.

Consequences
------------
The command count fell from 28 to 23 and the CLI's coverage of the library rose. Two
direct dependencies left with `kg watch` — `fsnotify` and `golang.org/x/term`, with
`golang.org/x/sys` falling out as an unneeded indirect — so `go.mod` is cobra + pflag +
yaml + mage.

Two contract details had to move so `kg merge`'s argv could survive the rewrite:
`Record.RequireSameType` makes `--cross-type` the boundary's decision rather than a
check `cmd/` holds, and it is **opt-in**, so a manifest that omits it merges across
types exactly as before; `Result.Merges` reports what a merge moved, which `kg apply`
gains too, and a `--dry-run` now previews each merge instead of returning before it
looks.

A removed verb is a contract change for some agent somewhere even when no caller can be
found, so each deletion carries a test in the `TestDaemonCommandIsGone` shape, and one
test asserts the generated `SKILL.md` names no command kg does not have.

**What was deliberately kept.** `set`, `link`, `unlink`, `create`, `delete`, `describe`,
`similar`, `stats`, `back`, `path`, and everything under `kg agent`. Several have zero
in-repo callers; for the agent surface that is the intended audience and not dead code,
as `docs/PRINCIPLES.md` already says. They are P7's coverage half, and deleting them
would force every consumer to hand-write manifests.

**Follow-up owed outside kg.** The pipeline's dashboard spawns `kg watch --daemon` and
its two entry points call `kg daemon status`; all three name commands that no longer
exist, and the dashboard has been receiving no live updates since increment 3 without
anyone noticing. Its TUI reads `kg watch --human` into a one-line status ticker, which
now stays blank. Those are changes in the consumer's repository, not this one.
