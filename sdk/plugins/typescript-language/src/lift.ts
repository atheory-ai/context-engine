// Lift a TypeScript function node into an IIR FunctionIntent, mirroring Context
// Engine's internal/iir extractor so plugin-produced IIR can reach parity with
// (and eventually replace) the host's Go lift. Deterministic AST walk, no model.
import type {
  FunctionIntent, IIRParam, IIRReturn, IIRExpr, IIRBehaviorClause, IIRConsequence, IIRSideEffect, IIRFailureMode, SyntaxNode,
} from "@atheory-ai/ce-plugin-sdk"
import { IIRTypeUnknown, childByField, childrenByType, fieldText, walk, walkTopLevel, classifyEffect } from "@atheory-ai/ce-plugin-sdk"

// collectImports gathers the identifiers a module imports (local aliases, named
// imports, namespace and default imports) — the set used to decide whether a
// call targets an imported client/service.
export function collectImports(tree: SyntaxNode): Map<string, string> {
  const imports = new Map<string, string>()
  for (const node of tree.children ?? []) {
    if (node.type !== "import_statement") continue
    // The module specifier (e.g. "axios", "./logger") lets the effect classifier
    // categorize a call on the binding; the binding name gates detection.
    const source = (childByField(node, "source")?.text ?? "").replace(/^['"`]|['"`]$/g, "")
    walk(node, (n) => {
      switch (n.type) {
        case "import_specifier": {
          // Prefer the local alias when present, else the imported name.
          const binding = childByField(n, "alias")?.text ?? childByField(n, "name")?.text
          if (binding) imports.set(binding, source)
          break
        }
        case "namespace_import": {
          const id = lastIdentifier(n)
          if (id) imports.set(id, source)
          break
        }
        case "import_clause":
          // default import: `import Foo from "..."` — a direct identifier child
          // of import_clause (named/namespace imports are nested deeper).
          for (const c of n.children ?? []) {
            if (c.type === "identifier") imports.set(c.text, source)
          }
          break
      }
    })
  }
  return imports
}

// liftFunction builds a FunctionIntent from a function-bearing node (a
// function_declaration or an arrow_function/function_expression value).
export function liftFunction(name: string, fnNode: SyntaxNode, exported: boolean, imports: Map<string, string>): FunctionIntent {
  const body = childByField(fnNode, "body")
  return {
    kind:         "FunctionIntent",
    name,
    language:     "typescript",
    origin:       "observed",
    visibility:   exported ? "public" : "private",
    inputs:       liftParams(fnNode),
    returns:      liftReturn(fnNode),
    behavior:     extractBehavior(body),
    sideEffects:  extractSideEffects(body, imports),
    failureModes: extractFailureModes(body),
    constraints:  [],
  }
}

// ── contract fields ─────────────────────────────────────────────────────────

function liftParams(fnNode: SyntaxNode): IIRParam[] {
  const params = childByField(fnNode, "parameters") ?? childByField(fnNode, "parameter")
  if (!params) {
    const bare = (fnNode.children ?? []).find(c => c.type === "identifier")
    return bare ? [{ name: bare.text, type: IIRTypeUnknown }] : []
  }
  if (params.type === "identifier") {
    return [{ name: params.text, type: IIRTypeUnknown }]
  }
  const out: IIRParam[] = []
  for (const kind of ["required_parameter", "optional_parameter", "rest_parameter"]) {
    for (const p of childrenByType(params, kind)) {
      out.push({ name: paramName(p, out.length), type: paramType(p) })
    }
  }
  return out
}

function paramName(p: SyntaxNode, index: number): string {
  const pattern = childByField(p, "pattern")
  if (pattern?.type === "rest_pattern") {
    const restName = (pattern.children ?? []).find(c => c.type === "identifier")?.text.trim()
    if (restName) return restName
  }
  if (pattern?.text.trim()) return pattern.text.trim()

  // Tree-sitter's parameter fields vary for TS-only forms (notably `this`
  // parameters and some default/destructured forms). Preserve an identifier
  // when one exists; otherwise use a stable synthetic name. An observed IIR
  // record must never contain an invalid empty input name.
  let identifier = ""
  walk(p, (node) => {
    if (!identifier && node.type === "identifier" && node.text.trim()) {
      identifier = node.text.trim()
    }
  })
  return identifier || `arg${index + 1}`
}

function paramType(p: SyntaxNode): string {
  const ann = childByField(p, "type") ?? (p.children ?? []).find(c => c.type === "type_annotation")
  return ann ? typeAnnotationText(ann) : IIRTypeUnknown
}

function liftReturn(fnNode: SyntaxNode): IIRReturn {
  const rt = childByField(fnNode, "return_type")
  if (!rt) return { type: "", explicit: false }
  return { type: typeAnnotationText(rt), explicit: true }
}

function typeAnnotationText(ann: SyntaxNode): string {
  const inner = ann.type === "type_annotation" ? (ann.children ?? []).find(c => c.isNamed) : ann
  return normWs(inner?.text ?? ann.text.replace(/^\s*:\s*/, ""))
}

// ── behavior (if -> when/then + normalized whenExpr) ────────────────────────

function extractBehavior(body: SyntaxNode | null): IIRBehaviorClause[] {
  const out: IIRBehaviorClause[] = []
  if (!body) return out
  // walkTopLevel stops at nested function scopes, so a callback's `if` is not
  // counted as the outer function's behavior.
  walkTopLevel(body, (n) => {
    if (n.type === "if_statement") { pushIf(n, out); return }
    if (n.type === "switch_statement") { pushSwitch(n, out); return }
  })
  // A clause needs a meaningful consequence: the IIR model (and the comparator)
  // require both when and then, so drop empty-then guards (e.g. `if (ok) {} else …`).
  return out.filter(c => c.then !== "")
}

function pushIf(n: SyntaxNode, out: IIRBehaviorClause[]): void {
  const cond = childByField(n, "condition")
  const clause: IIRBehaviorClause = { when: conditionText(cond), then: "" }
  setThen(clause, salientConsequence(childByField(n, "consequence")))
  const whenExpr = normalizeCondition(cond)
  if (whenExpr) clause.whenExpr = whenExpr
  out.push(clause)
  // A terminal `else { … }` (an else_clause wrapping a block, not another `if`,
  // which walkTopLevel visits on its own) adds an otherwise-clause.
  const alt = childByField(n, "alternative")
  const altBody = alt?.type === "else_clause"
    ? (alt.children ?? []).find(c => c.isNamed)
    : alt
  if (altBody && altBody.type === "statement_block") {
    const elseClause: IIRBehaviorClause = { when: "else", then: "" }
    setThen(elseClause, salientConsequence(altBody))
    out.push(elseClause)
  }
}

// pushSwitch turns `switch (subj) { case v: … }` into one clause per case:
// when = "subj === v", then = the case body summary. default becomes "else".
function pushSwitch(sw: SyntaxNode, out: IIRBehaviorClause[]): void {
  const subject = childByField(sw, "value")
  const subjExpr = normalizeCondition(subject)
  const subjText = conditionText(subject)
  const switchBody = childByField(sw, "body")
  for (const c of switchBody?.children ?? []) {
    if (c.type === "switch_case") {
      const value = childByField(c, "value")
      const valExpr = normalizeCondition(value)
      const clause: IIRBehaviorClause = { when: `${subjText} === ${value ? normWs(value.text) : ""}`, then: "" }
      setThen(clause, salientCaseStmt(c))
      if (subjExpr && valExpr) clause.whenExpr = { op: "===", args: [subjExpr, valExpr] }
      out.push(clause)
    } else if (c.type === "switch_default") {
      const clause: IIRBehaviorClause = { when: "else", then: "" }
      setThen(clause, salientCaseStmt(c))
      out.push(clause)
    }
  }
}

// salientCaseStmt / salientConsequence find the statement that stands for a
// branch's consequence: the first meaningful statement, preferring a return or
// throw. For a case, the `value` field is excluded.
function salientCaseStmt(c: SyntaxNode): SyntaxNode | undefined {
  let first: SyntaxNode | undefined
  for (const s of c.children ?? []) {
    if (!s.isNamed || s.fieldName === "value") continue
    if (!first) first = s
    if (s.type === "return_statement" || s.type === "throw_statement") return s
  }
  return first
}

// setThen fills a clause's raw `then` text and, when the consequence fits the
// action grammar, its normalized thenExpr — both from the same salient statement.
function setThen(clause: IIRBehaviorClause, salient: SyntaxNode | undefined): void {
  clause.then = salient ? trimStatement(normWs(salient.text)) : ""
  const action = thenAction(salient)
  if (action) clause.thenExpr = action
}

// thenAction classifies a salient statement into a normalized consequence:
// return (with the returned expression), throw (with the failure's identity), or
// invoke (a call, with the callee).
function thenAction(node: SyntaxNode | undefined): IIRConsequence | undefined {
  if (!node) return undefined
  if (node.type === "return_statement") {
    const val = (node.children ?? []).find(c => c.isNamed)
    const value = val ? trimStatement(normWs(val.text)) : ""
    return value ? { op: "return", value } : { op: "return" }
  }
  if (node.type === "throw_statement") {
    const fm = throwFailureMode(node)
    const value = fm ? (typeof fm === "string" ? fm : fm.code) : undefined
    return value ? { op: "throw", value } : { op: "throw" }
  }
  if (node.type === "expression_statement") {
    const call = (node.children ?? []).find(c => c.type === "call_expression")
    if (call) {
      const callee = normWs(childByField(call, "function")?.text ?? "")
      return callee ? { op: "invoke", value: callee } : { op: "invoke" }
    }
  }
  return undefined
}

function conditionText(cond: SyntaxNode | null): string {
  if (!cond) return ""
  if (cond.type === "parenthesized_expression") {
    const inner = (cond.children ?? []).find(c => c.isNamed)
    if (inner) return normWs(inner.text)
  }
  return normWs(cond.text)
}

function salientConsequence(cons: SyntaxNode | null): SyntaxNode | undefined {
  if (!cons) return undefined
  if (cons.type !== "statement_block") return cons
  let first: SyntaxNode | undefined
  for (const c of cons.children ?? []) {
    if (!c.isNamed) continue
    if (!first) first = c
    if (c.type === "return_statement" || c.type === "throw_statement") return c
  }
  return first
}

const comparisonOps = new Set(["<", "<=", ">", ">=", "==", "!=", "===", "!=="])
const logicalBinaryOps = new Set(["&&", "||"])

// normalizeCondition mirrors iir.normalizeCondition: a bounded grammar
// (comparisons, logical connectives, `!`, negative-number literals, static
// member/identifier paths, literals). Anything else yields undefined.
function normalizeCondition(node: SyntaxNode | null): IIRExpr | undefined {
  if (!node) return undefined
  switch (node.type) {
    case "parenthesized_expression": {
      const inner = (node.children ?? []).find(c => c.isNamed)
      return inner ? normalizeCondition(inner) : undefined
    }
    case "binary_expression": {
      const op = fieldText(node, "operator")
      if (!comparisonOps.has(op) && !logicalBinaryOps.has(op)) return undefined
      const left = normalizeCondition(childByField(node, "left"))
      const right = normalizeCondition(childByField(node, "right"))
      return left && right ? { op, args: [left, right] } : undefined
    }
    case "unary_expression": {
      const op = fieldText(node, "operator")
      if (op === "-") {
        const arg = childByField(node, "argument")
        return arg && arg.type === "number" ? { op: "lit", text: "-" + normWs(arg.text) } : undefined
      }
      if (op !== "!") return undefined
      const arg = normalizeCondition(childByField(node, "argument"))
      return arg ? { op: "!", args: [arg] } : undefined
    }
    case "identifier":
    case "member_expression": {
      const path = memberPath(node)
      return path ? { op: "path", text: path } : undefined
    }
    case "number":
    case "string":
      return { op: "lit", text: normWs(node.text) }
    case "true":
    case "false":
    case "null":
      return { op: "lit", text: node.type }
    default:
      return undefined
  }
}

function memberPath(node: SyntaxNode | null): string {
  if (!node) return ""
  switch (node.type) {
    case "identifier":
    case "property_identifier":
    case "this":
      return node.text
    case "member_expression": {
      const obj = memberPath(childByField(node, "object"))
      const prop = childByField(node, "property")
      if (!obj || !prop || prop.type !== "property_identifier") return ""
      return obj + "." + prop.text
    }
    default:
      return ""
  }
}

// ── side effects + failure modes ────────────────────────────────────────────

const sideEffectVerbs = ["track", "send", "emit", "publish", "save", "create", "update", "delete", "write"]

// Pure built-in namespaces: calls on them derive values without an observable
// effect (JSON.stringify, Math.max, Object.keys), so they are not side effects.
const purePackages = new Set(["JSON", "Math", "Object", "Array", "Number", "String"])

function extractSideEffects(body: SyntaxNode | null, imports: Map<string, string>): IIRSideEffect[] {
  const byName = new Map<string, IIRSideEffect>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call_expression") return
    const callee = childByField(n, "function")
    if (!callee) return
    const { method, rootObj, full } = calleeParts(callee)
    if (purePackages.has(rootObj)) return
    if (!imports.has(rootObj) && !matchesSideEffectVerb(method)) return
    if (byName.has(full)) return
    const { kind, basis } = classifyEffect({ method, root: rootObj, importPath: imports.get(rootObj) })
    byName.set(full, { name: full, kind, basis })
  })
  return [...byName.values()].sort((a, b) => effectName(a).localeCompare(effectName(b)))
}

