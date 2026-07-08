import { beforeAll, describe, expect, it } from "vitest"
import { createHash } from "crypto"
import type { SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { extract } from "../src/extract.js"

// The extractor walks the tree-sitter CST the host provides. These tests build
// faithful SyntaxNode trees (matching tree-sitter-python's node types and field
// names) and assert extraction — no regex, no parser dependency. Full
// end-to-end fidelity is validated by indexing with the real plugin in CE.

beforeAll(() => {
  const g = globalThis as unknown as Record<string, unknown>
  g.__ce_node_id = (p: string, t: string, c: string) => digest(`${p}:${t}:${c}`)
  g.__ce_edge_id = (s: string, t: string, tgt: string) => digest(`${s}:${t}:${tgt}`)
})

function digest(s: string): string {
  return createHash("sha256").update(s).digest("hex").slice(0, 32)
}

// n builds a SyntaxNode; `field` sets the node's fieldName in its parent.
function n(type: string, opts: {
  field?: string
  text?: string
  startByte?: number
  row?: number
  children?: SyntaxNode[]
} = {}): SyntaxNode {
  return {
    type,
    isNamed: true,
    fieldName: opts.field ?? null,
    text: opts.text ?? "",
    startByte: opts.startByte ?? 0,
    endByte: (opts.startByte ?? 0) + (opts.text?.length ?? 0),
    startPosition: { row: opts.row ?? 0, column: 0 },
    endPosition: { row: opts.row ?? 0, column: 0 },
    children: opts.children ?? [],
  }
}

const name = (t: string) => n("identifier", { field: "name", text: t })
const module = (...children: SyntaxNode[]) => n("module", { children })

// funcDef builds a function_definition; pass async:true to prepend the token.
function funcDef(fnName: string, opts: { async?: boolean; body?: SyntaxNode[] } = {}): SyntaxNode {
  const children: SyntaxNode[] = []
  if (opts.async) children.push(n("async", { text: "async" }))
  children.push(name(fnName), n("parameters", { field: "parameters" }))
  children.push(n("block", { field: "body", children: opts.body ?? [] }))
  return n("function_definition", { children })
}

function decorated(defNode: SyntaxNode, ...decoTexts: string[]): SyntaxNode {
  const decos = decoTexts.map(t => n("decorator", { text: t }))
  return n("decorated_definition", { children: [...decos, { ...defNode, fieldName: "definition" }] })
}

function classDef(clsName: string, bases: string[], body: SyntaxNode[] = []): SyntaxNode {
  const children: SyntaxNode[] = [name(clsName)]
  if (bases.length > 0) {
    children.push(n("argument_list", {
      field: "superclasses",
      children: bases.map(b => n(b.includes(".") ? "attribute" : "identifier", { text: b })),
    }))
  }
  children.push(n("block", { field: "body", children: body }))
  return n("class_definition", { children })
}

function syms(tree: SyntaxNode, filePath = "src/service.py") {
  const { nodes, edges } = extract(filePath, "line\nline\nline\n", tree)
  return {
    nodes,
    edges,
    symbols: nodes.filter(x => x.type === "symbol"),
    byLabel: (label: string) => nodes.find(x => x.type === "symbol" && x.label === label),
  }
}

describe("tree-based Python extraction", () => {
  it("emits file + module namespace even for an empty tree", () => {
    const { nodes, edges } = syms(module())
    expect(nodes.filter(x => x.type === "file")).toHaveLength(1)
    const mod = nodes.find(x => x.type === "namespace")
    expect(mod?.canonicalID).toBe("src.service")
    expect(edges.some(e => e.type === "defines" && e.targetID === mod?.id)).toBe(true)
  })

  it("emits only file + module namespace when no tree is provided", () => {
    const { nodes } = extract("src/service.py", "x", null)
    expect(nodes).toHaveLength(2)
    expect(nodes[0].type).toBe("file")
    expect(nodes[1].type).toBe("namespace")
  })

  it("extracts a top-level function with position properties", () => {
    const fn = n("function_definition", {
      startByte: 30, row: 4,
      children: [name("compute"), n("parameters", { field: "parameters" }), n("block", { field: "body" })],
    })
    const s = syms(module(fn)).byLabel("compute")
    expect(s?.properties.kind).toBe("function")
    expect(s?.properties.exported).toBe(true)
    expect(s?.properties.start_byte).toBe(30)
    expect(s?.properties.start_line).toBe(4)
  })

  it("extracts an async generator function", () => {
    const fn = funcDef("stream", { async: true, body: [n("expression_statement", { children: [n("yield")] })] })
    const s = syms(module(fn)).byLabel("stream")
    expect(s?.properties.async).toBe(true)
    expect(s?.properties.is_generator).toBe(true)
  })

  it("classifies dunder and private functions", () => {
    const { byLabel } = syms(module(funcDef("__init__"), funcDef("_helper"), funcDef("public")))
    expect(byLabel("__init__")?.properties.dunder).toBe(true)
    expect(byLabel("_helper")?.properties.private).toBe(true)
    expect(byLabel("_helper")?.properties.exported).toBe(false)
    expect(byLabel("public")?.properties.exported).toBe(true)
  })

  it("extracts a decorated class with bases, methods, and an extends edge", () => {
    const method = decorated(funcDef("total"), "@property")
    const cls = classDef("Order", ["Base", "ABC"], [method, funcDef("save")])
    const decoratedCls = decorated(cls, "@dataclass")
    const { byLabel, edges } = syms(module(decoratedCls))

    const c = byLabel("Order")
    expect(c?.properties.kind).toBe("class")
    expect(c?.properties.is_dataclass).toBe(true)
    expect(c?.properties.is_abstract).toBe(true)
    expect(c?.properties.bases).toEqual(["Base", "ABC"])
    // extends edge for a non-object base
    expect(edges.some(e => e.type === "extends" && e.properties.super_name === "Base")).toBe(true)

    // methods captured with kind "method" and not exported
    const total = byLabel("total")
    expect(total?.properties.kind).toBe("method")
    expect(total?.properties.exported).toBe(false)
    expect(total?.properties.is_property).toBe(true)
    expect(byLabel("save")?.properties.kind).toBe("method")
  })

  it("flags exception classes", () => {
    const cls = classDef("ValidationError", ["Exception"])
    expect(syms(module(cls)).byLabel("ValidationError")?.properties.is_exception).toBe(true)
  })

  it("emits namespace + imports edges for import and from-import", () => {
    const imp = n("import_statement", { children: [n("dotted_name", { text: "os.path" })] })
    const aliased = n("import_statement", {
      children: [n("aliased_import", {
        children: [n("dotted_name", { field: "name", text: "numpy" }), n("identifier", { field: "alias", text: "np" })],
      })],
    })
    const fromImp = n("import_from_statement", {
      children: [
        n("dotted_name", { field: "module_name", text: "collections.abc" }),
        n("dotted_name", { text: "Mapping" }),
      ],
    })
    const relFrom = n("import_from_statement", {
      children: [
        n("relative_import", { field: "module_name", text: ".models" }),
        n("dotted_name", { text: "User" }),
      ],
    })
    const { nodes, edges } = syms(module(imp, aliased, fromImp, relFrom))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "os.path")).toBe(true)
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "numpy")).toBe(true)
    // `.models` in src/service.py resolves against the package (`src`) to a
    // module-scoped canonical ID, so it can't collide with `.models` elsewhere.
    const rel = nodes.find(x => x.type === "namespace" && x.canonicalID === "src.models")
    expect(rel?.properties.relative).toBe(true)
    expect(rel?.properties.import_path).toBe(".models")
    expect(edges.filter(e => e.type === "imports" && e.properties.import_style === "direct")).toHaveLength(2)
    expect(edges.filter(e => e.type === "imports" && e.properties.import_style === "from")).toHaveLength(2)
  })

  it("extracts a module-level UPPER_CASE constant", () => {
    const assign = n("assignment", { children: [n("identifier", { field: "left", text: "MAX_SIZE" }), n("integer", { field: "right", text: "10" })] })
    const stmt = n("expression_statement", { children: [assign] })
    const s = syms(module(stmt)).byLabel("MAX_SIZE")
    expect(s?.properties.kind).toBe("constant")
    expect(s?.properties.exported).toBe(true)
  })

  it("ignores non-constant module-level assignments", () => {
    const assign = n("assignment", { children: [n("identifier", { field: "left", text: "counter" }), n("integer", { field: "right", text: "0" })] })
    const stmt = n("expression_statement", { children: [assign] })
    expect(syms(module(stmt)).byLabel("counter")).toBeUndefined()
  })

  it("tolerates leaf nodes with null children (as the host serializes them)", () => {
    // The host omits `children` on leaves (serialized null). Build a dotted_name
    // leaf with children:null inside an import — the real-tree shape that
    // crashed an array-assuming extractor.
    const leaf = { ...n("dotted_name", { text: "sys" }), children: null } as SyntaxNode
    const imp = n("import_statement", { children: [leaf] })
    const { nodes } = syms(module(imp))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "sys")).toBe(true)
  })
})
