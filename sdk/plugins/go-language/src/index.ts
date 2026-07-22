import { definePlugin }             from "@atheory-ai/ce-plugin-sdk"
import { match }                    from "./match.js"
import { extract }                  from "./extract.js"
import { goConceptSeeds }           from "./concepts.js"
import { interfaceImplAnalyzer }    from "./analyzers/interface-impl.js"
import { packageDepsAnalyzer }      from "./analyzers/package-deps.js"

export default definePlugin({
  id:      "com.atheory-ai.go-language",
  name:    "Go Language",
  version: "1.0.0",

  language: {
    match,
    extensions: [".go"],
    extract,
    concepts: goConceptSeeds,
  },

  analyzers: [interfaceImplAnalyzer, packageDepsAnalyzer],
})
