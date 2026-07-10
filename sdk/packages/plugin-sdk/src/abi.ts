import type { AnalyzerDefinition, IIRRulePack, Node, PluginDefinition, SubstrateQuery, SyntaxNode, ToolDefinition } from "./types.js"

declare const Javy: {
  IO: {
    readSync(fd: number, buffer: Uint8Array): number
    writeSync(fd: number, buffer: Uint8Array): number
  }
}
declare const TextEncoder: {
  new(): { encode(value: string): Uint8Array }
}
declare const TextDecoder: {
  new(): { decode(value: Uint8Array): string }
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

// PluginManifest is the JSON shape emitted by ce_plugin_manifest, mirroring the
// host's runtime.PluginManifest. Optional fields are omitted by JSON.stringify
// when undefined, matching the host's omitempty decoding.
export interface PluginManifest {
  id:       string
  name:     string
  version:  string
  abi:      { name: string; version: number; callConvention: string }
  capabilities: {
    language:  boolean
    role:      boolean
    analyzers: string[]
    tools:     string[]
  }
  language?: { extensions: string[]; grammar?: string }
  iirRules?: IIRRulePack
}

// buildPluginManifest is the pure manifest builder, kept separate from the IO so
// it can be unit-tested.
export function buildPluginManifest(plugin: PluginDefinition): PluginManifest {
  return {
    id: plugin.id,
    name: plugin.name,
    version: plugin.version,
    abi: {
      name: "ce-plugin",
      version: 1,
      callConvention: "javy-stream-io",
    },
    capabilities: {
      language: Boolean(plugin.language),
      role: Boolean(plugin.role),
      analyzers: plugin.analyzers?.map((analyzer) => analyzer.name) ?? [],
      tools: plugin.tools?.map((tool) => tool.name) ?? [],
    },
    language: plugin.language
      ? { extensions: plugin.language.extensions ?? [], grammar: plugin.language.grammar }
      : undefined,
    // Contributed IIR conformance rules; the host merges them over its defaults.
    iirRules: plugin.iirRules,
  }
}

export function cePluginManifest(): void {
  writeJSON(buildPluginManifest(getPlugin()))
}

export function ceLanguageMatch(): void {
  const plugin = getPlugin()
  // The host reads a single byte: 0x01 = match, 0x00 = no match (see
  // ceToolActivate, which uses the same convention). Writing "true"/"false" as
  // text would be read as byte 't'/'f' and never match.
  if (!plugin.language) {
    write("\x00")
    return
  }
  write(plugin.language.match(read()) ? "\x01" : "\x00")
}

export function ceLanguageExtract(): void {
  const plugin = getPlugin()
  if (!plugin.language) {
    writeJSON({ nodes: [], edges: [] })
    return
  }
  // The host sends the serialized tree as a { root, source, language } wrapper;
  // hand the extractor the root SyntaxNode directly.
  const input = parseInput<{
    file_path?: string
    filePath?:  string
    content?:   string
    tree?:      { root?: SyntaxNode | null } | null
  }>()
  writeJSON(plugin.language.extract(
    input.file_path ?? input.filePath ?? "",
    input.content ?? "",
    input.tree?.root ?? null,
  ))
}

export function ceLanguageConcepts(): void {
  writeJSON(getPlugin().language?.concepts ?? [])
}

export function ceAnalyzersList(): void {
  writeJSON((getPlugin().analyzers ?? []).map((analyzer) => descriptor(analyzer)))
}

export function ceAnalyzerRun(): void {
  const analyzers = getPlugin().analyzers ?? []
  const nodes = parseInput<Parameters<AnalyzerDefinition["analyze"]>[0]>()
  writeJSON(analyzers.flatMap((analyzer) => analyzer.analyze(nodes)))
}

export function ceToolsList(): void {
  writeJSON((getPlugin().tools ?? []).map((tool) => descriptor(tool)))
}

export function ceToolActivate(): void {
  const input = parseInput<{ tool_name?: string; toolName?: string; ir: Parameters<ToolDefinition["activate"]>[0] }>()
  const tool = findTool(input.tool_name ?? input.toolName ?? "")
  write(tool?.activate(input.ir) ? "\x01" : "\x00")
}

export function ceToolExecute(): void {
  const input = parseInput<{ tool_name?: string; toolName?: string; request: Parameters<ToolDefinition["execute"]>[0] }>()
  const tool = findTool(input.tool_name ?? input.toolName ?? "")
  if (!tool) {
    writeJSON({ emissions: [], proposedNodes: [], proposedEdges: [] })
    return
  }
  writeJSON(tool.execute(input.request, {
    query(q: SubstrateQuery): Node[] {
      const result = g.__ce_substrate_query?.(JSON.stringify(q)) ?? "[]"
      return JSON.parse(result) as Node[]
    },
  }))
}

function getPlugin(): PluginDefinition {
  if (!g.__ce_plugin_definition) {
    throw new Error("CE plugin definition was not registered")
  }
  return g.__ce_plugin_definition
}

function findTool(name: string): ToolDefinition | undefined {
  return getPlugin().tools?.find((tool) => tool.name === name)
}

function descriptor(item: { name: string; description?: string }): { name: string; description: string } {
  return { name: item.name, description: item.description ?? "" }
}

function parseInput<T>(): T {
  const raw = read()
  return raw ? JSON.parse(raw) as T : {} as T
}

function read(): string {
  const chunks: Uint8Array[] = []
  const buffer = new Uint8Array(4096)
  for (;;) {
    const n = Javy.IO.readSync(0, buffer)
    if (n <= 0) break
    chunks.push(buffer.slice(0, n))
  }
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const all = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    all.set(chunk, offset)
    offset += chunk.length
  }
  return new TextDecoder().decode(all)
}

function writeJSON(value: unknown): void {
  write(JSON.stringify(value))
}

function write(value: string): void {
  const buffer = new TextEncoder().encode(value)
  let offset = 0
  while (offset < buffer.length) {
    const written = Javy.IO.writeSync(1, buffer.subarray(offset))
    if (written <= 0) {
      throw new Error("failed to write CE plugin output")
    }
    offset += written
  }
}

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
