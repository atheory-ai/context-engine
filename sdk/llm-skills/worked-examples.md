# Worked Examples

Three complete annotated plugin examples.

---

## Example 1: Minimal File Counter

The simplest possible plugin — counts lines in any file type.

```typescript
import { definePlugin, nodeID } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.file-counter",
  name:    "File Counter",
  version: "1.0.0",

  language: {
    // Match any file that's not a binary
    match: (filePath) =>
      !filePath.match(/\.(png|jpg|gif|ico|woff|ttf|eot|zip|tar|gz)$/),

    extract: (filePath, content) => {
      const lineCount = content.split("\n").length
      const wordCount = content.split(/\s+/).filter(Boolean).length

      return {
        nodes: [{
          id:          nodeID("", "file", filePath),
          type:        "file",
          label:       filePath.split("/").pop()!,
          canonicalID: filePath,
          sourceClass: "structural",
          properties:  { lineCount, wordCount },
        }],
        edges: [],
      }
    },
  },
})
```

---

## Example 2: Python Module Extractor

Extracts Python functions, classes, and imports.

```typescript
import { definePlugin, nodeID, edgeID } from "@atheory-ai/ce-plugin-sdk"
import type { Node, Edge } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.python-language",
  name:    "Python Language Plugin",
  version: "1.0.0",

  language: {
    match: (filePath) =>
      filePath.endsWith(".py") && !filePath.includes("__pycache__"),

    extract: (filePath, content) => {
      const nodes: Node[] = []
      const edges: Edge[] = []

      // Derive module name from file path
      const moduleName = filePath.replace(/\//g, ".").replace(/\.py$/, "")

      const fileNode: Node = {
        id:          nodeID("", "file", filePath),
        type:        "file",
        label:       filePath.split("/").pop()!,
        canonicalID: filePath,
        sourceClass: "structural",
        properties:  { module: moduleName },
      }
      nodes.push(fileNode)

      const lines = content.split("\n")

      for (let i = 0; i < lines.length; i++) {
        const line = lines[i]

        // Functions and methods
        const fnMatch = line.match(/^(?:    )?(?:async\s+)?def\s+(\w+)\s*\(/)
        if (fnMatch) {
          const name = fnMatch[1]
          const isMethod = line.startsWith("    ")
          const canonical = `${moduleName}:${name}`

          const fnNode: Node = {
            id:          nodeID("", "symbol", canonical),
            type:        "symbol",
            label:       name,
            canonicalID: canonical,
            sourceClass: "structural",
            properties:  { kind: isMethod ? "method" : "function", line: i + 1 },
          }
          nodes.push(fnNode)
          edges.push({
            id:          edgeID(fileNode.id, "defines", fnNode.id),
            sourceID:    fileNode.id,
            targetID:    fnNode.id,
            type:        "defines",
            sourceClass: "structural",
            properties:  {},
          })
        }

        // Classes
        const classMatch = line.match(/^class\s+(\w+)/)
        if (classMatch) {
          const name = classMatch[1]
          const canonical = `${moduleName}:${name}`

          const classNode: Node = {
            id:          nodeID("", "symbol", canonical),
            type:        "symbol",
            label:       name,
            canonicalID: canonical,
            sourceClass: "structural",
            properties:  { kind: "class" },
          }
          nodes.push(classNode)
          edges.push({
            id:          edgeID(fileNode.id, "defines", classNode.id),
            sourceID:    fileNode.id,
            targetID:    classNode.id,
            type:        "defines",
            sourceClass: "structural",
            properties:  {},
          })
        }

        // Imports
        const importMatch = line.match(/^(?:import|from)\s+([\w.]+)/)
        if (importMatch) {
          const importedModule = importMatch[1]
          const impNode: Node = {
            id:          nodeID("", "namespace", importedModule),
            type:        "namespace",
            label:       importedModule,
            canonicalID: importedModule,
            sourceClass: "structural",
            properties:  {},
          }
          nodes.push(impNode)
          edges.push({
            id:          edgeID(fileNode.id, "imports", impNode.id),
            sourceID:    fileNode.id,
            targetID:    impNode.id,
            type:        "imports",
            sourceClass: "structural",
            properties:  {},
          })
        }
      }

      // Deduplicate
      const seenNodes = new Set<string>()
      const seenEdges = new Set<string>()
      return {
        nodes: nodes.filter(n => !seenNodes.has(n.id) && seenNodes.add(n.id) !== undefined),
        edges: edges.filter(e => !seenEdges.has(e.id) && seenEdges.add(e.id) !== undefined),
      }
    },

    concepts: [
      { term: "decorator",      definition: "Function that modifies another function or class" },
      { term: "generator",      definition: "Function using yield for lazy evaluation" },
      { term: "comprehension",  definition: "Concise syntax for creating lists, sets, or dicts" },
      { term: "dunder-method",  definition: "Special method with double underscore prefix (e.g., __init__)" },
      { term: "virtual-env",    definition: "Isolated Python environment with its own packages" },
    ],
  },
})
```

---

## Example 3: Security Audit Tool

A tool that activates when predicates indicate a security audit.

```typescript
import { definePlugin, createSubstrateClient } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.security-audit",
  name:    "Security Audit Tool",
  version: "1.0.0",

  tools: [{
    name:        "security-audit",
    description: "Find symbols with security-sensitive patterns near anchors",

    activate(ir) {
      return ir.predicates["security-audit"] === "true"
    },

    execute(request, substrate) {
      const SECURITY_PATTERNS = [
        "password", "secret", "token", "key", "auth",
        "credential", "hash", "encrypt", "decrypt", "sign",
      ]

      const findings: string[] = []

      for (const anchor of request.anchors) {
        if (!anchor.node) continue

        const neighbors = substrate.query({
          projectID: request.ir.anchors[0]?.id ?? "",
          nodeTypes:  ["symbol"],
          limit:      request.ir.kLimit,
        })

        for (const node of neighbors) {
          const label = node.label.toLowerCase()
          const matched = SECURITY_PATTERNS.filter(p => label.includes(p))
          if (matched.length > 0) {
            findings.push(`${node.label} (patterns: ${matched.join(", ")})`)
          }
        }
      }

      if (findings.length === 0) {
        return {
          emissions: [{
            channel: "action",
            content: "Security audit: no sensitive patterns found near anchors",
          }],
          proposedNodes: [],
          proposedEdges: [],
        }
      }

      return {
        emissions: [{
          channel: "action",
          content: `Security audit found ${findings.length} potentially sensitive symbols:\n` +
                   findings.map(f => `  - ${f}`).join("\n"),
        }],
        proposedNodes: [],
        proposedEdges: [],
      }
    },
  }],
})
```
