# @atheory-ai/ce-plugin-sdk

TypeScript SDK for authoring Context Engine plugins.

## Installation

```bash
pnpm add @atheory-ai/ce-plugin-sdk
```

## Usage

```typescript
import { definePlugin, nodeID, edgeID } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.my-plugin",
  name:    "My Plugin",
  version: "1.0.0",

  language: {
    match:   (filePath) => filePath.endsWith(".myext"),
    extract: (filePath, content, tree) => ({
      nodes: [/* ... */],
      edges: [/* ... */],
    }),
  },
})
```

`extract` receives `tree` — the host's tree-sitter CST for the file. Walk it
instead of matching raw text; the host already parsed the file.

## What a plugin can provide

- **`language`** — `match` + `extract` for a file type. To support a language
  the engine doesn't bundle (go, python, javascript, typescript, tsx), ship a
  tree-sitter grammar WASM and declare it — the host loads it at runtime, no
  engine rebuild:

  ```typescript
  language: {
    match:      (p) => p.endsWith(".ex"),
    extensions: [".ex", ".exs"],
    grammar:    "elixir.wasm",   // bundled next to the plugin .wasm
    extract:    (filePath, content, tree) => ({ nodes: [], edges: [] }),
  }
  ```

- **IIR lift** — `extract` may return `iir` alongside `nodes`/`edges`: a
  `FunctionIntent` per function, attached to its symbol node. This is how the
  engine gets [intent representation](https://github.com/atheory-ai/context-engine/blob/main/docs/iir.md)
  for a language.

- **`iirRules`** — a conformance rule pack the host merges over its defaults
  (e.g. forbid `== null` conditions). Declarative; no runtime call.

- **`analyzers`**, **`tools`**, **`role`**, **`concepts`** — graph analyzers,
  cognitive tools, an agent role, and domain concept seeds.

## Key Constraints

- No Node.js APIs (`fs`, `path`, `process`) — plugins run in a WASM sandbox
- Always use `nodeID()` and `edgeID()` — never construct IDs manually
- Tool `description` must be ≤ 100 characters
- Concept `term` must be `lowercase-hyphenated`
- `activate()` must be a pure function

## ESLint Plugin

The SDK ships an ESLint plugin that enforces these constraints at lint time:

```json
{
  "extends": ["@atheory-ai/ce-plugin-sdk/eslint-plugin-ce"]
}
```
