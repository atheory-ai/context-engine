# Anti-Patterns and Common Mistakes

## 1. Wrong return shape from extract()

```typescript
// WRONG
return { symbols: nodes, relationships: edges }
return { nodes }  // missing edges
return nodes      // not an object

// CORRECT
return { nodes, edges }
```

The engine expects exactly `{ nodes: Node[], edges: Edge[] }`.
Extra properties are ignored. Missing `edges` causes a parse error.

## 2. Using Node.js APIs

```typescript
// WRONG — these don't exist in WASM
import fs from "fs"
const content = fs.readFileSync(filePath)
process.exit(1)
__dirname
Buffer.from(str)

// CORRECT — content is passed as parameter
extract(filePath, content) {
  // content is already a string
}
```

The WASM sandbox has no file system access, no process APIs,
no network access, no timers.

## 3. Manual ID construction

```typescript
// WRONG — IDs must be deterministic across plugin and engine
const id = `${packageName}:${symbolName}`
const id = crypto.createHash("sha256").update(canonical).digest("hex").slice(0, 16)

// CORRECT
const id = nodeID(projectID, "symbol", canonical)
```

The engine uses `sha256(projectID + nodeType + canonicalID)[:16]` for node IDs.
Manual IDs won't match the engine's IDs, breaking graph links.

## 4. Description too long

```typescript
// WRONG — 120 chars
description: "This tool traverses the call graph starting from all anchor points to find all reachable symbols within the budget"

// CORRECT — 68 chars
description: "Traverse call graph from anchors to find reachable symbols"
```

The Strategizer receives descriptions in its prompt. Long descriptions
consume context budget without improving tool selection accuracy.

## 5. Side effects in activate()

```typescript
// WRONG
activate(ir) {
  log.debug("checking")      // side effect
  this.callCount++           // state mutation
  return ir.predicates["x"] === "true"
}

// CORRECT
activate(ir) {
  return ir.predicates["x"] === "true"
}
```

`activate()` may be called many times. It must be idempotent and pure.

## 6. Throwing errors from extract()

```typescript
// WRONG
extract(filePath, content) {
  if (!content) throw new Error("empty file")
  const ast = parseMyLanguage(content)  // throws on parse error
}

// CORRECT
extract(filePath, content) {
  if (!content) return { nodes: [], edges: [] }
  try {
    const ast = parseMyLanguage(content)
    // ...
  } catch {
    return { nodes: [], edges: [] }  // graceful degradation
  }
}
```

The engine indexes thousands of files. One malformed file should not
stop the entire index. Return empty results, never throw.

## 7. Uppercase concept terms

```typescript
// WRONG
{ term: "Goroutine" }
{ term: "HTTP_SERVER" }
{ term: "myPlugin" }

// CORRECT
{ term: "goroutine" }
{ term: "http-server" }
{ term: "my-plugin" }
```

## 8. Not handling method receivers in Go-style languages

```typescript
// WRONG — misses func (r *Receiver) Method()
const fnRegex = /^func\s+(\w+)\s*\(/gm

// CORRECT — handles both functions and methods
const fnRegex = /^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/gm
```

Methods with receivers are commonly missed. The regex must handle
the optional `(receiver Type)` before the function name.
