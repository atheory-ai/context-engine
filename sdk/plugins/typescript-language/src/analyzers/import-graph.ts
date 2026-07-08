import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Resolves relative imports to concrete file nodes and emits module-level
 * "depends_on" edges between file nodes.
 *
 * The extraction pass emits file → imports → namespace(importPath) edges.
 * This analyzer promotes relative-import namespaces into file → file edges
 * when the target file is present in the same node set (same project).
 */
export const importGraphAnalyzer: AnalyzerDefinition = {
  name:        "import-graph",
  description: "Resolve relative imports to file-level dependency edges",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []
    const seen  = new Set<string>()

    // Build a map of canonical file paths to their node IDs
    const fileByPath = new Map<string, string>() // canonicalID → nodeID
    for (const n of nodes) {
      if (n.type === "file") {
        fileByPath.set(n.canonicalID as string, n.id)
      }
    }

    // Find namespace nodes that look like relative imports
    for (const n of nodes) {
      if (n.type !== "namespace") continue
      const importPath = n.properties["import_path"] as string | undefined
      const fromFile   = n.properties["from_file"]  as string | undefined
      if (!importPath || !fromFile) continue
      if (!importPath.startsWith(".")) continue

      // Resolve relative path
      const resolved = resolveRelative(fromFile, importPath)
      if (!resolved) continue

      // Try each candidate extension to find the target file
      const candidates = [
        resolved,
        resolved + ".ts",
        resolved + ".tsx",
        resolved + ".js",
        resolved + ".jsx",
        resolved + "/index.ts",
        resolved + "/index.tsx",
        resolved + "/index.js",
      ]

      for (const candidate of candidates) {
        const targetID = fileByPath.get(candidate)
        if (!targetID) continue

        const sourceID = fileByPath.get(fromFile)
        if (!sourceID) break

        const key = `${sourceID}→${targetID}`
        if (seen.has(key)) break
        seen.add(key)

        edges.push({
          id:          edgeID(sourceID, "imports", targetID),
          sourceID,
          targetID,
          type:        "imports",
          sourceClass: "structural",
          properties: {
            analyzer:    "import-graph",
            import_path: importPath,
            resolved:    candidate,
          },
        })
        break
      }
    }

    return edges
  },
}

function resolveRelative(fromFile: string, importPath: string): string | null {
  const dir = fromFile.substring(0, fromFile.lastIndexOf("/")) || "."
  const parts = (dir + "/" + importPath).split("/")
  const resolved: string[] = []

  for (const part of parts) {
    if (part === ".") continue
    if (part === "..") {
      if (resolved.length > 0) resolved.pop()
    } else {
      resolved.push(part)
    }
  }

  return resolved.join("/") || null
}
