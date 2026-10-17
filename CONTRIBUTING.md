# Contributing

Thank you for your interest in contributing!

## Getting Started

1. Fork the repository and clone locally
2. Create a feature branch: `git checkout -b feat/your-feature`
3. Make your changes with tests
4. Commit using [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `chore:`
5. Open a Pull Request with a clear description of *why* the change is needed

## Development Setup

See [README.md](README.md) for environment setup and build instructions.

### golangci-lint (pre-commit hook)

The `golangci-lint` pre-commit hook (`.pre-commit-config.yaml`) expects a
`golangci-lint` binary on your `PATH` — it does not install one. Install the
same version CI uses (see `.github/workflows/knowledge-ingest.yml`), not
`@latest`: `golangci-lint-action@v6` with `install-mode: goinstall` pins
`v1.64.8`, and that's a v1 config (`.golangci.yml` has no `version:` key) —
`golangci-lint/v2` (the `@latest` module path) requires a v2-format config
and will refuse to run against this repo.

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
```

If this fails with `the Go language version (go1.2x) used to build
golangci-lint is lower than the targeted Go version`, see
[`errors/go/golangci-lint-go-version-mismatch.md`](errors/go/golangci-lint-go-version-mismatch.md)
for the underlying cause — it's a known golangci-lint/Go-toolchain
interaction, not a bug in this repo's config.

## Pull Request Guidelines

- Keep PRs focused — one change per PR
- Include tests for new behaviour
- All CI checks must pass before merge
- Maintainer reviews within 7 days of opening

## Issue Reports

Please include:
- Steps to reproduce
- Expected vs actual behaviour
- Version / environment details

## Code of Conduct

Be respectful and constructive. We welcome contributors of all experience levels.
