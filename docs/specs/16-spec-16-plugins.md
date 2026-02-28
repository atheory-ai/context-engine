# Context Engine — Spec 16: Default Plugins
## Implementation Spec — Go, TypeScript, Python Language Plugin Implementations
### Version 1.0 | February 2026

---

> This spec implements the three default language plugins.
> These live in the plugin SDK repository under examples/default-plugins/
> and are compiled to WASM and embedded in the engine binary.
> Hand to Claude Code alongside spec-7-plugin-sdk.md and spec-9-indexer.md.
> Companion: Context Engine PRD v0.5 Section 13.2. Decisions Log v1.0 Section 12.

---

## 1. Overview

The three default plugins provide language support for Go, TypeScript/JavaScript,
and Python. They are the reference implementations of the plugin interface —
every design decision here is something plugin authors can learn from.

Each plugin:
- Declares file extensions it handles
- References its bundled tree-sitter grammar WASM
- Implements `extract()` using the provided `SyntaxTree`
- Contributes concept seeds for domain vocabulary common to each language
- Implements analyzers for relationships the tree-sitter pass can't see

The plugins live in the SDK repo and are compiled as part of the SDK release
pipeline. Their `.wasm` artifacts are embedded in the engine binary at
engine build time (Spec 9, `go:embed`).

---

## 2. Repository Location

```
plugin-sdk/                          ← @ce/plugin-sdk monorepo
  packages/
    plugin-sdk/                      ← @ce/plugin-sdk
    create-ce-plugin/                ← scaffolding CLI
    plugin-sandbox/                  ← @ce/plugin-sandbox
  plugins/
    go-language/                     ← default Go plugin
      src/
        index.ts                     ← plugin entry point
        extract.ts                   ← extraction logic
        concepts.ts                  ← Go concept seeds
        analyzers/
          interface-impl.ts          ← interface implementation analyzer
          package-deps.ts            ← package dependency analyzer
      fixtures/                      ← test fixtures
        simple-service.go
        interface.go
        complex.go
      package.json
      tsconfig.json
      ce-plugin.json                 ← plugin manifest

    typescript-language/             ← default TypeScript/JS plugin
      src/
        index.ts
        extract.ts
        concepts.ts
        analyzers/
          import-graph.ts
          react-components.ts
      fixtures/
        service.ts
        component.tsx
        module.js
      package.json
      tsconfig.json
      ce-plugin.json

    python-language/                 ← default Python plugin
      src/
        index.ts
        extract.ts
        concepts.ts
        analyzers/
          module-deps.ts
          class-hierarchy.ts
      fixtures/
        service.py
        models.py
        utils.py
      package.json
      tsconfig.json
      ce-plugin.json
```

---

## 3. Go Language Plugin

### Plugin manifest (ce-plugin.json)

```json
{
  "id":      "com.atheory.go-language",
  "name":    "Go Language",
  "version": "1.0.0",
  "capabilities": {
    "language":  true,
    "role":      false,
    "analyzers": ["interface-impl", "package-deps"],
    "tools":     []
  },
  "language": {
    "extensions": [".go"],
    "grammar":    "./go-grammar.wasm"
  }
}
```

### Plugin entry point

```typescript
// plugins/go-language/src/index.ts

import { definePlugin }   from "@ce/plugin-sdk"
import { extract }        from "./extract"
import { goConceptSeeds } from "./concepts"
import { interfaceImplAnalyzer } from "./analyzers/interface-impl"
import { packageDepsAnalyzer }   from "./analyzers/package-deps"

export default definePlugin({
  id:      "com.atheory.go-language",
  name:    "Go Language",
  version: "1.0.0",

  language: {
    extensions: [".go"],
    grammar:    "./go-grammar.wasm",
    extract,
    concepts: goConceptSeeds,
    analyzers: [interfaceImplAnalyzer, packageDepsAnalyzer],
  },
})
```

### Extraction — what we pull from the Go CST

