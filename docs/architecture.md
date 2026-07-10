# Context Engine Architecture

This guide is the contributor-facing map of the CE codebase. The full implementation specs in [docs/specs](./specs/) remain authoritative; use this page to understand where changes belong before reading the relevant spec.

## Repository Boundary

This repository owns the CE runtime:

- `ce` CLI and local binary
- project configuration loading
- indexing and graph population
- storage, migrations, and write buffering
- cognitive runner and built-in tools
- LLM provider adapters
- MCP, REST, and WebSocket server surfaces
- release packaging for the CE binary

Sibling repositories own adjacent surfaces:

- [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk): TypeScript plugin SDK, plugin sandbox, templates, examples, and default plugin source.
- [atheory-ce-studio](https://github.com/atheory-ai/atheory-ce-studio): optional web UI client that consumes CE REST and WebSocket APIs.

## Package Map

```text
cmd/ce              thin entrypoint for the ce binary
cli/                Cobra command tree
tui/                Bubbletea interactive query UI
internal/core       shared types and interfaces; dependency floor
internal/config     ce.yaml loading and config defaults
internal/storage    SQLite openers, migrations, generated queries, write buffer
internal/graph      substrate reads/writes, ontology, activation, org graph
internal/indexer    file walking, parsing, plugin execution, graph writes
internal/plugins    Extism/wazero plugin loading and execution
internal/agent      strategizer, reviewer, synthesizer, preflight agents
internal/tools      built-in cognitive tools
internal/runner     query DAG and cognitive loop orchestration
internal/llm        provider abstraction and provider implementations
internal/server     MCP, REST API, and WebSocket server
scripts/            maintainer scripts; not compiled into ce
```

`internal/core` is intentionally boring and foundational. It defines shared IDs, graph shapes, emissions, run context, and interfaces. It must not import any other `internal/...` package.

## Runtime Flow

```text
user command
  -> cli or tui
  -> config load
  -> runner/server/indexer entrypoint
```

The main user-facing flows are:

- `ce index`: walks files, parses source, runs language plugins, writes graph changes.
- `ce query`: runs the cognitive loop against indexed context and returns an answer.
- `ce server`: exposes the same runtime through MCP, REST, and WebSocket APIs.

## Indexing Flow

```text
file walker
  -> parser
  -> all matching language/convention plugins
  -> IR nodes and edges
  -> write buffer
  -> project graph database
  -> org graph lifting
```

The indexer owns orchestration, not language knowledge. Language-specific extraction belongs in plugins. Framework and codebase convention extraction also belongs in plugins, and multiple matching plugins may contribute graph facts for the same file. The Go runtime may parse with tree-sitter so plugins can receive a syntax tree, but plugin source and plugin authoring tools live in `ce-plugin-sdk`.

All substrate writes must go through the write buffer. Do not write directly to graph databases from indexing or learning paths.

## Query Flow

```text
query
  -> strategizer
  -> activation
  -> tool fan-out
  -> reviewer
  -> synthesizer
  -> answer and channel events
```

The strategizer turns the user question into structured intent. Activation resolves anchors into graph context. Tools gather focused evidence. The reviewer decides whether another loop is needed. The synthesizer writes the final answer from gathered context.

Channel events are part of the product surface. The CLI, TUI, server, and Studio-facing APIs all depend on stable event shapes.

## Storage Model

CE uses SQLite databases under the configured data directory:

```text
meta.db        project registry, paths, settings, token metadata
audit.db       sessions, turns, access log
execution.db   run and tool execution records
graphs/*.db    project substrate graphs
graphs/org.db  org-wide graph
```

Storage code is split by responsibility:

- `internal/storage/db`: connection handling
- `internal/storage/migrations`: schema creation and migration runners
- `internal/storage/writebuffer`: batched, deduplicated graph writes
- `internal/graph/substrate`: graph read/write behavior over storage

Read-scoped token sessions must not write to `execution.db` or the substrate.

## Plugin Boundary

CE loads plugins with Extism and wazero. Do not introduce another WASM runtime.

The runtime side of the plugin contract lives here:

- plugin loading
- manifest validation
- host functions
- invoking language/tool/analyzer exports
- embedding default plugin artifacts in release builds

The authoring side lives in `ce-plugin-sdk`:

- TypeScript SDK types
- plugin sandbox
- plugin templates
- default plugin source
- plugin examples and author docs

## Server Boundary

`internal/server` exposes CE to external clients:

- MCP SSE endpoint for tool integrations
- REST endpoints for status, indexing, search, runs, and synchronous query
- WebSocket query streaming for Studio and other clients

Treat server request/response shapes as compatibility contracts. Studio is a separate repo, but it consumes these APIs directly.

## Release Boundary

Normal PR checks build only for the local CI environment. Tagged releases build the full single-binary matrix:

```text
darwin/amd64
darwin/arm64
linux/amd64
linux/arm64
windows/amd64
windows/arm64
```

The engine is pure Go (`CGO_ENABLED=0`) — tree-sitter runs as WASM on wazero (`internal/indexer/wasmparse`; see [Spec 18](./specs/18-spec-wasm-grammar-loader.md)) — so every target cross-compiles with plain `go build`, no C toolchain.

## Non-Negotiables

- `internal/core` imports nothing from other internal packages.
- All substrate writes go through the write buffer.
- The engine stays pure Go (`CGO_ENABLED=0`); tree-sitter runs as WASM.
- Release cross-compilation must remain supported.
- Read-scoped token sessions never write to `execution.db` or the substrate.
- Plugin loading uses wazero and Extism only.
- Studio remains an optional UI client, not a runtime dependency.

## Which Spec To Read

| Working area | Read first |
| ------------ | ---------- |
| Storage, migrations, write buffer | [Spec 1: Data Layer](./specs/1-spec-data-layer.md) |
| Package boundaries and dependency graph | [Spec 2: Packages](./specs/2-spec-packages.md) |
| Runner and query loop | [Spec 3: Engine Runner](./specs/3-spec-engine-runner.md) |
| Plugin runtime | [Spec 4: Plugin Engine](./specs/4-spec-plugin-engine.md) |
| Strategizer prompt and IR extraction | [Spec 5: Strategizer Prompt](./specs/5-spec-strategizer-prompt.md) |
| CLI and config | [Spec 6: CLI Config](./specs/6-spec-cli-config.md) |
| Indexer and tree-sitter | [Spec 9: Indexer](./specs/9-spec-9-indexer.md) |
| Activation | [Spec 10: Activation](./specs/10-spec-10-activation.md) |
| Built-in tools | [Spec 11: Tools](./specs/11-spec-11-tools.md) |
| LLM adapters and synthesis | [Spec 12: LLM](./specs/12-spec-12-llm.md) |
| TUI | [Spec 13: TUI](./specs/13-spec-13-tui.md) |
| Server API and WebSocket | [Spec 14: Server](./specs/14-spec-14-server.md) |
| Default plugins | [Spec 16: Plugins](./specs/16-spec-16-plugins.md) |
| Org graph | [Spec 17: Org Graph](./specs/17-spec-17-orggraph.md) |
