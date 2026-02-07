# 0010 - Implementation and Dependencies

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Confirm implementation language, core dependencies, packaging targets, and version pinning strategy.

Decision
--------
Status: Decided

Decision:
- Primary implementation language: Go
- Core dependencies: `github.com/cayleygraph/cayley`, `github.com/toon-format/toon-go`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `go.etcd.io/bbolt`.
- Packaging targets: macOS and Linux binaries via `goreleaser` (optional) and development builds via `go build`.
- Version pinning: Use `go.mod` to pin dependency versions; prefer stable releases for core deps.

Consequences:
- Leverages Go ecosystem and fits well with Cayley and BoltDB; cross-compilation is straightforward.


Alternatives Considered
-----------------------
- Use Go vs Rust vs Node.js
- Use Cayley vs other graph/index implementations

Consequences
------------
- Impacts build tooling, cross-compilation, and ecosystem compatibility.

Related Decisions
-----------------
- 0001-storage-and-index.md
- 0011-testing-and-observability.md
