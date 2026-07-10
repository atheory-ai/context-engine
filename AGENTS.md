# Context Engine — Claude Code Guide

## Module
github.com/atheory-ai/context-engine

## Specs (read before touching any component)
All specs are in docs/specs/.

| Working in...              | Read first                  |
|----------------------------|-----------------------------|
| internal/storage/          | spec-1-data-layer.md        |
| internal/core/             | spec-2-packages.md          |
| internal/runner/           | spec-3-engine-runner.md     |
| internal/plugins/          | spec-4-plugin-engine.md     |
| internal/agent/strategizer/| spec-5-strategizer-prompt.md|
| cli/ or internal/config/   | spec-6-cli-config.md        |
| anywhere                   | spec-2-packages.md (always) |

## Hard constraints
- internal/core imports nothing internal — it is the dependency floor
- All substrate writes go through the write buffer — never write directly to graph DBs
- The engine is pure Go (`CGO_ENABLED=0`); tree-sitter runs as WASM on wazero (`internal/indexer/wasmparse`). Do not reintroduce CGO — it would break `go build` cross-compilation
- Read-scoped token sessions never write to execution.db or the substrate
- wazero + Extism for all plugin loading — no other WASM runtime
