// Lift a TypeScript function node into an IIR FunctionIntent, mirroring Context
// Engine's internal/iir extractor so plugin-produced IIR can reach parity with
// (and eventually replace) the host's Go lift. Deterministic AST walk, no model.
import type {
  FunctionIntent, IIRParam, IIRReturn, IIRExpr, IIRBehaviorClause, SyntaxNode,
} from "@atheory-ai/ce-plugin-sdk"
import { IIRTypeUnknown, childByField, childrenByType, fieldText, walk, walkTopLevel } from "@atheory-ai/ce-plugin-sdk"

// collectImports gathers the identifiers a module imports (local aliases, named
// imports, namespace and default imports) — the set used to decide whether a
// call targets an imported client/service.
export function collectImports(tree: SyntaxNode): Set<string> {
  const imports = new Set<string>()
  for (const node of tree.children ?? []) {
    if (node.type !== "import_statement") continue
    walk(node, (n) => {
      switch (n.type) {
        case "import_specifier": {
          // Prefer the local alias when present, else the imported name.
          const alias = childByField(n, "alias")
          const name = childByField(n, "name")
          if (alias) imports.add(alias.text)
          else if (name) imports.add(name.text)
          break
        }
        case "namespace_import": {
          const id = lastIdentifier(n)
          if (id) imports.add(id)
          break
        }
        case "import_clause":
          // default import: `import Foo from "..."` — a direct identifier child
          // of import_clause (named/namespace imports are nested deeper).
          for (const c of n.children ?? []) {
            if (c.type === "identifier") imports.add(c.text)
          }
          break
      }
    })
  }
  return imports
}

// liftFunction builds a FunctionIntent from a function-bearing node (a
// function_declaration or an arrow_function/function_expression value).
export function liftFunction(name: string, fnNode: SyntaxNode, exported: boolean, imports: Set<string>): FunctionIntent {
  const body = childByField(fnNode, "body")
  return {
    kind:         "FunctionIntent",
    name,
    language:     "typescript",
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
      out.push({ name: paramName(p), type: paramType(p) })
    }
  }
  return out
}

function paramName(p: SyntaxNode): string {
  const pattern = childByField(p, "pattern")
  if (!pattern) return ""
  if (pattern.type === "rest_pattern") {
    return (pattern.children ?? []).find(c => c.type === "identifier")?.text ?? ""
  }
  return pattern.text
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
    if (n.type !== "if_statement") return
    const cond = childByField(n, "condition")
    const clause: IIRBehaviorClause = {
      when: conditionText(cond),
      then: summarizeConsequence(childByField(n, "consequence")),
    }
    const whenExpr = normalizeCondition(cond)
    if (whenExpr) clause.whenExpr = whenExpr
    out.push(clause)
  })
  return out
}

function conditionText(cond: SyntaxNode | null): string {
  if (!cond) return ""
  if (cond.type === "parenthesized_expression") {
    const inner = (cond.children ?? []).find(c => c.isNamed)
    if (inner) return normWs(inner.text)
  }
  return normWs(cond.text)
}

function summarizeConsequence(cons: SyntaxNode | null): string {
  if (!cons) return ""
  if (cons.type !== "statement_block") return trimStatement(normWs(cons.text))
  let first: SyntaxNode | undefined
  for (const c of cons.children ?? []) {
    if (!c.isNamed) continue
    if (!first) first = c
    if (c.type === "return_statement" || c.type === "throw_statement") return trimStatement(normWs(c.text))
  }
  return first ? trimStatement(normWs(first.text)) : ""
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

function extractSideEffects(body: SyntaxNode | null, imports: Set<string>): string[] {
  const seen = new Set<string>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call_expression") return
    const callee = childByField(n, "function")
    if (!callee) return
    const { method, rootObj, full } = calleeParts(callee)
    if (imports.has(rootObj) || matchesSideEffectVerb(method)) seen.add(full)
  })
  return [...seen].sort()
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

function extractFailureModes(body: SyntaxNode | null): string[] {
  const seen = new Set<string>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "throw_statement") return
    const lit = firstStringLiteral(n)
    if (lit) seen.add(lit)
  })
  return [...seen].sort()
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
