// Lift a Go function/method into an IIR FunctionIntent, mirroring Context
// Engine's TS extractor but bound to Go's grammar (selector_expression member
// access, nil/true/false as predeclared identifiers). Failures follow Go's real
// idiom — a returned error (errors.New / fmt.Errorf message, or an Err* sentinel)
// — plus panic. Go has no host-side extractor — the plugin is the sole IIR producer.
import type {
  FunctionIntent, IIRParam, IIRReturn, IIRExpr, IIRBehaviorClause, IIRSideEffect, IIRFailureMode, SyntaxNode,
} from "@atheory-ai/ce-plugin-sdk"
import { IIRTypeUnknown, childByField, childrenByType, fieldText, walk, classifyEffect } from "@atheory-ai/ce-plugin-sdk"

export function isExported(name: string): boolean {
  const first = name.charAt(0)
  return first !== "" && first === first.toUpperCase() && first !== first.toLowerCase()
}

// collectImports maps each imported package qualifier (the alias, or the last
// path segment) to its full import path. The qualifier decides whether a call is
// on an imported package; the full path lets the effect classifier categorize it
// (e.g. "net/http" → network) far more reliably than the local alias alone.
export function collectImports(tree: SyntaxNode): Map<string, string> {
  const imports = new Map<string, string>()
  walk(tree, (n) => {
    if (n.type !== "import_spec") return
    const path = (childByField(n, "path")?.text ?? "").replace(/^["'`]|["'`]$/g, "")
    const alias = childByField(n, "name")?.text
    if (alias) {
      if (alias !== "_" && alias !== ".") imports.set(alias, path)
      return
    }
    const segs = path.split("/")
    let pkg = segs.pop() ?? ""
    // A trailing /vN is a semantic-import-version dir, not the package name
    // (github.com/foo/bar/v2 → bar); a .vN suffix likewise (gopkg.in/yaml.v2 → yaml).
    if (/^v\d+$/.test(pkg)) pkg = segs.pop() ?? pkg
    pkg = pkg.replace(/\.v\d+$/, "")
    if (pkg) imports.set(pkg, path)
  })
  return imports
}

export function liftGoFunction(name: string, fnNode: SyntaxNode, imports: Map<string, string>): FunctionIntent {
  const body = childByField(fnNode, "body")
  return {
    kind:         "FunctionIntent",
    name,
    language:     "go",
    origin:       "observed",
    visibility:   isExported(name) ? "public" : "private",
    inputs:       liftParams(childByField(fnNode, "parameters")),
    returns:      liftResult(childByField(fnNode, "result")),
    behavior:     extractBehavior(body),
    sideEffects:  extractSideEffects(body, imports),
    failureModes: extractFailureModes(body, childByField(fnNode, "result")),
    constraints:  [],
  }
}

// ── contract fields ─────────────────────────────────────────────────────────

function liftParams(params: SyntaxNode | null): IIRParam[] {
  if (!params) return []
  const out: IIRParam[] = []
  for (const decl of childrenByType(params, "parameter_declaration")) {
    const type = childByField(decl, "type")?.text ?? IIRTypeUnknown
    const names = (decl.children ?? []).filter(c => c.fieldName === "name")
    // An unnamed parameter (`func f([]byte)`) has no name field; Go's blank
    // identifier "_" names it — a real, valid identifier, not an empty string
    // (which the IIR model rejects).
    if (names.length === 0) out.push({ name: "_", type: normWs(type) })
    else for (const n of names) out.push({ name: n.text, type: normWs(type) })
  }
  for (const decl of childrenByType(params, "variadic_parameter_declaration")) {
    const type = childByField(decl, "type")?.text ?? IIRTypeUnknown
    out.push({ name: childByField(decl, "name")?.text ?? "_", type: "..." + normWs(type) })
  }
  return out
}

function liftResult(result: SyntaxNode | null): IIRReturn {
  if (!result) return { type: "", explicit: false }
  return { type: normWs(result.text), explicit: true }
}

// ── behavior (if -> when/then + normalized whenExpr) ────────────────────────

function extractBehavior(body: SyntaxNode | null): IIRBehaviorClause[] {
  const out: IIRBehaviorClause[] = []
  if (!body) return out
  walkWithinFunc(body, (n) => {
    if (n.type === "if_statement") { pushIf(n, out); return }
    if (n.type === "expression_switch_statement") { pushExprSwitch(n, out); return }
    if (n.type === "type_switch_statement") { pushTypeSwitch(n, out); return }
  })
  // A clause needs a meaningful consequence: the IIR model (and the comparator)
  // require both when and then, so drop empty-then guards (e.g. `if ok {} else …`).
  return out.filter(c => c.then !== "")
}

function pushIf(n: SyntaxNode, out: IIRBehaviorClause[]): void {
  const cond = childByField(n, "condition")
  const clause: IIRBehaviorClause = {
    when: cond ? normWs(cond.text) : "",
    then: summarizeConsequence(childByField(n, "consequence")),
  }
  const whenExpr = normalizeCondition(cond)
  if (whenExpr) clause.whenExpr = whenExpr
  out.push(clause)
  // A terminal `else { … }` (not an `else if`, which the walk visits on its own)
  // adds an otherwise-clause. tree-sitter-go models it as an `alternative` field.
  const alt = childByField(n, "alternative")
  if (alt && alt.type === "block") {
    out.push({ when: "else", then: summarizeConsequence(alt) })
  }
}

// pushExprSwitch turns `switch subj { case v: … }` into one clause per case:
// when = "subj == v" (or the case's own boolean when subj is absent), then =
// the case body summary. A default becomes an "else" clause.
function pushExprSwitch(sw: SyntaxNode, out: IIRBehaviorClause[]): void {
  const subject = childByField(sw, "value")
  const subjExpr = subject ? normalizeCondition(subject) : undefined
  const subjText = subject ? normWs(subject.text) : ""
  for (const c of sw.children ?? []) {
    if (c.type === "expression_case") {
      const values = caseValueNodes(childByField(c, "value"))
      const parts = values.map(v => caseCondition(subject, subjExpr, subjText, v))
      const clause: IIRBehaviorClause = {
        when: parts.map(p => p.text).join(" || ") || normWs(c.text),
        then: summarizeCaseBody(c),
      }
      const exprs = parts.map(p => p.expr).filter((e): e is IIRExpr => !!e)
      if (exprs.length === values.length && exprs.length > 0) {
        clause.whenExpr = exprs.length === 1 ? exprs[0] : orFold(exprs)
      }
      out.push(clause)
    } else if (c.type === "default_case") {
      out.push({ when: "else", then: summarizeCaseBody(c) })
    }
  }
}

// pushTypeSwitch turns `switch v := x.(type) { case T: … }` into clauses. Type
// matching isn't in the whenExpr grammar, so these carry when-text only.
function pushTypeSwitch(sw: SyntaxNode, out: IIRBehaviorClause[]): void {
  const subject = childByField(sw, "value")
  const subjText = subject ? normWs(subject.text) : "value"
  for (const c of sw.children ?? []) {
    if (c.type === "type_case") {
      const t = childByField(c, "type")
      out.push({ when: `${subjText} is ${t ? normWs(t.text) : "?"}`, then: summarizeCaseBody(c) })
    } else if (c.type === "default_case") {
      out.push({ when: "else", then: summarizeCaseBody(c) })
    }
  }
}

function caseValueNodes(value: SyntaxNode | null): SyntaxNode[] {
  if (!value) return []
  // The case `value` field is an expression_list; a bare value may be the node itself.
  const list = value.type === "expression_list" ? value.children : [value]
  return (list ?? []).filter(v => v.isNamed && v.type !== ",")
}

// caseCondition builds the when-text and whenExpr for one case value. With a
// subject it's an equality (subject == value); without one the case value is
// itself a boolean condition.
function caseCondition(
  subject: SyntaxNode | null, subjExpr: IIRExpr | undefined, subjText: string, value: SyntaxNode,
): { text: string; expr?: IIRExpr } {
  if (!subject) {
    return { text: normWs(value.text), expr: normalizeCondition(value) }
  }
  const valExpr = normalizeCondition(value)
  const expr = subjExpr && valExpr ? { op: "==", args: [subjExpr, valExpr] } : undefined
  return { text: `${subjText} == ${normWs(value.text)}`, expr }
}

function orFold(exprs: IIRExpr[]): IIRExpr {
  return exprs.reduce((acc, e) => ({ op: "||", args: [acc, e] }))
}

// summarizeCaseBody summarizes a case's statements (which follow the value/type
// as direct children), preferring a return statement.
function summarizeCaseBody(c: SyntaxNode): string {
  let first: SyntaxNode | undefined
  for (const s of c.children ?? []) {
    if (!s.isNamed) continue
    if (s.fieldName === "value" || s.fieldName === "type" || s.type === "expression_list") continue
    if (!first) first = s
    if (s.type === "return_statement") return normWs(s.text)
  }
  return first ? normWs(first.text) : ""
}

// walkWithinFunc stops at nested func_literals so a closure's `if` is not counted
// as the outer function's behavior.
function walkWithinFunc(node: SyntaxNode | null, visit: (n: SyntaxNode) => void): void {
  if (!node) return
  visit(node)
  for (const c of node.children ?? []) {
    if (c.type === "func_literal") continue
    walkWithinFunc(c, visit)
  }
}

function summarizeConsequence(block: SyntaxNode | null): string {
  if (!block) return ""
  if (block.type !== "block") return normWs(block.text)
  let first: SyntaxNode | undefined
  for (const c of block.children ?? []) {
    if (!c.isNamed) continue
    if (!first) first = c
    if (c.type === "return_statement") return normWs(c.text)
  }
  return first ? normWs(first.text) : ""
}

const comparisonOps = new Set(["<", "<=", ">", ">=", "==", "!="])
const logicalOps = new Set(["&&", "||"])
const literalTypes = new Set([
  "int_literal", "float_literal", "imaginary_literal", "rune_literal",
  "interpreted_string_literal", "raw_string_literal",
])
// Go's nil/true/false/iota are predeclared identifiers; treat them as literals so
// the shared IL matches TS (where null/true/false are literals).
const constIdentifiers = new Set(["nil", "true", "false", "iota"])

function normalizeCondition(node: SyntaxNode | null): IIRExpr | undefined {
  if (!node) return undefined
  if (literalTypes.has(node.type)) return { op: "lit", text: normWs(node.text) }
  // tree-sitter-go models nil/true/false as their own node types (not
  // identifiers), so match on both the type and — below — the identifier text.
  if (node.type === "nil" || node.type === "true" || node.type === "false") {
    return { op: "lit", text: node.type }
  }
  switch (node.type) {
    case "parenthesized_expression": {
      const inner = (node.children ?? []).find(c => c.isNamed)
      return inner ? normalizeCondition(inner) : undefined
    }
    case "binary_expression": {
      const op = fieldText(node, "operator")
      if (!comparisonOps.has(op) && !logicalOps.has(op)) return undefined
      const left = normalizeCondition(childByField(node, "left"))
      const right = normalizeCondition(childByField(node, "right"))
      return left && right ? { op, args: [left, right] } : undefined
    }
    case "unary_expression": {
      const op = fieldText(node, "operator")
      if (op === "-") {
        const operand = childByField(node, "operand")
        return operand && literalTypes.has(operand.type) ? { op: "lit", text: "-" + normWs(operand.text) } : undefined
      }
      if (op !== "!") return undefined
      const arg = normalizeCondition(childByField(node, "operand"))
      return arg ? { op: "!", args: [arg] } : undefined
    }
    case "identifier":
      return constIdentifiers.has(node.text) ? { op: "lit", text: node.text } : { op: "path", text: node.text }
    case "selector_expression": {
      const path = memberPath(node)
      return path ? { op: "path", text: path } : undefined
    }
    default:
      return undefined
  }
}

function memberPath(node: SyntaxNode | null): string {
  if (!node) return ""
  if (node.type === "identifier") return node.text
  if (node.type === "selector_expression") {
    const operand = memberPath(childByField(node, "operand"))
    const field = childByField(node, "field")?.text
    return operand && field ? operand + "." + field : ""
  }
  return ""
}

// ── side effects + failure modes ────────────────────────────────────────────

const sideEffectVerbs = ["track", "send", "emit", "publish", "save", "create", "update", "delete", "write"]

// Read-only stdlib packages: a call on one constructs/derives a value without an
// observable effect, so it is not a side effect even though it's on an import.
const purePackages = new Set(["strings", "strconv", "math", "unicode", "utf8", "errors", "sort", "bytes", "regexp"])
// Individually-pure functions from packages that are otherwise effectful (fmt
// writes to stdout, but fmt.Errorf/Sprintf only format a value).
const pureCalls = new Set(["fmt.Errorf", "fmt.Sprintf", "fmt.Sprint", "fmt.Sprintln"])

function isPureCall(root: string, full: string): boolean {
  return purePackages.has(root) || pureCalls.has(full)
}

function extractSideEffects(body: SyntaxNode | null, imports: Map<string, string>): IIRSideEffect[] {
  const byName = new Map<string, IIRSideEffect>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call_expression") return
    const callee = childByField(n, "function")
    if (!callee) return
    const { method, root, full } = calleeParts(callee)
    if (isPureCall(root, full)) return
    if (!imports.has(root) && !matchesSideEffectVerb(method)) return
    if (byName.has(full)) return
    // Classify at the source, using the full import path when the root is an
    // imported package — a far stronger signal than the local qualifier.
    const { kind, confidence } = classifyEffect({ method, root, importPath: imports.get(root) })
    byName.set(full, { name: full, kind, confidence })
  })
  return [...byName.values()].sort((a, b) => effectName(a).localeCompare(effectName(b)))
}

