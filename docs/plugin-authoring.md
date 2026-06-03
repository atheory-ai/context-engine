# Plugin Authoring Guide

This guide connects CE runtime expectations to the TypeScript APIs in [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk). Use this when writing or reviewing plugins from the CE side. Use the SDK repository for package-specific build, test, and scaffolding details.

## Where Plugin Code Lives

Plugin authoring lives in `ce-plugin-sdk`:

- `@ce/plugin-sdk`: `definePlugin`, types, host helpers, and plugin TypeScript config.
- `@ce/plugin-sandbox`: local validation and fixture testing.
- `create-ce-plugin`: scaffolding for new plugins.
- `plugins/*`: default plugin source.
- `examples/*`: reference plugins.

CE owns the runtime:

- loading `.wasm` files
- validating required exports
- calling language, analyzer, tool, and role exports
- exposing host functions
- routing plugin emissions into CE channels
- embedding default plugin artifacts in releases

## Runtime Contract

CE loads plugins as WebAssembly modules through Extism and wazero. Do not depend on Node.js APIs, filesystem APIs, process globals, network access, or native extensions inside plugin code.

Every valid plugin must expose a manifest through the SDK-generated runtime export:

```text
ce_plugin_manifest
```

The manifest tells CE which capabilities exist:

```json
{
  "id": "com.example.my-plugin",
  "name": "My Plugin",
  "version": "1.0.0",
  "capabilities": {
    "language": true,
    "role": false,
    "analyzers": ["my-analyzer"],
    "tools": ["my-tool"]
  },
  "language": {
    "extensions": [".go"],
    "grammar": "my-grammar.wasm"
  }
}
```

Plugin authors normally do not hand-write this export. They call `definePlugin()` from `@ce/plugin-sdk`, and the SDK build output provides the runtime-facing exports.

## SDK Entry Point

Use `definePlugin()`:

```typescript
import { definePlugin } from "@ce/plugin-sdk"

export default definePlugin({
  id: "com.example.my-language",
  name: "My Language",
  version: "1.0.0",

  language: {
    match(filePath) {
      return filePath.endsWith(".my")
    },

    extract(filePath, content) {
      return {
        nodes: [],
        edges: [],
      }
    },

    concepts: [
      {
        term: "message-handler",
        definition: "Code that receives and dispatches inbound messages",
      },
    ],
  },
})
```

The TypeScript API lives in:

```text
ce-plugin-sdk/packages/plugin-sdk/src/types.ts
ce-plugin-sdk/packages/plugin-sdk/src/define.ts
ce-plugin-sdk/packages/plugin-sdk/src/host.ts
```

## Language Plugins

Language plugins decide which files they handle and return graph facts for each file.

CE calls the language handler during indexing:

```text
file path + file content + optional serialized syntax tree
  -> plugin language match/extract
  -> ExtractionResult
  -> CE write buffer
  -> project graph
```

At the CE runtime boundary, `ce_language_extract` receives a JSON payload with `file_path`, `content`, and `tree`. The SDK authoring API presents the stable TypeScript `language.extract` function; plugin authors should use the SDK surface instead of depending on the low-level payload shape directly.

Return an `ExtractionResult`:

```typescript
interface ExtractionResult {
  nodes: Node[]
  edges: Edge[]
}
```

Use these node types when possible:

| Type | Use for |
| ---- | ------- |
| `symbol` | functions, methods, classes, interfaces, variables |
| `namespace` | packages, modules, directories |
| `concept` | domain vocabulary or architectural concepts |
| `file` | source files; CE may create these automatically |
| `directory` | directory-level grouping when useful |
| `framework_hook` | framework hook/action/filter registration or invocation |
| `framework_route` | framework route or endpoint registration |
| `framework_entrypoint` | framework bootstrap or externally invoked entrypoint |
| `test_surface` | test file, fixture, helper, or test method surface |

Use these edge types when possible:

| Type | Use for |
| ---- | ------- |
| `defines` | file or namespace defines a symbol |
| `contains` | directory, namespace, or file contains another entity |
| `imports` | file or namespace imports another namespace |
| `calls` | symbol calls another symbol |
| `references` | symbol references another symbol |
| `implements` | type implements an interface |
| `extends` | type extends another type |
| `belongs_to` | entity belongs to a larger grouping |
| `annotates` | entity maps to a concept |
| `depends_on` | generic dependency when no more specific edge fits |
| `registers` | file/symbol registers a hook, route, shortcode, block, service, or entrypoint |
| `handles` | callback or handler handles a hook, route, or entrypoint |
| `fires` | symbol or file fires a hook/event |
| `participates_in` | file/symbol participates in a framework lifecycle concept |

Prefer specific edge types over generic ones. Consistent graph facts make activation and built-in tools more useful.

## IDs and Canonical IDs

Nodes and edges need deterministic IDs. Prefer SDK helpers instead of reimplementing CE hashing:

```typescript
import { nodeID, edgeID } from "@ce/plugin-sdk"

const id = nodeID(projectID, "symbol", canonicalID)
const edge = edgeID(sourceID, "calls", targetID)
```

Canonical IDs should be stable across runs for the same code entity. Good canonical IDs usually include:

- normalized file path or namespace
- symbol name
- receiver/type name for methods
- enough context to disambiguate overloads or nested symbols

