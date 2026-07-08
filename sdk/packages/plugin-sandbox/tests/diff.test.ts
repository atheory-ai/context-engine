import { describe, it, expect } from "vitest"
import { diffExtractions } from "../src/analysis/diff.js"

describe("diffExtractions", () => {
  it("identifies added nodes between builds", () => {
    const old = {
      nodes: [
        { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
          label: "FuncA", sourceClass: "structural" as const, properties: {} }
      ],
      edges: [],
    }
    const next = {
      nodes: [
        { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
          label: "FuncA", sourceClass: "structural" as const, properties: {} },
        { id: "2", canonicalID: "pkg:FuncB", type: "symbol" as const,
          label: "FuncB", sourceClass: "structural" as const, properties: {} },
      ],
      edges: [],
    }

    const diff = diffExtractions("main.go", old, next, 50, 100)

    expect(diff.addedNodes).toContain("pkg:FuncB")
    expect(diff.removedNodes).toHaveLength(0)
    expect(diff.coverageDelta).toBe(50)
  })

  it("identifies removed nodes between builds", () => {
    const old = {
      nodes: [
        { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
          label: "FuncA", sourceClass: "structural" as const, properties: {} },
        { id: "2", canonicalID: "pkg:FuncB", type: "symbol" as const,
          label: "FuncB", sourceClass: "structural" as const, properties: {} },
      ],
      edges: [],
    }
    const next = {
      nodes: [
        { id: "1", canonicalID: "pkg:FuncA", type: "symbol" as const,
          label: "FuncA", sourceClass: "structural" as const, properties: {} },
      ],
      edges: [],
    }

    const diff = diffExtractions("main.go", old, next, 100, 50)

    expect(diff.removedNodes).toContain("pkg:FuncB")
    expect(diff.addedNodes).toHaveLength(0)
    expect(diff.coverageDelta).toBe(-50)
  })

  it("tracks edge changes", () => {
    const old = {
      nodes: [],
      edges: [
        { id: "e1", sourceID: "a", targetID: "b", type: "calls" as const,
          sourceClass: "structural" as const, properties: {} },
      ],
    }
    const next = {
      nodes: [],
      edges: [
        { id: "e2", sourceID: "a", targetID: "c", type: "calls" as const,
          sourceClass: "structural" as const, properties: {} },
      ],
    }

    const diff = diffExtractions("main.go", old, next, 0, 0)

    expect(diff.addedEdges).toContain("e2")
    expect(diff.removedEdges).toContain("e1")
  })
})
