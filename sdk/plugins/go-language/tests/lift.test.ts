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
// resultErr builds a `(T, error)` result whose second entry is an `error` type.
const resultErr = () => n("parameter_list", {
  field: "result",
  children: [
    n("parameter_declaration", { children: [n("type_identifier", { field: "type", text: "T" })] }),
    n("parameter_declaration", { children: [n("type_identifier", { field: "type", text: "error" })] }),
  ],
})
const goBodyErr = (name: string, ...stmts: SyntaxNode[]) => n("function_declaration", {
  children: [n("identifier", { field: "name", text: name }), paramList(), resultErr(), n("block", { field: "body", children: stmts })],
})
// errCall builds `pkg.Method("msg")` (e.g. errors.New / fmt.Errorf).
const errCall = (pkg: string, method: string, msg: string) => n("call_expression", {
  text: `${pkg}.${method}("${msg}")`,
  children: [withField(selector(pkg, method), "function"), n("argument_list", { children: [strLit(msg)] })],
})
// retErr builds `return nil, <expr>`.
const retErr = (errExpr: SyntaxNode) => n("return_statement", {
  children: [n("expression_list", { children: [gnil(), errExpr] })],
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

  it("detects a call on an imported package and classifies it (verb → mutation)", () => {
    const fn = goBody("Record", n("expression_statement", { children: [selCall("analytics", "Track")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("example.com/analytics"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "analytics.Track", kind: "mutation", confidence: "high" }])
  })

  it("classifies by full import path — net/http → network (high confidence)", () => {
    const fn = goBody("Fetch", n("expression_statement", { children: [selCall("http", "Get")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("net/http"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "http.Get", kind: "network", confidence: "high" }])
  })

  it("does not misread a receiver root that merely contains a category word", () => {
    // "catalog" contains "log" — a substring classifier would call this a log
    // effect; structural matching on the root/method does not. The verb "Save"
    // still marks it a mutation.
    const fn = goBody("Store", n("expression_statement", { children: [selCall("catalog", "Save")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("example.com/catalog"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "catalog.Save", kind: "mutation", confidence: "high" }])
  })

  it("marks an uncategorizable imported call low-confidence unclassified", () => {
    const fn = goBody("Ping", n("expression_statement", { children: [selCall("widget", "Poke")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("example.com/widget"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "widget.Poke", kind: "unclassified", confidence: "low" }])
  })

  it("excludes pure stdlib calls (fmt.Errorf, strings.*) from side effects", () => {
    // fmt is imported and effectful for Println, but fmt.Errorf/Sprintf and the
    // read-only strings package only derive values — not observable effects.
    const errorf = n("call_expression", { children: [withField(selector("fmt", "Errorf"), "function"), n("argument_list", { children: [strLit("boom")] })] })
    const trimSpace = n("call_expression", { children: [withField(selector("strings", "TrimSpace"), "function"), n("argument_list")] })
    const fn = goBody("f",
      n("expression_statement", { children: [errorf] }),
      n("expression_statement", { children: [trimSpace] }),
    )
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("fmt"), importOf("strings"), fn))[0].intent
    expect(intent.sideEffects).toEqual([])
  })

  it("captures a panic string literal as a failure mode", () => {
    const fn = goBody("f", n("expression_statement", { children: [gpanic("nil_amount")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.failureModes).toEqual(["nil_amount"])
  })

  it("lifts a switch into one equality clause per case, default as else", () => {
    const gnum = (v: number) => n("int_literal", { text: String(v) })
    const exprCase = (value: SyntaxNode, body: SyntaxNode) => n("expression_case", {
      children: [n("expression_list", { field: "value", children: [value] }), body],
    })
    const sw = n("expression_switch_statement", {
      children: [
        withField(gid("x"), "value"),
        exprCase(gnum(1), greturn('return "one"')),
        n("default_case", { children: [greturn('return "other"')] }),
      ],
    })
    const intent = liftOf(sourceFile(pkgClause("svc"), goBody("f", sw)))[0].intent
    expect(intent.behavior).toEqual([
      { when: "x == 1", then: 'return "one"', whenExpr: { op: "==", args: [{ op: "path", text: "x" }, { op: "lit", text: "1" }] } },
      { when: "else", then: 'return "other"' },
    ])
  })

  it("captures a terminal else on an if and drops an empty-then guard", () => {
    const emptyGuard = n("if_statement", {
      children: [
        withField(gbin(gid("x"), "<", n("int_literal", { text: "0" })), "condition"),
        withField(n("block", { children: [] }), "consequence"), // empty then -> dropped
        withField(n("block", { children: [greturn('return "neg"')] }), "alternative"),
      ],
    })
    const intent = liftOf(sourceFile(pkgClause("svc"), goBody("f", emptyGuard)))[0].intent
    expect(intent.behavior.map(b => b.when)).toEqual(["else"])
  })

  it("captures returned errors.New / fmt.Errorf messages and Err* sentinels", () => {
    // func f() (T, error) with: return nil, errors.New("empty id");
    // return nil, ErrClosed; return nil, err (propagated, excluded).
    const fn = goBodyErr("Load",
      retErr(errCall("errors", "New", "empty id")),
      retErr(gid("ErrClosed")),
      retErr(gid("err")), // propagated variable — no stable name, excluded
    )
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.failureModes).toEqual(["ErrClosed", "empty id"])
  })

  it("does not treat returned values as failures when the function returns no error", () => {
    // No `error` in the result → error-return scanning is skipped entirely.
    const fn = goBody("pure", retErr(errCall("errors", "New", "unreachable")))
    const intent = liftOf(sourceFile(pkgClause("svc"), fn))[0].intent
    expect(intent.failureModes).toEqual([])
  })

  it("resolves the package qualifier for a versioned module path", () => {
    const fn = goBody("Save", n("expression_statement", { children: [selCall("store", "Save")] }))
    const intent = liftOf(sourceFile(pkgClause("svc"), importOf("github.com/acme/store/v2"), fn))[0].intent
    expect(intent.sideEffects).toEqual([{ name: "store.Save", kind: "mutation", confidence: "high" }])
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