function effectName(e: IIRSideEffect): string {
  return typeof e === "string" ? e : e.name
}

function calleeParts(callee: SyntaxNode): { method: string; root: string; full: string } {
  const full = callee.text
  if (callee.type === "identifier") return { method: callee.text, root: "", full }
  if (callee.type === "selector_expression") {
    return { method: childByField(callee, "field")?.text ?? "", root: leftmostIdentifier(childByField(callee, "operand")), full }
  }
  return { method: "", root: "", full }
}

function leftmostIdentifier(node: SyntaxNode | null): string {
  let cur = node
  while (cur) {
    if (cur.type === "identifier") return cur.text
    if (cur.type === "selector_expression") { cur = childByField(cur, "operand"); continue }
    return ""
  }
  return ""
}

function matchesSideEffectVerb(method: string): boolean {
  const lower = method.toLowerCase()
  return sideEffectVerbs.some(v => lower.includes(v))
}

// Go signals failure primarily by returning an error, secondarily by panic.
// Capture both: panic("msg") anywhere (constructed), and — for functions that
// return an error — the identity of each return's error operand (Go convention:
// the error is the last operand): errors.New/fmt.Errorf message (constructed), an
// Err* sentinel (sentinel), or a forwarded error variable (propagated, e.g.
// `return nil, err`). A `return …, nil` is a success path and contributes none.
function extractFailureModes(body: SyntaxNode | null, result: SyntaxNode | null): IIRFailureMode[] {
  const byCode = new Map<string, IIRFailureMode>()
  if (!body) return []

  walk(body, (n) => {
    if (n.type !== "call_expression") return
    if (childByField(n, "function")?.text !== "panic") return
    const lit = firstStringLiteral(n)
    if (lit) byCode.set(lit, { code: lit, kind: "constructed" })
  })

  if (returnsError(result)) {
    walkWithinFunc(body, (n) => {
      if (n.type !== "return_statement") return
      const fm = returnedFailure(n)
      if (fm) byCode.set(fm.code, fm)
    })
  }
  return [...byCode.keys()].sort().map(k => byCode.get(k)!)
}

