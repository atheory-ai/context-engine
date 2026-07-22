import { describe, expect, it } from "vitest"
import { decodeCompactExtractionInput, hydrateCompactTree } from "../src/compact-cst.js"
import type { SyntaxNode } from "../src/types.js"

function compactNode(startByte: number, endByte: number, children: SyntaxNode[] = []): SyntaxNode {
  return {
    type: "identifier",
    isNamed: true,
    fieldName: null,
    startByte,
    endByte,
    startPosition: { row: 0, column: startByte },
    endPosition: { row: 0, column: endByte },
    children,
  }
}

describe("hydrateCompactTree", () => {
  it("uses tree-sitter UTF-8 byte offsets, not JavaScript UTF-16 offsets", () => {
    const source = "<?php // café 🦀\nregister_post_type( 'book' );"
    const encoder = new TextEncoder()
    const startByte = encoder.encode(source.slice(0, source.indexOf("register_post_type"))).length
    const endByte = encoder.encode(source).length
    const child = compactNode(startByte, endByte)
    const root = compactNode(0, endByte, [child])

    const hydrated = hydrateCompactTree(root, source)

    expect(hydrated?.children?.[0]?.text).toBe("register_post_type( 'book' );")
    expect(Object.keys(hydrated?.children?.[0] ?? {})).not.toContain("text")
  })

  it("does not replace eager text supplied by development fixtures", () => {
    const node = { ...compactNode(0, 1), text: "kept" }
    expect(hydrateCompactTree(node, "different")?.text).toBe("kept")
  })
})

describe("decodeCompactExtractionInput", () => {
  it("keeps source as bytes and exposes a lazy, offset-backed node table", () => {
    const source = "<?php // café 🦀\nregister_post_type( 'book' );"
    const utf8 = new TextEncoder().encode(source)
    const start = new TextEncoder().encode(source.slice(0, source.indexOf("register_post_type"))).length
    const end = utf8.length
    const tree = compactTree([
      // root: its sole child is record 1
      [0, 0, 1, 0, end, 0, 0, 1, 0, 0, 1],
      [1, 1, 1, start, end, 1, 0, 1, end - start, 1, 0],
    ], [1], ["source_file", "identifier"], ["name"])
    const input = extractionInput("post-type.php", utf8, tree)

    const decoded = decodeCompactExtractionInput(input)
    expect(decoded.filePath).toBe("post-type.php")
    expect(decoded.content).toBe(source)
    expect(decoded.sourceAnchor.canonicalID).toBe("post-type.php")
    expect(decoded.tree?.children?.[0]?.fieldName).toBe("name")
    expect(decoded.tree?.children?.[0]?.text).toBe("register_post_type( 'book' );")
  })
})

function compactTree(records: number[][], children: number[], types: string[], fields: string[]): Uint8Array {
  const chunks: Uint8Array[] = [new TextEncoder().encode("CECT"), u32(1), u32(records.length), u32(children.length), u32(types.length), u32(fields.length)]
  for (const value of [...types, ...fields]) {
    const bytes = new TextEncoder().encode(value)
    chunks.push(u32(bytes.length), bytes)
  }
  for (const record of records) for (const value of record) chunks.push(u32(value))
  for (const child of children) chunks.push(u32(child))
  return concat(chunks)
}

function extractionInput(path: string, source: Uint8Array, tree: Uint8Array): Uint8Array {
  const encodedPath = new TextEncoder().encode(path)
  const chunks = [new TextEncoder().encode("CEIN"), u32(1), u32(encodedPath.length), u32(encodedPath.length), u32(source.length), u32(tree.length), encodedPath, encodedPath, source, tree]
  return concat(chunks)
}

function u32(value: number): Uint8Array {
  const bytes = new Uint8Array(4)
  new DataView(bytes.buffer).setUint32(0, value, true)
  return bytes
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const bytes = new Uint8Array(chunks.reduce((length, chunk) => length + chunk.length, 0))
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.length
  }
  return bytes
}
