# 0020 - Using "beads" for work tracking

Status: Obsolete

Date: 2026-02-01

Updated: 2026-04

Authors: Paul

Context
-------
Steve Yegge's "beads" concept is a lightweight personal work-tracking method that represents tasks as small, focused items recorded in a list. For this repository, beads was used to track work that AI agents need to know about.

Decision
--------
~~Adopt beads for local, in-repo work tracking. Follow normal beads conventions.~~

**Obsolete.** Removed in favor of GitHub Issues and native agentic tool capabilities. Advances in AI coding agents have made dedicated in-repo issue trackers unnecessary — agents can use GitHub Issues directly and manage session-level tasks in memory.

Rationale
---------
- Low friction: quick to add and update while working locally.
- Close to the code: ties tasks to the repository state and decisions.
- Visible: snapshot commits provide a lightweight public backlog.

Consequences
------------
- `.beads/` directory, desire skill, and next skill removed.
- AGENTS.md simplified to remove beads workflow.
