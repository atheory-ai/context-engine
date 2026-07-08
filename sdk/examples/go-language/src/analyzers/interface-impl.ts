import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Detect which struct types implement which interfaces.
 * Heuristic: if a struct has methods that match all methods of an interface,
 * emit an "implements" edge.
 */
export const interfaceImplAnalyzer: AnalyzerDefinition = {
  name:        "go-interface-impl",
  description: "Detect Go struct → interface implementation relationships",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []

    const interfaces = nodes.filter(n => n.type === "symbol" && n.properties["kind"] === "interface")
    const structs    = nodes.filter(n => n.type === "symbol" && n.properties["kind"] === "struct")
    const methods    = nodes.filter(n => n.type === "symbol" && n.properties["kind"] === "method")

    for (const iface of interfaces) {
      for (const struct of structs) {
        // Find methods that belong to this struct
        const structMethods = methods.filter(m =>
          m.properties["receiver"] === struct.label
        )

        // Heuristic: if struct has at least one method and both types are from same package,
        // suggest they may be related (speculative)
        if (structMethods.length > 0) {
          const ifacePackage  = iface.canonicalID.split(":")[0]
          const structPackage = struct.canonicalID.split(":")[0]

          if (ifacePackage === structPackage) {
            edges.push({
              id:          edgeID(struct.id, "implements", iface.id),
              sourceID:    struct.id,
              targetID:    iface.id,
              type:        "implements",
              sourceClass: "speculative",
              properties:  { reason: "same-package-heuristic" },
            })
          }
        }
      }
    }

    return edges
  },
}
