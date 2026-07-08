import { definePlugin, nodeID } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.hello-world",
  name:    "Hello World Plugin",
  version: "0.1.0",

  language: {
    match: (filePath) => filePath.endsWith(".hello"),

    extract: (filePath, content) => ({
      nodes: [{
        id:          nodeID("", "file", filePath),
        type:        "file",
        label:       filePath,
        canonicalID: filePath,
        sourceClass: "structural",
        properties:  { lineCount: content.split("\n").length },
      }],
      edges: [],
    }),

    concepts: [
      { term: "hello-file", definition: "A .hello source file" }
    ],
  },

  iirRules: {
    rules: [
      {
        id: "com.example.hello-world/forbid-null-equality",
        target: "FunctionIntent",
        severity: "warning",
        require: {
          forbidConditionShape: {
            ops: ["==", "!=", "===", "!=="],
            operandLiteral: "null",
          },
        },
      },
    ],
  },
})
