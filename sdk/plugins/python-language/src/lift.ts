// Lift a Python function/method into an IIR FunctionIntent, mirroring the TS/Go
// extractors but bound to Python's grammar (comparison_operator, attribute
// member access, boolean_operator, True/False/None literals, raise as the
// failure idiom). Python has no host-side extractor — the plugin is the sole
// IIR producer and owns the binding into the shared IIR model.
import type {
  FunctionIntent, IIRParam, IIRReturn, IIRExpr, IIRBehaviorClause, IIRConsequence, IIRSideEffect, IIRFailureMode, SyntaxNode,
} from "@atheory-ai/ce-plugin-sdk"
import { IIRTypeUnknown, childByField, fieldText, walk, classifyEffect } from "@atheory-ai/ce-plugin-sdk"

// collectImports gathers module qualifiers usable as a call root: the first
// component of an `import a.b`, an `import x as y` alias, and names from
// `from m import n`.
export function collectImports(tree: SyntaxNode): Map<string, string> {
  const imports = new Map<string, string>()
  walk(tree, (n) => {
    if (n.type === "import_statement") {
      // `import os` → os→os; `import numpy as np` → np→numpy. The binding gates
      // detection; the module name feeds the effect classifier.
      for (const c of n.children ?? []) {
        if (c.type === "dotted_name") {
          const first = (c.children ?? []).find(x => x.type === "identifier")?.text
          if (first) imports.set(first, c.text)
        } else if (c.type === "aliased_import") {
          const alias = childByField(c, "alias")?.text
          if (alias) imports.set(alias, childByField(c, "name")?.text ?? "")
        }
      }
    } else if (n.type === "import_from_statement") {
      // `from db import session` → session→db.
      const moduleName = childByField(n, "module_name")
      const moduleText = moduleName?.text ?? ""
      for (const c of n.children ?? []) {
        if (c === moduleName) continue
        const binding = fromImportBinding(c)
        if (binding) imports.set(binding, moduleText)
      }
    }
  })
  return imports
}

function fromImportBinding(c: SyntaxNode): string {
  if (c.type === "dotted_name") return (c.children ?? []).find(x => x.type === "identifier")?.text ?? ""
  if (c.type === "aliased_import") return childByField(c, "alias")?.text ?? ""
  if (c.type === "identifier") return c.text
  return ""
}

export function liftPyFunction(
  name: string, fnNode: SyntaxNode, isPrivate: boolean, isMethod: boolean, imports: Map<string, string>,
): FunctionIntent {
  const body = childByField(fnNode, "body")
  return {
    kind:         "FunctionIntent",
    name,
    language:     "python",
    origin:       "observed",
    visibility:   isPrivate ? "private" : "public",
    inputs:       liftParams(childByField(fnNode, "parameters"), isMethod),
    returns:      liftReturn(childByField(fnNode, "return_type")),
    behavior:     extractBehavior(body),
    sideEffects:  extractSideEffects(body, imports),
    failureModes: extractFailureModes(body),
    constraints:  [],
  }
}

// ── contract fields ─────────────────────────────────────────────────────────

const paramTypes = new Set([
  "identifier", "typed_parameter", "default_parameter", "typed_default_parameter",
  "list_splat_pattern", "dictionary_splat_pattern",
])

function liftParams(params: SyntaxNode | null, isMethod: boolean): IIRParam[] {
  if (!params) return []
  const nodes = (params.children ?? []).filter(c => paramTypes.has(c.type))
  let start = 0
  if (isMethod && nodes.length > 0) {
    const first = paramName(nodes[0])
    if (first === "self" || first === "cls") start = 1
  }
  const out: IIRParam[] = []
  for (let i = start; i < nodes.length; i++) {
    const name = paramName(nodes[i])
    if (name) out.push({ name, type: paramType(nodes[i]) })
  }
  return out
}

function paramName(p: SyntaxNode): string {
  switch (p.type) {
    case "identifier":
      return p.text
    case "typed_parameter": {
      const inner = (p.children ?? []).find(c =>
        c.type === "identifier" || c.type === "list_splat_pattern" || c.type === "dictionary_splat_pattern")
      return inner ? paramName(inner) : ""
    }
    case "default_parameter":
    case "typed_default_parameter":
      return childByField(p, "name")?.text ?? ""
    case "list_splat_pattern":
      return "*" + firstIdentifier(p)
    case "dictionary_splat_pattern":
      return "**" + firstIdentifier(p)
    default:
      return ""
  }
}

