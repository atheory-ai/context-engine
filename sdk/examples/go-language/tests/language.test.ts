import { describe, it, expect } from "vitest"
import { readFileSync } from "fs"
import { match }   from "../src/language/match.js"
import { extract } from "../src/language/extract.js"

describe("match()", () => {
  it("matches .go files", () => {
    expect(match("main.go")).toBe(true)
    expect(match("cmd/ce/main.go")).toBe(true)
  })

  it("excludes vendor/ directory", () => {
    expect(match("vendor/github.com/foo/bar.go")).toBe(false)
  })

  it("excludes protobuf generated files", () => {
    expect(match("api/types.pb.go")).toBe(false)
  })

  it("rejects non-Go files", () => {
    expect(match("main.ts")).toBe(false)
    expect(match("README.md")).toBe(false)
  })
})

describe("extract() — simple.go", () => {
  const content = readFileSync("tests/fixtures/simple.go", "utf8")
  const result  = extract("tests/fixtures/simple.go", content)

  it("produces nodes and edges arrays", () => {
    expect(Array.isArray(result.nodes)).toBe(true)
    expect(Array.isArray(result.edges)).toBe(true)
  })

  it("extracts a file node", () => {
    const fileNode = result.nodes.find(n => n.type === "file")
    expect(fileNode).toBeDefined()
    expect(fileNode?.canonicalID).toBe("tests/fixtures/simple.go")
  })

  it("extracts function nodes", () => {
    const labels = result.nodes.map(n => n.label)
    expect(labels).toContain("main")
    expect(labels).toContain("helper")
    expect(labels).toContain("NewConfig")
  })

  it("extracts type nodes", () => {
    const labels = result.nodes.map(n => n.label)
    expect(labels).toContain("Config")
  })

  it("emits defines edges from file to functions", () => {
    const definesEdges = result.edges.filter(e => e.type === "defines")
    expect(definesEdges.length).toBeGreaterThan(0)
  })
})

describe("extract() — interface.go", () => {
  const content = readFileSync("tests/fixtures/interface.go", "utf8")
  const result  = extract("tests/fixtures/interface.go", content)

  it("extracts interface type", () => {
    const labels = result.nodes.map(n => n.label)
    expect(labels).toContain("Storage")
  })

  it("extracts struct type", () => {
    const labels = result.nodes.map(n => n.label)
    expect(labels).toContain("MemoryStore")
  })

  it("extracts methods with receiver", () => {
    const methods = result.nodes.filter(n => n.properties["kind"] === "method")
    expect(methods.length).toBeGreaterThan(0)
    const methodLabels = methods.map(m => m.label)
    expect(methodLabels).toContain("Get")
    expect(methodLabels).toContain("Set")
    expect(methodLabels).toContain("Delete")
  })
})

describe("extract() — complex.go", () => {
  const content = readFileSync("tests/fixtures/complex.go", "utf8")
  const result  = extract("tests/fixtures/complex.go", content)

  it("extracts multiple types", () => {
    const labels = result.nodes.map(n => n.label)
    expect(labels).toContain("Engine")
    expect(labels).toContain("Task")
    expect(labels).toContain("Result")
  })

  it("extracts methods on Engine", () => {
    const engineMethods = result.nodes.filter(
      n => n.properties["kind"] === "method" && n.properties["receiver"] === "Engine"
    )
    const methodNames = engineMethods.map(m => m.label)
    expect(methodNames).toContain("Submit")
    expect(methodNames).toContain("Start")
    expect(methodNames).toContain("Shutdown")
  })
})
