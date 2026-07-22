import { afterEach, describe, expect, it } from "vitest"
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { scaffold } from "../src/scaffold.js"

const directories: string[] = []

afterEach(() => {
  for (const directory of directories.splice(0)) rmSync(directory, { recursive: true, force: true })
})

describe("scaffold", () => {
  it("creates a language-plugin project using the public SDK build and lint surfaces", async () => {
    const directory = mkdtempSync(join(tmpdir(), "create-ce-plugin-"))
    directories.push(directory)
    const project = join(directory, "example-plugin")

    await scaffold({
      name: "example-plugin",
      id: "com.example.example-plugin",
      description: "An example language plugin",
      capabilities: ["language"],
      author: "Example",
      dir: project,
    })

    expect(existsSync(join(project, "eslint.config.mjs"))).toBe(true)
    expect(existsSync(join(project, "wasm-toolkit.dev.config.mjs"))).toBe(true)
    expect(readFileSync(join(project, "package.json"), "utf8"))
      .toContain("ce-plugin-build --plugin . --output dist/example-plugin.wasm")
    expect(readFileSync(join(project, "eslint.config.mjs"), "utf8"))
      .toContain("@atheory-ai/ce-plugin-sdk/eslint-plugin-ce")
  })
})
