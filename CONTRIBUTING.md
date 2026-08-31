# Contributing to kg

Thanks for your interest in contributing! Here's how to get started.

## Development Setup

You'll need **Go 1.25+** and [Mage](https://magefile.org). Nothing else — kg is
pure Go, with no CGO and no native library to fetch:

```bash
git clone https://github.com/pbleisch/kg.git
cd kg
go install github.com/magefile/mage@latest
mage build
```

## Making Changes

1. Fork the repo and create a branch from `main`.
2. Make your changes.
3. Add or update tests as needed.
4. Run the checks:

```bash
mage check     # gofmt + go vet + tests + integration tests
mage testRace  # race detector
```

5. Open a pull request against `main`.

## What the CI Checks

Every PR runs, with `CGO_ENABLED=0` so a C dependency cannot creep back in:

- `gofmt -l .` (must be empty)
- `go vet`
- Unit tests
- Race detector tests (the detector itself needs cgo; the product does not)
- Integration tests (`-tags=integration`)
- Build verification

**CI is not currently a usable signal**, for reasons outside this repository —
see the note in [AGENTS.md](AGENTS.md). Verify locally.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- Keep comments minimal — only where the "why" isn't obvious.
- Architecture decisions are documented in `decisions/` as lightweight ADRs.
  If your change introduces a significant design choice, consider adding one.

## Reporting Bugs

Open a [GitHub Issue](https://github.com/pbleisch/kg/issues) with:

- What you expected to happen
- What actually happened
- Steps to reproduce
- `kg` version (`kg --version`) and OS

## Suggesting Features

Open a GitHub Issue describing the use case and why it matters. Linking to
related architecture decisions in `decisions/` is helpful context.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE).