function effectName(e: IIRSideEffect): string {
  return typeof e === "string" ? e : e.name
}

function calleeParts(callee: SyntaxNode): { method: string; rootObj: string; full: string } {
  const full = callee.text
  if (callee.type === "identifier") return { method: callee.text, rootObj: "", full }
  if (callee.type === "member_expression") {
    const method = childByField(callee, "property")?.text ?? ""
    return { method, rootObj: leftmostIdentifier(childByField(callee, "object")), full }
  }
  return { method: "", rootObj: "", full }
}

function leftmostIdentifier(node: SyntaxNode | null): string {
  let cur = node
  while (cur) {
    if (cur.type === "identifier") return cur.text
    if (cur.type === "member_expression") { cur = childByField(cur, "object"); continue }
    return ""
  }
  return ""
}

function matchesSideEffectVerb(method: string): boolean {
  const lower = method.toLowerCase()
  return sideEffectVerbs.some(v => lower.includes(v))
}

function extractFailureModes(body: SyntaxNode | null): IIRFailureMode[] {
  const byCode = new Map<string, IIRFailureMode>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "throw_statement") return
    const fm = throwFailureMode(n)
    if (fm) byCode.set(fm.code, fm)
  })
  return [...byCode.keys()].sort().map(k => byCode.get(k)!)
}