```typescript
// plugins/go-language/src/extract.ts

import type { SyntaxTree, SyntaxNode, ExtractionResult, Node, Edge } from "@ce/plugin-sdk"
import { nodeID, edgeID } from "@ce/plugin-sdk"

export function extract(
  filePath: string,
  content:  string,
  tree:     SyntaxTree | null
): ExtractionResult {
  if (!tree) {
    // No grammar — fall back to regex-based extraction
    return extractFallback(filePath, content)
  }

  const nodes: Node[] = []
  const edges: Edge[]  = []

  const packageName = extractPackageName(tree.root)

  // ── File node ──────────────────────────────────────────────────────────
  const fileNodeID = nodeID(`file:${filePath}`)
  nodes.push({
    id:          fileNodeID,
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties:  { package: packageName },
  })

  // ── Package / namespace node ───────────────────────────────────────────
  if (packageName && packageName !== "main") {
    const pkgDir     = filePath.substring(0, filePath.lastIndexOf("/"))
    const pkgNodeID  = nodeID(`namespace:${pkgDir}`)

    // Upsert-safe — multiple files in same package produce same node ID
    nodes.push({
      id:          pkgNodeID,
      type:        "namespace",
      label:       packageName,
      canonicalID: pkgDir,
      sourceClass: "structural",
      properties:  { package: packageName },
    })

    edges.push({
      id:          edgeID(fileNodeID, "belongs_to", pkgNodeID),
      sourceID:    fileNodeID,
      targetID:    pkgNodeID,
      type:        "belongs_to",
      sourceClass: "structural",
    })
  }

  // ── Imports ────────────────────────────────────────────────────────────
  const imports = extractImports(tree.root)
  for (const imp of imports) {
    const impNodeID = nodeID(`namespace:${imp.path}`)
    nodes.push({
      id:          impNodeID,
      type:        "namespace",
      label:       imp.alias || imp.path.split("/").pop() || imp.path,
      canonicalID: imp.path,
      sourceClass: "structural",
      properties:  { import_path: imp.path },
    })

    edges.push({
      id:          edgeID(fileNodeID, "imports", impNodeID),
      sourceID:    fileNodeID,
      targetID:    impNodeID,
      type:        "imports",
      sourceClass: "structural",
    })
  }

  // ── Functions and methods ──────────────────────────────────────────────
  walkNode(tree.root, (node) => {
    if (node.type === "function_declaration") {
      const fn = extractFunction(node, filePath, packageName, content)
      if (fn) {
        nodes.push(fn.node)
        edges.push(...fn.edges)
      }
    }

    if (node.type === "method_declaration") {
      const method = extractMethod(node, filePath, packageName, content)
      if (method) {
        nodes.push(method.node)
        edges.push(...method.edges)
      }
    }

    if (node.type === "type_declaration") {
      const typeDecl = extractTypeDecl(node, filePath, packageName, content)
      if (typeDecl) {
        nodes.push(...typeDecl.nodes)
        edges.push(...typeDecl.edges)
      }
    }

    if (node.type === "interface_type") {
      const iface = extractInterface(node, filePath, packageName, content)
      if (iface) {
        nodes.push(iface.node)
        edges.push(...iface.edges)
      }
    }
  })

  return { nodes, edges }
}

// ── Helpers ────────────────────────────────────────────────────────────────

function extractFunction(
  node:        SyntaxNode,
  filePath:    string,
  packageName: string,
  content:     string
): { node: Node; edges: Edge[] } | null {
  const nameNode = findChild(node, "name")
  if (!nameNode) return null

  const name       = nameNode.text
  const pkgDir     = filePath.substring(0, filePath.lastIndexOf("/"))
  const canonicalID = `${pkgDir}:${name}`
  const fnNodeID   = nodeID(`symbol:${canonicalID}`)

  const params   = extractParams(node)
  const returns  = extractReturns(node)
  const bodyText = extractBody(node, content)

  return {
    node: {
      id:          fnNodeID,
      type:        "symbol",
      label:       name,
      canonicalID: canonicalID,
      sourceClass: "structural",
      properties: {
        file_path:  filePath,
        package:    packageName,
        kind:       "function",
        params:     params,
        returns:    returns,
        exported:   isExported(name),
        start_line: node.startPosition.row + 1,
        end_line:   node.endPosition.row + 1,
        signature:  extractSignature(node, content),
      },
    },
    edges: [
      {
        id:          edgeID(fnNodeID, "defined_in", nodeID(`file:${filePath}`)),
        sourceID:    fnNodeID,
        targetID:    nodeID(`file:${filePath}`),
        type:        "defined_in",
        sourceClass: "structural",
      },
    ],
  }
}

function extractMethod(
  node:        SyntaxNode,
  filePath:    string,
  packageName: string,
  content:     string
): { node: Node; edges: Edge[] } | null {
  const nameNode     = findChild(node, "name")
  const receiverNode = findChild(node, "receiver")
  if (!nameNode || !receiverNode) return null

  const methodName   = nameNode.text
  const receiverType = extractReceiverType(receiverNode)
  if (!receiverType) return null

  const pkgDir      = filePath.substring(0, filePath.lastIndexOf("/"))
  const canonicalID = `${pkgDir}:${receiverType}.${methodName}`
  const mNodeID     = nodeID(`symbol:${canonicalID}`)
  const typeNodeID  = nodeID(`symbol:${pkgDir}:${receiverType}`)

  return {
    node: {
      id:          mNodeID,
      type:        "symbol",
      label:       `${receiverType}.${methodName}`,
      canonicalID: canonicalID,
      sourceClass: "structural",
      properties: {
        file_path:     filePath,
        package:       packageName,
        kind:          "method",
        receiver_type: receiverType,
        exported:      isExported(methodName),
        start_line:    node.startPosition.row + 1,
        end_line:      node.endPosition.row + 1,
        signature:     extractSignature(node, content),
      },
    },
    edges: [
      {
        id:          edgeID(mNodeID, "method_of", typeNodeID),
        sourceID:    mNodeID,
        targetID:    typeNodeID,
        type:        "method_of",
        sourceClass: "structural",
      },
      {
        id:          edgeID(mNodeID, "defined_in", nodeID(`file:${filePath}`)),
        sourceID:    mNodeID,
        targetID:    nodeID(`file:${filePath}`),
        type:        "defined_in",
        sourceClass: "structural",
      },
    ],
  }
}

// ── Tree walking utilities ─────────────────────────────────────────────────

function walkNode(node: SyntaxNode, fn: (n: SyntaxNode) => void): void {
  fn(node)
  for (const child of node.children) {
    walkNode(child, fn)
  }
}

function findChild(node: SyntaxNode, fieldName: string): SyntaxNode | null {
  return node.children.find(c => c.fieldName === fieldName) ?? null
}

function isExported(name: string): boolean {
  return name.length > 0 && name[0] >= "A" && name[0] <= "Z"
}

// ── Fallback (no grammar) ─────────────────────────────────────────────────

function extractFallback(filePath: string, content: string): ExtractionResult {
  // Simple regex-based extraction when tree-sitter grammar unavailable
  const nodes: Node[] = []
  const edges: Edge[]  = []

  const funcPattern = /^func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(/gm
  let match: RegExpExecArray | null
  while ((match = funcPattern.exec(content)) !== null) {
    const name        = match[1]
    const pkgDir      = filePath.substring(0, filePath.lastIndexOf("/"))
    const canonicalID = `${pkgDir}:${name}`
    nodes.push({
      id:          nodeID(`symbol:${canonicalID}`),
      type:        "symbol",
      label:       name,
      canonicalID: canonicalID,
      sourceClass: "structural",
      properties:  { file_path: filePath, kind: "function", fallback: true },
    })
  }

  return { nodes, edges }
}
```

