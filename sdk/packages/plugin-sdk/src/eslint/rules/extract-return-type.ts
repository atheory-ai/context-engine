import type { Rule } from "eslint"

type IdentifierNode = { type: "Identifier"; name: string }
type ParentNode = { type?: string; key?: { type?: string; name?: string } }
type FunctionLikeNode = {
  type: string
  id?: { name?: string } | null
  parent?: ParentNode
}

type PropertyNode = {
  type: string
  key: { type?: string; name?: string }
}

function isExtractFunction(node: unknown): node is FunctionLikeNode {
  if (typeof node !== "object" || node === null || !("type" in node)) {
    return false
  }

  const candidate = node as FunctionLikeNode
  if (candidate.type === "FunctionDeclaration") {
    return candidate.id?.name === "extract"
  }

  if (candidate.type !== "ArrowFunctionExpression") {
    return false
  }

  return candidate.parent?.type === "Property" && candidate.parent.key?.type === "Identifier" && candidate.parent.key.name === "extract"
}

function getUnexpectedPropertyName(prop: PropertyNode): string | null {
  if (prop.key.type !== "Identifier") {
    return null
  }

  const name = prop.key.name
  if (!name || name === "nodes" || name === "edges") {
    return null
  }

  return name
}

const rule: Rule.RuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "extract() must return { nodes, edges }",
      recommended: true,
    },
    messages: {
      wrongShape:
        "extract() must return { nodes: Node[], edges: Edge[] }. Found property '{{prop}}' — did you mean 'nodes' or 'edges'?",
    },
  },
  create(context) {
    return {
      ReturnStatement(node) {
        const fn = context.getAncestors().find(isExtractFunction)
        if (!fn) return

        const arg = node.argument
        if (!arg || arg.type !== "ObjectExpression") return

        for (const prop of arg.properties) {
          if (prop.type !== "Property") continue
          const propName = getUnexpectedPropertyName(prop as PropertyNode)
          if (propName) {
            context.report({
              node: (prop.key as IdentifierNode),
              messageId: "wrongShape",
              data: { prop: propName },
            })
          }
        }
      },
    }
  },
}

export default rule
