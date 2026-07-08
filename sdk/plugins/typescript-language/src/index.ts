import { definePlugin }           from "@atheory-ai/ce-plugin-sdk"
import { match }                  from "./match.js"
import { extract }                from "./extract.js"
import { tsConceptSeeds }         from "./concepts.js"
import { importGraphAnalyzer }    from "./analyzers/import-graph.js"
import { reactComponentAnalyzer } from "./analyzers/react-components.js"

export default definePlugin({
  id:      "com.atheory-ai.typescript",
  name:    "TypeScript & JavaScript",
  version: "1.0.0",

  language: {
    match,
    extract,
    concepts: tsConceptSeeds,
  },

  analyzers: [importGraphAnalyzer, reactComponentAnalyzer],
})
