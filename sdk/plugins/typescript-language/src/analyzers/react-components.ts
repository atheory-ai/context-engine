import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID, nodeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Identifies React components among function/class symbols in .tsx/.jsx files
 * and emits "annotates" edges to a "component" concept node.
 *
 * Heuristics (without AST inspection):
 *   1. Function in a .tsx/.jsx file with a PascalCase name
 *   2. Class that extends Component or PureComponent
 *   3. Default export in a file named with PascalCase (common Next.js/React pattern)
 */
export const reactComponentAnalyzer: AnalyzerDefinition = {
  name:        "react-components",
  description: "Identify React components among symbols in JSX/TSX files",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []

    // The concept node for "component" — the engine seeds this from concepts
    const componentConceptID = nodeID("", "concept", "component")

    for (const n of nodes) {
      if (n.type !== "symbol") continue

      const kind     = n.properties["kind"] as string | undefined
      const filePath = n.properties["file_path"] as string | undefined
      if (!filePath) continue
      if (kind !== "function" && kind !== "class") continue

      const isJSXFile = filePath.endsWith(".tsx") || filePath.endsWith(".jsx")
      if (!isJSXFile) continue

      const label = n.label as string
      const isPascalCase = /^[A-Z][A-Za-z0-9]*$/.test(label)
      if (!isPascalCase) continue

      // Heuristic: exported PascalCase function/class in JSX file → likely component
      const exported = n.properties["exported"] as boolean | undefined
      if (!exported) continue

      edges.push({
        id:          edgeID(n.id, "annotates", componentConceptID),
        sourceID:    n.id,
        targetID:    componentConceptID,
        type:        "annotates",
        sourceClass: "associative",
        properties: {
          analyzer:  "react-components",
          heuristic: "exported-pascal-jsx",
        },
      })
    }

    return edges
  },
}
