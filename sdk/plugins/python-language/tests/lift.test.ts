import { beforeAll, describe, expect, it } from "vitest"
import { createHash } from "crypto"
import type { SyntaxNode } from "@atheory-ai/ce-plugin-sdk"
import { extract } from "../src/extract.js"

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
const module = (...children: SyntaxNode[]) => n("module", { children })
const plainParam = (nm: string) => n("identifier", { text: nm })
const typedParam = (nm: string, ty: string) =>
  n("typed_parameter", { children: [n("identifier", { text: nm }), n("type", { field: "type", text: ty })] })
const params = (...p: SyntaxNode[]) => n("parameters", { field: "parameters", children: p })
const returns = (ty: string) => n("type", { field: "return_type", text: ty })

function funcDef(fnName: string, opts: { params?: SyntaxNode; returns?: SyntaxNode } = {}): SyntaxNode {
  const children: SyntaxNode[] = [name(fnName), opts.params ?? params()]
  if (opts.returns) children.push(opts.returns)
  children.push(n("block", { field: "body" }))
  return n("function_definition", { children })
}

function classDef(clsName: string, ...body: SyntaxNode[]): SyntaxNode {
  return n("class_definition", { children: [name(clsName), n("block", { field: "body", children: body })] })
}

function liftOf(tree: SyntaxNode) {
  return extract("service.py", "", tree).iir ?? []
}

describe("liftPyFunction (contract fields)", () => {
  it("lifts a public function with typed params and a return type", () => {
    const fn = funcDef("find_user", { params: params(typedParam("id", "str"), typedParam("limit", "int")), returns: returns("User") })
    const iir = liftOf(module(fn))
    expect(iir).toHaveLength(1)
    expect(iir[0].intent).toMatchObject({
      kind: "FunctionIntent", name: "find_user", language: "python", visibility: "public",
      inputs: [{ name: "id", type: "str" }, { name: "limit", type: "int" }],
      returns: { type: "User", explicit: true },
    })
  })

  it("marks a leading-underscore function private, absent return not explicit", () => {
    const intent = liftOf(module(funcDef("_helper")))[0].intent
    expect(intent.visibility).toBe("private")
    expect(intent.returns).toEqual({ type: "", explicit: false })
  })

  it("represents an untyped param as unknown", () => {
    const intent = liftOf(module(funcDef("f", { params: params(plainParam("x")) })))[0].intent
    expect(intent.inputs).toEqual([{ name: "x", type: "unknown" }])
  })

  it("names a typed splat parameter (*args: int) via its nested pattern", () => {
    // tree-sitter wraps a typed splat in a typed_parameter around the splat.
    const typedSplat = n("typed_parameter", {
      children: [n("list_splat_pattern", { children: [n("identifier", { text: "args" })] }), n("type", { field: "type", text: "int" })],
    })
    const intent = liftOf(module(funcDef("f", { params: params(typedSplat) })))[0].intent
    expect(intent.inputs).toEqual([{ name: "*args", type: "int" }])
  })

  it("drops the self receiver on a method and lifts by method name", () => {
    const method = funcDef("charge", { params: params(plainParam("self"), typedParam("amount", "int")), returns: returns("bool") })
    const result = extract("service.py", "", module(classDef("Wallet", method)))
    const m = (result.iir ?? []).find(e => e.intent.name === "charge")
    expect(m?.intent).toMatchObject({
      name: "charge", inputs: [{ name: "amount", type: "int" }], returns: { type: "bool", explicit: true },
    })
    const sym = result.nodes.find(nd => nd.label === "charge")
    expect(m?.nodeId).toBe(sym?.id)
  })
})

// ── behavior / effects / failures ────────────────────────────────────────────
const withField = (node: SyntaxNode, field: string): SyntaxNode => ({ ...node, fieldName: field })
const pid = (t: string) => n("identifier", { text: t })
const pnone = () => n("none", { text: "None" })
const unnamed = (type: string, text: string): SyntaxNode => ({ ...n(type, { text }), isNamed: false })
const attr = (objText: string, a: string) => n("attribute", {
  text: `${objText}.${a}`,
  children: [withField(pid(objText), "object"), n("identifier", { field: "attribute", text: a })],
})
const cmp = (left: SyntaxNode, op: string, right: SyntaxNode) => n("comparison_operator", {
  text: `${left.text} ${op} ${right.text}`,
  children: [left, unnamed(op, op), right],
})
const boolOp = (left: SyntaxNode, op: string, right: SyntaxNode) => n("boolean_operator", {
  text: `${left.text} ${op} ${right.text}`,
  children: [withField(left, "left"), n(op, { field: "operator", text: op }), withField(right, "right")],
})
const pif = (cond: SyntaxNode, thenStmt: SyntaxNode) => n("if_statement", {
  children: [withField(cond, "condition"), withField(n("block", { children: [thenStmt] }), "consequence")],
})
const pret = (text: string) => n("return_statement", { text })
const pstr = (s: string) => n("string", { children: [n("string_content", { text: s })] })
const praise = (msg: string) => n("raise_statement", {
  children: [n("call", { children: [withField(pid("ValueError"), "function"), n("argument_list", { children: [pstr(msg)] })] })],
})
const pcall = (objText: string, method: string) => n("call", {
  text: `${objText}.${method}`, children: [withField(attr(objText, method), "function"), n("argument_list")],
})
const importMod = (name: string) => n("import_statement", { children: [n("dotted_name", { children: [pid(name)] })] })
const pyBody = (fnName: string, ...stmts: SyntaxNode[]) => n("function_definition", {
  children: [name(fnName), params(), n("block", { field: "body", children: stmts })],
})

