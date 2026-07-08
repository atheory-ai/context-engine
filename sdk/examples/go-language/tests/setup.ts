/**
 * Mock the wazero host globals that are normally injected by the Javy/wazero
 * runtime.  These are not available in a plain Node / Vitest context.
 */

/* eslint-disable @typescript-eslint/no-explicit-any */
const g = globalThis as any

g.__ce_node_id = (projectID: string, type: string, canonicalID: string): string =>
  `${projectID}:${type}:${canonicalID}`

g.__ce_edge_id = (sourceID: string, type: string, targetID: string): string =>
  `${sourceID}:${type}:${targetID}`

g.__ce_log = (_level: string, _message: string): void => { /* no-op */ }

g.__ce_emit = (_channel: string, _content: string): void => { /* no-op */ }

g.__ce_substrate_query = (_queryJSON: string): string => "[]"

g.__ce_get_config = (_key: string): string => ""
