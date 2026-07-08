import type { Rule } from "eslint"

type ParentNode = { type?: string; key?: { type?: string; name?: string } }
type FunctionLikeNode = {
  type: string
  id?: { name?: string } | null
  parent?: ParentNode
}

type CallExpressionNode = {
  callee: { type?: string; name?: string }
}

function isActivateFn(node: unknown): node is FunctionLikeNode {
  if (typeof node !== "object" || node === null || !("type" in node)) {
    return false
  }

  const candidate = node as FunctionLikeNode
  return (
    (candidate.type === "FunctionDeclaration" && candidate.id?.name === "activate") ||
    (candidate.type === "ArrowFunctionExpression" &&
      candidate.parent?.type === "Property" &&
      candidate.parent.key?.type === "Identifier" &&
      candidate.parent.key.name === "activate")
  )
}

const rule: Rule.RuleModule = {
  meta: {
    type: "suggestion",
    docs: {
      description: "activate() should be a pure function (heuristic)",
      recommended: true,
    },
    messages: {
      hasSideEffect:
        "activate() should be a pure function. Found call to '{{name}}()' — side effects break tool selection.",
      hasAssignment:
        "activate() should be a pure function. Assignment statements are not allowed.",
    },
  },
  create(context) {
    let insideActivate = false

    return {
      "FunctionDeclaration, ArrowFunctionExpression"(node: unknown) {
        if (isActivateFn(node)) insideActivate = true
      },
      "FunctionDeclaration:exit, ArrowFunctionExpression:exit"(node: unknown) {
        if (isActivateFn(node)) insideActivate = false
      },
      CallExpression(node: unknown) {
        if (!insideActivate) return
        const { callee } = node as CallExpressionNode
        const calleeName = callee.name
        if (callee.type === "Identifier" && calleeName && calleeName !== "Boolean") {
          context.report({
            node: node as Rule.Node,
            messageId: "hasSideEffect",
            data: { name: calleeName },
          })
        }
      },
      AssignmentExpression(node: unknown) {
        if (insideActivate) {
          context.report({ node: node as Rule.Node, messageId: "hasAssignment" })
        }
      },
    }
  },
}

export default rule
