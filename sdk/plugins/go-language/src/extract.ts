import type { LanguageDefinition, ExtractionResult, ExtractedFunction, Node, Edge, SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { nodeID, edgeID, childByField, childrenByType, firstByType, firstDescendantByType, walk } from "@atheory-ai/ce-plugin-sdk"
import { liftGoFunction, collectImports } from "./lift.js"

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

  const pkgDir = filePath.substring(0, filePath.lastIndexOf("/")) || "."

  // package_clause → package_identifier gives the package name.
  const pkgClause = tree ? firstByType(tree, "package_clause") : null
  const pkgName   = pkgClause ? firstDescendantByType(pkgClause, "package_identifier")?.text ?? "" : ""

  // ── File node ─────────────────────────────────────────────────────────────
  const fileID = nodeID("", "file", filePath)
  nodes.push({
    id:          fileID,
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties:  { package: pkgName, line_count: content.split("\n").length },
  })

  // Without a grammar the host sends no tree; emit only the file node rather
  // than falling back to fragile text matching.
  if (!tree) {
    return deduplicate(nodes, edges)
  }

  // ── Package / namespace node ───────────────────────────────────────────────
  if (pkgName && pkgName !== "main") {
    const pkgID = nodeID("", "namespace", pkgDir)
    nodes.push({
      id:          pkgID,
      type:        "namespace",
      label:       pkgName,
      canonicalID: pkgDir,
      sourceClass: "structural",
      properties:  { package: pkgName },
    })
    edges.push({
      id:          edgeID(fileID, "belongs_to", pkgID),
      sourceID:    fileID,
      targetID:    pkgID,
      type:        "belongs_to",
      sourceClass: "structural",
      properties:  {},
    })
  }

  const symbol = (name: string, label: string, kind: string, node: SyntaxNode, extra: Record<string, unknown> = {}): string => {
    const canonical = `${pkgDir}:${name}`
    const id = nodeID("", "symbol", canonical)
    nodes.push({
      id,
      type:        "symbol",
      label,
      canonicalID: canonical,
      sourceClass: "structural",
      properties: {
        file_path:  filePath,
        package:    pkgName,
        kind,
        exported:   isExported(label.split(".").pop() ?? label),
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

  for (const node of tree.children ?? []) {
    switch (node.type) {
      case "import_declaration":
        addImports(node, fileID, nodes, edges)
        break

      case "function_declaration": {
        const name = childByField(node, "name")?.text
        if (name) {
          const id = symbol(name, name, "function", node)
          iir.push({ nodeId: id, intent: liftGoFunction(name, node, imports) })
        }
        break
      }

      case "method_declaration": {
        const name = childByField(node, "name")?.text
        if (!name) break
        const receiver     = childByField(node, "receiver")
        const receiverType = receiver ? firstDescendantByType(receiver, "type_identifier")?.text ?? null : null
        const canonicalName = receiverType ? `${receiverType}.${name}` : name
        const id = symbol(canonicalName, canonicalName, "method", node, { receiver_type: receiverType ?? undefined })
        iir.push({ nodeId: id, intent: liftGoFunction(name, node, imports) })
        break
      }

      case "type_declaration": {
        // A type_declaration wraps one or more type_spec / type_alias nodes.
        for (const spec of [...childrenByType(node, "type_spec"), ...childrenByType(node, "type_alias")]) {
          const name = childByField(spec, "name")?.text
          if (!name) continue
          const kind = typeKind(spec)
          symbol(name, name, kind, spec)
        }
        break
      }
    }
  }

  const result = deduplicate(nodes, edges)
  return iir.length ? { ...result, iir } : result
}

// typeKind maps a type_spec/type_alias's `type` field to struct/interface/alias.
function typeKind(spec: SyntaxNode): string {
  const t = childByField(spec, "type")?.type
  if (t === "struct_type")    return "struct"
  if (t === "interface_type") return "interface"
  return "alias"
}

// addImports emits a namespace node + imports edge per import_spec path.
function addImports(node: SyntaxNode, fileID: string, nodes: Node[], edges: Edge[]): void {
  // Imports may be a single import_spec or an import_spec_list of many.
  walk(node, spec => {
    if (spec.type !== "import_spec") return
    const path = childByField(spec, "path")
    if (!path) return
    const imp = path.text.replace(/^[`"]|[`"]$/g, "")
    if (!imp) return
    const impID = nodeID("", "namespace", imp)
    nodes.push({
      id:          impID,
      type:        "namespace",
      label:       imp.split("/").pop() ?? imp,
      canonicalID: imp,
      sourceClass: "structural",
      properties:  { import_path: imp },
    })
    edges.push({
      id:          edgeID(fileID, "imports", impID),
      sourceID:    fileID,
      targetID:    impID,
      type:        "imports",
      sourceClass: "structural",
      properties:  {},
    })
  })
}

// ── Utilities ─────────────────────────────────────────────────────────────────

function isExported(name: string): boolean {
  return name.length > 0 && name[0] >= "A" && name[0] <= "Z"
}

function deduplicate(nodes: Node[], edges: Edge[]): ExtractionResult {
  const seenN = new Set<string>()
  const seenE = new Set<string>()
  return {
    nodes: nodes.filter(n => seenN.has(n.id) ? false : (seenN.add(n.id), true)),
    edges: edges.filter(e => seenE.has(e.id) ? false : (seenE.add(e.id), true)),
  }
}
