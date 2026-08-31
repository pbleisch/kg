# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, email **pbleisch@acm.org** with:

- A description of the vulnerability
- Steps to reproduce
- Potential impact

I'll acknowledge receipt within 48 hours and work with you on a fix before
any public disclosure.

## Scope

kg stores data as YAML files on the local filesystem, alongside a gitignored
derived cache under `.kg/.cache/`. The MCP server communicates over stdio. kg
runs no resident process and listens on no socket.

Security-relevant areas include:

- **File system access** — kg reads and writes to `.kg/` within the working
  directory. It should never access files outside this scope.
- **MCP server** — runs over stdio, inheriting the caller's permissions.
- **The embedding command** — kg runs a program the operator configures, with
  the operator's own privileges, and passes it entity text on stdin. It comes
  from `KG_EMBED_COMMAND` or the gitignored `.kg/embedder.yaml`, never from a
  file that travels with a graph, so cloning a graph cannot cause kg to execute
  something its author chose. kg refuses a command whose path is relative to
  the working directory.
- **No network access** — kg makes no outbound network requests. It holds no
  provider SDK, no API key and no HTTP client; whether the configured embedding
  command reaches the network is the operator's choice.