### Go concept seeds

```typescript
// plugins/go-language/src/concepts.ts

import type { ConceptSeed } from "@ce/plugin-sdk"

export const goConceptSeeds: ConceptSeed[] = [
  {
    term:       "error-handling",
    definition: "Go's explicit error return pattern",
    synonyms:   ["error", "err", "errors"],
    related:    ["error-wrapping", "sentinel-error"],
  },
  {
    term:       "interface",
    definition: "Go implicit interface satisfaction",
    synonyms:   ["contract", "protocol"],
    related:    ["implementation", "duck-typing"],
  },
  {
    term:       "goroutine",
    definition: "Lightweight concurrent execution unit",
    synonyms:   ["go-routine", "concurrent"],
    related:    ["channel", "sync", "waitgroup"],
  },
  {
    term:       "channel",
    definition: "Go typed message passing primitive",
    synonyms:   ["chan"],
    related:    ["goroutine", "select", "concurrency"],
  },
  {
    term:       "context",
    definition: "Go context for cancellation and deadline propagation",
    synonyms:   ["ctx"],
    related:    ["cancellation", "deadline", "timeout"],
  },
  {
    term:       "middleware",
    definition: "HTTP handler wrapping pattern",
    synonyms:   ["handler-chain", "interceptor"],
    related:    ["http-handler", "router"],
  },
]
```

