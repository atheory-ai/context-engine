import type { LanguageDefinition, ExtractionResult, ExtractedFunction, Node, Edge, SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { nodeID, edgeID, childByField, childrenByType, firstByType, hasChildType, firstDescendantByType } from "@atheory-ai/ce-plugin-sdk"
import { liftFunction, collectImports } from "./lift.js"

// Structural extraction walks the tree-sitter CST the host provides — never
// regex. The host already parsed the file; we turn its tree into graph nodes.

export const extract: LanguageDefinition["extract"] = (
  filePath: string,
  content:  string,
  tree:     SyntaxNode | null,
): ExtractionResult => {
  const nodes: Node[] = []
  const edges: Edge[]  = []
  const iir:   ExtractedFunction[] = []
  const imports = tree ? collectImports(tree) : new Set<string>()

  const dir  = filePath.substring(0, filePath.lastIndexOf("/")) || "."
  const ext  = filePath.substring(filePath.lastIndexOf("."))
  const isTS = ext === ".ts" || ext === ".tsx"
  const isJSX = ext === ".tsx" || ext === ".jsx"

  // ── File node ─────────────────────────────────────────────────────────────
  const fileID = nodeID("", "file", filePath)
  nodes.push({
    id:          fileID,
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties: {
      extension: ext,
      is_typescript: isTS,
      is_jsx:        isJSX,
      line_count:    content.split("\n").length,
    },
  })

  // Without a grammar the host sends no tree; emit only the file node rather
  // than falling back to fragile text matching.
  if (!tree) {
    return deduplicate(nodes, edges)
  }

  const symbol = (name: string, kind: string, node: SyntaxNode, extra: Record<string, unknown> = {}): string => {
    const canonical = `${dir}:${name}`
    const id = nodeID("", "symbol", canonical)
    nodes.push({
      id,
      type:        "symbol",
      label:       name,
      canonicalID: canonical,
      sourceClass: "structural",
      properties: {
        file_path:  filePath,
        kind,
        start_byte: node.startByte,
        start_line: node.startPosition.row,
        ...extra,
      },
    })
    edges.push({
      id:          edgeID(fileID, "defines", id),
      sourceID:    fileID,
      targetID:    id,
      type:        "defines",
      sourceClass: "structural",
      properties:  {},
    })
    return id
  }

  // Walk top-level statements. Declarations may be wrapped in an
  // export_statement; unwrap it and mark the inner declaration exported.
  for (const stmt of tree.children ?? []) {
    let node = stmt
    let exported = false
    if (stmt.type === "export_statement") {
      exported = true
      const inner = (stmt.children ?? []).find(c => c.type.endsWith("_declaration"))
      if (!inner) {
        // `export { a, b }` / `export default <expr>` — no declaration to name.
        continue
      }
      node = inner
    }

    switch (node.type) {
      case "import_statement":
        addImport(node, filePath, fileID, nodes, edges)
        break

      case "function_declaration":
      case "generator_function_declaration": {
        const name = fieldName(node)
        if (name) {
          const id = symbol(name, "function", node, { exported, async: isAsync(node) })
          iir.push({ nodeId: id, intent: liftFunction(name, node, exported, imports) })
        }
        break
      }

      case "lexical_declaration":
      case "variable_declaration":
        // const/let name = (args) => ... — capture arrow functions as symbols.
        for (const declr of childrenByType(node, "variable_declarator")) {
          const value = childByField(declr, "value")
          if (value && (value.type === "arrow_function" || value.type === "function_expression")) {
            const name = fieldName(declr)
            if (name) {
              const id = symbol(name, "function", node, { exported, arrow: true, async: isAsync(value) })
              // Symbol anchors on the declaration (start_byte parity with the
              // host); params/return are lifted from the function value.
              iir.push({ nodeId: id, intent: liftFunction(name, value, exported, imports) })
            }
          }
        }
        break

      case "class_declaration":
      case "abstract_class_declaration": {
        const name = fieldName(node)
        if (!name) break
        const abstract = node.type === "abstract_class_declaration" || isAbstract(node)
        const classID = symbol(name, "class", node, { exported, abstract })
        addHeritage(node, dir, classID, edges)
        addMethods(node, symbol)
        break
      }

      case "interface_declaration": {
        const name = fieldName(node)
        if (name) symbol(name, "interface", node, { exported })
        break
      }

      case "type_alias_declaration": {
        const name = fieldName(node)
        if (name) symbol(name, "type", node, { exported })
        break
      }

      case "enum_declaration": {
        const name = fieldName(node)
        if (name) symbol(name, "enum", node, { exported })
        break
      }
    }
  }

  const result = deduplicate(nodes, edges)
  return iir.length ? { ...result, iir } : result
}

// fieldName returns the text of a declaration's `name` field.
function fieldName(node: SyntaxNode): string {
  return childByField(node, "name")?.text ?? ""
}

function isAsync(node: SyntaxNode): boolean {
  return hasChildType(node, "async")
}

function isAbstract(node: SyntaxNode): boolean {
  return hasChildType(node, "abstract")
}

// addImport emits a namespace node + imports edge for the import path.
function addImport(node: SyntaxNode, filePath: string, fileID: string, nodes: Node[], edges: Edge[]): void {
  const str = firstDescendantByType(node, "string")
  if (!str) return
  const importPath = str.text.replace(/^['"`]|['"`]$/g, "")
  if (!importPath) return
  const impID = nodeID("", "namespace", importPath)
  nodes.push({
    id:          impID,
    type:        "namespace",
    label:       importPath.split("/").pop() ?? importPath,
    canonicalID: importPath,
    sourceClass: "structural",
    properties:  { import_path: importPath, from_file: filePath },
  })
  edges.push({
    id:          edgeID(fileID, "imports", impID),
    sourceID:    fileID,
    targetID:    impID,
    type:        "imports",
    sourceClass: "structural",
    properties:  {},
  })
}

// addHeritage emits an extends edge for a class's superclass, if any.
function addHeritage(classNode: SyntaxNode, dir: string, classID: string, edges: Edge[]): void {
  const heritage = firstByType(classNode, "class_heritage")
  if (!heritage) return
  // Only an extends_clause is a superclass — an implements_clause is not.
  const extendsClause = firstDescendantByType(heritage, "extends_clause")
  if (!extendsClause) return
  const superId = firstDescendantByType(extendsClause, "identifier")
  if (!superId) return
  const superCanon = `${dir}:${superId.text}`
  const superNodeID = nodeID("", "symbol", superCanon)
  edges.push({
    id:          edgeID(classID, "extends", superNodeID),
    sourceID:    classID,
    targetID:    superNodeID,
    type:        "extends",
    sourceClass: "speculative",
    properties:  { super_name: superId.text },
  })
}

// addMethods emits a symbol per method in a class body — a case regex missed.
function addMethods(
  classNode: SyntaxNode,
  symbol: (name: string, kind: string, node: SyntaxNode, extra?: Record<string, unknown>) => string,
): void {
  const body = childByField(classNode, "body")
  if (!body) return
  const className = childByField(classNode, "name")?.text ?? ""
  for (const member of childrenByType(body, "method_definition")) {
    const name = childByField(member, "name")?.text
    if (name) symbol(`${className}.${name}`, "method", member, { async: isAsync(member) })
  }
}

function deduplicate(nodes: Node[], edges: Edge[]): ExtractionResult {
  const seenN = new Set<string>()
  const seenE = new Set<string>()
  return {
    nodes: nodes.filter(n => seenN.has(n.id) ? false : (seenN.add(n.id), true)),
    edges: edges.filter(e => seenE.has(e.id) ? false : (seenE.add(e.id), true)),
  }
}