function paramType(p: SyntaxNode): string {
  const t = childByField(p, "type")
  return t ? normWs(t.text) : IIRTypeUnknown
}

function firstIdentifier(p: SyntaxNode): string {
  return (p.children ?? []).find(c => c.type === "identifier")?.text ?? ""
}

function liftReturn(rt: SyntaxNode | null): IIRReturn {
  if (!rt) return { type: "", explicit: false }
  return { type: normWs(rt.text), explicit: true }
}

// ── behavior (if -> when/then + normalized whenExpr) ────────────────────────

function extractBehavior(body: SyntaxNode | null): IIRBehaviorClause[] {
  const out: IIRBehaviorClause[] = []
  if (!body) return out
  walkWithinFunc(body, (n) => {
    if (n.type === "if_statement") { pushIf(n, out); return }
    if (n.type === "match_statement") { pushMatch(n, out); return }
  })
  // A clause needs a meaningful consequence: the IIR model (and the comparator)
  // require both when and then, so drop empty-then guards.
  return out.filter(c => c.then !== "")
}

function condClause(cond: SyntaxNode | null, consequence: SyntaxNode | null): IIRBehaviorClause {
  const clause: IIRBehaviorClause = { when: cond ? normWs(cond.text) : "", then: "" }
  setThen(clause, salientConsequence(consequence))
  const whenExpr = normalizeCondition(cond)
  if (whenExpr) clause.whenExpr = whenExpr
  return clause
}

// elseClause builds an otherwise-clause with its consequence normalized.
function elseClause(consequence: SyntaxNode | null): IIRBehaviorClause {
  const clause: IIRBehaviorClause = { when: "else", then: "" }
  setThen(clause, salientConsequence(consequence))
  return clause
}

// pushIf emits the if clause plus each elif/else. Python models elif/else as
// alternative-field children of if_statement (not separate if nodes), so unlike
// the other languages they must be handled here rather than by the walk.
function pushIf(n: SyntaxNode, out: IIRBehaviorClause[]): void {
  out.push(condClause(childByField(n, "condition"), childByField(n, "consequence")))
  for (const c of n.children ?? []) {
    if (c.type === "elif_clause") {
      out.push(condClause(childByField(c, "condition"), childByField(c, "consequence")))
    } else if (c.type === "else_clause") {
      out.push(elseClause(childByField(c, "body")))
    }
  }
}

// pushMatch turns `match subj: case p: …` into one clause per case: when =
// "subj == p", then = the case body summary. A `case _:` wildcard becomes "else".
function pushMatch(m: SyntaxNode, out: IIRBehaviorClause[]): void {
  const subject = childByField(m, "subject")
  const subjExpr = normalizeCondition(subject)
  const subjText = subject ? normWs(subject.text) : ""
  const matchBody = childByField(m, "body")
  for (const c of matchBody?.children ?? []) {
    if (c.type !== "case_clause") continue
    const pattern = (c.children ?? []).find(p => p.type === "case_pattern")
    const patText = pattern ? normWs(pattern.text) : ""
    const consequence = childByField(c, "consequence")
    if (patText === "_") {
      out.push(elseClause(consequence))
      continue
    }
    const inner = pattern ? (pattern.children ?? []).find(p => p.isNamed) : null
    const valExpr = normalizeCondition(inner ?? pattern)
    const clause: IIRBehaviorClause = { when: `${subjText} == ${patText}`, then: "" }
    setThen(clause, salientConsequence(consequence))
    if (subjExpr && valExpr) clause.whenExpr = { op: "==", args: [subjExpr, valExpr] }
    out.push(clause)
  }
}

// walkWithinFunc stops at nested function scopes (def / lambda) so a nested
// function's `if` is not counted as the outer function's behavior.
function walkWithinFunc(node: SyntaxNode | null, visit: (n: SyntaxNode) => void): void {
  if (!node) return
  visit(node)
  for (const c of node.children ?? []) {
    if (c.type === "function_definition" || c.type === "lambda") continue
    walkWithinFunc(c, visit)
  }
}

// salientConsequence finds the statement that stands for a branch's consequence:
// the first meaningful statement, preferring a return or raise.
function salientConsequence(block: SyntaxNode | null): SyntaxNode | undefined {
  if (!block) return undefined
  if (block.type !== "block") return block
  let first: SyntaxNode | undefined
  for (const c of block.children ?? []) {
    if (!c.isNamed) continue
    if (!first) first = c
    if (c.type === "return_statement" || c.type === "raise_statement") return c
  }
  return first
}

