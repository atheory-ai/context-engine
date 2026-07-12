import type { LanguageDefinition, ExtractionResult, ExtractedFunction, Node, Edge, SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { nodeID, edgeID, childByField, childrenByType, firstByType, hasChildType, walkTopLevel } from "@atheory-ai/ce-plugin-sdk"
import { liftPyFunction, collectImports } from "./lift.js"

// Structural extraction walks the tree-sitter CST the host provides — never
// regex. The host already parsed the file; we turn its tree into graph nodes.

export const extract: LanguageDefinition["extract"] = (
  filePath: string,
  content:  string,
  tree:     SyntaxNode | null,
): ExtractionResult => {
  const nodes: Node[] = []
  const edges: Edge[] = []
  const iir:   ExtractedFunction[] = []
  const imports = tree ? collectImports(tree) : new Map<string, string>()

  const ext = filePath.substring(filePath.lastIndexOf("."))

  // ── File node ─────────────────────────────────────────────────────────────
  const fileID = nodeID("", "file", filePath)
  nodes.push({
    id:          fileID,
    type:        "file",
    label:       filePath.split("/").pop() ?? filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties: {
      extension:   ext,
      is_stub:     ext === ".pyi",
      line_count:  content.split("\n").length,
    },
  })

  // ── Module namespace ──────────────────────────────────────────────────────
  // Derive module name from file path (e.g. "src/utils/helpers.py" → "utils.helpers")
  const moduleName = filePath
    .replace(/\.pyi?w?$/, "")
    .replace(/\//g, ".")
    .replace(/^\.+/, "")

  const modID = nodeID("", "namespace", moduleName)
  nodes.push({
    id:          modID,
    type:        "namespace",
    label:       moduleName.split(".").pop() ?? moduleName,
    canonicalID: moduleName,
    sourceClass: "structural",
    properties:  { module_path: moduleName, file_path: filePath },
  })
  edges.push({
    id:          edgeID(fileID, "defines", modID),
    sourceID:    fileID,
    targetID:    modID,
    type:        "defines",
    sourceClass: "structural",
    properties:  {},
  })

  // Without a grammar the host sends no tree; emit only file + module rather
  // than falling back to fragile text matching.
  if (!tree) {
    return deduplicate(nodes, edges)
  }

  const importNamespace = (importPath: string, style: string, relative: boolean): void => {
    if (!importPath) return
    // Relative imports (`.models`) are meaningless across packages, so resolve
    // them against this module's package before using as a canonical ID —
    // otherwise `.models` from two different packages collapse into one node.
    const canonical = relative ? resolveRelativeImport(importPath, moduleName) : importPath
    const impID = nodeID("", "namespace", canonical)
    nodes.push({
      id:          impID,
      type:        "namespace",
      label:       canonical.split(".").pop() ?? canonical,
      canonicalID: canonical,
      sourceClass: "structural",
      properties:  { import_path: importPath, from_file: filePath, ...(style === "from" ? { relative } : {}) },
    })
    edges.push({
      id:          edgeID(fileID, "imports", impID),
      sourceID:    fileID,
      targetID:    impID,
      type:        "imports",
      sourceClass: "structural",
      properties:  { import_style: style },
    })
  }

  const symbol = (
    name: string,
    kind: string,
    node: SyntaxNode,
    extra: Record<string, unknown> = {},
    qualifiedName = name,
  ): string => {
    // Qualify by module (and enclosing class for methods) so common names like
    // `save` in different classes/files don't collapse into one symbol node.
    const canonical = `${moduleName}:${qualifiedName}`
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

  // Walk the whole module. Collect defs/classes/imports/constants, tracking the
  // enclosing class so a def inside a class body is a method.
  for (const stmt of tree.children ?? []) {
    switch (stmt.type) {
      case "import_statement":
        addImportStatement(stmt, importNamespace)
        break
      case "import_from_statement":
        addImportFrom(stmt, importNamespace)
        break
      case "class_definition":
        addClass(stmt, [], moduleName, symbol, edges, iir, imports)
        break
      case "decorated_definition": {
        const inner = childByField(stmt, "definition")
        if (!inner) break
        const decos = decoratorNames(stmt)
        if (inner.type === "class_definition") addClass(inner, decos, moduleName, symbol, edges, iir, imports)
        else if (inner.type === "function_definition") addFunction(inner, decos, false, symbol, iir, imports)
        break
      }
      case "function_definition":
        addFunction(stmt, [], false, symbol, iir, imports)
        break
      case "expression_statement":
        addConstant(stmt, symbol)
        break
    }
  }

  const result = deduplicate(nodes, edges)
  return iir.length ? { ...result, iir } : result
}

// addImportStatement handles `import a`, `import a.b`, `import a as b`.
function addImportStatement(
  node: SyntaxNode,
  emit: (path: string, style: string, relative: boolean) => void,
): void {
  for (const child of node.children ?? []) {
    if (child.type === "dotted_name") emit(child.text, "direct", false)
    else if (child.type === "aliased_import") {
      const name = childByField(child, "name")
      if (name) emit(name.text, "direct", false)
    }
  }
}

// addImportFrom handles `from x import a`, `from .rel import a`.
function addImportFrom(
  node: SyntaxNode,
  emit: (path: string, style: string, relative: boolean) => void,
): void {
  const module = childByField(node, "module_name")
  if (!module) return
  const fromPath = module.text
  if (!fromPath) return
  emit(fromPath, "from", fromPath.startsWith("."))
}

// resolveRelativeImport turns a Python relative import (`.models`, `..pkg.mod`)
// into an absolute dotted path, resolved against the importing module. One dot
// is the current package; each extra dot climbs one package higher.
function resolveRelativeImport(importPath: string, moduleName: string): string {
  const dots = (importPath.match(/^\.+/)?.[0] ?? "").length
  if (dots === 0) return importPath
  const rest = importPath.slice(dots)
  // The importing module's own package = its dotted path minus the final part.
  const pkg = moduleName.split(".").slice(0, -1)
  const base = pkg.slice(0, Math.max(0, pkg.length - (dots - 1)))
  const parts = [...base, ...(rest ? rest.split(".") : [])].filter(Boolean)
  // If resolution runs off the top (over-deep relative), fall back to the raw
  // path scoped by module so it stays unique rather than collapsing globally.
  return parts.length > 0 ? parts.join(".") : `${moduleName}:${importPath}`
}

// addClass emits a class symbol, its base metadata, and extends edges, then its methods.
function addClass(
  node: SyntaxNode,
  decos: string[],
  moduleName: string,
  symbol: (name: string, kind: string, node: SyntaxNode, extra?: Record<string, unknown>, qualifiedName?: string) => string,
  edges: Edge[],
  iir: ExtractedFunction[],
  imports: Set<string>,
): void {
  const name = childByField(node, "name")?.text
  if (!name) return

  const isDunder  = name.startsWith("__") && name.endsWith("__")
  const isPrivate = name.startsWith("_") && !isDunder
  const bases = classBases(node)
  const isDataclass = decos.some(d => d.includes("dataclass"))
  const isAbstract  = bases.some(b => b === "ABC" || b === "ABCMeta" || b.endsWith(".ABC") || b.endsWith(".ABCMeta"))
  const isException = bases.some(b => b === "Exception" || b === "BaseException" || b.endsWith("Error") || b.endsWith("Exception"))

  const classID = symbol(name, "class", node, {
    exported:     !isPrivate,
    bases:        bases.length > 0 ? bases : undefined,
    is_dataclass: isDataclass,
    is_abstract:  isAbstract,
    is_exception: isException,
    decorators:   decos.length > 0 ? decos : undefined,
  })

  // Inheritance edges (speculative — base may be external).
  for (const base of bases) {
    if (!base || base === "object") continue
    const baseName  = base.split(".").pop() ?? base
    const baseCanon = `${moduleName}:${baseName}`
    const baseID    = nodeID("", "symbol", baseCanon)
    edges.push({
      id:          edgeID(classID, "extends", baseID),
      sourceID:    classID,
      targetID:    baseID,
      type:        "extends",
      sourceClass: "speculative",
      properties:  { super_name: base },
    })
  }

  // Methods: function definitions directly in the class body.
  const body = childByField(node, "body")
  for (const member of body?.children ?? []) {
    if (member.type === "function_definition") addFunction(member, [], true, symbol, iir, imports, name)
    else if (member.type === "decorated_definition") {
      const inner = childByField(member, "definition")
      if (inner?.type === "function_definition") addFunction(inner, decoratorNames(member), true, symbol, iir, imports, name)
    }
  }
}

// addFunction emits a function/method symbol with Python-specific metadata.
function addFunction(
  node: SyntaxNode,
  decos: string[],
  isMethod: boolean,
  symbol: (name: string, kind: string, node: SyntaxNode, extra?: Record<string, unknown>, qualifiedName?: string) => string,
  iir: ExtractedFunction[],
  imports: Set<string>,
  ownerName?: string,
): void {
  const name = childByField(node, "name")?.text
  if (!name) return

  const isDunder  = name.startsWith("__") && name.endsWith("__")
  const isPrivate = name.startsWith("_") && !isDunder
  const isExported = !isMethod && !isPrivate

  const id = symbol(name, isMethod ? "method" : "function", node, {
    exported:     isExported,
    async:        hasChildType(node, "async"),
    dunder:       isDunder,
    private:      isPrivate,
    is_property:  decos.includes("property"),
    is_generator: hasYield(node),
    classmethod:  decos.includes("classmethod"),
    staticmethod: decos.includes("staticmethod"),
    decorators:   decos.length > 0 ? decos : undefined,
  }, ownerName ? `${ownerName}.${name}` : name)

  iir.push({ nodeId: id, intent: liftPyFunction(name, node, isPrivate, isMethod, imports) })
}

// addConstant emits a symbol for a module-level UPPER_CASE assignment.
function addConstant(
  node: SyntaxNode,
  symbol: (name: string, kind: string, node: SyntaxNode, extra?: Record<string, unknown>) => string,
): void {
  const assign = firstByType(node, "assignment")
  if (!assign) return
  const target = childByField(assign, "left")
  if (!target || target.type !== "identifier") return
  const name = target.text
  if (!/^[A-Z][A-Z0-9_]{2,}$/.test(name)) return
  symbol(name, "constant", node, { exported: true })
}

// decoratorNames returns each decorator's name (e.g. "dataclass", "app.route").
function decoratorNames(decorated: SyntaxNode): string[] {
  return childrenByType(decorated, "decorator").map(d => {
    // "@dataclass" / "@app.route(...)" → strip "@", drop call args.
    return d.text.replace(/^@/, "").split("(")[0].trim()
  })
}

// classBases returns positional superclass names from a class's argument_list.
function classBases(node: SyntaxNode): string[] {
  const args = childByField(node, "superclasses")
  if (!args) return []
  const bases: string[] = []
  for (const child of args.children ?? []) {
    if (child.type === "identifier" || child.type === "attribute") bases.push(child.text)
  }
  return bases
}

// hasYield reports whether a function's own body contains a yield (generator),
// without descending into nested function scopes.
function hasYield(fn: SyntaxNode): boolean {
  const body = childByField(fn, "body")
  if (!body) return false
  let found = false
  walkTopLevel(body, n => {
    if (n.type === "yield") found = true
  })
  return found
}

function deduplicate(nodes: Node[], edges: Edge[]): ExtractionResult {
  const seenN = new Set<string>()
  const seenE = new Set<string>()
  return {
    nodes: nodes.filter(n => seenN.has(n.id) ? false : (seenN.add(n.id), true)),
    edges: edges.filter(e => seenE.has(e.id) ? false : (seenE.add(e.id), true)),
  }
}
