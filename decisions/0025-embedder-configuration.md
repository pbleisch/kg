# 0025 — Where the Embedding Command Is Configured

Status: Decided

Date: 2026-08-30

Authors: Paul

Context
-------
kg does not embed. P8 says the embedder is an external command the *operator*
configures, and kg holds no provider SDK, no API key, no model name and no
network client. Everything below is about where that command is written down,
not about what it does.

Until now it was written in `.kg/config.yaml`, in the graph directory. On the
one real corpus that file said:

```yaml
embedding:
  command: ".venv/bin/python .kg/scripts/embed.py"
  fields: [name, description]
  batch: true
```

Two things are wrong with that, and only the second is obvious.

**The path is relative to the working directory.** `store.RunEmbedBatch` builds
an `exec.Command` with no `Dir` set, so `.venv/bin/python` resolves against
wherever kg happened to be started. `kg sync` from the repository root embedded;
`kg sync` from anywhere else did not — and did not say so, because
`store.syncEmbed` discarded the error and `SyncWithResult` discarded the count.

**The file travels with the data.** `.kg/config.yaml` is committed. It names a
virtualenv that exists on one machine, in one checkout, at one moment. The
moment the graph moves — which is exactly what increment 3 does, splitting a
659.6 MB corpus into a 15 MB data repository and a code repository holding the
pipeline and the `.venv` — the path stops resolving. `kg sync` then embeds
nothing, silently, and the first symptom is `kg similar` quietly going stale.

`docs/increments.md` names three ways out.

1. **A kg-side built-in embedder.** Refused: P8 forbids kg choosing, hosting or
   calling a model, and this would be all three.
2. **An absolute path configured per checkout.**
3. **Move the configuration to the code repository.**

Decision
--------
Take 2 and 3. They are not alternatives — they are the same rule seen from two
sides, and one mechanism serves both.

**The embedding command is operator configuration, not graph content.** kg no
longer reads `embedding:` from `.kg/config.yaml`. It resolves the embedder from,
first hit winning:

1. `KG_EMBED_COMMAND`, with `KG_EMBED_FIELDS` and `KG_EMBED_BATCH`. This is
   option 3: a pipeline in the code repository exports it and points kg at its
   own virtualenv, writing nothing into the data repository.
2. `<kgDir>/embedder.yaml` — option 2. Gitignored, written per checkout,
   holding absolute paths. `kg embed --init` scaffolds it and adds both it and
   `.cache/` to the graph's `.gitignore`.

**A working-directory-relative command is refused, not resolved.** Not the
program, and not its arguments: `python .kg/scripts/embed.py` is exactly as
CWD-dependent as `.venv/bin/python` is. The command's first word must be an
absolute path or a program on `PATH`, and every path-shaped argument must be
absolute. Resolving these against `kgDir` instead would work today and break
the next time the graph moves; resolving them against the CWD is the bug.

**A graph that still configures embedding in `config.yaml` fails loudly.** Not
"is ignored": ignoring it leaves a file that reads as configuration and does
nothing, which is the failure this decision exists to end. `ResolveEmbedder`
returns an error naming the file, printing the `embedder.yaml` to write, and
naming this ADR.

**`kg sync` with no embedder exits non-zero.** A graph with no embedder has no
derived data to build, and `--no-embed` is how an operator says so on purpose.
Exiting 0 having embedded nothing is how this went unnoticed for so long.

`.kg/config.yaml` survives with nothing in it that kg reads. It stays because a
graph directory should say what it is, and because the `backend:` key it also
held went away with the index (ADR-0027).

Consequences
------------
- **Positive:** A graph can be moved, cloned, or split without its embedding
  configuration silently ceasing to work. What breaks now is a resolution error
  at the point of use, naming the file and the fix.
- **Positive:** The data repository stops carrying a machine-specific path, so
  two people cloning the same corpus no longer overwrite each other's config to
  make it run.
- **Positive:** kg can say where its embedder came from. `kg embed --show`
  prints the source, the command, the fields and the digest, so "no vectors" is
  distinguishable from "nobody configured an embedder" without guessing.
- **Negative:** Every existing corpus must migrate. It is one file to write and
  one block to delete, and the error message contains both, but it is a break
  and it is not automatic.
- **Negative:** `embedder.yaml` is per checkout, so a fresh clone has no
  embedder until someone writes one. That is the honest state — the previous
  arrangement only *looked* like it travelled.
- **Negative:** kg now refuses commands that would have worked, for people who
  always run from one directory. The refusal is the point; being wrong later is
  worse than being inconvenient now.

Related
-------
- P8 (kg never chooses, hosts, or calls a model) — the constraint that rules
  option 1 out.
- P2 (derived data never lives in the source of truth) — ADR-0026 and ADR-0027
  are the rest of increment 3.
- ADR-0024 (CLI similarity search) — `kg similar` reads what this configures.
