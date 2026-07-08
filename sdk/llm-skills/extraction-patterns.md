# Extraction Patterns

## General Approach

The `extract()` function receives raw file content as a string.
No file system access. No external dependencies. Pure string processing.

## Always start with a file node

```typescript
const fileNode: Node = {
  id:          nodeID(projectID, "file", filePath),
  type:        "file",
  label:       filePath.split("/").pop()!,
  canonicalID: filePath,
  sourceClass: "structural",
  properties:  {},
}
```

## Go-style languages (explicit function declarations)

```typescript
const fnRegex = /^func\s+(?:\(([^)]+)\)\s+)?(\w+)\s*\(/gm
for (const m of content.matchAll(fnRegex)) {
  const receiver = m[1]  // null if not a method
  const name = m[2]
  // ...
}
```

## TypeScript/JavaScript

```typescript
// Named functions
/^(?:export\s+)?(?:async\s+)?function\s+(\w+)/gm
// Arrow functions assigned to const
/^(?:export\s+)?const\s+(\w+)\s*=\s*(?:async\s+)?\(/gm
// Classes
/^(?:export\s+)?(?:abstract\s+)?class\s+(\w+)/gm
// Interfaces
/^(?:export\s+)?interface\s+(\w+)/gm
// Type aliases
/^(?:export\s+)?type\s+(\w+)\s*=/gm
```

## Python

```typescript
// Functions and methods
/^(?:    )?(?:async\s+)?def\s+(\w+)\s*\(/gm
// Classes
/^class\s+(\w+)/gm
// Decorators (attach to next symbol)
/^@(\w+)/gm
```

## Handling imports

Always emit import edges — they create the dependency graph:

```typescript
// Go single import
/^import\s+"([^"]+)"/gm
// Go block import
content.match(/import\s*\(([^)]+)\)/s)
// TypeScript ES imports
/^import\s+.*?from\s+['"]([^'"]+)['"]/gm
// Python imports
/^(?:import|from)\s+([\w.]+)/gm
```

## Deduplication is your responsibility

The same node might be emitted multiple times (e.g. an import from two files).
Always deduplicate before returning:

```typescript
const seen = new Set<string>()
const uniqueNodes = nodes.filter(n => {
  if (seen.has(n.id)) return false
  seen.add(n.id)
  return true
})
```

## Edge patterns

```typescript
// File defines symbol
{ type: "defines", sourceID: fileNode.id, targetID: symbolNode.id }
// File imports module
{ type: "imports", sourceID: fileNode.id, targetID: namespaceNode.id }
// Type extends another
{ type: "extends", sourceID: childNode.id, targetID: parentNode.id }
// Type implements interface
{ type: "implements", sourceID: structNode.id, targetID: ifaceNode.id }
// Symbol belongs to namespace
{ type: "belongs_to", sourceID: symbolNode.id, targetID: namespaceNode.id }
```