// returnsError reports whether the function's result list includes an `error`.
function returnsError(result: SyntaxNode | null): boolean {
  if (!result) return false
  if (result.type === "type_identifier") return result.text === "error"
  for (const decl of childrenByType(result, "parameter_declaration")) {
    if (childByField(decl, "type")?.text === "error") return true
  }
  return false
}

// returnExpressions returns the value expressions of a return statement, whether
// it returns a single value or a comma-separated list.
function returnExpressions(ret: SyntaxNode): SyntaxNode[] {
  const list = (ret.children ?? []).find(c => c.type === "expression_list")
  const src = list ? list.children : ret.children
  return (src ?? []).filter(c => c.isNamed && c.type !== "return")
}

// Err* sentinel convention (ErrNotFound, ErrClosed, …).
const sentinelRE = /^Err[A-Z0-9_]/

// returnedFailure names the failure carried by a return's error operand — the
// last operand, per Go's `(T, error)` convention. A `nil` there is success.
function returnedFailure(ret: SyntaxNode): IIRFailureMode | null {
  const exprs = returnExpressions(ret)
  if (exprs.length === 0) return null
  const err = exprs[exprs.length - 1]

  if (err.type === "call_expression") {
    const callee = childByField(err, "function")?.text ?? ""
    if (callee === "errors.New" || callee === "fmt.Errorf") {
      const lit = firstStringLiteral(err)
      if (lit) return { code: lit, kind: "constructed" }
    }
    return null
  }
  if (err.type === "identifier") {
    if (sentinelRE.test(err.text)) return { code: err.text, kind: "sentinel" }
    if (err.text === "nil") return null
    return { code: err.text, kind: "propagated", source: err.text }
  }
  if (err.type === "selector_expression") {
    const field = childByField(err, "field")?.text ?? ""
    if (sentinelRE.test(field)) return { code: field, kind: "sentinel" }
  }
  return null
}

function firstStringLiteral(node: SyntaxNode): string {
  let found = ""
  walk(node, (n) => {
    if (found) return
    if (n.type === "interpreted_string_literal" || n.type === "raw_string_literal") {
      found = n.text.replace(/^["'`]|["'`]$/g, "")
    }
  })
  return found
}

function normWs(s: string): string {
  return s.split(/\s+/).filter(Boolean).join(" ")
}
