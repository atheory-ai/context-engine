import type { Rule } from "eslint"

const BANNED_IDENTIFIERS = new Set([
  "require",
  "__dirname",
  "__filename",
  "Buffer",
  "setTimeout",
  "setInterval",
  "clearTimeout",
  "clearInterval",
  "fetch",
  "XMLHttpRequest",
  "WebSocket",
])

const BANNED_MEMBER_EXPRESSIONS: Array<[string, string]> = [
  ["process", "env"],
  ["process", "exit"],
  ["process", "argv"],
  ["fs", "readFile"],
  ["fs", "writeFile"],
  ["fs", "readFileSync"],
  ["path", "join"],
  ["path", "resolve"],
]

const rule: Rule.RuleModule = {
  meta: {
    type: "problem",
    docs: {
      description: "Disallow Node.js APIs in CE plugins (not available in WASM sandbox)",
      recommended: true,
    },
    messages: {
      bannedIdentifier: "'{{name}}' is not available in the CE WASM sandbox.",
      bannedMember: "'{{obj}}.{{prop}}' is not available in the CE WASM sandbox.",
    },
  },
  create(context) {
    return {
      Identifier(node) {
        if (BANNED_IDENTIFIERS.has(node.name)) {
          context.report({
            node,
            messageId: "bannedIdentifier",
            data: { name: node.name },
          })
        }
      },
      MemberExpression(node) {
        const obj = node.object
        const prop = node.property
        if (
          obj.type === "Identifier" &&
          prop.type === "Identifier"
        ) {
          for (const [bannedObj, bannedProp] of BANNED_MEMBER_EXPRESSIONS) {
            if (obj.name === bannedObj && prop.name === bannedProp) {
              context.report({
                node,
                messageId: "bannedMember",
                data: { obj: bannedObj, prop: bannedProp },
              })
            }
          }
        }
      },
    }
  },
}

export default rule