### Interface implementation analyzer

```typescript
// plugins/go-language/src/analyzers/interface-impl.ts

import type { Analyzer, Node, Edge } from "@ce/plugin-sdk"
import { nodeID, edgeID }            from "@ce/plugin-sdk"

// Detects which types implement which interfaces by method set matching.
// Produces "implements" edges between type nodes and interface nodes.
export const interfaceImplAnalyzer: Analyzer = {
  name: "interface-impl",
  description: "Detects interface implementations by method set matching",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []

    // Collect interfaces and their method sets
    const interfaces = nodes.filter(n =>
      n.type === "symbol" && n.properties?.kind === "interface"
    )

    // Collect types and their methods
    const typeMethodSets = buildTypeMethodSets(nodes)

    for (const iface of interfaces) {
      const requiredMethods: string[] = iface.properties?.methods ?? []

      for (const [typeCanonicalID, methods] of typeMethodSets) {
        if (implementsInterface(methods, requiredMethods)) {
          const typeNodeID_ = nodeID(`symbol:${typeCanonicalID}`)

          edges.push({
            id:          edgeID(typeNodeID_, "implements", iface.id),
            sourceID:    typeNodeID_,
            targetID:    iface.id,
            type:        "implements",
            sourceClass: "structural",
            properties:  { analyzer: "interface-impl" },
          })
        }
      }
    }

    return edges
  }
}

function buildTypeMethodSets(nodes: Node[]): Map<string, Set<string>> {
  const result = new Map<string, Set<string>>()

  for (const node of nodes) {
    if (node.type === "symbol" && node.properties?.kind === "method") {
      const receiverType = node.properties.receiver_type as string
      if (!receiverType) continue

      const pkgDir     = node.canonicalID.substring(0, node.canonicalID.lastIndexOf(":"))
      const typeCanon  = `${pkgDir}:${receiverType}`
      const methodName = node.label.split(".")[1]

      if (!result.has(typeCanon)) {
        result.set(typeCanon, new Set())
      }
      result.get(typeCanon)!.add(methodName)
    }
  }

  return result
}

function implementsInterface(
  typeMethods:     Set<string>,
  requiredMethods: string[]
): boolean {
  return requiredMethods.every(m => typeMethods.has(m))
}
```

---

## 4. TypeScript/JavaScript Plugin

### Plugin manifest

```json
{
  "id":      "com.atheory.typescript",
  "name":    "TypeScript & JavaScript",
  "version": "1.0.0",
  "capabilities": {
    "language":  true,
    "role":      false,
    "analyzers": ["import-graph", "react-components"],
    "tools":     []
  },
  "language": {
    "extensions": [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"],
    "grammar":    "./typescript-grammar.wasm"
  }
}
```

### Extraction

