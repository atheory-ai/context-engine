import { describe, expect, it } from "vitest"
import type { PluginDefinition } from "../src/types.js"
import { buildPluginManifest } from "../src/abi.js"

const base: PluginDefinition = {
  id: "com.example.lang",
  name: "Lang Plugin",
  version: "1.0.0",
}

const lang = {
  match: () => true,
  extract: () => ({ nodes: [], edges: [] }),
}

describe("buildPluginManifest language", () => {
  it("emits declared extensions and grammar path", () => {
    const m = buildPluginManifest({
      ...base,
      language: { ...lang, extensions: [".ex", ".exs"], grammar: "./elixir.wasm" },
    })
    const round = JSON.parse(JSON.stringify(m))
    expect(round.language.extensions).toEqual([".ex", ".exs"])
    expect(round.language.grammar).toBe("./elixir.wasm")
    expect(round.capabilities.language).toBe(true)
  })

  it("defaults extensions to [] and omits grammar when not declared", () => {
    const m = buildPluginManifest({ ...base, language: lang })
    const round = JSON.parse(JSON.stringify(m))
    expect(round.language.extensions).toEqual([])
    expect(round.language).not.toHaveProperty("grammar")
  })

  it("omits language entirely when the plugin has none", () => {
    const round = JSON.parse(JSON.stringify(buildPluginManifest(base)))
    expect(round).not.toHaveProperty("language")
    expect(round.capabilities.language).toBe(false)
  })
})
