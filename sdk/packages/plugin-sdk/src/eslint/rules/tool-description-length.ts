import type { Rule } from "eslint"

const rule: Rule.RuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "Tool descriptions must be 100 characters or fewer",
      recommended: true,
    },
    messages: {
      tooLong:
        "Tool description is {{length}} characters (max 100). The Strategizer receives this in its prompt — keep it concise.",
    },
  },
  create(context) {
    return {
      Property(node) {
        if (
          node.key.type === "Identifier" &&
          node.key.name === "description" &&
          node.value.type === "Literal" &&
          typeof node.value.value === "string" &&
          node.value.value.length > 100
        ) {
          context.report({
            node: node.value,
            messageId: "tooLong",
            data: { length: String(node.value.value.length) },
          })
        }
      },
    }
  },
}

export default rule
