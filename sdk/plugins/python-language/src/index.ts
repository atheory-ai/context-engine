import { definePlugin }           from "@atheory-ai/ce-plugin-sdk"
import { match }                  from "./match.js"
import { extract }                from "./extract.js"
import { pythonConceptSeeds }     from "./concepts.js"
import { moduleDepsAnalyzer }     from "./analyzers/module-deps.js"
import { classHierarchyAnalyzer } from "./analyzers/class-hierarchy.js"

export default definePlugin({
  id:      "com.atheory-ai.python",
  name:    "Python Language",
  version: "1.0.0",

  language: {
    match,
    extensions: [".py", ".pyi", ".pyw"],
    extract,
    concepts: pythonConceptSeeds,
  },

  analyzers: [moduleDepsAnalyzer, classHierarchyAnalyzer],
})
