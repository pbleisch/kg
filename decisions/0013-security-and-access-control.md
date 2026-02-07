# 0013 - Security and Access Control

Status: Proposed

Date: 2026-02-01

Authors: Paul

Context
-------
Define auth model, ACLs, multi-tenancy, and secrets handling for daemon and MCP.

Decision
--------
Status: Decided

Decision:
- Default auth: Unix socket with OS-level file permissions for daemon and MCP; token-based auth is documented as an optional extension for remote deployments.
- ACL model: Not implemented in v1; document recommended patterns and file-per-tenant separation for multi-tenancy.
- Secrets: Disallow storing secrets in YAML by default; document secure alternatives (external secret manager, encrypted values) and optionally support encrypted fields in later versions.

Consequences:
- Secure-by-default for single-user local workflows; remote deployments will require additional configuration and caution.

Alternatives Considered
-----------------------
- Local unix-socket only with OS-level permissions vs token-based access over TCP

Consequences
------------
- Impacts deployability in multi-user or multi-tenant environments and security auditing.

Related Decisions
-----------------
- 0007-mcp.md
- 0008-daemon.md