describe("liftPyFunction (behavior, effects, failures)", () => {
  it("lifts an if to a when/then clause with an attribute-path whenExpr", () => {
    const fn = pyBody("f", pif(cmp(attr("amount", "cents"), "<", pid("limit")), pret("return err")))
    const intent = liftOf(module(fn))[0].intent
    expect(intent.behavior).toEqual([{
      when: "amount.cents < limit",
      then: "return err",
      whenExpr: { op: "<", args: [{ op: "path", text: "amount.cents" }, { op: "path", text: "limit" }] },
    }])
  })

  it("binds `is None` to == with a none literal (cross-language None/nil/null)", () => {
    const fn = pyBody("f", pif(cmp(pid("x"), "is", pnone()), pret("return")))
    expect(liftOf(module(fn))[0].intent.behavior[0].whenExpr).toEqual({
      op: "==", args: [{ op: "path", text: "x" }, { op: "lit", text: "none" }],
    })
  })

  it("captures elif and else branches of an if (previously missed)", () => {
    const pint = (v: number) => n("integer", { text: String(v) })
    const consequence = (stmt: SyntaxNode) => withField(n("block", { children: [stmt] }), "consequence")
    const ifNode = n("if_statement", {
      children: [
        withField(cmp(pid("n"), "<", pint(0)), "condition"),
        consequence(pret("return 'neg'")),
        n("elif_clause", { children: [withField(cmp(pid("n"), "==", pint(0)), "condition"), consequence(pret("return 'zero'"))] }),
        n("else_clause", { children: [withField(n("block", { children: [pret("return 'pos'")] }), "body")] }),
      ],
    })
    const intent = liftOf(module(pyBody("f", ifNode)))[0].intent
    expect(intent.behavior.map(b => b.when)).toEqual(["n < 0", "n == 0", "else"])
  })

  it("lifts a match into one == clause per case, `case _` as else", () => {
    const pint = (v: number) => n("integer", { text: String(v) })
    const caseClause = (pattern: SyntaxNode, body: SyntaxNode) => n("case_clause", {
      children: [n("case_pattern", { text: pattern.text, children: [pattern] }), withField(n("block", { children: [body] }), "consequence")],
    })
    const match = n("match_statement", {
      children: [
        withField(pid("score"), "subject"),
        withField(n("block", { children: [
          caseClause(pint(100), pret("return 'perfect'")),
          n("case_clause", { children: [n("case_pattern", { text: "_" }), withField(n("block", { children: [pret("return 'other'")] }), "consequence")] }),
        ] }), "body"),
      ],
    })
    const intent = liftOf(module(pyBody("f", match)))[0].intent
    expect(intent.behavior).toEqual([
      { when: "score == 100", then: "return 'perfect'", whenExpr: { op: "==", args: [{ op: "path", text: "score" }, { op: "lit", text: "100" }] } },
      { when: "else", then: "return 'other'" },
    ])
  })

  it("binds `is not None` to != with a none literal", () => {
    const fn = pyBody("f", pif(n("comparison_operator", {
      text: "x is not None",
      children: [pid("x"), unnamed("is", "is"), unnamed("not", "not"), pnone()],
    }), pret("return")))
    expect(liftOf(module(fn))[0].intent.behavior[0].whenExpr).toEqual({
      op: "!=", args: [{ op: "path", text: "x" }, { op: "lit", text: "none" }],
    })
  })

  it("maps `and` to the shared && operator", () => {
    const fn = pyBody("f", pif(boolOp(pid("a"), "and", pid("b")), pret("return")))
    expect(liftOf(module(fn))[0].intent.behavior[0].whenExpr?.op).toBe("&&")
  })

  it("detects a call on an imported module and classifies it (verb → mutation)", () => {
    const fn = pyBody("record", n("expression_statement", { children: [pcall("analytics", "track")] }))
    const intent = liftOf(module(importMod("analytics"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "analytics.track", kind: "mutation", basis: "heuristic" }])
  })

  it("classifies a call on an imported stdlib module (os → io)", () => {
    const fn = pyBody("cleanup", n("expression_statement", { children: [pcall("os", "remove")] }))
    const intent = liftOf(module(importMod("os"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "os.remove", kind: "io", basis: "resolved" }])
  })

  it("captures a raised string literal as a failure mode", () => {
    const fn = pyBody("f", praise("nil_amount"))
    expect(liftOf(module(fn))[0].intent.failureModes).toEqual(["nil_amount"])
  })

  it("names a raised exception type without a message, and skips a bare re-raise", () => {
    const raiseCall = n("raise_statement", { children: [n("call", { children: [withField(pid("NotFoundError"), "function"), n("argument_list")] })] })
    const raiseName = n("raise_statement", { children: [pid("Closed")] })
    const bareRaise = n("raise_statement", {})
    const fn = pyBody("f", raiseCall, raiseName, bareRaise)
    expect(liftOf(module(fn))[0].intent.failureModes).toEqual(["Closed", "NotFoundError"])
  })

  it("does not count an if inside a nested def", () => {
    const nested = pyBody("inner", pif(cmp(pid("y"), ">", pid("z")), pret("return")))
    const outer = pif(cmp(pid("a"), "==", pid("b")), pret("return"))
    const fn = pyBody("f", outer, nested)
    const intent = liftOf(module(fn))[0].intent
    expect(intent.behavior).toHaveLength(1)
    expect(intent.behavior[0].when).toBe("a == b")
  })
})
