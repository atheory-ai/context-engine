# Contributing to Context Engine

Thanks for contributing to Context Engine.

## Workflow

- Create a short-lived branch from `main`.
- Open a pull request back to `main`.
- Keep changes focused. Separate unrelated refactors from behavior changes.
- Update docs and tests when commands, workflows, or user-facing behavior change.

## Contributor Quickstart

These steps are intended to work from a clean machine.

### 1. Install prerequisites

Required for normal development and pull requests:

| Tool | Version | Notes |
| ---- | ------- | ----- |
| Go | 1.24.3+ | Uses the version declared in [go.mod](./go.mod) |
| Git | Any current version | Required for checkout and formatting checks |
| Make | Any current version | Runs the project verification targets |

Release-only tools:

- GoReleaser is only needed for `make build-cross`, release dry runs, and tagged release validation. The engine is pure Go: tree-sitter runs as WASM on wazero, so no C compiler or Zig toolchain is required.

### 2. Clone and build

```bash
git clone https://github.com/atheory-ai/context-engine.git
cd context-engine
make build
```

This produces a local `./ce` binary for your current OS and architecture.

### 3. Run the PR checks

Before opening a pull request, run the same local checks CI expects:

```bash
make fmt-check
make verify-unit
make test-coverage
make test-acceptance
```

For a single command that includes formatting, vet, unit tests, acceptance tests, and a local build:

```bash
make verify
```

### 4. Optional local smoke test

```bash
./ce --help
./ce project --help
./ce server --help
```

Indexing and querying require language plugins and provider configuration. Production release binaries embed default plugins; local source builds can use plugins built from the [SDK workspace](./sdk/README.md).

## Verification

Before opening a pull request, run:

```bash
make verify
```

If you changed release packaging or `.goreleaser.yaml`, also run:

```bash
make build-cross
```

## Pull Requests

Please include:

- a clear description of the change
- tests for behavior changes where practical
- documentation updates when user-facing behavior or operator workflows changed
- explicit callouts for breaking changes, storage changes, or release impact

## Architecture Notes

Start with the contributor-facing [architecture guide](./docs/architecture.md), then read the relevant implementation spec in [docs/specs](./docs/specs/).

The high-level constraints are:

- `internal/core` is the dependency floor and must not import other internal packages.
- All substrate writes go through the write buffer.
- Keep the project pure Go (`CGO_ENABLED=0`); tree-sitter runs as WASM on wazero.
- Plugin loading uses wazero and Extism only.

For snapshot-style fixtures, follow [How To Write A Golden Test](./docs/golden-tests.md).

## Code of Conduct

By participating in this project, you agree to follow [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).
