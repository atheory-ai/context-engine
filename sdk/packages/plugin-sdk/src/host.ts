import type { SubstrateQuery, Node, SubstrateClient, NodeType, EdgeType } from "./types.js"

/**
 * Low-level host function declarations.
 * These are provided by the engine's wazero runtime.
 */
declare function __ce_log(level: string, message: string): void
declare function __ce_emit(channel: string, content: string): void
declare function __ce_substrate_query(queryJSON: string): string
declare function __ce_get_config(key: string): string
declare function __ce_node_id(projectID: string, type: string, canonicalID: string): string
declare function __ce_edge_id(sourceID: string, type: string, targetID: string): string

// ── Logging ────────────────────────────────────────────────────────────────

export const log = {
  debug: (message: string) => __ce_log("debug", message),
  info:  (message: string) => __ce_log("info",  message),
  warn:  (message: string) => __ce_log("warn",  message),
  error: (message: string) => __ce_log("error", message),
}

// ── Emit ───────────────────────────────────────────────────────────────────

export function emit(channel: "thinking" | "action" | "debug" | "warning", content: string): void {
  __ce_emit(channel, content)
}

// ── Substrate ──────────────────────────────────────────────────────────────

export function createSubstrateClient(): SubstrateClient {
  return {
    query(q: SubstrateQuery): Node[] {
      const result = __ce_substrate_query(JSON.stringify(q))
      return JSON.parse(result) as Node[]
    }
  }
}

// ── Config ─────────────────────────────────────────────────────────────────

export function getConfig<T = unknown>(key: string): T | undefined {
  const result = __ce_get_config(key)
  if (!result) return undefined
  return JSON.parse(result) as T
}

// ── ID generation ──────────────────────────────────────────────────────────

export function nodeID(projectID: string, type: NodeType, canonicalID: string): string {
  return __ce_node_id(projectID, type as string, canonicalID)
}

export function edgeID(sourceID: string, type: EdgeType, targetID: string): string {
  return __ce_edge_id(sourceID, type as string, targetID)
}
