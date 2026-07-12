import { describe, expect, it } from "vitest"
import { classifyEffect } from "../src/effects.js"

describe("classifyEffect", () => {
  it("classifies by full import path (strongest signal)", () => {
    expect(classifyEffect({ root: "http", method: "Get", importPath: "net/http" })).toEqual({ kind: "network", confidence: "high" })
    expect(classifyEffect({ root: "sql", method: "Query", importPath: "database/sql" })).toEqual({ kind: "db", confidence: "high" })
    expect(classifyEffect({ root: "os", method: "WriteFile", importPath: "os" })).toEqual({ kind: "io", confidence: "high" })
    expect(classifyEffect({ root: "log", method: "Printf", importPath: "log" })).toEqual({ kind: "log", confidence: "high" })
  })

  it("falls back to the receiver root when there is no import path", () => {
    expect(classifyEffect({ root: "logger", method: "info" })).toEqual({ kind: "log", confidence: "high" })
    expect(classifyEffect({ root: "redis", method: "get" })).toEqual({ kind: "db", confidence: "high" })
  })

  it("uses a mutation verb in the method as a last categorized signal", () => {
    expect(classifyEffect({ root: "analytics", method: "track" })).toEqual({ kind: "mutation", confidence: "high" })
    expect(classifyEffect({ root: "store", method: "Save" })).toEqual({ kind: "mutation", confidence: "high" })
  })

  it("does not misread a root that merely contains a category word", () => {
    // "catalog" contains "log"; structural root matching must not read it as log.
    expect(classifyEffect({ root: "catalog", method: "Save" })).toEqual({ kind: "mutation", confidence: "high" })
  })

  it("marks an uncategorizable call low-confidence unclassified", () => {
    expect(classifyEffect({ root: "widget", method: "poke" })).toEqual({ kind: "unclassified", confidence: "low" })
  })
})
