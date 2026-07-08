// CST-walking helpers for language extractors. The host hands plugins a parsed
// tree-sitter tree (SyntaxNode); these make walking it ergonomic so extractors
// never fall back to regex.

import type { SyntaxNode } from "./types.js"

// The host omits `children` on leaf nodes (serialized as null), so every access
// goes through this — helpers must never assume `children` is an array.
function kids(node: SyntaxNode): SyntaxNode[] {
  return node.children ?? []
}

/**
 * walk visits node and every descendant in pre-order. Iterative (explicit
 * stack) so a deeply nested CST — long expression chains, minified/generated
 * code — cannot overflow the call stack.
 */
export function walk(node: SyntaxNode | null, visit: (n: SyntaxNode) => void): void {
  if (!node) return
  const stack: SyntaxNode[] = [node]
  while (stack.length > 0) {
    const n = stack.pop()!
    visit(n)
    const cs = kids(n)
    for (let i = cs.length - 1; i >= 0; i--) stack.push(cs[i]) // reversed → pop in order
  }
}

/**
 * walkTopLevel visits node and its descendants, but does NOT descend into
 * nested function/closure scopes — a boundary node itself is visited, its body
 * is not. Use it to collect a scope's own declarations without pulling in ones
 * nested inside inner functions.
 */
export function walkTopLevel(node: SyntaxNode | null, visit: (n: SyntaxNode) => void): void {
  if (!node) return
  // Stack entries carry whether to descend into that node's children — a
  // boundary node is visited but its subtree is not walked. Iterative to avoid
  // call-stack overflow on deep trees.
  const stack: Array<{ n: SyntaxNode; descend: boolean }> = [{ n: node, descend: true }]
  while (stack.length > 0) {
    const { n, descend } = stack.pop()!
    visit(n)
    if (!descend) continue
    const cs = kids(n)
    for (let i = cs.length - 1; i >= 0; i--) {
      stack.push({ n: cs[i], descend: !SCOPE_BOUNDARY.has(cs[i].type) })
    }
  }
}

const SCOPE_BOUNDARY = new Set([
  "function_declaration",
  "function_expression",
  "arrow_function",
  "method_definition",
  "generator_function",
  "generator_function_declaration",
])

/** childByField returns the child occupying the given field, or null. */
export function childByField(node: SyntaxNode, field: string): SyntaxNode | null {
  for (const child of kids(node)) {
    if (child.fieldName === field) return child
  }
  return null
}

/** childrenByType returns direct children of a given type. */
export function childrenByType(node: SyntaxNode, type: string): SyntaxNode[] {
  return kids(node).filter(c => c.type === type)
}

/** firstByType returns the first direct child of a given type, or null. */
export function firstByType(node: SyntaxNode, type: string): SyntaxNode | null {
  return kids(node).find(c => c.type === type) ?? null
}

/** hasChildType reports whether node has a direct child (named or token) of the given type. */
export function hasChildType(node: SyntaxNode, type: string): boolean {
  return kids(node).some(c => c.type === type)
}

/** fieldText returns the text of the child in the given field, or "". */
export function fieldText(node: SyntaxNode, field: string): string {
  return childByField(node, field)?.text ?? ""
}

/** firstDescendantByType returns the first descendant of a given type (pre-order), or null. */
export function firstDescendantByType(node: SyntaxNode, type: string): SyntaxNode | null {
  let found: SyntaxNode | null = null
  walk(node, n => {
    if (found === null && n.type === type) found = n
  })
  return found
}
