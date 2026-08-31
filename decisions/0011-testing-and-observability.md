# 0011 - Testing and Observability

**AMENDED 2026-08-30 by [0027](0027-delete-index-and-daemon.md).** CI no longer runs
against a real LadybugDB index; it builds and tests with `CGO_ENABLED=0`, which is the
check that a C dependency does not return. The Prometheus endpoint "in daemon mode" was
already unimplemented (PRINCIPLES.md violation #7) and is now unimplementable.


Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define validation rules, test types and coverage targets, logging, metrics, and upgrade/migration testing.

Decision
--------
Status: Decided

Decision:
- Validation: `kg validate` performs strict checks and reports issues; `--strict` blocks mutations if violations exist. Validation includes required attributes, type coercion checks, and relation cardinality warnings.
- Test matrix: unit tests, integration tests, and E2E tests that run against a real LadybugDB index in CI. CI should run the full matrix on merges.
- Observability: structured logs (JSON) at levels debug/info/warn/error written to stdout; metrics exported via Prometheus endpoint in daemon mode. Provide sensible defaults and allow configuration via `config.yaml`.

Consequences:
- Higher confidence and early detection of regressions; more CI runtime cost.


Alternatives Considered
-----------------------
- Strict validation with blocking errors vs permissive warnings

Consequences
------------
- Affects reliability, developer confidence, and operational observability.

Related Decisions
-----------------
- 0010-implementation-and-dependencies.md
- 0014-performance-and-scaling.md
