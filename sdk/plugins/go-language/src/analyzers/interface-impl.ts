import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Detects which struct types implement which interfaces by method-set matching.
 * For types in the same package where the struct's method set is a superset of
 * the interface's method set, emit an "implements" edge.
 *
 * Coverage target: catch all explicit same-package implementations.
 * Cross-package implementations require the cross-project tool.
 */
export const interfaceImplAnalyzer: AnalyzerDefinition = {
  name:        "interface-impl",
  description: "Detect Go struct → interface implementation by method-set matching",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []

    const interfaces = nodes.filter(n =>
      n.type === "symbol" && n.properties["kind"] === "interface"
    )
    const typeMethodSets = buildTypeMethodSets(nodes)

    for (const iface of interfaces) {
      const ifacePkg = packageOf(iface.canonicalID)

      for (const [typeCanon, methods] of typeMethodSets) {
        // Only match within the same package
        if (packageOf(typeCanon) !== ifacePkg) continue

        // Every struct with at least one method that overlaps with the interface
        // gets a speculative "implements" edge.
        // Without tree-sitter we can't be certain, so use speculative source class.
        const ifaceMethods = extractInterfaceMethods(iface, nodes)
        if (ifaceMethods.length === 0) continue

        const overlap = ifaceMethods.filter(m => methods.has(m))
        if (overlap.length === ifaceMethods.length) {
          // Full match — structural
          edges.push({
            id:          edgeID(typeCanon, "implements", iface.id),
            sourceID:    typeCanon,
            targetID:    iface.id,
            type:        "implements",
            sourceClass: "structural",
            properties:  { analyzer: "interface-impl", matched_methods: overlap },
          })
        } else if (overlap.length > 0) {
          // Partial overlap — speculative
          edges.push({
            id:          edgeID(typeCanon, "implements", iface.id),
            sourceID:    typeCanon,
            targetID:    iface.id,
            type:        "implements",
            sourceClass: "speculative",
            properties:  { analyzer: "interface-impl", matched_methods: overlap },
          })
        }
      }
    }

    return edges
  },
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function packageOf(canonicalID: string): string {
  // canonicalID format: "pkg/dir:TypeName.Method" or "pkg/dir:TypeName"
  return canonicalID.split(":")[0] ?? ""
}

function buildTypeMethodSets(nodes: Node[]): Map<string, Set<string>> {
  const result = new Map<string, Set<string>>()

  for (const n of nodes) {
    if (n.type !== "symbol" || n.properties["kind"] !== "method") continue

    const receiverType = n.properties["receiver_type"] as string | undefined
    if (!receiverType) continue

    const pkg       = packageOf(n.canonicalID)
    const typeCanon = `${pkg}:${receiverType}`
    const methodName = (n.label as string).split(".").pop() ?? n.label as string

    if (!result.has(typeCanon)) result.set(typeCanon, new Set())
    result.get(typeCanon)!.add(methodName)
  }

  return result
}

function extractInterfaceMethods(ifaceNode: Node, _nodes: Node[]): string[] {
  // If the extraction stored method names in properties, use them.
  // Otherwise we can only do heuristic matching.
  const methods = ifaceNode.properties["methods"]
  if (Array.isArray(methods)) return methods as string[]
  return []
}
