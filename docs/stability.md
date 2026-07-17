# Roadmap and Stability

This page describes which CE surfaces contributors should treat as stable, compatibility-managed, experimental, or internal. It is a practical guide for pull requests and releases, not a replacement for the implementation specs.

## Stability Levels

| Level | Meaning |
| ----- | ------- |
| Stable | Changes should be backward compatible. Breaking changes need a clear migration note and release callout. |
| Compatibility-managed | Still evolving, but consumed by sibling repos or external users. Coordinate changes and update compatibility checks. |
| Experimental | Can change between minor releases. Prefer clear docs over compatibility guarantees. |
| Internal | No compatibility guarantee outside this repository. Keep package boundaries clean. |

## Stable

These are the surfaces users and operators should be able to rely on:

- Core local commands for normal use: `ce project`, `ce index`, `ce query`, `ce server`.
- Project configuration file shape for documented fields in `ce.yaml`.
- Data directory layout at the level users interact with: project registry, graph storage, and server state under the configured CE data directory.
- Single-binary release model for supported platforms.
- Release artifact names and archive formats, unless release notes explicitly say otherwise.

Stable does not mean frozen. It means changes should be deliberate, documented, and compatible where practical.

## Compatibility-Managed

These surfaces may evolve, but they are consumed by Studio, SDK, plugins, or integrations:

- REST API under `/api/v1`.
- WebSocket query streaming frames.
- MCP server tools and request/response shapes.
- Channel event types emitted by the runner.
- Plugin manifest fields and runtime host behavior.
- Serialized syntax tree shape passed to language plugins.
- Default plugin artifact names embedded into CE releases.
- Execution log and run trace shapes used by Studio.

Changes here should usually include at least one of:

- an update to [atheory-ce-studio](https://github.com/atheory-ai/atheory-ce-studio)
- an update to the in-tree [SDK workspace](../sdk/README.md)
- an acceptance or contract test in this repository
- a release note describing the compatibility impact

Runtime releases gate on the in-tree SDK build and IIR contract corpus that is
embedded in the binary. Studio and other external clients are compatibility-
managed through contract tests and release notes; they do not block publication
of the canonical GitHub Release binaries.

## Experimental

These areas are expected to change as the product matures:

- Prompt wording and agent heuristics.
- Strategizer IR details beyond validated public behavior.
- Activation scoring and graph weighting.
- Hebbian learning behavior.
- Org graph lifting and cross-project matching.
- Built-in cognitive tool selection heuristics.
- TUI presentation details.
- Local LLM provider behavior.
- Plugin-provided grammar loading.

Experimental areas still need tests when behavior matters, but contributors should not assume their exact output or tuning is a long-term contract.

## Internal

These surfaces are implementation details:

- Unexported Go types and functions.
- Package-private storage helpers.
- SQL query organization and generated query names.
- Runner DAG internals.
- Indexer batching details.
- Cache layout.
- Maintainer scripts under `scripts/`.

Internal changes should preserve the architectural constraints in [architecture.md](./architecture.md), especially `internal/core` dependency direction and write-buffer-only substrate writes.

## Roadmap

Near-term stabilization work:

- Keep PR CI focused on local correctness: formatting, unit verification, coverage, and acceptance tests.
- Keep release workflow responsible for cross-platform builds, the in-tree SDK
  contract gate, signed GitHub Release assets, and provenance.
- Add contract tests for REST, WebSocket, MCP, and plugin runtime shapes as those surfaces harden.
- Document user-visible API changes in release notes.
- Create a separate demo repository with a sample codebase, repeatable indexing fixtures, scripted queries, and a Studio demo flow.

Longer-term compatibility goals:

- Version the external API contract explicitly.
- Publish machine-readable schemas for Studio-facing REST and WebSocket payloads.
- Make plugin runtime compatibility testable from both CE and `ce-plugin-sdk`.
- Define a formal deprecation policy before broad public releases.

## Breaking Change Checklist

Before merging a breaking or potentially breaking change:

- Identify whether the affected surface is stable, compatibility-managed, experimental, or internal.
- Update the relevant spec or contributor doc.
- Update sibling repos when they consume the changed contract.
- Add or adjust tests that would catch the old/new behavior.
- Call out the change in release notes or `CHANGELOG.md`.
