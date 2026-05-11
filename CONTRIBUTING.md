# Contributing to Context Engine

Thanks for contributing to Context Engine.

## Workflow

- Create a short-lived branch from `main`.
- Open a pull request back to `main`.
- Keep changes focused. Separate unrelated refactors from behavior changes.
- Update docs and tests when commands, workflows, or user-facing behavior change.

## Local Setup

Requirements:

- Go 1.24.3+
- A C compiler available on your machine for tree-sitter CGO builds
- Optional: `goreleaser` if you are validating release packaging locally

Clone and build locally:

```bash
make build
```

## Verification

Before opening a pull request, run:

```bash
make verify
```

If you changed release packaging or `.goreleaser.yaml`, also run:

```bash
make release-snapshot
```

## Pull Requests

Please include:

- a clear description of the change
- tests for behavior changes where practical
- documentation updates when user-facing behavior or operator workflows changed
- explicit callouts for breaking changes, storage changes, or release impact

## Architecture Notes

- `internal/core` is the dependency floor and must not import other internal packages
- All substrate writes go through the write buffer
- Keep the project pure Go except for the existing tree-sitter CGO constraint

## Code of Conduct

By participating in this project, you agree to follow [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).
