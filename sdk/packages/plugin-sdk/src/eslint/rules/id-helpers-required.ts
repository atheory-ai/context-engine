import type { Rule } from "eslint"

// Patterns that suggest manually constructed IDs
const MANUAL_ID_PATTERNS = [
  /sha256/i,
  /crypto\.createHash/,
  /\.toString\(16\)/,
  /\+ ":" \+/,
  /\+ "\/" \+/,
]

const rule: Rule.RuleModule = {
  meta: {
    type: "suggestion",
    docs: {
      description: "Use nodeID() and edgeID() helpers — never construct IDs manually",
      recommended: true,
    },
    messages: {
      manualID:
        "Looks like a manually constructed node/edge ID. Use nodeID() or edgeID() helpers instead — the engine uses deterministic hashing and inconsistent IDs break the graph.",
    },
  },
  create(context) {
    return {
      Property(node) {
        if (
          node.key.type === "Identifier" &&
          (node.key.name === "id") &&
          node.value.type !== "CallExpression"
        ) {
          const src = context.getSourceCode().getText(node.value)
          if (MANUAL_ID_PATTERNS.some(p => p.test(src))) {
            context.report({ node: node.value, messageId: "manualID" })
          }
        }
      },
    }
  },
}

export default rule
