# Contributing to CE Plugin SDK

Thanks for contributing to CE Plugin SDK.

## Workflow

- Create a short-lived branch from `main`.
- Open a pull request back to `main`.
- Keep changes focused. Separate SDK API changes, plugin updates, and tooling changes when practical.
- Update docs when package APIs, generated scaffolds, or plugin workflows change.

## Local Setup

Requirements:

- Node.js 20+
- pnpm 9+
- A local `ce` binary in `PATH` if you need to validate built plugins against the engine

Install dependencies:

```bash
pnpm install
```

## Verification

Before opening a pull request, run:

```bash
pnpm lint
pnpm build
pnpm test
```

If you changed scaffolding, package manifests, or the Javy install flow, include the relevant manual verification notes in the pull request.

## Pull Requests

Please include:

- a clear description of the change
- tests for behavior changes where practical
- fixture updates when extractor behavior changes
- documentation updates when package APIs or plugin authoring workflows changed
- explicit callouts for breaking changes in published packages or generated scaffolds

## Code of Conduct

By participating in this project, you agree to follow [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).
