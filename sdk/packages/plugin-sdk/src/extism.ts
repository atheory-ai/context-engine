// Production CE plugin ABI for Extism's byte input/output calling convention.
//
// This module is compiled by `ce-plugin-build` with the Extism JavaScript PDK.
// Unlike the development-only Javy adapter, a call receives its bytes through
// Host.inputString and returns its bytes with Host.outputString.

import { buildPluginManifest } from "./abi.js"
import { decodeCompactExtractionInput, toBytes } from "./compact-cst.js"
import type { AnalyzerDefinition, Node, PluginDefinition, SubstrateQuery, ToolDefinition } from "./types.js"

declare const Host: {
  inputString(): string
  inputBytes(): Uint8Array | ArrayBuffer
  outputString(value: string): void
}

type GlobalWithPlugin = typeof globalThis & {
  __ce_plugin_definition?: PluginDefinition
  __ce_node_id?: (projectID: string, type: string, canonicalID: string) => string
  __ce_edge_id?: (sourceID: string, type: string, targetID: string) => string
  __ce_log?: (level: string, message: string) => void
  __ce_emit?: (channel: string, content: string) => void
  __ce_substrate_query?: (queryJSON: string) => string
  __ce_get_config?: (key: string) => string
}

const g = globalThis as GlobalWithPlugin
installHostFallbacks()

export function ce_plugin_manifest(): number {
  writeJSON({ ...buildPluginManifest(plugin()), abi: { name: "ce-plugin", version: 4, callConvention: "extism-input-output" } })
  return 0
}

export function ce_language_match(): number {
  const language = plugin().language
  // outputString, rather than outputBytes, is intentional: it preserves the
  // single-byte response while remaining compatible with the Go Extism host.
  Host.outputString(language?.match(read()) ? "\x01" : "\x00")
  return 0
}

export function ce_language_extract(): number {
  const language = plugin().language
  if (!language) {
    writeJSON({ nodes: [], edges: [] })
    return 0
  }
  const input = decodeCompactExtractionInput(toBytes(Host.inputBytes()))
  writeJSON(language.extract(
    input.filePath,
    input.content,
    input.tree,
    input.sourceAnchor,
  ))
  return 0
}

export function ce_language_concepts(): number {
  writeJSON(plugin().language?.concepts ?? [])
  return 0
}

export function ce_analyzers_list(): number {
  writeJSON((plugin().analyzers ?? []).map(descriptor))
  return 0
}

export function ce_analyzer_run(): number {
  const nodes = parseInput<Parameters<AnalyzerDefinition["analyze"]>[0]>()
  writeJSON((plugin().analyzers ?? []).flatMap((analyzer) => analyzer.analyze(nodes)))
  return 0
}

export function ce_tools_list(): number {
  writeJSON((plugin().tools ?? []).map(descriptor))
  return 0
}

export function ce_tool_activate(): number {
  const input = parseInput<{ tool_name?: string; toolName?: string; ir: Parameters<ToolDefinition["activate"]>[0] }>()
  const tool = findTool(input.tool_name ?? input.toolName ?? "")
  Host.outputString(tool?.activate(input.ir) ? "\x01" : "\x00")
  return 0
}

export function ce_tool_execute(): number {
  const input = parseInput<{ tool_name?: string; toolName?: string; request: Parameters<ToolDefinition["execute"]>[0] }>()
  const tool = findTool(input.tool_name ?? input.toolName ?? "")
  if (!tool) {
    writeJSON({ emissions: [], proposedNodes: [], proposedEdges: [] })
    return 0
  }
  writeJSON(tool.execute(input.request, {
    query(q: SubstrateQuery): Node[] {
      return JSON.parse(g.__ce_substrate_query?.(JSON.stringify(q)) ?? "[]") as Node[]
    },
  }))
  return 0
}

function plugin(): PluginDefinition {
  if (!g.__ce_plugin_definition) throw new Error("CE plugin definition was not registered")
  return g.__ce_plugin_definition
}

function findTool(name: string): ToolDefinition | undefined {
  return plugin().tools?.find((tool) => tool.name === name)
}

function descriptor(item: { name: string; description?: string }): { name: string; description: string } {
  return { name: item.name, description: item.description ?? "" }
}

function parseInput<T>(): T {
  const raw = read()
  return raw ? JSON.parse(raw) as T : {} as T
}

function read(): string { return Host.inputString() }
function writeJSON(value: unknown): void { Host.outputString(JSON.stringify(value)) }

function installHostFallbacks(): void {
  g.__ce_log ??= () => undefined
  g.__ce_emit ??= () => undefined
  g.__ce_substrate_query ??= () => "[]"
  g.__ce_get_config ??= () => ""
  g.__ce_node_id ??= (projectID, type, canonicalID) => stableID(`${projectID}:${type}:${canonicalID}`)
  g.__ce_edge_id ??= (sourceID, type, targetID) => stableID(`${sourceID}:${type}:${targetID}`)
}

function stableID(input: string): string {
  let h1 = 0xdeadbeef
  let h2 = 0x41c6ce57
  for (let i = 0; i < input.length; i++) {
    const ch = input.charCodeAt(i)
    h1 = Math.imul(h1 ^ ch, 2654435761)
    h2 = Math.imul(h2 ^ ch, 1597334677)
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909)
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909)
  return `${(h2 >>> 0).toString(16).padStart(8, "0")}${(h1 >>> 0).toString(16).padStart(8, "0")}`
}
