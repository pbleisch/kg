// Package store reads and writes the graph.
//
// The entity files are the graph; there is no index and nothing to keep in
// sync with them. Which files a read opens depends on what it needs:
//
//   - [ReadEntity] for a point read — one file, independent of corpus size.
//   - [FindByType] for a typed find — one directory.
//   - [Loader] for traversal — reads entities as it reaches them and caches
//     what it read, so a path costs the reachable set.
//   - [LoadGraph] for the reads that genuinely need every entity: reverse
//     lookups, statistics, validation, and the referrer scan a merge does.
//
// Reaching for LoadGraph in a read that does not need it is how a cheap read
// stops being cheap.
//
// Vectors live in a gitignored sidecar under <kgDir>/.cache — see
// [github.com/pbleisch/kg/pkg/vector] — never in an entity file. [Sync]
// rebuilds it and is the one command P2's check asks for. The embedding
// command that fills it is the operator's, resolved by [ResolveEmbedder]:
// kg never chooses, hosts or calls a model.
package store
