# Contributing to kg

Thanks for your interest in contributing! Here's how to get started.

## Development Setup

You'll need **Go 1.25+** and [Mage](https://magefile.org):

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
mage check     # lint + tests
mage testRace  # race detector
```

5. Open a pull request against `main`.

## What the CI Checks

Every PR runs:

- `go vet` (lint)
- Unit tests
- Race detector tests
- Integration tests (`-tags=integration`)
- Build verification

If your change touches LadybugDB code (anything behind the `ladybug` build
tag), the CI also runs a separate job with CGO enabled against the native
library.

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
