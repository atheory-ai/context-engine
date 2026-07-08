import { describe, expect, it } from "vitest"
import type { SyntaxNode } from "../src/types.js"
import {
  walk,
  walkTopLevel,
  childByField,
  childrenByType,
  firstByType,
  hasChildType,
  fieldText,
  firstDescendantByType,
} from "../src/tree.js"

function node(type: string, opts: { field?: string; text?: string; children?: SyntaxNode[] } = {}): SyntaxNode {
  return {
    type,
    isNamed: true,
    fieldName: opts.field ?? null,
    text: opts.text ?? "",
    startByte: 0,
    endByte: 0,
    startPosition: { row: 0, column: 0 },
    endPosition: { row: 0, column: 0 },
    children: opts.children ?? [],
  }
}

// function foo(x) { const y = () => bar(); }
const tree = node("function_declaration", {
  children: [
    node("identifier", { field: "name", text: "foo" }),
    node("formal_parameters", { field: "parameters", children: [node("identifier", { text: "x" })] }),
    node("statement_block", {
      field: "body",
      children: [
        node("lexical_declaration", {
          children: [
            node("variable_declarator", {
              children: [
                node("identifier", { field: "name", text: "y" }),
                node("arrow_function", { field: "value" }),
              ],
            }),
          ],
        }),
      ],
    }),
  ],
})

describe("tree helpers", () => {
  it("childByField / fieldText find a node's field", () => {
    expect(childByField(tree, "name")?.text).toBe("foo")
    expect(fieldText(tree, "name")).toBe("foo")
    expect(childByField(tree, "missing")).toBeNull()
    expect(fieldText(tree, "missing")).toBe("")
  })

  it("childrenByType / firstByType / hasChildType inspect direct children", () => {
    expect(childrenByType(tree, "identifier")).toHaveLength(1)
    expect(firstByType(tree, "statement_block")?.type).toBe("statement_block")
    expect(hasChildType(tree, "formal_parameters")).toBe(true)
    expect(hasChildType(tree, "arrow_function")).toBe(false) // nested, not direct
  })

  it("walk visits every descendant; firstDescendantByType finds nested nodes", () => {
    const types: string[] = []
    walk(tree, n => types.push(n.type))
    expect(types).toContain("arrow_function")
    expect(firstDescendantByType(tree, "arrow_function")?.type).toBe("arrow_function")
    expect(firstDescendantByType(tree, "nope")).toBeNull()
  })

  it("walkTopLevel does not descend into nested function scopes", () => {
    // Nest an inner arrow whose body contains a marker; walkTopLevel must skip it.
    const inner = node("arrow_function", { children: [node("marker")] })
    const outer = node("statement_block", { children: [inner] })
    const seen: string[] = []
    walkTopLevel(outer, n => seen.push(n.type))
    expect(seen).toContain("arrow_function")
    expect(seen).not.toContain("marker") // inside the nested scope
  })

  it("walk tolerates null", () => {
    let count = 0
    walk(null, () => count++)
    expect(count).toBe(0)
  })

  it("walk handles a very deep tree without stack overflow", () => {
    // Build a 50k-deep chain; a recursive walk would RangeError here.
    let root = node("leaf")
    for (let i = 0; i < 50000; i++) root = node("wrap", { children: [root] })
    let count = 0
    expect(() => walk(root, () => count++)).not.toThrow()
    expect(count).toBe(50001)
  })
})
