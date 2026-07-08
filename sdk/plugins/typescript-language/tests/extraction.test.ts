import { beforeAll, describe, expect, it } from "vitest"
import { createHash } from "crypto"
import type { SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { extract } from "../src/extract.js"

// The extractor walks the tree-sitter CST the host provides. These tests build
// faithful SyntaxNode trees (matching tree-sitter-typescript's node types and
// field names) and assert extraction — no regex, no parser dependency. Full
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
const program = (...children: SyntaxNode[]) => n("program", { children })
const exported = (decl: SyntaxNode) => n("export_statement", { children: [decl] })

function syms(tree: SyntaxNode) {
  const { nodes, edges } = extract("src/mod.ts", "line\nline\nline\n", tree)
  return {
    nodes,
    edges,
    symbols: nodes.filter(x => x.type === "symbol"),
    byLabel: (label: string) => nodes.find(x => x.type === "symbol" && x.label === label),
  }
}

describe("tree-based extraction", () => {
  it("emits a file node even for an empty tree", () => {
    const { nodes } = syms(program())
    expect(nodes.filter(x => x.type === "file")).toHaveLength(1)
  })

  it("emits only the file node when no tree is provided", () => {
    const { nodes } = extract("src/mod.ts", "x", null)
    expect(nodes).toHaveLength(1)
    expect(nodes[0].type).toBe("file")
  })

  it("extracts an exported function with position properties", () => {
    const fn = n("function_declaration", {
      text: "function validate() {}", startByte: 42, row: 3,
      children: [name("validate"), n("statement_block", { field: "body" })],
    })
    const { byLabel } = syms(program(exported(fn)))
    const s = byLabel("validate")
    expect(s?.properties.kind).toBe("function")
    expect(s?.properties.exported).toBe(true)
    expect(s?.properties.start_byte).toBe(42)
    expect(s?.properties.start_line).toBe(3)
  })

  it("extracts an async arrow function assigned to const", () => {
    const arrow = n("arrow_function", {
      field: "value",
      children: [n("async"), n("formal_parameters"), n("statement_block", { field: "body" })],
    })
    const declr = n("variable_declarator", { children: [name("record"), arrow] })
    const lex = n("lexical_declaration", { children: [declr] })
    const s = syms(program(exported(lex))).byLabel("record")
    expect(s?.properties.kind).toBe("function")
    expect(s?.properties.arrow).toBe(true)
    expect(s?.properties.async).toBe(true)
    expect(s?.properties.exported).toBe(true)
  })

  it("extracts a class, its extends edge, and its methods (regex missed methods)", () => {
    const method = n("method_definition", {
      children: [name("charge"), n("formal_parameters"), n("statement_block", { field: "body" })],
    })
    const body = n("class_body", { field: "body", children: [method] })
    const heritage = n("class_heritage", {
      children: [n("extends_clause", { children: [n("identifier", { text: "BaseService" })] })],
    })
    const cls = n("class_declaration", {
      children: [name("PaymentService"), heritage, body],
    })
    const { byLabel, edges } = syms(program(exported(cls)))

    expect(byLabel("PaymentService")?.properties.kind).toBe("class")
    // method captured as a symbol — the key win over regex
    expect(byLabel("PaymentService.charge")?.properties.kind).toBe("method")
    // extends edge present
    expect(edges.some(e => e.type === "extends" && e.properties.super_name === "BaseService")).toBe(true)
  })

  it("extracts an abstract class (abstract_class_declaration node)", () => {
    const cls = n("abstract_class_declaration", {
      children: [n("abstract"), name("Base"), n("class_body", { field: "body" })],
    })
    const s = syms(program(exported(cls))).byLabel("Base")
    expect(s?.properties.kind).toBe("class")
    expect(s?.properties.abstract).toBe(true)
  })

  it("does not emit an extends edge for an implements-only class", () => {
    const heritage = n("class_heritage", {
      children: [n("implements_clause", { children: [n("identifier", { text: "Serializable" })] })],
    })
    const cls = n("class_declaration", {
      children: [name("Widget"), heritage, n("class_body", { field: "body" })],
    })
    const { edges } = syms(program(cls))
    expect(edges.some(e => e.type === "extends")).toBe(false)
  })

  it("extracts interface, type alias, and enum", () => {
    const tree = program(
      exported(n("interface_declaration", { children: [name("Money")] })),
      exported(n("type_alias_declaration", { children: [name("Result")] })),
      exported(n("enum_declaration", { children: [name("Status")] })),
    )
    const { byLabel } = syms(tree)
    expect(byLabel("Money")?.properties.kind).toBe("interface")
    expect(byLabel("Result")?.properties.kind).toBe("type")
    expect(byLabel("Status")?.properties.kind).toBe("enum")
  })

  it("emits a namespace node + imports edge for an import", () => {
    const imp = n("import_statement", {
      children: [n("string", { text: '"./analytics"' })],
    })
    const { nodes, edges } = syms(program(imp))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "./analytics")).toBe(true)
    expect(edges.some(e => e.type === "imports")).toBe(true)
  })

  it("marks a non-exported declaration as not exported", () => {
    const fn = n("function_declaration", { children: [name("helper"), n("statement_block", { field: "body" })] })
    expect(syms(program(fn)).byLabel("helper")?.properties.exported).toBe(false)
  })

  it("tolerates leaf nodes with null children (as the host serializes them)", () => {
    // The host omits `children` on leaves (serialized null). Build an import
    // whose string leaf has children:null — the real-tree shape that crashed
    // an array-assuming extractor.
    const str = { ...n("string", { text: '"./x"' }), children: null } as SyntaxNode
    const imp = n("import_statement", { children: [str] })
    const { nodes } = syms(program(imp))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "./x")).toBe(true)
  })
})
