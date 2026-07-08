import { definePlugin } from "@atheory-ai/ce-plugin-sdk"
import { match }    from "./language/match.js"
import { extract }  from "./language/extract.js"
import { concepts } from "./language/concepts.js"
import { interfaceImplAnalyzer } from "./analyzers/interface-impl.js"

export default definePlugin({
  id:      "com.atheory-ai.go-language",
  name:    "Go Language Plugin",
  version: "0.1.0",

  language: {
    match,
    extract,
    concepts,
  },

  analyzers: [interfaceImplAnalyzer],
})
