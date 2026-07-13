import { beforeAll, describe, expect, it } from "vitest"
import { createHash } from "crypto"
import type { SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { extract } from "../src/extract.js"

// These build faithful tree-sitter-typescript SyntaxNode trees and assert the
// lifted IIR (ExtractionResult.iir). Host consumption of plugin-lifted IIR
// arrives in a later slice; here we prove the plugin produces node-attached IIR.

beforeAll(() => {
  const g = globalThis as unknown as Record<string, unknown>
  g.__ce_node_id = (p: string, t: string, c: string) => digest(`${p}:${t}:${c}`)
  g.__ce_edge_id = (s: string, t: string, tgt: string) => digest(`${s}:${t}:${tgt}`)
})

function digest(s: string): string {
  return createHash("sha256").update(s).digest("hex").slice(0, 32)
}

function n(type: string, opts: { field?: string; text?: string; children?: SyntaxNode[] } = {}): SyntaxNode {
  return {
    type, isNamed: true, fieldName: opts.field ?? null, text: opts.text ?? "",
    startByte: 0, endByte: 0,
    startPosition: { row: 0, column: 0 }, endPosition: { row: 0, column: 0 },
    children: opts.children ?? [],
  }
}

const name = (t: string) => n("identifier", { field: "name", text: t })
const program = (...children: SyntaxNode[]) => n("program", { children })
const exported = (decl: SyntaxNode) => n("export_statement", { children: [decl] })
const param = (nm: string, ty?: string) => n("required_parameter", {
  children: ty
    ? [n("identifier", { field: "pattern", text: nm }), n("type_annotation", { field: "type", children: [n("type_identifier", { text: ty })] })]
    : [n("identifier", { field: "pattern", text: nm })],
})
const params = (...p: SyntaxNode[]) => n("formal_parameters", { field: "parameters", children: p })
const returnType = (ty: string) => n("type_annotation", { field: "return_type", children: [n("type_identifier", { text: ty })] })
const body = () => n("statement_block", { field: "body" })
const bodyOf = (...stmts: SyntaxNode[]) => n("statement_block", { field: "body", children: stmts })

function liftOf(tree: SyntaxNode) {
  const result = extract("f.ts", "", tree)
  return result.iir ?? []
}

// ── expression/statement builders for behavior/effects/failures ──────────────
const withField = (node: SyntaxNode, field: string): SyntaxNode => ({ ...node, fieldName: field })
const ident = (t: string) => n("identifier", { text: t })
const nullLit = () => n("null", { text: "null" })
const member = (obj: string, prop: string) => n("member_expression", {
  text: `${obj}.${prop}`,
  children: [n("identifier", { field: "object", text: obj }), n("property_identifier", { field: "property", text: prop })],
})
const bin = (left: SyntaxNode, op: string, right: SyntaxNode) => n("binary_expression", {
  text: `${left.text} ${op} ${right.text}`,
  children: [withField(left, "left"), n(op, { field: "operator", text: op }), withField(right, "right")],
})
const ifStmt = (cond: SyntaxNode, thenStmt: SyntaxNode) => n("if_statement", {
  children: [
    withField(n("parenthesized_expression", { children: [cond] }), "condition"),
    withField(n("statement_block", { children: [thenStmt] }), "consequence"),
  ],
})
const returnStmt = (text: string) => n("return_statement", { text })
const callExpr = (callee: SyntaxNode) => n("call_expression", {
  text: callee.text, children: [withField(callee, "function"), n("arguments")],
})
const throwStr = (msg: string) => n("throw_statement", { children: [n("string", { text: `"${msg}"` })] })
const fnWith = (bodyNode: SyntaxNode) => n("function_declaration", { children: [name("f"), params(), bodyNode] })

describe("liftFunction (contract fields)", () => {
  it("lifts name, public visibility, typed inputs, and explicit return", () => {
    const fn = n("function_declaration", {
      children: [name("findUser"), params(param("id", "string"), param("opts", "Opts")), returnType("User"), body()],
    })
    const iir = liftOf(program(exported(fn)))
    expect(iir).toHaveLength(1)
    expect(iir[0].nodeId).toBeTruthy()
    expect(iir[0].intent).toMatchObject({
      kind: "FunctionIntent",
      name: "findUser",
      language: "typescript",
      visibility: "public",
      inputs: [{ name: "id", type: "string" }, { name: "opts", type: "Opts" }],
      returns: { type: "User", explicit: true },
      behavior: [],
      sideEffects: [],
    })
  })

  it("marks a non-exported function private", () => {
    const fn = n("function_declaration", { children: [name("helper"), params(), body()] })
    expect(liftOf(program(fn))[0].intent.visibility).toBe("private")
  })

  it("represents a missing param type as unknown and a missing return as not explicit", () => {
    const fn = n("function_declaration", { children: [name("f"), params(param("x")), body()] })
    const intent = liftOf(program(fn))[0].intent
    expect(intent.inputs).toEqual([{ name: "x", type: "unknown" }])
    expect(intent.returns).toEqual({ type: "", explicit: false })
  })

  it("lifts an arrow function bound in a lexical declaration", () => {
    const arrow = n("arrow_function", {
      field: "value",
      children: [params(param("amount", "Money")), returnType("void"), body()],
    })
    const declr = n("variable_declarator", { children: [name("record"), arrow] })
    const lex = n("lexical_declaration", { children: [declr] })
    const iir = liftOf(program(exported(lex)))
    expect(iir).toHaveLength(1)
    expect(iir[0].intent).toMatchObject({
      name: "record",
      visibility: "public",
      inputs: [{ name: "amount", type: "Money" }],
      returns: { type: "void", explicit: true },
    })
  })

  it("attaches each intent to the same node id the symbol uses", () => {
    const fn = n("function_declaration", { children: [name("g"), params(), body()] })
    const result = extract("f.ts", "", program(fn))
    const sym = result.nodes.find(node => node.label === "g")
    expect(result.iir?.[0].nodeId).toBe(sym?.id)
  })
})

describe("liftFunction (behavior, effects, failures)", () => {
  it("lifts an if branch to a when/then clause with a normalized whenExpr", () => {
    const fn = fnWith(bodyOf(ifStmt(bin(ident("id"), "==", nullLit()), returnStmt("return undefined;"))))
    const intent = liftOf(program(fn))[0].intent
    expect(intent.behavior).toEqual([{
      when: "id == null",
      then: "return undefined",
      whenExpr: { op: "==", args: [{ op: "path", text: "id" }, { op: "lit", text: "null" }] },
    }])
  })

  it("normalizes a member-path comparison in whenExpr", () => {
    const cond = bin(member("amount", "cents"), "<", n("number", { text: "0" }))
    const intent = liftOf(program(fnWith(bodyOf(ifStmt(cond, returnStmt("return")))))).at(0)!.intent
    expect(intent.behavior[0].whenExpr).toEqual({
      op: "<", args: [{ op: "path", text: "amount.cents" }, { op: "lit", text: "0" }],
    })
  })

  it("lifts a switch into one strict-equality clause per case, default as else", () => {
    const num = (v: number) => n("number", { text: String(v) })
    const switchCase = (value: SyntaxNode, body: SyntaxNode) => n("switch_case", {
      children: [withField(value, "value"), body],
    })
    const sw = n("switch_statement", {
      children: [
        withField(n("parenthesized_expression", { children: [ident("x")] }), "value"),
        withField(n("switch_body", { children: [
          switchCase(num(1), returnStmt("return 'one';")),
          n("switch_default", { children: [returnStmt("return 'other';")] }),
        ] }), "body"),
      ],
    })
    const intent = liftOf(program(fnWith(bodyOf(sw))))[0].intent
    expect(intent.behavior).toEqual([
      { when: "x === 1", then: "return 'one'", whenExpr: { op: "===", args: [{ op: "path", text: "x" }, { op: "lit", text: "1" }] } },
      { when: "else", then: "return 'other'" },
    ])
  })

  it("detects a side effect from a verb method call and classifies it", () => {
    const intent = liftOf(program(fnWith(bodyOf(callExpr(member("analytics", "track")))))).at(0)!.intent
    expect(intent.sideEffects).toEqual([{ name: "analytics.track", kind: "mutation", confidence: "high" }])
  })

  it("detects a call on an imported client and classifies by its root", () => {
    // import { db } from "./db"; db.query()
    const imp = n("import_statement", {
      children: [n("import_clause", { children: [n("named_imports", {
        children: [n("import_specifier", { children: [n("identifier", { field: "name", text: "db" })] })],
      })] })],
    })
    const fn = fnWith(bodyOf(callExpr(member("db", "query"))))
    const intent = liftOf(program(imp, fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "db.query", kind: "db", confidence: "high" }])
  })

  it("classifies a call on an imported network client (axios) as network", () => {
    // import axios from "axios"; axios.get(...)
    const imp = n("import_statement", {
      children: [
        n("import_clause", { children: [n("identifier", { text: "axios" })] }),
        n("string", { field: "source", text: `"axios"` }),
      ],
    })
    const fn = fnWith(bodyOf(callExpr(member("axios", "get"))))
    const intent = liftOf(program(imp, fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "axios.get", kind: "network", confidence: "high" }])
  })

  it("captures a thrown string literal as a failure mode", () => {
    const intent = liftOf(program(fnWith(bodyOf(throwStr("amount_below_minimum"))))).at(0)!.intent
    expect(intent.failureModes).toEqual([{ code: "amount_below_minimum", kind: "constructed" }])
  })

  it("classifies a custom error class as a sentinel and a re-throw as propagated", () => {
    const throwNew = (cls: string) => n("throw_statement", {
      children: [n("new_expression", { children: [withField(ident(cls), "constructor"), n("arguments")] })],
    })
    const rethrow = n("throw_statement", { children: [ident("err")] })
    const intent = liftOf(program(fnWith(bodyOf(throwNew("NotFoundError"), rethrow)))).at(0)!.intent
    expect(intent.failureModes).toEqual([
      { code: "NotFoundError", kind: "sentinel" },
      { code: "err", kind: "propagated", source: "err" },
    ])
  })

  it("does not descend into nested closures for behavior", () => {
    // outer if + a callback with its own if — only the outer branch counts.
    const inner = ifStmt(bin(ident("x"), ">", n("number", { text: "0" })), returnStmt("return true"))
    const arrow = n("arrow_function", { children: [params(), n("statement_block", { field: "body", children: [inner] })] })
    const outer = ifStmt(bin(ident("xs"), "===", n("number", { text: "0" })), returnStmt("return"))
    const intent = liftOf(program(fnWith(bodyOf(outer, n("expression_statement", { children: [arrow] })))))[0].intent
    expect(intent.behavior).toHaveLength(1)
    expect(intent.behavior[0].when).toBe("xs === 0")
  })
})
