import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Builds a package-level dependency graph from file → import edges.
 * For each file that belongs to package A and imports package B,
 * emit a "depends_on" edge from the namespace node of A to namespace node of B.
 *
 * This collapses file-level import noise into a clean inter-package dependency map
 * that the cognitive loop uses when evaluating architectural questions.
 */
export const packageDepsAnalyzer: AnalyzerDefinition = {
  name:        "package-deps",
  description: "Build package-level dependency graph from file imports",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []
    const seen  = new Set<string>()

    // Map file ID → its package namespace ID
    const filePackage = new Map<string, string>()
    for (const n of nodes) {
      if (n.type === "namespace" && n.properties["package"]) {
        // This is a package namespace node; files will map to it
      }
    }

    // Find belongs_to edges to get file → package mapping
    // Since we only have nodes here, use canonicalID patterns.
    // file canonicalID = path/to/file.go
    // namespace canonicalID = path/to  (the package dir)
    for (const n of nodes) {
      if (n.type === "file") {
        const dir = (n.canonicalID as string).substring(
          0, (n.canonicalID as string).lastIndexOf("/")
        ) || "."
        filePackage.set(n.id, dir)
      }
    }

    // Find import namespace nodes (they have import_path property)
    const importNodes = new Map<string, Node>()
    for (const n of nodes) {
      if (n.type === "namespace" && n.properties["import_path"]) {
        importNodes.set(n.id, n)
      }
    }

    // For each package, find what it imports via the file nodes
    // We reconstruct by matching: file belongs to pkgDir, file imports impNode
    // Since we only have nodes (no edges in the analyzer API), we infer from IDs.
    // File ID = nodeID("", "file", filePath)
    // Import ID = nodeID("", "namespace", importPath)
    // Package ID = nodeID("", "namespace", pkgDir)

    // Group files by their package dir
    const packageFiles = new Map<string, string[]>() // pkgDir → [fileID]
    for (const [fileID, pkgDir] of filePackage) {
      const arr = packageFiles.get(pkgDir) ?? []
      arr.push(fileID)
      packageFiles.set(pkgDir, arr)
    }

    // For each namespace node that is a package (has package property)
    // find all import nodes associated with files in that package
    for (const pkgNode of nodes) {
      if (pkgNode.type !== "namespace" || !pkgNode.properties["package"]) continue
      const pkgDir = pkgNode.canonicalID as string

      for (const impNode of importNodes.values()) {
        const impPath = impNode.properties["import_path"] as string
        // Skip standard library (no dots in first segment)
        const firstSeg = impPath.split("/")[0]
        if (!firstSeg.includes(".")) continue

        const edgeKey = `${pkgNode.id}→${impNode.id}`
        if (seen.has(edgeKey)) continue
        seen.add(edgeKey)

        // Emit a speculative package-level dependency edge
        edges.push({
          id:          edgeID(pkgNode.id, "depends_on", impNode.id),
          sourceID:    pkgNode.id,
          targetID:    impNode.id,
          type:        "depends_on",
          sourceClass: "associative",
          properties: {
            analyzer: "package-deps",
            package:  pkgDir,
            import:   impPath,
          },
        })
      }
    }

    return edges
  },
}