```typescript
// plugins/typescript-language/src/extract.ts

export function extract(
  filePath: string,
  content:  string,
  tree:     SyntaxTree | null
): ExtractionResult {
  if (!tree) return extractFallback(filePath, content)

  const nodes: Node[] = []
  const edges: Edge[]  = []

  const fileNodeID_ = nodeID(`file:${filePath}`)
  nodes.push({
    id:          fileNodeID_,
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties:  { extension: filePath.split(".").pop() },
  })

  // Walk top-level declarations
  for (const child of tree.root.children) {
    switch (child.type) {

      case "import_declaration":
        extractImport(child, filePath, nodes, edges)
        break

      case "export_statement": {
        const inner = child.children.find(c =>
          ["function_declaration", "class_declaration",
           "lexical_declaration", "interface_declaration",
           "type_alias_declaration", "enum_declaration"].includes(c.type)
        )
        if (inner) extractDeclaration(inner, filePath, true, nodes, edges)
        break
      }

      case "function_declaration":
      case "class_declaration":
      case "lexical_declaration":
      case "interface_declaration":
      case "type_alias_declaration":
      case "enum_declaration":
        extractDeclaration(child, filePath, false, nodes, edges)
        break
    }
  }

  return { nodes, edges }
}

function extractDeclaration(
  node:      SyntaxNode,
  filePath:  string,
  exported:  boolean,
  nodes:     Node[],
  edges:     Edge[]
): void {
  const dir = filePath.substring(0, filePath.lastIndexOf("/"))

  switch (node.type) {

    case "function_declaration": {
      const name = findChild(node, "name")?.text
      if (!name) return
      const canonicalID = `${dir}:${name}`
      nodes.push({
        id:          nodeID(`symbol:${canonicalID}`),
        type:        "symbol",
        label:       name,
        canonicalID: canonicalID,
        sourceClass: "structural",
        properties: {
          file_path:  filePath,
          kind:       "function",
          exported:   exported,
          async:      !!findChild(node, "async"),
          start_line: node.startPosition.row + 1,
          end_line:   node.endPosition.row + 1,
        },
      })
      break
    }

    case "class_declaration": {
      const name = findChild(node, "name")?.text
      if (!name) return
      const canonicalID = `${dir}:${name}`
      const classNodeID_ = nodeID(`symbol:${canonicalID}`)

      // Check for extends
      const heritage = findChild(node, "class_heritage")
      const extendsNode = heritage?.children.find(c => c.type === "extends_clause")
      const superName   = extendsNode?.children.find(c => c.type === "identifier")?.text

      nodes.push({
        id:          classNodeID_,
        type:        "symbol",
        label:       name,
        canonicalID: canonicalID,
        sourceClass: "structural",
        properties: {
          file_path:  filePath,
          kind:       "class",
          exported:   exported,
          extends:    superName,
          start_line: node.startPosition.row + 1,
          end_line:   node.endPosition.row + 1,
        },
      })

      if (superName) {
        // Speculative edge — superclass may be in another file
        const superCanon   = `${dir}:${superName}`
        const superNodeID_ = nodeID(`symbol:${superCanon}`)
        edges.push({
          id:          edgeID(classNodeID_, "extends", superNodeID_),
          sourceID:    classNodeID_,
          targetID:    superNodeID_,
          type:        "extends",
          sourceClass: "speculative",
          properties:  { super_name: superName },
        })
      }

      // Extract methods
      const body = findChild(node, "class_body")
      if (body) {
        for (const member of body.children) {
          if (member.type === "method_definition") {
            const methodName = findChild(member, "name")?.text
            if (!methodName) continue
            const methodCanon   = `${canonicalID}.${methodName}`
            const methodNodeID_ = nodeID(`symbol:${methodCanon}`)
            nodes.push({
              id:          methodNodeID_,
              type:        "symbol",
              label:       `${name}.${methodName}`,
              canonicalID: methodCanon,
              sourceClass: "structural",
              properties: {
                file_path:  filePath,
                kind:       "method",
                class:      name,
                static:     !!findChild(member, "static"),
                start_line: member.startPosition.row + 1,
                end_line:   member.endPosition.row + 1,
              },
            })
            edges.push({
              id:          edgeID(methodNodeID_, "method_of", classNodeID_),
              sourceID:    methodNodeID_,
              targetID:    classNodeID_,
              type:        "method_of",
              sourceClass: "structural",
            })
          }
        }
      }
      break
    }

    case "interface_declaration": {
      const name = findChild(node, "name")?.text
      if (!name) return
      const canonicalID = `${dir}:${name}`
      const methods = extractInterfaceMethods(node)
      nodes.push({
        id:          nodeID(`symbol:${canonicalID}`),
        type:        "symbol",
        label:       name,
        canonicalID: canonicalID,
        sourceClass: "structural",
        properties: {
          file_path:  filePath,
          kind:       "interface",
          exported:   exported,
          methods:    methods,
          start_line: node.startPosition.row + 1,
        },
      })
      break
    }
  }
}
```

---

## 5. Python Plugin

### Plugin manifest

```json
{
  "id":      "com.atheory.python",
  "name":    "Python",
  "version": "1.0.0",
  "capabilities": {
    "language":  true,
    "role":      false,
    "analyzers": ["module-deps", "class-hierarchy"],
    "tools":     []
  },
  "language": {
    "extensions": [".py", ".pyi"],
    "grammar":    "./python-grammar.wasm"
  }
}
```

