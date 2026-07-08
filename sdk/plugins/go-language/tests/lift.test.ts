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

const pkgClause = (name: string) =>
  n("package_clause", { children: [n("package_identifier", { text: name })] })
const sourceFile = (...children: SyntaxNode[]) => n("source_file", { children })
const goParam = (nm: string, ty: string) => n("parameter_declaration", {
  children: [n("identifier", { field: "name", text: nm }), n("type_identifier", { field: "type", text: ty })],
})
const sharedParam = (names: string[], ty: string) => n("parameter_declaration", {
  children: [...names.map(nm => n("identifier", { field: "name", text: nm })), n("type_identifier", { field: "type", text: ty })],
})
const paramList = (...p: SyntaxNode[]) => n("parameter_list", { field: "parameters", children: p })
const resultType = (ty: string) => n("type_identifier", { field: "result", text: ty })
const resultTuple = (text: string) => n("parameter_list", { field: "result", text })
const goFunc = (name: string, params: SyntaxNode, res?: SyntaxNode) => n("function_declaration", {
  children: [n("identifier", { field: "name", text: name }), params, ...(res ? [res] : []), n("block", { field: "body" })],
})

function liftOf(tree: SyntaxNode) {
  return extract("svc.go", "", tree).iir ?? []
}

describe("liftGoFunction (contract fields)", () => {
  it("lifts an exported function with typed params and a single result", () => {
    const fn = goFunc("FindUser", paramList(goParam("id", "string")), resultType("User"))
    const iir = liftOf(sourceFile(pkgClause("main"), fn))
    expect(iir).toHaveLength(1)
    expect(iir[0].intent).toMatchObject({
      kind: "FunctionIntent", name: "FindUser", language: "go", visibility: "public",
      inputs: [{ name: "id", type: "string" }],
      returns: { type: "User", explicit: true },
    })
  })

  it("marks a lowercase-named function private and an absent result not explicit", () => {
    const fn = goFunc("helper", paramList())
    const intent = liftOf(sourceFile(pkgClause("main"), fn))[0].intent
    expect(intent.visibility).toBe("private")
    expect(intent.returns).toEqual({ type: "", explicit: false })
  })

  it("expands a shared-type parameter group", () => {
    const fn = goFunc("add", paramList(sharedParam(["a", "b"], "int")))
    const intent = liftOf(sourceFile(pkgClause("main"), fn))[0].intent
    expect(intent.inputs).toEqual([{ name: "a", type: "int" }, { name: "b", type: "int" }])
  })

  it("renders a multi-value result as its tuple text", () => {
    const fn = goFunc("load", paramList(), resultTuple("(User, error)"))
    const intent = liftOf(sourceFile(pkgClause("main"), fn))[0].intent
    expect(intent.returns).toEqual({ type: "(User, error)", explicit: true })
  })

  it("lifts a method by its method name, attached to the method's node", () => {
    const method = n("method_declaration", {
      children: [
        n("identifier", { field: "name", text: "Charge" }),
        n("parameter_list", { field: "receiver", children: [
          n("parameter_declaration", { children: [n("identifier", { field: "name", text: "s" }), n("pointer_type", { field: "type", text: "*Service", children: [n("type_identifier", { text: "Service" })] })] }),
        ] }),
        paramList(goParam("amt", "int")),
        resultType("error"),
        n("block", { field: "body" }),
      ],
    })
    const result = extract("svc.go", "", sourceFile(pkgClause("main"), method))
    expect(result.iir).toHaveLength(1)
    expect(result.iir![0].intent).toMatchObject({
      name: "Charge", visibility: "public", inputs: [{ name: "amt", type: "int" }], returns: { type: "error", explicit: true },
    })
    const sym = result.nodes.find(nd => nd.label === "Service.Charge")
    expect(result.iir![0].nodeId).toBe(sym?.id)
  })
})

