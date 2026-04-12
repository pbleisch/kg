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
- Core dependencies: `github.com/LadybugDB/go-ladybug`, `github.com/toon-format/toon-go`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`.
- Packaging targets: macOS and Linux binaries via `goreleaser` (optional) and development builds via `go build`.
- Version pinning: Use `go.mod` to pin dependency versions; prefer stable releases for core deps.

Consequences:
- Leverages Go ecosystem and fits well with LadybugDB; cross-compilation requires CGO.


Alternatives Considered
-----------------------
- Use Go vs Rust vs Node.js
- Use LadybugDB vs other graph/index implementations

Consequences
------------
- Impacts build tooling, cross-compilation, and ecosystem compatibility.

Related Decisions
-----------------
- 0001-storage-and-index.md
- 0011-testing-and-observability.md
