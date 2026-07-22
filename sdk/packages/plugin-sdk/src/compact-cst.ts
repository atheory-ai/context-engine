import type { SourceAnchor, SyntaxNode } from "./types.js"

declare const TextEncoder: {
  new(): { encode(value: string): Uint8Array }
}
declare const TextDecoder: {
  new(): { decode(value: Uint8Array): string }
}

const decoder = new TextDecoder()
const treeMagic = "CECT"
const extractionMagic = "CEIN"
const treeVersion = 1
const extractionVersion = 1
const nodeWords = 11
const nodeBytes = nodeWords * 4

type CompactNode = SyntaxNode & { __ce_source_bytes?: Uint8Array }

export interface CompactExtractionInput {
  filePath: string
  content: string
  tree: SyntaxNode | null
  sourceAnchor: SourceAnchor
}

export function toBytes(value: Uint8Array | ArrayBuffer): Uint8Array {
  return value instanceof Uint8Array ? value : new Uint8Array(value)
}

// decodeCompactExtractionInput consumes ABI-v4's byte envelope without ever
// converting the full payload into a JSON string. Source is one Uint8Array
// view plus the string required by the public language.extract API; the tree
// is a lazy node-table view, not a parsed JS object graph.
export function decodeCompactExtractionInput(bytes: Uint8Array): CompactExtractionInput {
  const reader = new Reader(bytes)
  if (reader.readMagic() !== extractionMagic) throw new Error("invalid CE extraction input")
  if (reader.u32() !== extractionVersion) throw new Error("unsupported CE extraction input version")
  const filePathLength = reader.u32()
  const anchorLength = reader.u32()
  const sourceLength = reader.u32()
  const treeLength = reader.u32()
  const filePath = decoder.decode(reader.bytes(filePathLength))
  const anchor = decoder.decode(reader.bytes(anchorLength))
  const source = reader.bytes(sourceLength)
  const treeBytes = reader.bytes(treeLength)
  if (!reader.done()) throw new Error("trailing CE extraction input bytes")
  return {
    filePath,
    content: decoder.decode(source),
    tree: treeBytes.length === 0 ? null : decodeCompactTree(treeBytes, source),
    sourceAnchor: { type: "file", canonicalID: anchor || filePath },
  }
}

function decodeCompactTree(bytes: Uint8Array, source: Uint8Array): SyntaxNode {
  const reader = new Reader(bytes)
  if (reader.readMagic() !== treeMagic) throw new Error("invalid CE compact CST")
  if (reader.u32() !== treeVersion) throw new Error("unsupported CE compact CST version")
  const nodeCount = reader.u32()
  const childCount = reader.u32()
  const typeCount = reader.u32()
  const fieldCount = reader.u32()
  const types = reader.strings(typeCount)
  const fields = reader.strings(fieldCount)
  const recordsOffset = reader.offset
  const recordsSize = checkedProduct(nodeCount, nodeBytes)
  reader.skip(recordsSize)
  const childrenOffset = reader.offset
  reader.skip(checkedProduct(childCount, 4))
  if (!reader.done()) throw new Error("trailing CE compact CST bytes")
  if (nodeCount === 0) throw new Error("CE compact CST has no root")
  return new CompactTree(bytes, source, types, fields, nodeCount, childCount, recordsOffset, childrenOffset).node(0)
}

class CompactTree {
  readonly view: DataView
  private readonly nodes: Array<SyntaxNode | undefined>

  constructor(
    readonly bytes: Uint8Array,
    readonly source: Uint8Array,
    readonly types: string[],
    readonly fields: string[],
    readonly nodeCount: number,
    readonly childCount: number,
    readonly recordsOffset: number,
    readonly childrenOffset: number,
  ) {
    this.view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
    this.nodes = new Array<SyntaxNode | undefined>(nodeCount)
  }

  word(index: number, word: number): number {
    if (index < 0 || index >= this.nodeCount) throw new Error("invalid CE compact CST node index")
    return this.view.getUint32(this.recordsOffset + index * nodeBytes + word * 4, true)
  }

  child(index: number): number {
    if (index < 0 || index >= this.childCount) throw new Error("invalid CE compact CST child index")
    return this.view.getUint32(this.childrenOffset + index * 4, true)
  }

  node(index: number): SyntaxNode {
    if (index < 0 || index >= this.nodeCount) throw new Error("invalid CE compact CST node index")
    const existing = this.nodes[index]
    if (existing) return existing
    const node = new CompactSyntaxNode(this, index)
    this.nodes[index] = node
    return node
  }
}

