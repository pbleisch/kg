# Embedding Guide

kg supports automatic vector embedding generation for entity attributes
via an external command. This enables semantic similarity search with
`kg similar`.

## Quick Start

```bash
# 1. Scaffold the default embedding script
kg embed --init

# 2. Install Python dependencies
pip install -r .kg/scripts/requirements.txt

# 3. Generate embeddings for all entities
kg embed

# 4. Search by similarity
kg similar Person --field name --like person:alice
```

## How It Works

1. You configure which attributes to embed and an external command in
   `.kg/config.yaml`.
2. `kg embed` computes a content checksum for each configured attribute
   on each entity. If the text has changed since the last embedding, it
   sends the text to your command and stores the returned vector as a
   `<field>_embedding` attribute on the entity.
3. `kg similar` uses these stored vectors for cosine/dot-product search.

Embeddings are tracked in `.cache/embed-manifest.json` so unchanged
entities are skipped on subsequent runs.

## Configuration

Add an `embedding` section to `.kg/config.yaml`:

```yaml
backend: auto

embedding:
  command: "python3 .kg/scripts/embed.py"
  fields: [name, description]
  batch: true
  auto: false
```

| Field     | Required | Description |
|-----------|----------|-------------|
| `command` | yes      | External command to run (receives JSON on stdin) |
| `fields`  | yes      | Entity attributes to embed |
| `batch`   | no       | Use batch protocol (default: false) |
| `auto`    | no       | Auto-embed after create/set/merge (default: false) |

## Commands

### `kg embed`

Generate embeddings for all entities with stale or missing vectors.

```bash
kg embed              # embed stale entities
kg embed --rebuild    # force re-embed everything
kg embed --dry-run    # show what would be embedded
kg embed --json       # JSON output
```

### `kg embed --init`

Scaffold a working embedding setup:

- Creates `.kg/scripts/embed.py` (uses `sentence-transformers` all-MiniLM-L6-v2)
- Creates `.kg/scripts/requirements.txt`
- Adds `embedding:` config to `.kg/config.yaml`

The default script runs locally on CPU with no API key needed. First run
downloads ~80MB model.

## Auto-Embedding

When `auto: true`, embeddings are automatically generated after these
commands modify entity content:

- `kg create` / `kg create --upsert`
- `kg set`
- `kg merge`
- `kg apply` (for create/upsert/set/merge actions)

Only the affected entity is re-embedded, not the whole graph.

Commands that only change relations (`kg link`, `kg unlink`) or remove
entities (`kg delete`) do not trigger auto-embedding.

## External Command Protocol

Your embedding command receives JSON on stdin and must write JSON to stdout.

### Single Mode (default)

**stdin:**
```json
{
  "text": "Alice Smith",
  "field": "name",
  "entity_id": "person:alice",
  "type": "Person"
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
    {"text": "Alice Smith", "field": "name", "entity_id": "person:alice", "type": "Person"},
    {"text": "Bob Jones", "field": "name", "entity_id": "person:bob", "type": "Person"}
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

Vectors are stored as entity attributes with the `_embedding` suffix:

```yaml
id: person:alice
type: Person
attributes:
  name: Alice Smith
  name_embedding: [0.1, 0.2, 0.3, ...]
```

These attributes are regular YAML values — they're versioned in git,
visible in `kg get`, and indexed by `kg sync`.