### Extraction

```typescript
// plugins/python-language/src/extract.ts

export function extract(
  filePath: string,
  content:  string,
  tree:     SyntaxTree | null
): ExtractionResult {
  if (!tree) return extractFallback(filePath, content)

  const nodes: Node[] = []
  const edges: Edge[]  = []

  const dir         = filePath.substring(0, filePath.lastIndexOf("/"))
  const moduleName  = filePath
    .replace(/\//g, ".")
    .replace(/\.py$/, "")
    .replace(/\/__init__$/, "")

  // File node
  nodes.push({
    id:          nodeID(`file:${filePath}`),
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties:  { module: moduleName },
  })

  // Module node
  nodes.push({
    id:          nodeID(`namespace:${moduleName}`),
    type:        "namespace",
    label:       moduleName,
    canonicalID: moduleName,
    sourceClass: "structural",
  })

  // Walk top-level statements
  for (const child of tree.root.children) {
    switch (child.type) {

      case "import_statement":
      case "import_from_statement":
        extractPythonImport(child, filePath, nodes, edges)
        break

      case "function_definition":
        extractPythonFunction(child, filePath, moduleName, nodes, edges)
        break

      case "class_definition":
        extractPythonClass(child, filePath, moduleName, nodes, edges)
        break

      case "decorated_definition": {
        // @decorator\ndef foo() or @decorator\nclass Foo
        const inner = child.children.find(c =>
          c.type === "function_definition" || c.type === "class_definition"
        )
        const decorators = child.children
          .filter(c => c.type === "decorator")
          .map(c => c.text.replace(/^@/, "").trim())

        if (inner?.type === "function_definition") {
          extractPythonFunction(inner, filePath, moduleName, nodes, edges, decorators)
        } else if (inner?.type === "class_definition") {
          extractPythonClass(inner, filePath, moduleName, nodes, edges, decorators)
        }
        break
      }
    }
  }

  return { nodes, edges }
}

function extractPythonClass(
  node:       SyntaxNode,
  filePath:   string,
  moduleName: string,
  nodes:      Node[],
  edges:      Edge[],
  decorators: string[] = []
): void {
  const nameNode = node.children.find(c => c.type === "identifier")
  if (!nameNode) return

  const className   = nameNode.text
  const canonicalID = `${moduleName}:${className}`
  const classNodeID_ = nodeID(`symbol:${canonicalID}`)

  // Base classes
  const argList  = node.children.find(c => c.type === "argument_list")
  const baseNames = argList?.children
    .filter(c => c.type === "identifier")
    .map(c => c.text) ?? []

  nodes.push({
    id:          classNodeID_,
    type:        "symbol",
    label:       className,
    canonicalID: canonicalID,
    sourceClass: "structural",
    properties: {
      file_path:  filePath,
      module:     moduleName,
      kind:       "class",
      bases:      baseNames,
      decorators: decorators,
      start_line: node.startPosition.row + 1,
      end_line:   node.endPosition.row + 1,
    },
  })

  // Speculative extends edges
  for (const base of baseNames) {
    if (base === "object") continue
    const baseCanon   = `${moduleName}:${base}`
    const baseNodeID_ = nodeID(`symbol:${baseCanon}`)
    edges.push({
      id:          edgeID(classNodeID_, "extends", baseNodeID_),
      sourceID:    classNodeID_,
      targetID:    baseNodeID_,
      type:        "extends",
      sourceClass: "speculative",
      properties:  { base_class: base },
    })
  }

  // Methods inside the class body
  const body = node.children.find(c => c.type === "block")
  if (body) {
    for (const stmt of body.children) {
      if (stmt.type === "function_definition") {
        const methodName = stmt.children.find(c => c.type === "identifier")?.text
        if (!methodName) continue
        const methodCanon   = `${canonicalID}.${methodName}`
        const methodNodeID_ = nodeID(`symbol:${methodCanon}`)

        nodes.push({
          id:          methodNodeID_,
          type:        "symbol",
          label:       `${className}.${methodName}`,
          canonicalID: methodCanon,
          sourceClass: "structural",
          properties: {
            file_path:  filePath,
            module:     moduleName,
            kind:       "method",
            class:      className,
            is_dunder:  methodName.startsWith("__"),
            start_line: stmt.startPosition.row + 1,
            end_line:   stmt.endPosition.row + 1,
          },
        })

        edges.push({
          id:          edgeID(methodNodeID_, "method_of", classNodeID_),
          sourceID:    methodNodeID_,
          targetID:    classNodeID_,
          type:        "method_of",
          sourceClass: "structural",
        })
      }
    }
  }
}
```

