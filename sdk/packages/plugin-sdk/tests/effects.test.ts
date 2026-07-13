import { describe, expect, it } from "vitest"
import { classifyEffect } from "../src/effects.js"

describe("classifyEffect", () => {
  it("resolves by full import path (strongest signal)", () => {
    expect(classifyEffect({ root: "http", method: "Get", importPath: "net/http" })).toEqual({ kind: "network", basis: "resolved" })
    expect(classifyEffect({ root: "sql", method: "Query", importPath: "database/sql" })).toEqual({ kind: "db", basis: "resolved" })
    expect(classifyEffect({ root: "os", method: "WriteFile", importPath: "os" })).toEqual({ kind: "io", basis: "resolved" })
    expect(classifyEffect({ root: "log", method: "Printf", importPath: "log" })).toEqual({ kind: "log", basis: "resolved" })
  })

  it("resolves by the receiver root when there is no import path", () => {
    expect(classifyEffect({ root: "logger", method: "info" })).toEqual({ kind: "log", basis: "resolved" })
    expect(classifyEffect({ root: "redis", method: "get" })).toEqual({ kind: "db", basis: "resolved" })
  })

  it("treats a mutation verb in the method as a heuristic guess", () => {
    expect(classifyEffect({ root: "analytics", method: "track" })).toEqual({ kind: "mutation", basis: "heuristic" })
    expect(classifyEffect({ root: "store", method: "Save" })).toEqual({ kind: "mutation", basis: "heuristic" })
  })

  it("does not misread a root that merely contains a category word", () => {
    // "catalog" contains "log"; structural root matching must not read it as log.
    expect(classifyEffect({ root: "catalog", method: "Save" })).toEqual({ kind: "mutation", basis: "heuristic" })
  })

  it("marks an uncategorizable call heuristic/unclassified", () => {
    expect(classifyEffect({ root: "widget", method: "poke" })).toEqual({ kind: "unclassified", basis: "heuristic" })
  })
})
