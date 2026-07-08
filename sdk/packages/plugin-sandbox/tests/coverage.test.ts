import { describe, it, expect } from "vitest"
import { computeCoverage } from "../src/analysis/coverage.js"

describe("computeCoverage", () => {
  it("matches extracted nodes to expected symbols", () => {
    const content = `
package main

func main() {}
func helper(x int) string { return "" }
type Config struct { Port int }
`
    const extraction = {
      nodes: [
        { id: "abc", type: "symbol", label: "main",
          canonicalID: "main:main", sourceClass: "structural" as const, properties: {} },
        { id: "def", type: "symbol", label: "helper",
          canonicalID: "main:helper", sourceClass: "structural" as const, properties: {} },
      ],
      edges: [],
    }

    const result = computeCoverage("main.go", content, extraction)

    expect(result.coveragePct).toBeCloseTo(66.7, 0)
    expect(result.matchedSymbols).toContain("main")
    expect(result.matchedSymbols).toContain("helper")
    expect(result.missingSymbols).toContain("Config")
  })

  it("returns -1 coverage for unknown file extensions", () => {
    const result = computeCoverage("file.xyz", "some content", { nodes: [], edges: [] })
    expect(result.coveragePct).toBe(-1)
  })

  it("handles empty fixture", () => {
    const result = computeCoverage("empty.go", "", { nodes: [], edges: [] })
    expect(result.coveragePct).toBe(0)
    expect(result.expectedSymbols).toHaveLength(0)
  })

  it("counts extra nodes not in expected symbols", () => {
    const content = `func main() {}`
    const extraction = {
      nodes: [
        { id: "1", type: "symbol", label: "main",
          canonicalID: "pkg:main", sourceClass: "structural" as const, properties: {} },
        { id: "2", type: "symbol", label: "internalHelper",
          canonicalID: "pkg:internalHelper", sourceClass: "structural" as const, properties: {} },
      ],
      edges: [],
    }
    const result = computeCoverage("main.go", content, extraction)
    expect(result.matchedSymbols).toContain("main")
    expect(result.extraNodes).toHaveLength(1)
    expect(result.extraNodes[0].label).toBe("internalHelper")
  })
})