// throwFailureMode classifies a thrown failure: a string-literal message
// (throw new Error("msg") / throw "msg") is constructed; a custom error class
// with no message (throw new NotFoundError()) is a sentinel; a bare `throw err`
// re-throw forwards an upstream failure (propagated, source = the identifier).
function throwFailureMode(node: SyntaxNode): IIRFailureMode | null {
  const lit = firstStringLiteral(node)
  if (lit) return { code: lit, kind: "constructed" }
  const arg = (node.children ?? []).find(c => c.isNamed)
  if (!arg) return null
  if (arg.type === "new_expression") {
    const cls = childByField(arg, "constructor")?.text ?? ""
    return cls ? { code: cls, kind: "sentinel" } : null
  }
  if (arg.type === "identifier") return { code: arg.text, kind: "propagated", source: arg.text }
  return null
}

function firstStringLiteral(node: SyntaxNode): string {
  let found = ""
  walk(node, (n) => {
    if (found) return
    if (n.type === "string") found = n.text.replace(/^['"`]|['"`]$/g, "")
  })
  return found
}

function lastIdentifier(node: SyntaxNode): string {
  let id = ""
  walk(node, (n) => { if (n.type === "identifier") id = n.text })
  return id
}

// ── shared ──────────────────────────────────────────────────────────────────

function normWs(s: string): string {
  return s.split(/\s+/).filter(Boolean).join(" ")
}

function trimStatement(s: string): string {
  return s.replace(/;$/, "")
}