// setThen fills a clause's raw `then` text and, when the consequence fits the
// action grammar, its normalized thenExpr — both from the same salient statement.
function setThen(clause: IIRBehaviorClause, salient: SyntaxNode | undefined): void {
  clause.then = salient ? normWs(salient.text) : ""
  const action = thenAction(salient)
  if (action) clause.thenExpr = action
}

// thenAction classifies a salient statement into a normalized consequence:
// return (with the returned expression), throw (Python raise, with the failure's
// identity), or invoke (a call, with the callee).
function thenAction(node: SyntaxNode | undefined): IIRConsequence | undefined {
  if (!node) return undefined
  if (node.type === "return_statement") {
    const val = (node.children ?? []).find(c => c.isNamed)
    const value = val ? normWs(val.text) : ""
    return value ? { op: "return", value } : { op: "return" }
  }
  if (node.type === "raise_statement") {
    const fm = raiseFailureMode(node)
    const value = fm ? (typeof fm === "string" ? fm : fm.code) : undefined
    return value ? { op: "throw", value } : { op: "throw" }
  }
  if (node.type === "expression_statement") {
    const call = (node.children ?? []).find(c => c.type === "call")
    if (call) {
      const callee = normWs(childByField(call, "function")?.text ?? "")
      return callee ? { op: "invoke", value: callee } : { op: "invoke" }
    }
  }
  return undefined
}

// Python comparison tokens map straight to the shared IL, except `is`/`is not`
// (identity, idiomatic for None checks) which bind to ==/!= so a cross-language
// null/nil/None rule matches Python too.
const comparisonMap: Record<string, string> = {
  "<": "<", "<=": "<=", ">": ">", ">=": ">=", "==": "==", "!=": "!=", "is": "==",
}

// comparisonOp maps a comparison's operator token(s) to a shared IL operator.
// `is not` is two tokens and binds to != (so `x is not None` normalizes like a
// != null/nil check); `not in` and other multi-token forms stay out of grammar.
function comparisonOp(opTokens: SyntaxNode[]): string | undefined {
  if (opTokens.length === 1) return comparisonMap[opTokens[0].type]
  if (opTokens.length === 2 && opTokens[0].type === "is" && opTokens[1].type === "not") return "!="
  return undefined
}
const literalTypes = new Set(["integer", "float", "string", "concatenated_string"])
const boolNoneTypes = new Set(["true", "false", "none"])

function normalizeCondition(node: SyntaxNode | null): IIRExpr | undefined {
  if (!node) return undefined
  if (literalTypes.has(node.type)) return { op: "lit", text: normWs(node.text) }
  if (boolNoneTypes.has(node.type)) return { op: "lit", text: node.type }
  switch (node.type) {
    case "parenthesized_expression": {
      const inner = (node.children ?? []).find(c => c.isNamed)
      return inner ? normalizeCondition(inner) : undefined
    }
    case "comparison_operator": {
      const operands = (node.children ?? []).filter(c => c.isNamed)
      if (operands.length !== 2) return undefined // chained comparison — out of grammar
      const op = comparisonOp((node.children ?? []).filter(c => !c.isNamed))
      if (!op) return undefined
      const left = normalizeCondition(operands[0])
      const right = normalizeCondition(operands[1])
      return left && right ? { op, args: [left, right] } : undefined
    }
    case "boolean_operator": {
      const op = fieldText(node, "operator") === "and" ? "&&" : fieldText(node, "operator") === "or" ? "||" : ""
      if (!op) return undefined
      const left = normalizeCondition(childByField(node, "left"))
      const right = normalizeCondition(childByField(node, "right"))
      return left && right ? { op, args: [left, right] } : undefined
    }
    case "not_operator": {
      const arg = normalizeCondition(childByField(node, "argument"))
      return arg ? { op: "!", args: [arg] } : undefined
    }
    case "unary_operator": {
      const op = fieldText(node, "operator")
      const operand = childByField(node, "argument")
      if (op === "-" && operand && (operand.type === "integer" || operand.type === "float")) {
        return { op: "lit", text: "-" + normWs(operand.text) }
      }
      return undefined
    }
    case "identifier":
      return { op: "path", text: node.text }
    case "attribute": {
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
  if (node.type === "attribute") {
    const obj = memberPath(childByField(node, "object"))
    const attr = childByField(node, "attribute")?.text
    return obj && attr ? obj + "." + attr : ""
  }
  return ""
}

// ── side effects + failure modes ────────────────────────────────────────────

const sideEffectVerbs = ["track", "send", "emit", "publish", "save", "create", "update", "delete", "write"]

// Read-only stdlib modules: a call constructs/derives a value without an
// observable effect, so it's not a side effect even though it's on an import.
const purePackages = new Set(["math", "re", "itertools", "functools", "operator", "collections"])
// Individually-pure functions from otherwise-effectful modules (json.dump/load
// do file I/O, but json.dumps/loads only serialize).
const pureCalls = new Set(["json.dumps", "json.loads"])

function isPureCall(root: string, full: string): boolean {
  return purePackages.has(root) || pureCalls.has(full)
}

function extractSideEffects(body: SyntaxNode | null, imports: Map<string, string>): IIRSideEffect[] {
  const byName = new Map<string, IIRSideEffect>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call") return
    const callee = childByField(n, "function")
    if (!callee) return
    const { method, root, full } = calleeParts(callee)
    if (isPureCall(root, full)) return
    if (!imports.has(root) && !matchesSideEffectVerb(method)) return
    if (byName.has(full)) return
    const { kind, basis } = classifyEffect({ method, root, importPath: imports.get(root) })
    byName.set(full, { name: full, kind, basis })
  })
  return [...byName.values()].sort((a, b) => effectName(a).localeCompare(effectName(b)))
}

