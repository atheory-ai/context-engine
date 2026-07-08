// Lift a Go function/method into an IIR FunctionIntent, mirroring Context
// Engine's TS extractor but bound to Go's grammar (selector_expression member
// access, nil/true/false as predeclared identifiers, panic as the failure
// idiom). Go has no host-side extractor — the plugin is the sole IIR producer.
import type {
  FunctionIntent, IIRParam, IIRReturn, IIRExpr, IIRBehaviorClause, SyntaxNode,
} from "@atheory-ai/ce-plugin-sdk"
import { IIRTypeUnknown, childByField, childrenByType, fieldText, walk } from "@atheory-ai/ce-plugin-sdk"

export function isExported(name: string): boolean {
  const first = name.charAt(0)
  return first !== "" && first === first.toUpperCase() && first !== first.toLowerCase()
}

// collectImports gathers imported package qualifiers (the alias, or the last
// path segment) — the set used to decide whether a call is on an imported package.
export function collectImports(tree: SyntaxNode): Set<string> {
  const imports = new Set<string>()
  walk(tree, (n) => {
    if (n.type !== "import_spec") return
    const alias = childByField(n, "name")?.text
    if (alias) {
      if (alias !== "_" && alias !== ".") imports.add(alias)
      return
    }
    const segs = (childByField(n, "path")?.text ?? "").replace(/^["'`]|["'`]$/g, "").split("/")
    let pkg = segs.pop() ?? ""
    // A trailing /vN is a semantic-import-version dir, not the package name
    // (github.com/foo/bar/v2 → bar); a .vN suffix likewise (gopkg.in/yaml.v2 → yaml).
    if (/^v\d+$/.test(pkg)) pkg = segs.pop() ?? pkg
    pkg = pkg.replace(/\.v\d+$/, "")
    if (pkg) imports.add(pkg)
  })
  return imports
}

export function liftGoFunction(name: string, fnNode: SyntaxNode, imports: Set<string>): FunctionIntent {
  const body = childByField(fnNode, "body")
  return {
    kind:         "FunctionIntent",
    name,
    language:     "go",
    visibility:   isExported(name) ? "public" : "private",
    inputs:       liftParams(childByField(fnNode, "parameters")),
    returns:      liftResult(childByField(fnNode, "result")),
    behavior:     extractBehavior(body),
    sideEffects:  extractSideEffects(body, imports),
    failureModes: extractFailureModes(body),
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
    if (names.length === 0) out.push({ name: "", type: normWs(type) })
    else for (const n of names) out.push({ name: n.text, type: normWs(type) })
  }
  for (const decl of childrenByType(params, "variadic_parameter_declaration")) {
    const type = childByField(decl, "type")?.text ?? IIRTypeUnknown
    out.push({ name: childByField(decl, "name")?.text ?? "", type: "..." + normWs(type) })
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
    if (n.type !== "if_statement") return
    const cond = childByField(n, "condition")
    const clause: IIRBehaviorClause = {
      when: cond ? normWs(cond.text) : "",
      then: summarizeConsequence(childByField(n, "consequence")),
    }
    const whenExpr = normalizeCondition(cond)
    if (whenExpr) clause.whenExpr = whenExpr
    out.push(clause)
  })
  return out
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

function extractSideEffects(body: SyntaxNode | null, imports: Set<string>): string[] {
  const seen = new Set<string>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call_expression") return
    const callee = childByField(n, "function")
    if (!callee) return
    const { method, root, full } = calleeParts(callee)
    if (imports.has(root) || matchesSideEffectVerb(method)) seen.add(full)
  })
  return [...seen].sort()
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

// Go's failure idiom nearest to a thrown error is panic(...); capture its string
// literal argument. Error returns are not modeled (too ambiguous).
function extractFailureModes(body: SyntaxNode | null): string[] {
  const seen = new Set<string>()
  if (!body) return []
  walk(body, (n) => {
    if (n.type !== "call_expression") return
    if (childByField(n, "function")?.text !== "panic") return
    const lit = firstStringLiteral(n)
    if (lit) seen.add(lit)
  })
  return [...seen].sort()
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
