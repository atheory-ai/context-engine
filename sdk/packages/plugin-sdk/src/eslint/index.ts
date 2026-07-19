import noNodeApis from "./rules/no-node-apis.js"
import toolDescriptionLength from "./rules/tool-description-length.js"
import conceptTermFormat from "./rules/concept-term-format.js"
import extractReturnType from "./rules/extract-return-type.js"
import pureActivate from "./rules/pure-activate.js"
import idHelpersRequired from "./rules/id-helpers-required.js"

const recommended = {
  plugins: {} as Record<string, unknown>,
  rules: {
    "ce-plugin-sdk/no-node-apis":          "error",
    "ce-plugin-sdk/tool-description-length": "error",
    "ce-plugin-sdk/concept-term-format":   "error",
    "ce-plugin-sdk/extract-return-type":   "warn",
    "ce-plugin-sdk/pure-activate":         "warn",
    "ce-plugin-sdk/id-helpers-required":   "warn",
  },
}

const plugin = {
  meta: {
    name: "@atheory-ai/ce-plugin-sdk",
    version: "0.1.0",
  },
  rules: {
    "no-node-apis":          noNodeApis,
    "tool-description-length": toolDescriptionLength,
    "concept-term-format":   conceptTermFormat,
    "extract-return-type":   extractReturnType,
    "pure-activate":         pureActivate,
    "id-helpers-required":   idHelpersRequired,
  },
  configs: {
    recommended,
  },
}

recommended.plugins["ce-plugin-sdk"] = plugin

export default plugin
