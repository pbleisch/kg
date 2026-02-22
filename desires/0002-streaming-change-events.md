# 0002 — Streaming Change Events

Status: Open

Date: 2026-02-22

## What hurts

External tools that need to react to knowledge graph changes have no clean way to do it. They must either poll `kg find` and diff results, implement their own filesystem watcher on `.kg/entities/` and parse YAML, or dig into `kg daemon` internals. All three approaches are fragile and duplicate change-detection logic that kg already has.

## Why it matters

Without a change stream, kg is a dead-end in any pipeline — you can write to it and query it, but you can't subscribe to it. This blocks use cases like live dashboards, CI triggers on graph mutations, changelog generation, and any integration that needs to react in real time. As the graph grows and more tools depend on it, the lack of a standard event interface becomes a bigger bottleneck.

## Ideal outcome

A consumer can subscribe to structured change events (entity created/updated/deleted, relation linked/unlinked) from kg via a simple CLI command piped to stdout. Events are machine-readable (NDJSON), filterable by type, and composable with standard Unix tools. No custom watcher code required.

## Notes

- A detailed design sketch exists covering event schema, daemon integration, debouncing, and snapshot bootstrapping — can be referenced when implementation begins.
- Should complement `kg daemon` (index maintenance) rather than replace it — daemon keeps the index consistent, this exposes changes externally.
- Debouncing during bulk ingestion (e.g., many `kg create` calls) would be valuable to avoid event floods.
