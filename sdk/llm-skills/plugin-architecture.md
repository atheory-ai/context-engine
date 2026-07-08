# Context Engine Plugin Architecture

## The definePlugin Contract

Every plugin exports a single default export from `definePlugin()`.
All sections are optional. The engine inspects capabilities from the
plugin manifest and only calls functions that are declared.

```typescript
import { definePlugin } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.my-plugin",   // reverse-domain, required
  name:    "My Plugin",               // human name, required
  version: "1.0.0",                   // semver, required

  language:  { match, extract, concepts },  // optional
  role:      { name, systemPrompt, tools }, // optional
  analyzers: [{ name, description, analyze }], // optional
  tools:     [{ name, description, activate, execute }], // optional
})
```

## Language Handler

`match(filePath)` is called for every file during indexing.
Return true if your plugin handles this file. Keep it fast — regex only.

`extract(filePath, content)` receives the file path and raw content.
Return `{ nodes: Node[], edges: Edge[] }`.

Always use `nodeID()` and `edgeID()` helpers — never construct IDs manually.
The engine uses deterministic hashing; inconsistent IDs break the graph.

## Node ID Conventions

```
Symbol nodes:    canonicalID = "package/path:SymbolName"
Method nodes:    canonicalID = "package/path:TypeName.MethodName"
Namespace nodes: canonicalID = "package/path"
Concept nodes:   canonicalID = "lowercase-hyphenated-term"
File nodes:      canonicalID = "relative/path/from/root.ext"
```

## Edge Source Classes

```
"structural"  — from static analysis (highest trust, use for AST edges)
"associative" — learned from patterns (let the engine set this)
"speculative" — uncertain relationships (use for inferred edges)
"derived"     — computed from other relationships
```

## Tool Activation

`activate(ir)` must be a PURE FUNCTION. No side effects, no logging, no state.
It is called on every IR to determine if the tool should run.

```typescript
activate(ir) {
  // Explicit predicate activation
  return ir.predicates["my-tool"] === "true"
  // Or: implicit activation based on anchor types
  // return ir.anchors.some(a => a.type === "symbol")
}
```

## Common Mistakes

1. Returning `{ symbols: [] }` from extract() instead of `{ nodes: [], edges: [] }`
2. Using `fs`, `path`, or `process` — these don't exist in the WASM sandbox
3. Generating node IDs as strings instead of using `nodeID()`
4. `tool.description` over 100 characters
5. Side effects in `activate()`
6. Forgetting to deduplicate nodes (same file imported multiple times)
