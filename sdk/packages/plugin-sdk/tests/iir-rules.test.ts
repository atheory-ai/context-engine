import { describe, expect, it } from "vitest"
import type { IIRRulePack, PluginDefinition } from "../src/types.js"
import { buildPluginManifest } from "../src/abi.js"
import { definePlugin } from "../src/define.js"

const base: PluginDefinition = {
  id: "com.example.rules",
  name: "Rules Plugin",
  version: "1.0.0",
}

const pack: IIRRulePack = {
  rules: [
    {
      id: "forbid-null-equality",
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
}

describe("iirRules in the plugin manifest", () => {
  it("omits iirRules when the plugin declares none", () => {
    // Present as undefined on the object, but must not survive serialization —
    // the host decodes iirRules with omitempty.
    const round = JSON.parse(JSON.stringify(buildPluginManifest(base)))
    expect(round).not.toHaveProperty("iirRules")
  })

  it("emits the rule pack under the top-level iirRules key", () => {
    const manifest = buildPluginManifest({ ...base, iirRules: pack })
    const round = JSON.parse(JSON.stringify(manifest))
    expect(round.iirRules).toEqual(pack)
    // Sibling of capabilities, not nested inside it (matches PluginManifest).
    expect(round.capabilities).not.toHaveProperty("iirRules")
  })
})

describe("definePlugin iirRules validation", () => {
  it("accepts a well-formed rule pack", () => {
    expect(() => definePlugin({ ...base, iirRules: pack })).not.toThrow()
  })

  it("rejects an empty rules array", () => {
    expect(() => definePlugin({ ...base, iirRules: { rules: [] } })).toThrow(/non-empty/)
  })

  it("rejects a rule missing id", () => {
    const bad = { rules: [{ target: "FunctionIntent", severity: "error" }] } as unknown as IIRRulePack
    expect(() => definePlugin({ ...base, iirRules: bad })).toThrow(/id/)
  })

  it("rejects a rule missing target or severity", () => {
    const bad = { rules: [{ id: "x" }] } as unknown as IIRRulePack
    expect(() => definePlugin({ ...base, iirRules: bad })).toThrow(/target and severity/)
  })
})