function effectName(e: IIRSideEffect): string {
  return typeof e === "string" ? e : e.name
}

function calleeParts(callee: SyntaxNode): { method: string; root: string; full: string } {
  const full = callee.text
  if (callee.type === "identifier") return { method: callee.text, root: "", full }
  if (callee.type === "attribute") {
    return { method: childByField(callee, "attribute")?.text ?? "", root: leftmostIdentifier(childByField(callee, "object")), full }
  }
  return { method: "", root: "", full }
}

function leftmostIdentifier(node: SyntaxNode | null): string {
  let cur = node
  while (cur) {
    if (cur.type === "identifier") return cur.text
    if (cur.type === "attribute") { cur = childByField(cur, "object"); continue }
    return ""
  }
  return ""
}

function matchesSideEffectVerb(method: string): boolean {
  const lower = method.toLowerCase()
  return sideEffectVerbs.some(v => lower.includes(v))
}

// Python's failure idiom is `raise Error("msg")`. Classify each raise: a
// string-literal message is constructed; a named exception type is a sentinel; a
// bare `raise` re-raise or a lower-case bound name forwards an upstream failure
// (propagated).
function extractFailureModes(body: SyntaxNode | null): IIRFailureMode[] {
  const byCode = new Map<string, IIRFailureMode>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "raise_statement") return
    const fm = raiseFailureMode(n)
    if (fm) byCode.set(fm.code, fm)
  })
  return [...byCode.keys()].sort().map(k => byCode.get(k)!)
}

function raiseFailureMode(node: SyntaxNode): IIRFailureMode | null {
  const lit = firstStringLiteral(node)
  if (lit) return { code: lit, kind: "constructed" }
  const arg = (node.children ?? []).find(c => c.isNamed)
  if (!arg) return { code: "propagated", kind: "propagated" } // bare `raise` re-raise
  if (arg.type === "call") {
    const fn = childByField(arg, "function")?.text ?? ""
    return fn ? { code: fn, kind: "sentinel" } : null
  }
  if (arg.type === "identifier" || arg.type === "attribute") {
    // Upper-case leaf → an exception class (sentinel); a lower-case bound name
    // → a forwarded exception variable (propagated).
    const leaf = arg.text.split(".").pop() ?? arg.text
    if (/^[A-Z]/.test(leaf)) return { code: arg.text, kind: "sentinel" }
    return { code: arg.text, kind: "propagated", source: arg.text }
  }
  return null
}

function firstStringLiteral(node: SyntaxNode): string {
  let found = ""
  walk(node, (n) => {
    if (found) return
    if (n.type === "string") found = pyStringContent(n)
  })
  return found
}

// A tree-sitter-python `string` node includes its quotes (and optional prefix);
// pull the inner text so failure tags read like the source literal's content.
function pyStringContent(node: SyntaxNode): string {
  const content = (node.children ?? []).find(c => c.type === "string_content")
  if (content) return content.text
  return node.text.replace(/^[a-zA-Z]*(['"]{1,3})([\s\S]*)\1$/, "$2")
}

function normWs(s: string): string {
  return s.split(/\s+/).filter(Boolean).join(" ")
}