---

## 6. Build Pipeline

Each plugin is compiled from TypeScript to WASM via Javy.
The grammar WASM files come from the tree-sitter project.

```bash
# plugins/go-language/package.json (build script)
{
  "scripts": {
    "build":     "tsc && javy compile dist/index.js -o go-language.wasm",
    "build:dev": "tsc --watch",
    "test":      "ce-sandbox coverage --fixtures fixtures/",
    "validate":  "ce plugin validate go-language.wasm"
  }
}
```

### Grammar WASM sourcing

Grammar WASM files are built from the tree-sitter grammar repositories.
They are checked into the plugin directory as binary artifacts (not generated
at build time — too slow and fragile).

```bash
# scripts/update-grammars.sh
# Run when tree-sitter grammars need updating

# Go grammar
cd /tmp && git clone https://github.com/tree-sitter/tree-sitter-go
cd tree-sitter-go
npx tree-sitter build --wasm
cp tree-sitter-go.wasm /path/to/plugin-sdk/plugins/go-language/go-grammar.wasm

# TypeScript grammar
cd /tmp && git clone https://github.com/tree-sitter/tree-sitter-typescript
cd tree-sitter-typescript
npx tree-sitter build --wasm typescript/
cp typescript/tree-sitter-typescript.wasm \
   /path/to/plugin-sdk/plugins/typescript-language/typescript-grammar.wasm

# Python grammar
cd /tmp && git clone https://github.com/tree-sitter/tree-sitter-python
cd tree-sitter-python
npx tree-sitter build --wasm
cp tree-sitter-python.wasm /path/to/plugin-sdk/plugins/python-language/python-grammar.wasm
```

### Engine build-time embedding

```makefile
# Makefile at engine repo root

.PHONY: embed-plugins
embed-plugins:
	@echo "Copying default plugin WASMs from SDK release..."
	cp $(SDK_RELEASE_DIR)/go-language.wasm      internal/indexer/defaults/
	cp $(SDK_RELEASE_DIR)/go-grammar.wasm        internal/indexer/defaults/
	cp $(SDK_RELEASE_DIR)/typescript.wasm        internal/indexer/defaults/
	cp $(SDK_RELEASE_DIR)/typescript-grammar.wasm internal/indexer/defaults/
	cp $(SDK_RELEASE_DIR)/python.wasm            internal/indexer/defaults/
	cp $(SDK_RELEASE_DIR)/python-grammar.wasm    internal/indexer/defaults/

.PHONY: build
build: embed-plugins
	goreleaser build --snapshot --clean
```

---

## 7. Coverage Targets

Minimum coverage (from ce-sandbox) before a plugin ships:

| Plugin | Target | Measures |
|--------|--------|---------|
| go-language | 85% | functions, methods, types, interfaces |
| typescript | 80% | functions, classes, interfaces, exports |
| python | 80% | functions, classes, methods |

---

## 8. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Plugin location | SDK repo under plugins/go-language etc. |
| Grammar WASMs | Checked in as binary artifacts, updated by script |
| Fallback extraction | Regex-based when grammar unavailable (non-fatal) |
| Interface detection | Analyzer pass (not tree-sitter) — method set matching |
| Extends edges | Speculative source class — may cross file boundaries |
| Method extraction | Always produced, linked to parent type via method_of edge |
| Canonical ID format | Go: `pkg/dir:TypeName.MethodName`  TS: `dir:ClassName.method`  Python: `module.path:ClassName.method` |
| Export detection | Go: uppercase first letter  TS: explicit export keyword  Python: no leading underscore |
| Grammar updates | Script-based, not automated — grammar stability is a feature |
| Coverage minimum | 80-85% depending on language |

---

*Spec 16: Default Plugins — v1.0 — February 2026*
*Next: Spec 17 — Org Graph*
*Companion: Context Engine PRD v0.5 Section 13.2 | Decisions Log v1.0 Section 12*
