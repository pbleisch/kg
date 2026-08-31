# Embedding Guide

kg generates vector embeddings for entity attributes by running a command you
configure, and searches them with `kg similar`. kg never chooses, hosts or calls
a model itself (P8): which model runs, where, and at what cost are yours.

The vectors live in a gitignored sidecar beside the graph, never in the entity
files (P2).

## Quick Start

```bash
# 1. Scaffold an embedding script and the file that names it
kg embed --init

# 2. Install its dependencies
pip install -r .kg/scripts/requirements.txt

# 3. Check what kg resolved, and from where
kg embed --show

# 4. Build the vector sidecar
kg sync

# 5. Search by similarity
kg similar person --field name --like person:alice
```

## How It Works

1. You configure which attributes to embed and an external command — see
   [Configuration](#configuration) below.
2. `kg sync` computes a SHA-256 of each configured attribute's text on each
   entity and compares it against what the sidecar recorded. Text that has not
   changed is carried over untouched; text that has is sent to your command,
   and the vector that comes back is written to the sidecar.
3. `kg similar` scans the sidecar. The scan is exact and brute-force: 9 ms over
   54,000 vectors on the one real corpus, which is faster than any index kg
   would have to keep warm (see
   [decisions/0026](../decisions/0026-read-budget-and-scale-ceiling.md)).

Two files under `.kg/.cache/` hold everything:

| file | contents |
|---|---|
| `vectors.bin` | packed rows of `dim` float64s, row *i* at byte offset `i*dim*8`, host byte order, and nothing else |
| `vectors.idx` | JSON: dimension, row count, and one entry per (entity, field) with its row, the entity's type, and the SHA-256 of the text it was built from |

Both are derived. Delete `.kg/.cache/` and `kg sync` rebuilds it; `git status`
is clean afterwards.

The index doubles as the staleness record, so there is no separate embed
manifest. It also records a digest of the embedder that produced it — the
command string plus the contents of any script the command names — so changing
the model invalidates the vectors even though every text checksum still matches.
kg cannot detect every model change: a hosted model swapped behind the same
endpoint leaves the digest alone. `kg sync --rebuild` is the answer to that, and
P8 is why kg cannot do better.

## Configuration

The embedding command is **operator configuration, not graph content**: it names
a program on one machine. It does not go in `.kg/config.yaml`, which travels
with the graph. See
[decisions/0025](../decisions/0025-embedder-configuration.md) for the argument
and for what went wrong when it did.

kg resolves it from, first hit winning:

**1. The environment** — how a pipeline in a separate code repository points kg
at its own virtualenv without writing into the data repository:

```bash
export KG_EMBED_COMMAND="/abs/path/.venv/bin/python /abs/path/embed.py"
export KG_EMBED_FIELDS="name,description"
export KG_EMBED_BATCH=1
```

**2. `.kg/embedder.yaml`** — gitignored, written per checkout:

```yaml
command: "/usr/bin/python3 /abs/path/to/.kg/scripts/embed.py"
fields: [name, description]
batch: true
```

| Field     | Required | Description |
|-----------|----------|-------------|
| `command` | yes      | External command to run (receives JSON on stdin) |
| `fields`  | yes      | Entity attributes to embed |
| `batch`   | no       | Use batch protocol (default: false) |

**Paths must be absolute**, or a bare program name on `PATH` — and that applies
to the arguments too. `python .kg/scripts/embed.py` is refused, because it
resolves against whatever directory kg happens to be run from: `kg sync` from
one place would embed and from another would not.

`kg embed --show` prints what kg resolved and which source it came from, so
"no vectors" is never a guess.

## Sync

`kg sync` is the one command that rebuilds every derived artifact. It:

1. Reads the graph.
2. Compares each configured attribute's checksum against the sidecar.
3. Calls the external command for the stale ones, in a stable order.
4. Writes a new sidecar: fresh vectors, plus the unchanged ones carried across.

```
27158 entities; vectors: 54164 embedded, 0 carried, 0 dropped
```

A graph with **no embedder configured has no derived data to build**, and
`kg sync` exits non-zero saying so. Pass `--no-embed` to say it deliberately.
Exiting 0 having embedded nothing is how this used to go unnoticed.

```bash
kg sync                     # bring the sidecar level with the graph
kg sync --rebuild           # discard it and re-embed everything
kg sync --dry-run           # report what would happen
kg sync --no-embed          # this graph has no vectors, and that is on purpose
kg embed --show             # what kg resolved, and from where
kg embed --dry-run          # same, for the embed pass alone
kg embed --json             # JSON output
kg embed <id>...            # re-embed named entities only
```

## Migrating a corpus that has vectors in its YAML

Corpora written before vectors moved out carry them as `<field>_embedding`
attributes. Harvest them into the sidecar — no model run needed:

```bash
kg sync --migrate-vectors --dry-run   # see what would move
kg sync --migrate-vectors             # move them, and rewrite the files without
```

The sidecar is written before any entity file is touched, so an interrupted
migration leaves the vectors in both places rather than neither. On the one real
corpus this moved 54,148 vectors and took the entity files from 659.7 MB to
14.9 MB, with the packed git repository going from 229 MB to 13 MB.

`kg sync --strip-embeddings` deletes them instead of harvesting them, for a
corpus you intend to re-embed anyway.

## External Command Protocol

Your embedding command receives JSON on stdin and must write JSON to stdout.

### Single Mode (default)

**stdin:**
```json
{
  "text": "Alice Smith",
  "field": "name",
  "entity_id": "person:alice",
  "type": "person"
}
```

**stdout:**
```json
[0.1, 0.2, 0.3, ...]
```

### Batch Mode (`batch: true`)

**stdin:**
```json
{
  "items": [
    {"text": "Alice Smith", "field": "name", "entity_id": "person:alice", "type": "person"},
    {"text": "Bob Jones", "field": "name", "entity_id": "person:bob", "type": "person"}
  ]
}
```

**stdout:**
```json
{
  "vectors": [
    [0.1, 0.2, ...],
    [0.3, 0.4, ...]
  ]
}
```

## Writing a Custom Embedding Command

Any program that reads JSON from stdin and writes a vector to stdout works.
Examples:

**OpenAI API (bash + curl):**
```bash
#!/bin/bash
TEXT=$(jq -r .text)
curl -s https://api.openai.com/v1/embeddings \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d "{\"model\":\"text-embedding-3-small\",\"input\":\"$TEXT\"}" \
  | jq '.data[0].embedding'
```

**Ollama (local):**
```python
import json, sys, requests
data = json.load(sys.stdin)
resp = requests.post("http://localhost:11434/api/embeddings",
    json={"model": "nomic-embed-text", "prompt": data["text"]})
json.dump(resp.json()["embedding"], sys.stdout)
```

## Storage

Vectors are **not** entity attributes. An entity file holds what someone
authored:

```yaml
id: person:alice
type: person
attributes:
  name: Alice Smith
```

and the vector for `person:alice`'s `name` lives at a row in
`.kg/.cache/vectors.bin`, found through `.kg/.cache/vectors.idx`. Nothing under
`.kg/.cache/` is committed, and nothing there is a source of truth.

`kg get --include-embeddings` still shows `<field>_embedding` attributes if a
corpus has not been migrated yet; on a migrated corpus there are none to show.

### Why the sidecar is machine-local

`vectors.bin` is written in host byte order and mapped in directly, which is
what makes the scan fast. A sidecar carried to a host of the opposite
endianness is reported stale and rebuilt rather than misread. That is not a
limitation to work around: the file is derived, it is gitignored, and it is
never meant to travel.
