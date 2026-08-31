// Package vector stores embedding vectors in a gitignored sidecar beside the
// graph rather than inside the entity files that are the graph.
//
// Two files live under <kgDir>/.cache/:
//
//	vectors.bin  packed rows, dim float64 values each, host byte order,
//	             row i at byte offset i*dim*8 and nothing else in the file
//	vectors.idx  JSON: dimension, row count, and one entry per (entity, field)
//	             carrying the row number, the entity type, and the SHA-256 of
//	             the text that was embedded
//
// The index doubles as the staleness record: an entry whose checksum still
// matches the entity's current text needs no re-embedding, so no separate
// embed manifest is kept. The bin file is read with mmap and searched by
// brute force; at the corpus sizes ADR-0026 admits, that is faster than any
// index kg could keep warm.
//
// Both files are derived (P2). Deleting <kgDir>/.cache/ costs nothing that
// `kg sync` cannot rebuild, and no consumer may treat either as a source of
// truth. Because vectors.bin is written in host byte order it is machine-local
// by construction; a sidecar carried to a host of the opposite endianness is
// reported stale rather than misread.
package vector
