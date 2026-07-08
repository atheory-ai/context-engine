import fs from "fs"
import path from "path"

interface ScaffoldAnswers {
  name:         string
  id:           string
  description:  string
  capabilities: string[]
  author:       string
  dir:          string
}

export async function scaffold(answers: ScaffoldAnswers): Promise<void> {
  const { name, id, description, capabilities, author, dir } = answers
  const hasLanguage  = capabilities.includes("language")
  const hasRole      = capabilities.includes("role")
  const hasAnalyzers = capabilities.includes("analyzers")
  const hasTools     = capabilities.includes("tools")

  const vars = { NAME: name, ID: id, DESCRIPTION: description, AUTHOR: author, SLUG: name }

  fs.mkdirSync(dir, { recursive: true })
  fs.mkdirSync(path.join(dir, "src"), { recursive: true })
  fs.mkdirSync(path.join(dir, "tests", "fixtures"), { recursive: true })

  if (hasLanguage) {
    fs.mkdirSync(path.join(dir, "src", "language"), { recursive: true })
    write(dir, "src/language/match.ts",    renderTemplate(MATCH_TEMPLATE, vars))
    write(dir, "src/language/extract.ts",  renderTemplate(EXTRACT_TEMPLATE, vars))
    write(dir, "src/language/concepts.ts", renderTemplate(CONCEPTS_TEMPLATE, vars))
  }
  if (hasRole) {
    fs.mkdirSync(path.join(dir, "src", "roles"), { recursive: true })
    write(dir, "src/roles/index.ts", renderTemplate(ROLE_TEMPLATE, vars))
  }
  if (hasAnalyzers) {
    fs.mkdirSync(path.join(dir, "src", "analyzers"), { recursive: true })
    write(dir, "src/analyzers/index.ts", renderTemplate(ANALYZER_TEMPLATE, vars))
  }
  if (hasTools) {
    fs.mkdirSync(path.join(dir, "src", "tools"), { recursive: true })
    write(dir, "src/tools/index.ts", renderTemplate(TOOL_TEMPLATE, vars))
  }

  write(dir, "src/index.ts",        renderIndexTemplate(vars, hasLanguage, hasRole, hasAnalyzers, hasTools))
  write(dir, "ce-plugin.json",      renderTemplate(CE_PLUGIN_JSON_TEMPLATE, vars))
  write(dir, "package.json",        renderTemplate(PACKAGE_JSON_TEMPLATE, vars))
  write(dir, "tsconfig.json",       TSCONFIG_TEMPLATE)
  write(dir, ".eslintrc.json",      ESLINTRC_TEMPLATE)
  write(dir, ".gitignore",          GITIGNORE_TEMPLATE)
  write(dir, "README.md",           renderTemplate(README_TEMPLATE, vars))

  if (hasLanguage) {
    write(dir, "tests/fixtures/example.txt", FIXTURE_TEMPLATE)
    write(dir, "tests/language.test.ts",     renderTemplate(LANGUAGE_TEST_TEMPLATE, vars))
  }
  if (hasTools) {
    write(dir, "tests/tools.test.ts", renderTemplate(TOOLS_TEST_TEMPLATE, vars))
  }
}

function write(dir: string, file: string, content: string): void {
  const fullPath = path.join(dir, file)
  fs.mkdirSync(path.dirname(fullPath), { recursive: true })
  fs.writeFileSync(fullPath, content, "utf8")
}

function renderTemplate(template: string, vars: Record<string, string>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key) => vars[key] ?? `{{${key}}}`)
}

// ── Templates ──────────────────────────────────────────────────────────────

const MATCH_TEMPLATE = `import type { LanguageDefinition } from "@atheory-ai/ce-plugin-sdk"

export const match: LanguageDefinition["match"] = (filePath) => {
  // Return true for files this plugin should process
  return filePath.endsWith(".txt")
}
`

const EXTRACT_TEMPLATE = `import type { LanguageDefinition, ExtractionResult } from "@atheory-ai/ce-plugin-sdk"
import { nodeID } from "@atheory-ai/ce-plugin-sdk"

export const extract: LanguageDefinition["extract"] = (filePath, content): ExtractionResult => {
  const nodes = []
  const edges = []

  // TODO: parse content and produce nodes + edges
  // Example:
  // nodes.push({
  //   id:          nodeID("", "file", filePath),
  //   type:        "file",
  //   label:       filePath,
  //   canonicalID: filePath,
  //   sourceClass: "structural",
  //   properties:  { lineCount: content.split("\\n").length },
  // })

  return { nodes, edges }
}
`

const CONCEPTS_TEMPLATE = `import type { ConceptSeed } from "@atheory-ai/ce-plugin-sdk"

export const concepts: ConceptSeed[] = [
  // { term: "my-concept", definition: "A description of this concept" },
]
`

const ROLE_TEMPLATE = `import type { RoleDefinition } from "@atheory-ai/ce-plugin-sdk"

export const role: RoleDefinition = {
  name:         "{{NAME}}",
  systemPrompt: "You are a specialized agent for {{DESCRIPTION}}.",
  tools:        [],  // empty = access to all tools
}
`

const ANALYZER_TEMPLATE = `import type { AnalyzerDefinition } from "@atheory-ai/ce-plugin-sdk"

export const myAnalyzer: AnalyzerDefinition = {
  name:        "{{NAME}}-analyzer",
  description: "Post-extraction analysis for {{NAME}}",
  analyze(nodes) {
    const edges = []
    // TODO: examine nodes and produce additional edges
    return edges
  },
}
`

