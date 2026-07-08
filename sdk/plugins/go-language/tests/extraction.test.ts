import { beforeAll, describe, expect, it } from "vitest"
import { createHash } from "crypto"
import type { SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { extract } from "../src/extract.js"

// The extractor walks the tree-sitter CST the host provides. These tests build
// faithful SyntaxNode trees (matching tree-sitter-go's node types and field
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

const pkgClause = (name: string) =>
  n("package_clause", { children: [n("package_identifier", { text: name })] })
const sourceFile = (...children: SyntaxNode[]) => n("source_file", { children })

function syms(tree: SyntaxNode, filePath = "internal/service/svc.go") {
  const { nodes, edges } = extract(filePath, "line\nline\nline\n", tree)
  return {
    nodes,
    edges,
    symbols: nodes.filter(x => x.type === "symbol"),
    byLabel: (label: string) => nodes.find(x => x.type === "symbol" && x.label === label),
  }
}

describe("tree-based Go extraction", () => {
  it("emits a file node even for an empty tree", () => {
    const { nodes } = syms(sourceFile(pkgClause("service")))
    expect(nodes.filter(x => x.type === "file")).toHaveLength(1)
  })

  it("emits only the file node when no tree is provided", () => {
    const { nodes } = extract("internal/service/svc.go", "x", null)
    expect(nodes).toHaveLength(1)
    expect(nodes[0].type).toBe("file")
  })

  it("emits a package namespace + belongs_to edge for a non-main package", () => {
    const { nodes, edges } = syms(sourceFile(pkgClause("service")))
    const ns = nodes.find(x => x.type === "namespace")
    expect(ns?.label).toBe("service")
    expect(ns?.canonicalID).toBe("internal/service")
    expect(edges.some(e => e.type === "belongs_to")).toBe(true)
  })

  it("does not emit a package namespace for package main", () => {
    const { nodes } = syms(sourceFile(pkgClause("main")))
    expect(nodes.some(x => x.type === "namespace")).toBe(false)
  })

  it("extracts a top-level function with position properties", () => {
    const fn = n("function_declaration", {
      text: "func NewEngine() *Engine {", startByte: 40, row: 5,
      children: [
        n("identifier", { field: "name", text: "NewEngine" }),
        n("parameter_list", { field: "parameters" }),
        n("block", { field: "body" }),
      ],
    })
    const s = syms(sourceFile(pkgClause("service"), fn)).byLabel("NewEngine")
    expect(s?.properties.kind).toBe("function")
    expect(s?.properties.exported).toBe(true)
    expect(s?.properties.package).toBe("service")
    expect(s?.properties.start_byte).toBe(40)
    expect(s?.properties.start_line).toBe(5)
  })

  it("marks a lowercase function as not exported", () => {
    const fn = n("function_declaration", {
      children: [n("identifier", { field: "name", text: "helper" }), n("block", { field: "body" })],
    })
    expect(syms(sourceFile(pkgClause("service"), fn)).byLabel("helper")?.properties.exported).toBe(false)
  })

  it("extracts a method with a pointer receiver type", () => {
    const receiver = n("parameter_list", {
      field: "receiver",
      children: [n("parameter_declaration", {
        children: [
          n("identifier", { field: "name", text: "e" }),
          n("pointer_type", { field: "type", children: [n("type_identifier", { text: "Engine" })] }),
        ],
      })],
    })
    const method = n("method_declaration", {
      children: [
        receiver,
        n("field_identifier", { field: "name", text: "Register" }),
        n("parameter_list", { field: "parameters" }),
        n("block", { field: "body" }),
      ],
    })
    const s = syms(sourceFile(pkgClause("service"), method)).byLabel("Engine.Register")
    expect(s?.properties.kind).toBe("method")
    expect(s?.properties.receiver_type).toBe("Engine")
    expect(s?.canonicalID).toBe("internal/service:Engine.Register")
  })

  it("extracts struct, interface, and alias type declarations", () => {
    const structDecl = n("type_declaration", {
      children: [n("type_spec", {
        children: [n("type_identifier", { field: "name", text: "Engine" }), n("struct_type", { field: "type" })],
      })],
    })
    const ifaceDecl = n("type_declaration", {
      children: [n("type_spec", {
        children: [n("type_identifier", { field: "name", text: "Plugin" }), n("interface_type", { field: "type" })],
      })],
    })
    const aliasDecl = n("type_declaration", {
      children: [n("type_spec", {
        children: [n("type_identifier", { field: "name", text: "Middleware" }), n("function_type", { field: "type" })],
      })],
    })
    const { byLabel } = syms(sourceFile(pkgClause("service"), structDecl, ifaceDecl, aliasDecl))
    expect(byLabel("Engine")?.properties.kind).toBe("struct")
    expect(byLabel("Plugin")?.properties.kind).toBe("interface")
    expect(byLabel("Middleware")?.properties.kind).toBe("alias")
  })

  it("extracts grouped type declarations (type (...) blocks)", () => {
    // Grouped declarations keep type_spec / type_alias as direct children of the
    // single type_declaration (tree-sitter-go emits no type_spec_list wrapper).
    const groupDecl = n("type_declaration", {
      children: [
        n("type_spec", {
          children: [n("type_identifier", { field: "name", text: "Handler" }), n("struct_type", { field: "type" })],
        }),
        n("type_alias", {
          children: [n("type_identifier", { field: "name", text: "HandlerFunc" }), n("function_type", { field: "type" })],
        }),
      ],
    })
    const { byLabel } = syms(sourceFile(pkgClause("service"), groupDecl))
    expect(byLabel("Handler")?.properties.kind).toBe("struct")
    expect(byLabel("HandlerFunc")?.properties.kind).toBe("alias")
  })

  it("emits namespace nodes + imports edges for a block import", () => {
    const importDecl = n("import_declaration", {
      children: [n("import_spec_list", {
        children: [
          n("import_spec", { children: [n("interpreted_string_literal", { field: "path", text: '"context"' })] }),
          n("import_spec", {
            children: [
              n("package_identifier", { field: "name", text: "core" }),
              n("interpreted_string_literal", { field: "path", text: '"github.com/atheory-ai/context-engine/internal/core"' }),
            ],
          }),
        ],
      })],
    })
    const { nodes, edges } = syms(sourceFile(pkgClause("service"), importDecl))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "context")).toBe(true)
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "github.com/atheory-ai/context-engine/internal/core")).toBe(true)
    expect(edges.filter(e => e.type === "imports")).toHaveLength(2)
  })

  it("tolerates leaf nodes with null children (as the host serializes them)", () => {
    // The host omits `children` on leaves (serialized null). Build a single
    // import whose string leaf has children:null — the real-tree shape that
    // crashed an array-assuming extractor.
    const str = { ...n("interpreted_string_literal", { field: "path", text: '"fmt"' }), children: null } as SyntaxNode
    const importDecl = n("import_declaration", {
      children: [n("import_spec", { children: [str] })],
    })
    const { nodes } = syms(sourceFile(pkgClause("service"), importDecl))
    expect(nodes.some(x => x.type === "namespace" && x.canonicalID === "fmt")).toBe(true)
  })
})