class CompactSyntaxNode implements SyntaxNode {
  private cachedChildren: SyntaxNode[] | null | undefined

  constructor(private readonly tree: CompactTree, private readonly index: number) {}

  get type(): string {
    const index = this.tree.word(this.index, 0)
    if (index >= this.tree.types.length) throw new Error("invalid CE compact CST type")
    return this.tree.types[index]
  }

  get isNamed(): boolean { return (this.tree.word(this.index, 2) & 1) !== 0 }

  get fieldName(): string | null {
    const value = this.tree.word(this.index, 1)
    if (value === 0) return null
    if (value > this.tree.fields.length) throw new Error("invalid CE compact CST field")
    return this.tree.fields[value - 1]
  }

  get text(): string {
    const start = Math.min(this.startByte, this.tree.source.length)
    const end = Math.max(start, Math.min(this.endByte, this.tree.source.length))
    // subarray is a view, unlike Uint8Array#slice; decoding is the only
    // allocation and happens only when a plugin actually asks for node text.
    return decoder.decode(this.tree.source.subarray(start, end))
  }

  get startByte(): number { return this.tree.word(this.index, 3) }
  get endByte(): number { return this.tree.word(this.index, 4) }
  get startPosition(): { row: number, column: number } {
    return { row: this.tree.word(this.index, 5), column: this.tree.word(this.index, 6) }
  }
  get endPosition(): { row: number, column: number } {
    return { row: this.tree.word(this.index, 7), column: this.tree.word(this.index, 8) }
  }

  get children(): SyntaxNode[] | null {
	if (this.cachedChildren !== undefined) return this.cachedChildren
    const offset = this.tree.word(this.index, 9)
    const count = this.tree.word(this.index, 10)
	if (count === 0) return (this.cachedChildren = null)
    if (offset + count > this.tree.childCount) throw new Error("invalid CE compact CST children")
    const children = new Array<SyntaxNode>(count)
    for (let i = 0; i < count; i++) children[i] = this.tree.node(this.tree.child(offset + i))
	return (this.cachedChildren = children)
  }
}

class Reader {
  readonly view: DataView
  offset = 0

  constructor(readonly bytesValue: Uint8Array) {
    this.view = new DataView(bytesValue.buffer, bytesValue.byteOffset, bytesValue.byteLength)
  }

  readMagic(): string { return decoder.decode(this.bytes(4)) }
  u32(): number {
    this.require(4)
    const value = this.view.getUint32(this.offset, true)
    this.offset += 4
    return value
  }
  bytes(length: number): Uint8Array {
    this.require(length)
    const value = this.bytesValue.subarray(this.offset, this.offset + length)
    this.offset += length
    return value
  }
  strings(count: number): string[] {
    const values = new Array<string>(count)
    for (let i = 0; i < count; i++) {
      const length = this.u32()
      values[i] = decoder.decode(this.bytes(length))
    }
    return values
  }
  skip(length: number): void { this.require(length); this.offset += length }
  done(): boolean { return this.offset === this.bytesValue.length }
  private require(length: number): void {
    if (length < 0 || length > this.bytesValue.length - this.offset) throw new Error("truncated CE compact CST")
  }
}

function checkedProduct(a: number, b: number): number {
  const value = a * b
  if (!Number.isSafeInteger(value)) throw new Error("CE compact CST is too large")
  return value
}

// hydrateCompactTree remains for development fixtures that use the old JSON
// shape. Production ABI-v4 uses decodeCompactExtractionInput above.
const compactNodePrototype = {
  get text(): string {
    const node = this as CompactNode
    const source = node.__ce_source_bytes
    if (!source) return ""
    const start = Math.min(node.startByte, source.length)
    const end = Math.max(start, Math.min(node.endByte, source.length))
    return decoder.decode(source.subarray(start, end))
  },
}

export function hydrateCompactTree(root: SyntaxNode | null, source: string): SyntaxNode | null {
  if (!root) return null
  const sourceBytes = new TextEncoder().encode(source)
  const visit = (node: SyntaxNode): void => {
    if (!("text" in node)) {
      Object.defineProperty(node, "__ce_source_bytes", { value: sourceBytes, enumerable: false })
      Object.setPrototypeOf(node, compactNodePrototype)
    }
    for (const child of node.children ?? []) visit(child)
  }
  visit(root)
  return root
}
