# 0001 — YAML Storage Scaling

Status: Open

Date: 2026-02-08

## What hurts

The knowledge graph stores every entity as an individual YAML file
committed to git. As the graph grows, the repository will accumulate a
large number of small files, inflating repo size and slowing common git
operations (clone, status, diff). Merge conflicts become more likely
when multiple writers touch overlapping entity files.

## Why it matters

A bloated repo and sluggish git ergonomics make day-to-day work harder
for anyone using or contributing to the graph. If the repo becomes
unwieldy enough, it discourages exploration and contribution.

## Ideal outcome

The storage layer handles hundreds-to-thousands of entities without
noticeably degrading git clone times, repo size, or merge ergonomics.

## Notes

This is a known future risk, not an immediate blocker. The current
YAML-in-git approach is simple and working — this desire is a
placeholder to revisit when the pain becomes real. Related:
decisions/0001-storage-and-index.md.
