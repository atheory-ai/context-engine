import prompts from "prompts"
import { scaffold } from "./scaffold.js"

async function main() {
  console.log("\nContext Engine Plugin Scaffolder\n")

  const answers = await prompts([
    {
      type:     "text",
      name:     "name",
      message:  "Plugin name (e.g., my-framework-plugin)",
      validate: (v: string) => /^[a-z][a-z0-9-]*$/.test(v) || "Use lowercase-hyphenated name",
    },
    {
      type:     "text",
      name:     "id",
      message:  "Plugin ID (reverse-domain, e.g., com.example.my-plugin)",
      initial:  (_prev: string, values: Record<string, string>) => `com.example.${values.name}`,
      validate: (v: string) => v.includes(".") || "Must be reverse-domain format",
    },
    {
      type:    "text",
      name:    "description",
      message: "Short description",
    },
    {
      type:    "multiselect",
      name:    "capabilities",
      message: "What will this plugin contribute? (space to select)",
      choices: [
        { title: "Language handler (file parsing + AST extraction)", value: "language", selected: true },
        { title: "Agent role (specialized reasoning perspective)",    value: "role" },
        { title: "Analyzers (post-extraction analysis passes)",       value: "analyzers" },
        { title: "Tools (cognitive loop tools)",                      value: "tools" },
      ],
    },
    {
      type:    "text",
      name:    "author",
      message: "Author name",
    },
    {
      type:    "text",
      name:    "dir",
      message: "Output directory",
      initial: (_prev: unknown, values: Record<string, string>) => `./${values.name}`,
    },
  ], {
    onCancel: () => { console.log("Cancelled."); process.exit(0) }
  })

  await scaffold(answers)

  console.log(`
Plugin scaffolded at ./${answers.dir}

Next steps:
  cd ${answers.dir}
  pnpm install
  pnpm build            # compile to .wasm
  ce plugin validate dist/${answers.name}.wasm
  ce plugin dev         # live development with coverage
`)
}

main().catch(console.error)
