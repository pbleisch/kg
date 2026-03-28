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

kg stores data as YAML files on the local filesystem and maintains a local
index cache. The MCP server communicates over stdio. The daemon uses a
Unix domain socket with filesystem permission checks.

Security-relevant areas include:

- **File system access** — kg reads and writes to `.kg/` within the working
  directory. It should never access files outside this scope.
- **MCP server** — runs over stdio, inheriting the caller's permissions.
- **Daemon** — listens on a Unix socket at `.kg/kg.sock` with filesystem-level
  access control.
- **No network access** — kg does not make outbound network requests during
  normal operation.
