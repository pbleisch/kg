# 0024 — CLI Command for Similarity/Vector Search

**Status:** Proposed  
**Date:** 2026-02-23  
**Authors:** [Your Name]

## Context

The kg CLI currently supports attribute-based filtering for entity search. The LadybugDB backend (used in the `kg-ladybug` binary) is a property graph database with Cypher query support and can support advanced features such as vector (embedding) search for similarity queries. However, the CLI does not expose any vector or similarity search capabilities.

## Decision


Introduce a dedicated `kg similar` command for similarity/vector search. This command will:

- Search for entities most similar to a given vector (embedding).
- Search for entities similar to a reference entity (by ID).
- Specify one or more vector fields/properties to use for similarity (supporting multiple fields per query).
- Choose the similarity metric (cosine, dot product, etc., as supported by the backend).
- Limit the number of results.


### CLI Syntax

Example usage:

```bash
kg similar <type> --vector "0.12,0.34,0.56,..." --field embedding --top 10
kg similar <type> --like <type:id> --field embedding --top 5
kg similar <type> --vector-file query.vec --field embedding --metric cosine --top 20
kg similar <type> --vector-file query.vec --field title_embedding,body_embedding --top 10
```

- `--vector`: Query vector as a comma-separated list (for small vectors or demos).
- `--vector-file`: Path to a file containing the query vector (recommended for real use).
- `--like`: Reference entity ID to use its vector.
- `--field`: One or more logical or embedding field names, comma-separated (e.g., `--field name,description` or `--field title_embedding,body_embedding`).
	- If a logical/text field is specified (e.g., `name`), kg will automatically resolve it to the corresponding embedding field (e.g., `name_embedding`) if available, based on naming conventions or schema metadata. This allows users to specify familiar field names without needing to know the exact embedding property.
  
## Field Resolution Behavior

When a user specifies a logical/text field (such as `name` or `description`) with `--field`, kg will attempt to resolve it to the appropriate embedding/vector field (such as `name_embedding` or `description_embedding`). This mapping should follow a clear convention (e.g., appending `_embedding`) or use schema metadata if available. If no embedding field is found for a given logical field, kg should return a helpful error message.

This approach keeps the CLI intuitive and user-friendly, allowing users to work with familiar field names while leveraging vector search capabilities under the hood.
- `--metric`: Similarity metric (default: cosine).
- `--top`: Number of results to return.

When multiple fields are specified, the backend computes similarity for each field and returns the top N entities by the highest similarity across any of the specified fields. Output may indicate which field matched best for each result. If fields have incompatible dimensions, those fields are skipped or an error is returned.


### Implementation Notes

- Only available when using the LadybugDB backend.
- Requires backend support for vector storage and similarity search, including searching across multiple fields.
- Fallback to error if the backend does not support vector search.
- Extend the backend interface to support these options, including iterating over multiple fields and selecting the best match per entity.
- Support both direct vector input and file-based input for flexibility.

## Consequences


**Positive:**
- Enables semantic and similarity search use cases (e.g., recommendations, semantic retrieval).
- Allows searching across multiple embeddings per entity for richer results.
- Leverages LadybugDB’s advanced capabilities.
- Keeps CLI consistent and user-friendly.
- Clear separation of concerns between attribute-based and similarity-based search.

**Negative:**
- Increases CLI and backend complexity.
- Requires additional testing and documentation.

**Neutral:**
- LadybugDB is the only backend; all users have vector search available.

## Alternatives Considered

- Overloading `kg find` with vector options (less clear, more cluttered).
- Relying on external tools for similarity search (less integrated).

---