// ── behavior / effects / failures ────────────────────────────────────────────
const withField = (node: SyntaxNode, field: string): SyntaxNode => ({ ...node, fieldName: field })
const gid = (t: string) => n("identifier", { text: t })
const gnil = () => n("nil", { text: "nil" })
const selector = (objText: string, field: string) => n("selector_expression", {
  text: `${objText}.${field}`,
  children: [withField(gid(objText), "operand"), n("field_identifier", { field: "field", text: field })],
})
const gbin = (left: SyntaxNode, op: string, right: SyntaxNode) => n("binary_expression", {
  text: `${left.text} ${op} ${right.text}`,
  children: [withField(left, "left"), n(op, { field: "operator", text: op }), withField(right, "right")],
})
const gif = (cond: SyntaxNode, thenStmt: SyntaxNode) => n("if_statement", {
  children: [withField(cond, "condition"), withField(n("block", { children: [thenStmt] }), "consequence")],
})
const greturn = (text: string) => n("return_statement", { text })
const strLit = (s: string) => n("interpreted_string_literal", { text: `"${s}"` })
const selCall = (objText: string, method: string) => n("call_expression", {
  text: `${objText}.${method}`,
  children: [withField(selector(objText, method), "function"), n("argument_list")],
})
const gpanic = (msg: string) => n("call_expression", {
  text: `panic("${msg}")`,
  children: [withField(gid("panic"), "function"), n("argument_list", { children: [strLit(msg)] })],
})
const importOf = (path: string) => n("import_declaration", {
  children: [n("import_spec", { children: [n("interpreted_string_literal", { field: "path", text: `"${path}"` })] })],
})
const goBody = (name: string, ...stmts: SyntaxNode[]) => n("function_declaration", {
  children: [n("identifier", { field: "name", text: name }), paramList(), n("block", { field: "body", children: stmts })],
})

describe("liftGoFunction (behavior, effects, failures)", () => {
  it("lifts an if to a when/then clause with a member-path whenExpr", () => {
    const fn = goBody("f", gif(gbin(selector("amount", "Cents"), "<", gid("limit")), greturn("return errBelow")))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.behavior).toEqual([{
      when: "amount.Cents < limit",
      then: "return errBelow",
      whenExpr: { op: "<", args: [{ op: "path", text: "amount.Cents" }, { op: "path", text: "limit" }] },
    }])
  })

  it("treats nil as a literal in whenExpr", () => {
    const fn = goBody("f", gif(gbin(gid("x"), "==", gnil()), greturn("return")))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.behavior[0].whenExpr).toEqual({ op: "==", args: [{ op: "path", text: "x" }, { op: "lit", text: "nil" }] })
  })

  it("detects a call on an imported package as a side effect", () => {
    const fn = goBody("Record", n("expression_statement", { children: [selCall("analytics", "Track")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("example.com/analytics"), fn))[0].intent
    expect(intent.sideEffects).toEqual(["analytics.Track"])
  })

  it("captures a panic string literal as a failure mode", () => {
    const fn = goBody("f", n("expression_statement", { children: [gpanic("nil_amount")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.failureModes).toEqual(["nil_amount"])
  })

  it("resolves the package qualifier for a versioned module path", () => {
    const fn = goBody("Save", n("expression_statement", { children: [selCall("store", "Save")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("github.com/acme/store/v2"), fn))[0].intent
    expect(intent.sideEffects).toEqual(["store.Save"])
  })

  it("does not count an if inside a func literal (closure)", () => {
    const inner = gif(gbin(gid("y"), ">", gid("z")), greturn("return"))
    const closure = n("func_literal", { children: [paramList(), n("block", { field: "body", children: [inner] })] })
    const outer = gif(gbin(gid("a"), "==", gid("b")), greturn("return"))
    const fn = goBody("f", outer, n("expression_statement", { children: [closure] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.behavior).toHaveLength(1)
    expect(intent.behavior[0].when).toBe("a == b")
  })
})
