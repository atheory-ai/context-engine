import type { Rule } from "eslint"

const VALID_TERM = /^[a-z][a-z0-9-]*$/

const rule: Rule.RuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "Concept terms must be lowercase-hyphenated",
      recommended: true,
    },
    messages: {
      badFormat:
        "Concept term '{{term}}' must be lowercase-hyphenated (e.g., 'my-concept').",
    },
  },
  create(context) {
    return {
      Property(node) {
        if (
          node.key.type === "Identifier" &&
          node.key.name === "term" &&
          node.value.type === "Literal" &&
          typeof node.value.value === "string" &&
          !VALID_TERM.test(node.value.value)
        ) {
          context.report({
            node: node.value,
            messageId: "badFormat",
            data: { term: node.value.value },
          })
        }
      },
    }
  },
}

export default rule