const TOOL_TEMPLATE = `import type { ToolDefinition } from "@atheory-ai/ce-plugin-sdk"

export const myTool: ToolDefinition = {
  name:        "{{NAME}}-tool",
  description: "TODO: describe what this tool does (max 100 chars)",

  activate(ir) {
    return ir.predicates["{{NAME}}-tool"] === "true"
  },

  execute(request, substrate) {
    return {
      emissions:     [],
      proposedNodes: [],
      proposedEdges: [],
    }
  },
}
`

function renderIndexTemplate(
  vars: Record<string, string>,
  hasLanguage: boolean,
  hasRole: boolean,
  hasAnalyzers: boolean,
  hasTools: boolean,
): string {
  const imports: string[] = [`import { definePlugin } from "@atheory-ai/ce-plugin-sdk"`]
  const defProps: string[] = []

  if (hasLanguage) {
    imports.push(
      `import { match } from "./language/match.js"`,
      `import { extract } from "./language/extract.js"`,
      `import { concepts } from "./language/concepts.js"`,
    )
    defProps.push(`  language: { match, extract, concepts },`)
  }
  if (hasRole) {
    imports.push(`import { role } from "./roles/index.js"`)
    defProps.push(`  role,`)
  }
  if (hasAnalyzers) {
    imports.push(`import { myAnalyzer } from "./analyzers/index.js"`)
    defProps.push(`  analyzers: [myAnalyzer],`)
  }
  if (hasTools) {
    imports.push(`import { myTool } from "./tools/index.js"`)
    defProps.push(`  tools: [myTool],`)
  }

  return `${imports.join("\n")}

export default definePlugin({
  id:      "${vars.ID}",
  name:    "${vars.NAME}",
  version: "0.1.0",

${defProps.join("\n")}
})
`
}

const CE_PLUGIN_JSON_TEMPLATE = `{
  "id":          "{{ID}}",
  "name":        "{{NAME}}",
  "version":     "0.1.0",
  "description": "{{DESCRIPTION}}",
  "author":      "{{AUTHOR}}",
  "entry":       "./src/index.ts",
  "output":      "./dist/{{SLUG}}.wasm"
}
`

const PACKAGE_JSON_TEMPLATE = `{
  "name": "{{SLUG}}",
  "version": "0.1.0",
  "description": "{{DESCRIPTION}}",
  "type": "module",
  "scripts": {
    "build": "javy compile dist/bundle.js -o dist/{{SLUG}}.wasm",
    "bundle": "esbuild src/index.ts --bundle --format=esm --outfile=dist/bundle.js --platform=neutral --target=es2020",
    "test":  "vitest run",
    "lint":  "eslint src",
    "clean": "rm -rf dist"
  },
  "dependencies": {
    "@atheory-ai/ce-plugin-sdk": "^0.1.0"
  },
  "devDependencies": {
    "@atheory-ai/ce-plugin-sdk": "^0.1.0",
    "typescript":     "^5.x",
    "esbuild":        "^0.20.x",
    "vitest":         "^1.x",
    "eslint":         "^8.x"
  }
}
`

const TSCONFIG_TEMPLATE = `{
  "extends": "@atheory-ai/ce-plugin-sdk/tsconfig.plugin.json",
  "compilerOptions": {
    "outDir":  "./dist",
    "rootDir": "./src"
  },
  "include": ["src/**/*"],
  "exclude": ["node_modules", "dist"]
}
`

const ESLINTRC_TEMPLATE = `{
  "extends": ["@atheory-ai/ce-plugin-sdk/eslint-plugin-ce"],
  "parserOptions": {
    "project": "./tsconfig.json"
  }
}
`

const GITIGNORE_TEMPLATE = `node_modules/
dist/
*.wasm
`

const README_TEMPLATE = `# {{NAME}}

{{DESCRIPTION}}

## Development

\`\`\`bash
pnpm install
pnpm bundle && pnpm build   # compile to .wasm
ce plugin validate dist/{{SLUG}}.wasm
ce-sandbox coverage         # check extraction coverage
\`\`\`

## Testing

\`\`\`bash
pnpm test
\`\`\`
`

const FIXTURE_TEMPLATE = `# Example fixture file
# Add real source files here that your plugin should be able to extract from.
`

const LANGUAGE_TEST_TEMPLATE = `import { describe, it, expect } from "vitest"
import { extract } from "../src/language/extract.js"
import { match } from "../src/language/match.js"
import { readFileSync } from "fs"

describe("language plugin", () => {
  it("match() returns true for handled files", () => {
    expect(match("example.txt")).toBe(true)
    expect(match("example.go")).toBe(false)
  })

  it("extract() returns nodes and edges", () => {
    const content = readFileSync("tests/fixtures/example.txt", "utf8")
    const result = extract("tests/fixtures/example.txt", content)
    expect(result).toHaveProperty("nodes")
    expect(result).toHaveProperty("edges")
    expect(Array.isArray(result.nodes)).toBe(true)
    expect(Array.isArray(result.edges)).toBe(true)
  })
})
`

const TOOLS_TEST_TEMPLATE = `import { describe, it, expect } from "vitest"
import { myTool } from "../src/tools/index.js"

describe("{{NAME}}-tool", () => {
  it("activate() returns true when predicate is set", () => {
    const ir = {
      mode:        "thinking" as const,
      anchors:     [],
      predicates:  { "{{NAME}}-tool": "true" },
      openQueries: [],
      maxLoops:    10,
      kLimit:      50,
      roleHint:    "",
      modelTier:   "standard",
    }
    expect(myTool.activate(ir)).toBe(true)
  })

  it("activate() returns false when predicate is not set", () => {
    const ir = {
      mode:        "thinking" as const,
      anchors:     [],
      predicates:  {},
      openQueries: [],
      maxLoops:    10,
      kLimit:      50,
      roleHint:    "",
      modelTier:   "standard",
    }
    expect(myTool.activate(ir)).toBe(false)
  })
})
`