Avoid canonical IDs based on line numbers unless there is no better stable identifier.

## Source Classes

Each node and edge has a `sourceClass`:

| Source class | Use for |
| ------------ | ------- |
| `structural` | facts directly extracted from code |
| `associative` | learned or co-occurrence relationships |
| `speculative` | inferred relationships requiring caution |
| `derived` | facts computed from other graph facts |

Most language extraction output should be `structural`. Analyzer output is often `derived`. Avoid `speculative` unless the plugin is intentionally proposing uncertain relationships.

## Concepts

Concept seeds help CE connect user intent to code. Use concepts for domain or architecture vocabulary that is not just a symbol name:

```typescript
concepts: [
  {
    term: "event-sourcing",
    definition: "Persisting state changes as an append-only sequence of events",
    related: ["cqrs", "domain-event"],
    synonyms: ["event store"],
  },
]
```

Concepts should be stable vocabulary, not one-off comments.

## Analyzers

Analyzers run after extraction and can add edges based on nodes from a file:

```typescript
analyzers: [
  {
    name: "interface-impl",
    description: "Detect interface implementations",
    analyze(nodes) {
      return []
    },
  },
]
```

Use analyzers for facts that need a second pass, such as:

- implementation relationships
- import graph refinement
- framework-specific annotations
- concept mapping

Analyzers should return edges only. They should not mutate CE storage directly.

## Plugin Tools

Plugin tools participate in the query loop. A tool declares when it should activate and what evidence it can return:

```typescript
tools: [
  {
    name: "my-tool",
    description: "Finds framework routes",
    activationHint: "Use when the query asks about HTTP routes.",
    activate(ir) {
      return ir.predicates["routes"] === "true"
    },
    execute(request) {
      return {
        emissions: [
          { channel: "action", content: `inspected ${request.anchors.length} anchors` },
        ],
        proposedNodes: [],
        proposedEdges: [],
      }
    },
  },
]
```

Tool output is evidence for the reviewer and synthesizer. Keep emissions concise and grounded in what the tool actually found.

## Host Helpers

The SDK wraps CE host functions:

| SDK helper | Runtime behavior |
| ---------- | ---------------- |
| `log.debug/info/warn/error` | writes plugin logs to CE debug output |
| `emit(channel, content)` | emits to allowed CE channels |
| `createSubstrateClient()` | creates a read-only substrate query client |
| `getConfig(key)` | reads this plugin's config from `ce.yaml` |
| `nodeID(...)` | asks CE to generate a deterministic node ID |
| `edgeID(...)` | asks CE to generate a deterministic edge ID |

Plugins may emit only to these channels:

```text
thinking
action
debug
warning
```

Plugins cannot write to message or system channels.

Substrate access from plugins is read-only. Plugins propose graph changes through extraction, analyzers, or tool results; CE decides how and when to write.

## Configuration

Plugin-specific config is declared under `plugins` in `ce.yaml`:

```yaml
plugins:
  - path: ./plugins/my-plugin.wasm
    config:
      framework: nextjs
      include_tests: false
```

Read config through the SDK:

```typescript
import { getConfig } from "@ce/plugin-sdk"

const framework = getConfig<string>("framework")
```

Do not read local config files directly from plugin code. WASM plugins should treat CE host functions as the integration boundary.

## Build and Validate

The SDK build pipeline is:

```text
TypeScript
  -> esbuild bundle
  -> Javy JavaScript-to-WASM compile
  -> plugin .wasm
```

From the SDK repo:

```bash
pnpm install
pnpm build
pnpm test
```

From the CE repo or any machine with `ce` installed:

```bash
ce plugin validate /path/to/plugin.wasm
ce plugin list
ce index . --full
```

For local source builds of CE, put default plugin artifacts under:

```text
~/.ce/plugins/defaults/
```

Production CE releases embed default plugin artifacts and extract them on first run.

Convention plugins are additive. A generic language plugin should emit syntax
structure, while framework plugins such as WordPress or WooCommerce emit
semantic graph facts for hooks, routes, lifecycle paths, and tests from the
same source files.

## Runtime Expectations Checklist

Before publishing or relying on a plugin:

- `ce plugin validate plugin.wasm` succeeds.
- The plugin has a stable reverse-DNS-style `id`.
- `version` changes when behavior changes.
- `language.match` only claims files the plugin can process.
- `extract` returns deterministic nodes and edges.
- IDs are generated with SDK helpers or exactly match CE's deterministic ID contract.
- Node and edge `sourceClass` values are accurate.
- Tool emissions are concise and use allowed channels only.
- Substrate queries are read-only and bounded with reasonable `limit` values.
- Plugin config is read through `getConfig`.
- No Node.js, filesystem, process, or network APIs are required at runtime.

## Related Docs

- [Architecture guide](./architecture.md)
- [Roadmap and stability](./stability.md)
- [Troubleshooting](./troubleshooting.md)
- [Spec 4: Plugin Engine](./specs/4-spec-plugin-engine.md)
- [Spec 9: Indexer](./specs/9-spec-9-indexer.md)
- [Spec 16: Plugins](./specs/16-spec-16-plugins.md)
- [ce-plugin-sdk README](https://github.com/atheory-ai/ce-plugin-sdk)
